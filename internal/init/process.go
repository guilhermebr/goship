package gsinit

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	v1 "github.com/guilhermebr/goship/pkg/api/v1"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

const (
	// maxRestarts is the maximum number of auto-restarts before giving up.
	maxRestarts = 5
	// maxBackoff is the maximum backoff delay between restarts.
	maxBackoff = 30 * time.Second
	// minProcStatusFields is the minimum number of fields expected in /proc/[pid]/status State line.
	minProcStatusFields = 2
)

// ProcessManager manages direct processes inside the VM.
type ProcessManager struct {
	mu        sync.RWMutex
	processes map[string]*managedProcess
}

// managedProcess represents a tracked running process.
type managedProcess struct {
	name          string
	binary        string
	args          []string
	env           []string
	workDir       string
	pid           int
	cmd           *exec.Cmd
	startedAt     time.Time
	state         entities.AppState
	done          chan struct{} // closed when cmd.Wait() returns in monitorProcess
	logFile       *os.File
	restartPolicy entities.RestartPolicy
	restartCount  int
	appSpec       *entities.AppSpec
	stopping      bool // true when Stop/Remove requested (suppress auto-restart)
}

// Ensure ProcessManager implements AppExecutor.
var _ AppExecutor = (*ProcessManager)(nil)

// NewProcessManager creates a new process manager.
func NewProcessManager() (*ProcessManager, error) {
	return &ProcessManager{
		processes: make(map[string]*managedProcess),
	}, nil
}

// processLogPath returns the log file path for a given app.
func processLogPath(appName string) string {
	return filepath.Join("/var/log", fmt.Sprintf("goship-%s.log", appName))
}

// Deploy starts a process for the given app spec.
func (m *ProcessManager) Deploy(ctx context.Context, app *entities.AppSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop existing process if running.
	if existing, ok := m.processes[app.Name]; ok {
		existing.stopping = true
		m.stopProcess(existing)
		delete(m.processes, app.Name)
	}

	binary := app.Binary
	if binary == "" {
		return errors.New("binary path is required for process mode")
	}

	if _, err := os.Stat(binary); os.IsNotExist(err) {
		return fmt.Errorf("binary not found: %s", binary)
	}

	// Build arguments.
	var args []string
	if len(app.Command) > 0 {
		args = append(args, app.Command...)
	}
	if len(app.Args) > 0 {
		args = append(args, app.Args...)
	}

	// Build environment.
	env := os.Environ()
	for k, v := range app.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Open log file for stdout/stderr.
	logPath := processLogPath(app.Name)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", logPath, err)
	}

	// Create and start command.
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if app.WorkingDir != "" {
		cmd.Dir = app.WorkingDir
	}

	// Set process group for clean shutdown.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("failed to start process: %w", err)
	}

	proc := &managedProcess{
		name:          app.Name,
		binary:        binary,
		args:          args,
		env:           env,
		workDir:       app.WorkingDir,
		pid:           cmd.Process.Pid,
		cmd:           cmd,
		startedAt:     time.Now(),
		state:         entities.AppStateRunning,
		done:          make(chan struct{}),
		logFile:       logFile,
		restartPolicy: app.RestartPolicy,
		restartCount:  0,
		appSpec:       app,
	}

	m.processes[app.Name] = proc

	// Monitor process in background.
	go m.monitorProcess(proc)

	return nil
}

// Stop stops a running process by name.
func (m *ProcessManager) Stop(ctx context.Context, appName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	proc, ok := m.processes[appName]
	if !ok {
		return nil
	}

	proc.stopping = true
	m.stopProcess(proc)
	proc.state = entities.AppStateStopped
	return nil
}

// Remove stops and removes a process by name.
func (m *ProcessManager) Remove(ctx context.Context, appName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	proc, ok := m.processes[appName]
	if !ok {
		return nil
	}

	proc.stopping = true
	m.stopProcess(proc)
	if proc.logFile != nil {
		_ = proc.logFile.Close()
	}
	delete(m.processes, appName)
	return nil
}

