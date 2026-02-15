package gsinit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	v1 "github.com/guilhermebr/goship/pkg/api/v1"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// ProcessManager manages direct processes inside the VM.
type ProcessManager struct {
	mu        sync.RWMutex
	processes map[string]*managedProcess
}

// managedProcess represents a tracked running process.
type managedProcess struct {
	name      string
	binary    string
	args      []string
	env       []string
	workDir   string
	pid       int
	cmd       *exec.Cmd
	startedAt time.Time
	state     entities.AppState
	done      chan struct{} // closed when cmd.Wait() returns in monitorProcess
}

// Ensure ProcessManager implements AppExecutor.
var _ AppExecutor = (*ProcessManager)(nil)

// NewProcessManager creates a new process manager.
func NewProcessManager() (*ProcessManager, error) {
	return &ProcessManager{
		processes: make(map[string]*managedProcess),
	}, nil
}

// Deploy starts a process for the given app spec.
func (m *ProcessManager) Deploy(ctx context.Context, app *entities.AppSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop existing process if running.
	if existing, ok := m.processes[app.Name]; ok {
		m.stopProcess(existing)
		delete(m.processes, app.Name)
	}

	binary := app.Binary
	if binary == "" {
		return fmt.Errorf("binary path is required for process mode")
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

	// Create and start command.
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if app.WorkingDir != "" {
		cmd.Dir = app.WorkingDir
	}

	// Set process group for clean shutdown.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	proc := &managedProcess{
		name:      app.Name,
		binary:    binary,
		args:      args,
		env:       env,
		workDir:   app.WorkingDir,
		pid:       cmd.Process.Pid,
		cmd:       cmd,
		startedAt: time.Now(),
		state:     entities.AppStateRunning,
		done:      make(chan struct{}),
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

	m.stopProcess(proc)
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
		status := v1.AppStatus{
			Name:          proc.name,
			ExecutionMode: entities.ExecutionModeProcess,
			ID:            strconv.Itoa(proc.pid),
			Binary:        proc.binary,
			State:         proc.state,
			Status:        string(proc.state),
			StartedAt:     &startedAt,
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// Close stops all managed processes.
func (m *ProcessManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, proc := range m.processes {
		m.stopProcess(proc)
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

// monitorProcess waits for process exit and updates state.
func (m *ProcessManager) monitorProcess(proc *managedProcess) {
	if proc.cmd == nil {
		close(proc.done)
		return
	}

	err := proc.cmd.Wait()
	close(proc.done)

	m.mu.Lock()
	defer m.mu.Unlock()

	if tracked, ok := m.processes[proc.name]; ok && tracked == proc {
		if err != nil {
			proc.state = entities.AppStateFailed
		} else {
			proc.state = entities.AppStateStopped
		}
	}
}
