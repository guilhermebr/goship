package gsinit

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	v1 "github.com/guilhermebr/goship/pkg/api/v1"
)

const (
	// DefaultLogFile is the path to the goship-init log file inside the VM.
	DefaultLogFile = "/var/log/goship-init.log"
	// DefaultLogLines is the default number of log lines to return.
	DefaultLogLines = 100
)

// Init is the GoShip Init agent orchestrator.
// It manages the communicator lifecycle and registers command handlers.
type Init struct {
	comm   *Communicator
	ctx    context.Context
	cancel context.CancelFunc
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
	ctx, cancel := context.WithCancel(context.Background())

	i := &Init{
		comm:   comm,
		ctx:    ctx,
		cancel: cancel,
	}

	comm.RegisterHandler(v1.ActionPing, i.handlePing)
	comm.RegisterHandler(v1.ActionStatus, i.handleStatus)
	comm.RegisterHandler(v1.ActionLogs, i.handleLogs)

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
