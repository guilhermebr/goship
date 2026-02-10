package gsinit

import (
	"os"
	"testing"

	v1 "github.com/guilhermebr/goship/pkg/api/v1"
)

func newTestInit(t *testing.T) (*Init, *os.File, *os.File) {
	t.Helper()
	comm, hostWriter, hostReader := newTestPipes(t)
	agent := newFromCommunicator(comm)
	return agent, hostWriter, hostReader
}

func TestHandlePing(t *testing.T) {
	agent, _, _ := newTestInit(t)

	resp := agent.handlePing(&v1.InitCommand{Action: v1.ActionPing})
	if resp.Status != v1.StatusOK {
		t.Fatalf("expected status %q, got %q", v1.StatusOK, resp.Status)
	}
	if resp.Error != "" {
		t.Fatalf("expected no error, got %q", resp.Error)
	}
}

func TestHandleStatus(t *testing.T) {
	agent, _, _ := newTestInit(t)

	resp := agent.handleStatus(&v1.InitCommand{Action: v1.ActionStatus})
	if resp.Status != v1.StatusOK {
		t.Fatalf("expected status %q, got %q", v1.StatusOK, resp.Status)
	}
	if resp.Error != "" {
		t.Fatalf("expected no error, got %q", resp.Error)
	}
}

func TestShutdown(t *testing.T) {
	agent, _, _ := newTestInit(t)

	agent.Shutdown()

	// Context should be cancelled.
	select {
	case <-agent.ctx.Done():
		// Expected.
	default:
		t.Fatal("expected context to be cancelled after Shutdown")
	}
}
