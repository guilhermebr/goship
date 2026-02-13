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

	// VMInfo should be present with hostname and at least one IP.
	if resp.VMInfo == nil {
		t.Fatal("expected VMInfo to be set")
	}
	if resp.VMInfo.Hostname == "" {
		t.Fatal("expected hostname to be set")
	}
	// The test host should have at least one non-loopback IP.
	if len(resp.VMInfo.IPAddresses) == 0 {
		t.Fatal("expected at least one IP address")
	}
}

func TestHandleLogs(t *testing.T) {
	agent, _, _ := newTestInit(t)

	// Create a temporary log file.
	tmpFile := t.TempDir() + "/goship-init.log"
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Override the default log file path for testing via readLastNLines directly.
	// Test readLastNLines with the temp file.
	result, err := readLastNLines(tmpFile, 3)
	if err != nil {
		t.Fatalf("readLastNLines: %v", err)
	}
	if result != "line3\nline4\nline5" {
		t.Fatalf("expected last 3 lines, got %q", result)
	}

	// Test with more lines than file has.
	result, err = readLastNLines(tmpFile, 100)
	if err != nil {
		t.Fatalf("readLastNLines: %v", err)
	}
	if result != "line1\nline2\nline3\nline4\nline5" {
		t.Fatalf("expected all 5 lines, got %q", result)
	}

	// Test handleLogs returns error for missing log file (DefaultLogFile won't exist in test).
	resp := agent.handleLogs(&v1.InitCommand{Action: v1.ActionLogs, Lines: 10})
	if resp.Status != v1.StatusError {
		t.Fatalf("expected error status for missing log file, got %q", resp.Status)
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
