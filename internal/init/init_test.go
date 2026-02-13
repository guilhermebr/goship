package gsinit

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	v1 "github.com/guilhermebr/goship/pkg/api/v1"
)

func newTestInit(t *testing.T) (*Init, *os.File, *os.File) {
	t.Helper()
	comm, hostWriter, hostReader := newTestPipes(t)
	agent := newFromCommunicator(comm)
	return agent, hostWriter, hostReader
}

func newTestInitWithDir(t *testing.T, dir string) (*Init, *os.File, *os.File) {
	t.Helper()
	comm, hostWriter, hostReader := newTestPipes(t)
	agent := newFromCommunicatorWithDir(comm, dir)
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

func TestHandleUpdateInit_FullCycle(t *testing.T) {
	dir := t.TempDir()
	agent, _, _ := newTestInitWithDir(t, dir)

	// Create a fake binary payload.
	payload := []byte("#!/bin/sh\necho hello from goship-init v2\n")
	checksum := fmt.Sprintf("%x", sha256.Sum256(payload))

	// Phase: begin
	resp := agent.handleUpdateInit(&v1.InitCommand{
		Action:   v1.ActionUpdateInit,
		Phase:    "begin",
		Size:     int64(len(payload)),
		Checksum: checksum,
	})
	if resp.Status != v1.StatusOK {
		t.Fatalf("begin: expected OK, got %q: %s", resp.Status, resp.Error)
	}

	// Phase: data (send in two chunks)
	mid := len(payload) / 2
	chunk1 := base64.StdEncoding.EncodeToString(payload[:mid])
	chunk2 := base64.StdEncoding.EncodeToString(payload[mid:])

	resp = agent.handleUpdateInit(&v1.InitCommand{
		Action: v1.ActionUpdateInit,
		Phase:  "data",
		Data:   chunk1,
	})
	if resp.Status != v1.StatusOK {
		t.Fatalf("data chunk1: expected OK, got %q: %s", resp.Status, resp.Error)
	}
	if resp.BytesReceived != int64(mid) {
		t.Fatalf("data chunk1: expected %d bytes received, got %d", mid, resp.BytesReceived)
	}

	resp = agent.handleUpdateInit(&v1.InitCommand{
		Action: v1.ActionUpdateInit,
		Phase:  "data",
		Data:   chunk2,
	})
	if resp.Status != v1.StatusOK {
		t.Fatalf("data chunk2: expected OK, got %q: %s", resp.Status, resp.Error)
	}
	if resp.BytesReceived != int64(len(payload)) {
		t.Fatalf("data chunk2: expected %d bytes received, got %d", len(payload), resp.BytesReceived)
	}

	// Phase: finish
	resp = agent.handleUpdateInit(&v1.InitCommand{
		Action: v1.ActionUpdateInit,
		Phase:  "finish",
	})
	if resp.Status != v1.StatusOK {
		t.Fatalf("finish: expected OK, got %q: %s", resp.Status, resp.Error)
	}

	// Verify binary was placed correctly.
	binaryPath := filepath.Join(dir, "goship-init")
	content, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("failed to read installed binary: %v", err)
	}
	if string(content) != string(payload) {
		t.Fatalf("binary content mismatch: got %q", string(content))
	}

	// Verify file is executable.
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("failed to stat binary: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Fatalf("binary is not executable: mode=%v", info.Mode())
	}

	// Verify update state is cleared.
	if agent.update != nil {
		t.Fatal("expected update state to be nil after finish")
	}
}

