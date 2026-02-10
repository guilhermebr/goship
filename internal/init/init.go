package gsinit

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	v1 "github.com/guilhermebr/goship/pkg/api/v1"
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

func (i *Init) handleStatus(_ *v1.InitCommand) *v1.InitResponse {
	return &v1.InitResponse{Status: v1.StatusOK}
}
