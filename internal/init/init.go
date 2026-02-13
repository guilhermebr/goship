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
	"syscall"

	v1 "github.com/guilhermebr/goship/pkg/api/v1"
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
// It manages the communicator lifecycle and registers command handlers.
type Init struct {
	comm    *Communicator
	ctx     context.Context
	cancel  context.CancelFunc
	initDir string       // directory where goship-init binary lives
	update  *updateState // in-progress update transfer (nil when idle)
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

	i := &Init{
		comm:    comm,
		ctx:     ctx,
		cancel:  cancel,
		initDir: initDir,
	}

	comm.RegisterHandler(v1.ActionPing, i.handlePing)
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
	if err := i.comm.Close(); err != nil {
		log.Printf("goship-init: error closing communicator: %v", err)
	}
}

func (i *Init) handlePing(_ *v1.InitCommand) *v1.InitResponse {
	return &v1.InitResponse{Status: v1.StatusOK}
}

func (i *Init) handleLogs(cmd *v1.InitCommand) *v1.InitResponse {
	lines := cmd.Lines
	if lines <= 0 {
		lines = DefaultLogLines
	}

	content, err := readLastNLines(DefaultLogFile, lines)
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
	vmInfo := &v1.VMInfo{}

	if hostname, err := os.Hostname(); err == nil {
		vmInfo.Hostname = hostname
	}

	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			// Skip loopback and interfaces that are down.
			if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip == nil || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
					continue
				}
				vmInfo.IPAddresses = append(vmInfo.IPAddresses, ip.String())
			}
		}
	}

	return &v1.InitResponse{Status: v1.StatusOK, VMInfo: vmInfo}
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