func TestHandleUpdateInit_ChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	agent, _, _ := newTestInitWithDir(t, dir)

	payload := []byte("binary data")

	// Begin with wrong checksum.
	resp := agent.handleUpdateInit(&v1.InitCommand{
		Action:   v1.ActionUpdateInit,
		Phase:    "begin",
		Size:     int64(len(payload)),
		Checksum: "0000000000000000000000000000000000000000000000000000000000000000",
	})
	if resp.Status != v1.StatusOK {
		t.Fatalf("begin: expected OK, got %q: %s", resp.Status, resp.Error)
	}

	// Send all data.
	resp = agent.handleUpdateInit(&v1.InitCommand{
		Action: v1.ActionUpdateInit,
		Phase:  "data",
		Data:   base64.StdEncoding.EncodeToString(payload),
	})
	if resp.Status != v1.StatusOK {
		t.Fatalf("data: expected OK, got %q: %s", resp.Status, resp.Error)
	}

	// Finish should fail with checksum mismatch.
	resp = agent.handleUpdateInit(&v1.InitCommand{
		Action: v1.ActionUpdateInit,
		Phase:  "finish",
	})
	if resp.Status != v1.StatusError {
		t.Fatalf("finish: expected error, got %q", resp.Status)
	}
	if resp.Error == "" {
		t.Fatal("finish: expected error message")
	}

	// Temp file should be cleaned up.
	tmpPath := filepath.Join(dir, "goship-init.new")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("expected temp file to be removed, got err=%v", err)
	}

	// Update state should be cleared.
	if agent.update != nil {
		t.Fatal("expected update state to be nil after checksum mismatch")
	}
}

func TestHandleUpdateInit_DataWithoutBegin(t *testing.T) {
	dir := t.TempDir()
	agent, _, _ := newTestInitWithDir(t, dir)

	// Send data without begin.
	resp := agent.handleUpdateInit(&v1.InitCommand{
		Action: v1.ActionUpdateInit,
		Phase:  "data",
		Data:   base64.StdEncoding.EncodeToString([]byte("some data")),
	})
	if resp.Status != v1.StatusError {
		t.Fatalf("expected error, got %q", resp.Status)
	}

	// Send finish without begin.
	resp = agent.handleUpdateInit(&v1.InitCommand{
		Action: v1.ActionUpdateInit,
		Phase:  "finish",
	})
	if resp.Status != v1.StatusError {
		t.Fatalf("expected error, got %q", resp.Status)
	}
}

func TestHandleUpdateInit_CancelPrevious(t *testing.T) {
	dir := t.TempDir()
	agent, _, _ := newTestInitWithDir(t, dir)

	payload := []byte("first attempt data")
	checksum := fmt.Sprintf("%x", sha256.Sum256(payload))

	// Start first transfer.
	resp := agent.handleUpdateInit(&v1.InitCommand{
		Action:   v1.ActionUpdateInit,
		Phase:    "begin",
		Size:     int64(len(payload)),
		Checksum: checksum,
	})
	if resp.Status != v1.StatusOK {
		t.Fatalf("first begin: expected OK, got %q: %s", resp.Status, resp.Error)
	}

	// Send some data.
	resp = agent.handleUpdateInit(&v1.InitCommand{
		Action: v1.ActionUpdateInit,
		Phase:  "data",
		Data:   base64.StdEncoding.EncodeToString(payload[:5]),
	})
	if resp.Status != v1.StatusOK {
		t.Fatalf("first data: expected OK, got %q: %s", resp.Status, resp.Error)
	}

	// Start new transfer (should cancel the first one).
	newPayload := []byte("second attempt")
	newChecksum := fmt.Sprintf("%x", sha256.Sum256(newPayload))

	resp = agent.handleUpdateInit(&v1.InitCommand{
		Action:   v1.ActionUpdateInit,
		Phase:    "begin",
		Size:     int64(len(newPayload)),
		Checksum: newChecksum,
	})
	if resp.Status != v1.StatusOK {
		t.Fatalf("second begin: expected OK, got %q: %s", resp.Status, resp.Error)
	}

	// Complete the second transfer.
	resp = agent.handleUpdateInit(&v1.InitCommand{
		Action: v1.ActionUpdateInit,
		Phase:  "data",
		Data:   base64.StdEncoding.EncodeToString(newPayload),
	})
	if resp.Status != v1.StatusOK {
		t.Fatalf("second data: expected OK, got %q: %s", resp.Status, resp.Error)
	}

	resp = agent.handleUpdateInit(&v1.InitCommand{
		Action: v1.ActionUpdateInit,
		Phase:  "finish",
	})
	if resp.Status != v1.StatusOK {
		t.Fatalf("second finish: expected OK, got %q: %s", resp.Status, resp.Error)
	}

	// Verify the second payload was installed.
	content, err := os.ReadFile(filepath.Join(dir, "goship-init"))
	if err != nil {
		t.Fatalf("failed to read binary: %v", err)
	}
	if string(content) != string(newPayload) {
		t.Fatalf("expected second payload, got %q", string(content))
	}
}