// GetStatus returns the status of all managed processes.
func (m *ProcessManager) GetStatus(ctx context.Context) ([]v1.AppStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var statuses []v1.AppStatus
	for _, proc := range m.processes {
		startedAt := proc.startedAt
		statusStr := string(proc.state)

		// Enrich status with /proc-based detection for running processes.
		if proc.state == entities.AppStateRunning {
			procStatus := getProcessStatus(proc.pid)
			if procStatus == "exited" {
				proc.state = entities.AppStateStopped
				statusStr = "stopped (exited)"
			} else {
				statusStr = fmt.Sprintf("running (pid %d, %s)", proc.pid, procStatus)
			}
		}
		if proc.restartCount > 0 {
			statusStr = fmt.Sprintf("%s, restarts: %d", statusStr, proc.restartCount)
		}

		status := v1.AppStatus{
			Name:          proc.name,
			ExecutionMode: entities.ExecutionModeProcess,
			ID:            strconv.Itoa(proc.pid),
			Binary:        proc.binary,
			State:         proc.state,
			Status:        statusStr,
			StartedAt:     &startedAt,
			RestartCount:  proc.restartCount,
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// GetLogs retrieves the last N lines from the process log file.
func (m *ProcessManager) GetLogs(_ context.Context, appName string, lines int) (string, error) {
	return readLastNLines(processLogPath(appName), lines)
}

// Close stops all managed processes.
func (m *ProcessManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, proc := range m.processes {
		proc.stopping = true
		m.stopProcess(proc)
		if proc.logFile != nil {
			_ = proc.logFile.Close()
		}
	}
	m.processes = make(map[string]*managedProcess)
	return nil
}

// stopProcess sends SIGTERM, waits 10s, then SIGKILL if needed.
// It waits on proc.done which is closed by monitorProcess after cmd.Wait() returns.
func (m *ProcessManager) stopProcess(proc *managedProcess) {
	if proc.cmd == nil || proc.cmd.Process == nil {
		return
	}

	_ = proc.cmd.Process.Signal(syscall.SIGTERM)

	select {
	case <-proc.done:
		// monitorProcess detected exit.
	case <-time.After(10 * time.Second):
		_ = proc.cmd.Process.Kill()
		<-proc.done
	}
}

// monitorProcess waits for process exit and handles auto-restart.
//
//nolint:funlen // Process monitoring includes restart logic with backoff
func (m *ProcessManager) monitorProcess(proc *managedProcess) {
	if proc.cmd == nil {
		close(proc.done)
		return
	}

	err := proc.cmd.Wait()
	close(proc.done)

	m.mu.Lock()

	tracked, ok := m.processes[proc.name]
	if !ok || tracked != proc {
		m.mu.Unlock()
		return
	}

	// If stopping was requested, don't auto-restart.
	if proc.stopping {
		if err != nil {
			proc.state = entities.AppStateFailed
		} else {
			proc.state = entities.AppStateStopped
		}
		m.mu.Unlock()
		return
	}

	// Determine if we should restart.
	var shouldRestart bool
	switch proc.restartPolicy {
	case entities.RestartPolicyAlways:
		shouldRestart = true
	case entities.RestartPolicyOnFailure:
		shouldRestart = err != nil
	default: // entities.RestartPolicyNever, "", or unknown
		shouldRestart = false
	}

	if !shouldRestart {
		if err != nil {
			proc.state = entities.AppStateFailed
		} else {
			proc.state = entities.AppStateStopped
		}
		m.mu.Unlock()
		return
	}

	if proc.restartCount >= maxRestarts {
		log.Printf("process %s: max restarts (%d) reached, giving up", proc.name, maxRestarts)
		proc.state = entities.AppStateFailed
		m.mu.Unlock()
		return
	}

	proc.restartCount++
	restartCount := proc.restartCount
	m.mu.Unlock()

	// Exponential backoff: 1s, 2s, 4s, 8s, 16s, capped at 30s.
	backoff := min(time.Duration(math.Pow(2, float64(restartCount-1)))*time.Second, maxBackoff)

	log.Printf("process %s: restarting in %s (attempt %d/%d)", proc.name, backoff, restartCount, maxRestarts)
	time.Sleep(backoff)

	m.mu.Lock()
	// Re-check that we're still the tracked process and not stopping.
	if tracked, ok := m.processes[proc.name]; !ok || tracked != proc || proc.stopping {
		m.mu.Unlock()
		return
	}

	m.restartProcess(proc)
	m.mu.Unlock()
}

// restartProcess re-creates the exec.Cmd and starts a new process.
// Caller must hold m.mu.
func (m *ProcessManager) restartProcess(proc *managedProcess) {
	cmd := exec.Command(proc.binary, proc.args...)
	cmd.Env = proc.env
	cmd.Stdout = proc.logFile
	cmd.Stderr = proc.logFile

	if proc.workDir != "" {
		cmd.Dir = proc.workDir
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		log.Printf("process %s: restart failed: %v", proc.name, err)
		proc.state = entities.AppStateFailed
		return
	}

	proc.cmd = cmd
	proc.pid = cmd.Process.Pid
	proc.startedAt = time.Now()
	proc.state = entities.AppStateRunning
	proc.done = make(chan struct{})

	log.Printf("process %s: restarted with pid %d (restart %d)", proc.name, proc.pid, proc.restartCount)

	go m.monitorProcess(proc)
}

// getProcessStatus reads /proc/<pid>/status to determine the process state.
func getProcessStatus(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return "exited"
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "State:") {
			fields := strings.Fields(line)
			if len(fields) >= minProcStatusFields {
				// Return the state description, e.g., "S (sleeping)", "R (running)"
				return strings.Join(fields[1:], " ")
			}
		}
	}
	return "unknown"
}
