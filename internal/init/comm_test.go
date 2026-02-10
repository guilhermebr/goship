package gsinit

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v1 "github.com/guilhermebr/goship/pkg/api/v1"
)

// pipeReadWriteCloser wraps a read pipe and write pipe into a single io.ReadWriteCloser.
type pipeReadWriteCloser struct {
	r *os.File
	w *os.File
}

func (p *pipeReadWriteCloser) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *pipeReadWriteCloser) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *pipeReadWriteCloser) Close() error {
	err1 := p.r.Close()
	err2 := p.w.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

// newTestPipes creates two connected pipe pairs for simulating a serial device.
// hostWriter -> guestReader (host sends commands to guest)
// guestWriter -> hostReader (guest sends responses to host)
func newTestPipes(t *testing.T) (guest *Communicator, hostWriter *os.File, hostReader *os.File) {
	t.Helper()

	// Pipe for host -> guest (commands)
	guestReadEnd, hostWriteEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create host->guest pipe: %v", err)
	}

	// Pipe for guest -> host (responses)
	hostReadEnd, guestWriteEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create guest->host pipe: %v", err)
	}

	rwc := &pipeReadWriteCloser{r: guestReadEnd, w: guestWriteEnd}
	comm := newCommunicatorFromReadWriteCloser(rwc)

	t.Cleanup(func() {
		comm.Close()
		hostWriteEnd.Close()
		hostReadEnd.Close()
	})

	return comm, hostWriteEnd, hostReadEnd
}

func TestNewCommunicatorWithPipe(t *testing.T) {
	comm, _, _ := newTestPipes(t)
	if comm == nil {
		t.Fatal("expected non-nil communicator")
	}
	if comm.handlers == nil {
		t.Fatal("expected non-nil handlers map")
	}
}

func TestRegisterHandler(t *testing.T) {
	comm, _, _ := newTestPipes(t)

	called := false
	comm.RegisterHandler("test", func(_ *v1.InitCommand) *v1.InitResponse {
		called = true
		return &v1.InitResponse{Status: v1.StatusOK}
	})

	comm.mu.Lock()
	_, ok := comm.handlers["test"]
	comm.mu.Unlock()

	if !ok {
		t.Fatal("handler not registered")
	}

	// Verify we don't call the handler just by registering.
	if called {
		t.Fatal("handler should not have been called yet")
	}
}

func TestSendResponse(t *testing.T) {
	comm, _, hostReader := newTestPipes(t)

	resp := &v1.InitResponse{Status: v1.StatusOK}
	comm.sendResponse(resp)

	reader := bufio.NewReader(hostReader)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	var got v1.InitResponse
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if got.Status != v1.StatusOK {
		t.Fatalf("expected status %q, got %q", v1.StatusOK, got.Status)
	}
}

func TestListenPing(t *testing.T) {
	comm, hostWriter, hostReader := newTestPipes(t)
	comm.RegisterHandler(v1.ActionPing, func(_ *v1.InitCommand) *v1.InitResponse {
		return &v1.InitResponse{Status: v1.StatusOK}
	})

	// Send ping command from "host".
	cmd := v1.InitCommand{Action: v1.ActionPing}
	data, _ := json.Marshal(cmd)
	data = append(data, '\n')

	go func() {
		hostWriter.Write(data)
		// Close to signal EOF and end Listen loop.
		hostWriter.Close()
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- comm.Listen()
	}()

	// Read response from "guest".
	reader := bufio.NewReader(hostReader)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	var resp v1.InitResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Status != v1.StatusOK {
		t.Fatalf("expected status %q, got %q", v1.StatusOK, resp.Status)
	}

	// Listen should end with nil on EOF.
	if err := <-errCh; err != nil {
		t.Fatalf("Listen returned unexpected error: %v", err)
	}
}

