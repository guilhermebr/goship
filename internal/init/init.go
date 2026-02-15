package gsinit

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	v1 "github.com/guilhermebr/goship/pkg/api/v1"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

const (
	// DefaultLogFile is the path to the goship-init log file inside the VM.
	DefaultLogFile = "/var/log/goship-init.log"
	// DefaultLogLines is the default number of log lines to return.
	DefaultLogLines = 100
	// DefaultInitDir is the directory where goship-init binary lives inside the VM.
	DefaultInitDir = "/opt/goship"
)

// updateState tracks an in-progress binary update transfer.
type updateState struct {
	tmpFile   *os.File
	totalSize int64
	checksum  string // expected sha256 hex
	received  int64
}

// Init is the GoShip Init agent orchestrator.
// It manages the communicator lifecycle, Docker/Process executors, and command handlers.
type Init struct {
	comm     *Communicator
	docker   *DockerManager
	process  *ProcessManager
	executor *ExecutorManager
	ctx      context.Context
	cancel   context.CancelFunc
	initDir  string       // directory where goship-init binary lives
	update   *updateState // in-progress update transfer (nil when idle)
}

// New creates a new Init agent with the given serial device path.
func New(serialDevice string) (*Init, error) {
	comm, err := NewCommunicator(serialDevice)
	if err != nil {
		return nil, err
	}
	return newFromCommunicator(comm), nil
}

// newFromCommunicator creates an Init from an existing Communicator (used by tests).
func newFromCommunicator(comm *Communicator) *Init {
	return newFromCommunicatorWithDir(comm, DefaultInitDir)
}

// newFromCommunicatorWithDir creates an Init with a custom init directory (used by tests).
func newFromCommunicatorWithDir(comm *Communicator, initDir string) *Init {
	ctx, cancel := context.WithCancel(context.Background())

	// Wait for Docker daemon (optional for container mode).
	docker, err := waitForDocker(ctx, 60*time.Second)
	if err != nil {
		log.Printf("goship-init: Docker not available: %v (process mode only)", err)
		docker = nil
	}

	// Process manager is always available.
	process, err := NewProcessManager()
	if err != nil {
		log.Printf("goship-init: failed to create process manager: %v", err)
		process = nil
	}

	executor := NewExecutorManager(docker, process)

	i := &Init{
		comm:     comm,
		docker:   docker,
		process:  process,
		executor: executor,
		ctx:      ctx,
		cancel:   cancel,
		initDir:  initDir,
	}

	comm.RegisterHandler(v1.ActionPing, i.handlePing)
	comm.RegisterHandler(v1.ActionDeploy, i.handleDeploy)
	comm.RegisterHandler(v1.ActionStop, i.handleStop)
	comm.RegisterHandler(v1.ActionRemove, i.handleRemove)
	comm.RegisterHandler(v1.ActionStatus, i.handleStatus)
	comm.RegisterHandler(v1.ActionLogs, i.handleLogs)
	comm.RegisterHandler(v1.ActionUpdateInit, i.handleUpdateInit)

	return i
}

// Run starts the agent. It listens for commands on the serial device and
// handles OS signals for graceful shutdown. Run blocks until shutdown.
func (i *Init) Run() error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	errCh := make(chan error, 1)
	go func() {
		errCh <- i.comm.Listen()
	}()

	log.Println("goship-init: agent ready, listening for commands")

	select {
	case sig := <-sigCh:
		log.Printf("goship-init: received signal %s, shutting down", sig)
		i.Shutdown()
		return nil
	case err := <-errCh:
		i.Shutdown()
		return err
	case <-i.ctx.Done():
		return nil
	}
}

// Shutdown gracefully stops the agent.
func (i *Init) Shutdown() {
	i.cancel()
	if err := i.executor.Close(); err != nil {
		log.Printf("goship-init: error closing executor manager: %v", err)
	}
	if err := i.comm.Close(); err != nil {
		log.Printf("goship-init: error closing communicator: %v", err)
	}
}

func (i *Init) handlePing(_ *v1.InitCommand) *v1.InitResponse {
	return &v1.InitResponse{Status: v1.StatusOK}
}

func (i *Init) handleDeploy(cmd *v1.InitCommand) *v1.InitResponse {
	if cmd.App == nil {
		return &v1.InitResponse{Status: v1.StatusError, Error: "app spec is required"}
	}

	if cmd.App.IsContainerMode() {
		if cmd.App.Image == "" {
			return &v1.InitResponse{Status: v1.StatusError, Error: "image is required for container mode"}
		}
		if i.docker == nil {
			return &v1.InitResponse{Status: v1.StatusError, Error: "Docker is not available for container mode"}
		}
	} else if cmd.App.IsProcessMode() {
		if cmd.App.Binary == "" {
			return &v1.InitResponse{Status: v1.StatusError, Error: "binary is required for process mode"}
		}
	}

	if err := i.executor.Deploy(i.ctx, cmd.App); err != nil {
		return &v1.InitResponse{Status: v1.StatusError, Error: err.Error()}
	}
	return &v1.InitResponse{Status: v1.StatusOK}
}

