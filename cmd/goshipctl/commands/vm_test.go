package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("cannot get home dir: %v", err)
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"~/foo/bar", filepath.Join(home, "foo/bar")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}

	for _, tc := range tests {
		result := expandPath(tc.input)
		if result != tc.expected {
			t.Errorf("expandPath(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestVMCreateCmd_RequiresName(t *testing.T) {
	rootCmd.SetArgs([]string{"vm", "create"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when name argument is not provided")
	}
}

func TestVMDestroyCmd_RequiresName(t *testing.T) {
	rootCmd.SetArgs([]string{"vm", "destroy"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when name argument is not provided")
	}
}

func TestVMPingCmd_RequiresName(t *testing.T) {
	rootCmd.SetArgs([]string{"vm", "ping"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when name argument is not provided")
	}
}

func TestVMCreateCmd_MissingInitBinary(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{
		"vm", "create", "test-vm",
		"--base-image", "/tmp/nonexistent-base.qcow2",
		"--goship-init", "/tmp/nonexistent-goship-init",
	})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when goship-init binary is missing")
	}
	if !strings.Contains(err.Error(), "goship-init binary not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}