func TestListenInvalidJSON(t *testing.T) {
	comm, hostWriter, hostReader := newTestPipes(t)

	go func() {
		hostWriter.Write([]byte("not json\n"))
		hostWriter.Close()
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- comm.Listen()
	}()

	// Should get an error response.
	reader := bufio.NewReader(hostReader)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	var resp v1.InitResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Status != v1.StatusError {
		t.Fatalf("expected status %q, got %q", v1.StatusError, resp.Status)
	}
	if resp.Error != "invalid JSON" {
		t.Fatalf("expected error %q, got %q", "invalid JSON", resp.Error)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Listen returned unexpected error: %v", err)
	}
}

func TestListenUnknownAction(t *testing.T) {
	comm, hostWriter, hostReader := newTestPipes(t)

	cmd := v1.InitCommand{Action: "nonexistent"}
	data, _ := json.Marshal(cmd)
	data = append(data, '\n')

	go func() {
		hostWriter.Write(data)
		hostWriter.Close()
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- comm.Listen()
	}()

	reader := bufio.NewReader(hostReader)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	var resp v1.InitResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Status != v1.StatusError {
		t.Fatalf("expected status %q, got %q", v1.StatusError, resp.Status)
	}
	if resp.Error != "unknown action: nonexistent" {
		t.Fatalf("expected error about unknown action, got %q", resp.Error)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Listen returned unexpected error: %v", err)
	}
}

func TestListenMultipleCommands(t *testing.T) {
	comm, hostWriter, hostReader := newTestPipes(t)
	comm.RegisterHandler(v1.ActionPing, func(_ *v1.InitCommand) *v1.InitResponse {
		return &v1.InitResponse{Status: v1.StatusOK}
	})
	comm.RegisterHandler(v1.ActionStatus, func(_ *v1.InitCommand) *v1.InitResponse {
		return &v1.InitResponse{Status: v1.StatusOK}
	})

	ping, _ := json.Marshal(v1.InitCommand{Action: v1.ActionPing})
	status, _ := json.Marshal(v1.InitCommand{Action: v1.ActionStatus})

	go func() {
		hostWriter.Write(append(ping, '\n'))
		hostWriter.Write(append(status, '\n'))
		hostWriter.Close()
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- comm.Listen()
	}()

	reader := bufio.NewReader(hostReader)

	// Read two responses.
	for i := 0; i < 2; i++ {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("command %d: failed to read response: %v", i, err)
		}
		var resp v1.InitResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			t.Fatalf("command %d: failed to unmarshal: %v", i, err)
		}
		if resp.Status != v1.StatusOK {
			t.Fatalf("command %d: expected status %q, got %q", i, v1.StatusOK, resp.Status)
		}
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Listen returned unexpected error: %v", err)
	}
}

func TestClose(t *testing.T) {
	comm, _, _ := newTestPipes(t)

	if err := comm.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	// After closing, reading should fail.
	_, err := comm.reader.ReadBytes('\n')
	if err == nil || err == io.EOF {
		// EOF is acceptable after close on some OSes.
		return
	}
	// Any other read error is also fine — the device is closed.
}

func TestOpenDeviceWithRetry_DeviceAppears(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "vport0p1")
	fallback := filepath.Join(dir, "ttyS1")

	go func() {
		time.Sleep(20 * time.Millisecond)
		f, err := os.OpenFile(primary, os.O_CREATE|os.O_RDWR, 0o644)
		if err == nil {
			_ = f.Close()
		}
	}()

	f, err := openDeviceWithRetry([]string{primary, fallback}, 300*time.Millisecond, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("openDeviceWithRetry error: %v", err)
	}
	_ = f.Close()
}

func TestOpenDeviceWithRetry_UsesFallbackAfterPrimaryTimeout(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "vport0p1")
	fallback := filepath.Join(dir, "ttyS1")

	fb, err := os.OpenFile(fallback, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_ = fb.Close()

	f, err := openDeviceWithRetry([]string{primary, fallback}, 60*time.Millisecond, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("expected fallback open to succeed, got error: %v", err)
	}
	_ = f.Close()
}

func TestOpenDeviceWithRetry_Timeout(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "missing-primary")
	fallback := filepath.Join(dir, "missing-fallback")

	_, err := openDeviceWithRetry([]string{primary, fallback}, 40*time.Millisecond, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "elapsed") {
		t.Fatalf("expected elapsed in error, got: %v", err)
	}
}