func (i *Init) handleStop(cmd *v1.InitCommand) *v1.InitResponse {
	if cmd.AppName == "" {
		return &v1.InitResponse{Status: v1.StatusError, Error: "app name is required"}
	}

	if err := i.executor.Stop(i.ctx, cmd.AppName); err != nil {
		return &v1.InitResponse{Status: v1.StatusError, Error: err.Error()}
	}
	return &v1.InitResponse{Status: v1.StatusOK}
}

func (i *Init) handleRemove(cmd *v1.InitCommand) *v1.InitResponse {
	if cmd.AppName == "" {
		return &v1.InitResponse{Status: v1.StatusError, Error: "app name is required"}
	}

	if err := i.executor.Remove(i.ctx, cmd.AppName); err != nil {
		return &v1.InitResponse{Status: v1.StatusError, Error: err.Error()}
	}
	return &v1.InitResponse{Status: v1.StatusOK}
}

func (i *Init) handleLogs(cmd *v1.InitCommand) *v1.InitResponse {
	lines := cmd.Lines
	if lines <= 0 {
		lines = DefaultLogLines
	}

	logFile := cmd.LogFile
	if logFile == "" {
		logFile = DefaultLogFile
	}

	// Validate the path: clean it and restrict to /var/log/ to prevent arbitrary file reads.
	logFile = filepath.Clean(logFile)
	if !strings.HasPrefix(logFile, "/var/log/") {
		return &v1.InitResponse{
			Status: v1.StatusError,
			Error:  "log file must be under /var/log/",
		}
	}

	content, err := readLastNLines(logFile, lines)
	if err != nil {
		return &v1.InitResponse{
			Status: v1.StatusError,
			Error:  fmt.Sprintf("failed to read logs: %v", err),
		}
	}

	return &v1.InitResponse{
		Status: v1.StatusOK,
		Logs:   content,
	}
}

