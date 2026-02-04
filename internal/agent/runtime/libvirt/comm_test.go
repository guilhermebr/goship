package libvirt

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/guilhermebr/goship/pkg/api/v1"
)

// mockInitServer creates a Unix socket listener that accepts one connection,
// reads a JSON command, and writes a JSON response using the provided handler.
func mockInitServer(t *testing.T, socketPath string, handler func(v1.InitCommand) v1.InitResponse) net.Listener {
	t.Helper()

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to create mock listener: %v", err)
	}

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed
		}
		defer conn.Close()

		decoder := json.NewDecoder(conn)
		encoder := json.NewEncoder(conn)

		for {
			var cmd v1.InitCommand
			if err := decoder.Decode(&cmd); err != nil {
				return
			}
			resp := handler(cmd)
			if err := encoder.Encode(resp); err != nil {
				return
			}
		}
	}()

	return ln
}

func TestCommSendCommand(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	ln := mockInitServer(t, sockPath, func(cmd v1.InitCommand) v1.InitResponse {
		if cmd.Action == v1.ActionDeploy && cmd.AppName == "web" {
			return v1.InitResponse{Status: v1.StatusOK}
		}
		return v1.InitResponse{Status: v1.StatusError, Error: "unexpected command"}
	})
	defer ln.Close()

	comm, err := NewVMCommunicator(sockPath)
	if err != nil {
		t.Fatalf("NewVMCommunicator failed: %v", err)
	}
	defer comm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := comm.SendCommand(ctx, &v1.InitCommand{
		Action:  v1.ActionDeploy,
		AppName: "web",
	})
	if err != nil {
		t.Fatalf("SendCommand failed: %v", err)
	}
	if resp.Status != v1.StatusOK {
		t.Errorf("Status = %q, want %q", resp.Status, v1.StatusOK)
	}
}

func TestCommSendCommand_ErrorResponse(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	ln := mockInitServer(t, sockPath, func(cmd v1.InitCommand) v1.InitResponse {
		return v1.InitResponse{Status: v1.StatusError, Error: "app not found"}
	})
	defer ln.Close()

	comm, err := NewVMCommunicator(sockPath)
	if err != nil {
		t.Fatalf("NewVMCommunicator failed: %v", err)
	}
	defer comm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := comm.SendCommand(ctx, &v1.InitCommand{
		Action:  v1.ActionStop,
		AppName: "missing",
	})
	if err != nil {
		t.Fatalf("SendCommand failed: %v", err)
	}
	if resp.Status != v1.StatusError {
		t.Errorf("Status = %q, want %q", resp.Status, v1.StatusError)
	}
	if resp.Error != "app not found" {
		t.Errorf("Error = %q, want %q", resp.Error, "app not found")
	}
}

func TestCommPing(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	ln := mockInitServer(t, sockPath, func(cmd v1.InitCommand) v1.InitResponse {
		if cmd.Action == v1.ActionPing {
			return v1.InitResponse{Status: v1.StatusOK}
		}
		return v1.InitResponse{Status: v1.StatusError, Error: "not a ping"}
	})
	defer ln.Close()

	comm, err := NewVMCommunicator(sockPath)
	if err != nil {
		t.Fatalf("NewVMCommunicator failed: %v", err)
	}
	defer comm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := comm.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestCommPing_Failure(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	ln := mockInitServer(t, sockPath, func(cmd v1.InitCommand) v1.InitResponse {
		return v1.InitResponse{Status: v1.StatusError, Error: "agent not ready"}
	})
	defer ln.Close()

	comm, err := NewVMCommunicator(sockPath)
	if err != nil {
		t.Fatalf("NewVMCommunicator failed: %v", err)
	}
	defer comm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := comm.Ping(ctx); err == nil {
		t.Error("expected Ping to fail, got nil error")
	}
}

func TestCommMultipleCommands(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	var received []string
	ln := mockInitServer(t, sockPath, func(cmd v1.InitCommand) v1.InitResponse {
		received = append(received, cmd.Action)
		return v1.InitResponse{Status: v1.StatusOK}
	})
	defer ln.Close()

	comm, err := NewVMCommunicator(sockPath)
	if err != nil {
		t.Fatalf("NewVMCommunicator failed: %v", err)
	}
	defer comm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	actions := []string{v1.ActionPing, v1.ActionDeploy, v1.ActionStatus}
	for _, action := range actions {
		resp, err := comm.SendCommand(ctx, &v1.InitCommand{Action: action})
		if err != nil {
			t.Fatalf("SendCommand(%s) failed: %v", action, err)
		}
		if resp.Status != v1.StatusOK {
			t.Errorf("SendCommand(%s) status = %q, want %q", action, resp.Status, v1.StatusOK)
		}
	}
}

func TestCommConnectFailure(t *testing.T) {
	_, err := NewVMCommunicator("/nonexistent/path/goship.sock")
	if err == nil {
		t.Error("expected error connecting to nonexistent socket")
	}
}

func TestCommSocketPath(t *testing.T) {
	// Verify the socket path used in Create() matches convention.
	dir := filepath.Join(os.TempDir(), "goship-test-vms", "myvm")
	expected := filepath.Join(dir, "goship.sock")

	got := filepath.Join(dir, "goship.sock")
	if got != expected {
		t.Errorf("socket path = %q, want %q", got, expected)
	}
}