// readLastNLines reads the last n lines from a file.
func readLastNLines(path string, n int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Use a ring buffer to keep only the last n lines.
	ring := make([]string, 0, n)
	for scanner.Scan() {
		if len(ring) < n {
			ring = append(ring, scanner.Text())
		} else {
			copy(ring, ring[1:])
			ring[n-1] = scanner.Text()
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	result := ""
	for i, line := range ring {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result, nil
}

func (i *Init) handleStatus(_ *v1.InitCommand) *v1.InitResponse {
	apps, err := i.executor.GetAllStatus(i.ctx)
	if err != nil {
		return &v1.InitResponse{Status: v1.StatusError, Error: err.Error()}
	}

	// Build backwards-compatible Containers list from container-mode apps.
	var containers []v1.ContainerStatus
	for _, app := range apps {
		if app.ExecutionMode == entities.ExecutionModeContainer || app.ExecutionMode == "" {
			containers = append(containers, app.ToContainerStatus())
		}
	}

	return &v1.InitResponse{
		Status:     v1.StatusOK,
		Apps:       apps,
		Containers: containers,
		VMInfo:     i.getVMInfo(),
	}
}

// getVMInfo returns information about the VM.
func (i *Init) getVMInfo() *v1.VMInfo {
	hostname, _ := os.Hostname()

	var dockerVersion string
	if i.docker != nil {
		dockerVersion, _ = i.docker.GetDockerVersion(i.ctx)
	}

	return &v1.VMInfo{
		Hostname:      hostname,
		IPAddresses:   getIPAddresses(),
		DockerVersion: dockerVersion,
		UptimeSeconds: getUptime(),
	}
}

// getIPAddresses returns all non-loopback IPv4 addresses.
func getIPAddresses() []string {
	var ips []string

	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	return ips
}

// getUptime returns the system uptime in seconds.
func getUptime() int64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	var uptime float64
	if _, err := fmt.Sscanf(string(data), "%f", &uptime); err != nil {
		return 0
	}
	return int64(uptime)
}

// waitForDocker waits for the Docker daemon to become available.
func waitForDocker(ctx context.Context, timeout time.Duration) (*DockerManager, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		docker, err := NewDockerManager()
		if err == nil {
			if _, err = docker.GetDockerVersion(ctx); err == nil {
				return docker, nil
			}
			docker.Close()
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, fmt.Errorf("timeout waiting for Docker daemon")
}

func (i *Init) handleUpdateInit(cmd *v1.InitCommand) *v1.InitResponse {
	switch cmd.Phase {
	case "begin":
		return i.handleUpdateBegin(cmd)
	case "data":
		return i.handleUpdateData(cmd)
	case "finish":
		return i.handleUpdateFinish(cmd)
	default:
		return &v1.InitResponse{
			Status: v1.StatusError,
			Error:  fmt.Sprintf("unknown update phase: %s", cmd.Phase),
		}
	}
}

func (i *Init) handleUpdateBegin(cmd *v1.InitCommand) *v1.InitResponse {
	if cmd.Size <= 0 {
		return &v1.InitResponse{Status: v1.StatusError, Error: "size must be positive"}
	}
	if cmd.Checksum == "" {
		return &v1.InitResponse{Status: v1.StatusError, Error: "checksum is required"}
	}

	// Cancel any previous in-progress update.
	i.cleanupUpdate()

	// Ensure init directory exists.
	if err := os.MkdirAll(i.initDir, 0755); err != nil {
		return &v1.InitResponse{
			Status: v1.StatusError,
			Error:  fmt.Sprintf("failed to create init dir: %v", err),
		}
	}

	tmpPath := filepath.Join(i.initDir, "goship-init.new")
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return &v1.InitResponse{
			Status: v1.StatusError,
			Error:  fmt.Sprintf("failed to create temp file: %v", err),
		}
	}

	i.update = &updateState{
		tmpFile:   f,
		totalSize: cmd.Size,
		checksum:  cmd.Checksum,
	}

	log.Printf("goship-init: update-init begin, size=%d checksum=%s", cmd.Size, cmd.Checksum)
	return &v1.InitResponse{Status: v1.StatusOK}
}

func (i *Init) handleUpdateData(cmd *v1.InitCommand) *v1.InitResponse {
	if i.update == nil {
		return &v1.InitResponse{Status: v1.StatusError, Error: "no update in progress (send begin first)"}
	}

	decoded, err := base64.StdEncoding.DecodeString(cmd.Data)
	if err != nil {
		i.cleanupUpdate()
		return &v1.InitResponse{
			Status: v1.StatusError,
			Error:  fmt.Sprintf("failed to decode data: %v", err),
		}
	}

	n, err := i.update.tmpFile.Write(decoded)
	if err != nil {
		i.cleanupUpdate()
		return &v1.InitResponse{
			Status: v1.StatusError,
			Error:  fmt.Sprintf("failed to write data: %v", err),
		}
	}

	i.update.received += int64(n)

	return &v1.InitResponse{
		Status:        v1.StatusOK,
		BytesReceived: i.update.received,
	}
}

func (i *Init) handleUpdateFinish(_ *v1.InitCommand) *v1.InitResponse {
	if i.update == nil {
		return &v1.InitResponse{Status: v1.StatusError, Error: "no update in progress (send begin first)"}
	}

	// Close the temp file before checksumming.
	tmpPath := i.update.tmpFile.Name()
	expectedChecksum := i.update.checksum
	if err := i.update.tmpFile.Close(); err != nil {
		i.update = nil
		os.Remove(tmpPath)
		return &v1.InitResponse{
			Status: v1.StatusError,
			Error:  fmt.Sprintf("failed to close temp file: %v", err),
		}
	}

	// Compute SHA256 of the written file.
	actualChecksum, err := fileSHA256(tmpPath)
	if err != nil {
		i.update = nil
		os.Remove(tmpPath)
		return &v1.InitResponse{
			Status: v1.StatusError,
			Error:  fmt.Sprintf("failed to compute checksum: %v", err),
		}
	}

	if actualChecksum != expectedChecksum {
		i.update = nil
		os.Remove(tmpPath)
		return &v1.InitResponse{
			Status: v1.StatusError,
			Error:  fmt.Sprintf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum),
		}
	}

	// Atomic swap: current -> .old, .new -> current.
	currentPath := filepath.Join(i.initDir, "goship-init")
	oldPath := filepath.Join(i.initDir, "goship-init.old")

	// Remove any previous .old backup.
	os.Remove(oldPath)

	// Rename current binary to .old (may not exist on first install).
	if err := os.Rename(currentPath, oldPath); err != nil && !os.IsNotExist(err) {
		i.update = nil
		os.Remove(tmpPath)
		return &v1.InitResponse{
			Status: v1.StatusError,
			Error:  fmt.Sprintf("failed to backup current binary: %v", err),
		}
	}

	// Rename .new to current.
	if err := os.Rename(tmpPath, currentPath); err != nil {
		// Try to restore from .old.
		os.Rename(oldPath, currentPath)
		i.update = nil
		return &v1.InitResponse{
			Status: v1.StatusError,
			Error:  fmt.Sprintf("failed to install new binary: %v", err),
		}
	}

	if err := os.Chmod(currentPath, 0755); err != nil {
		log.Printf("goship-init: warning: chmod failed: %v", err)
	}

	i.update = nil
	log.Printf("goship-init: update-init complete, binary replaced at %s", currentPath)
	return &v1.InitResponse{Status: v1.StatusOK}
}

// cleanupUpdate cancels any in-progress update transfer.
func (i *Init) cleanupUpdate() {
	if i.update == nil {
		return
	}
	tmpPath := i.update.tmpFile.Name()
	i.update.tmpFile.Close()
	os.Remove(tmpPath)
	i.update = nil
}

// fileSHA256 computes the SHA256 hex digest of a file.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
