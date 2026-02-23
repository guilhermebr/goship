package libvirt

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"libvirt.org/go/libvirt"
)

func TestCreateDiskImage_BaseImageNotFound(t *testing.T) {
	err := CreateDiskImage("/nonexistent/base.qcow2", "/tmp/disk.qcow2")
	if err == nil {
		t.Fatal("expected error for missing base image")
	}
	if !strings.Contains(err.Error(), "base image not found") {
		t.Errorf("expected 'base image not found' error, got: %v", err)
	}
}

func TestCreateDiskImage_Success(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img not available, skipping")
	}

	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.qcow2")
	diskPath := filepath.Join(tmpDir, "disk.qcow2")

	// Create a real base qcow2 image.
	out, err := exec.Command("qemu-img", "create", "-f", "qcow2", basePath, "1G").CombinedOutput()
	if err != nil {
		t.Fatalf("failed to create base image: %v\n%s", err, out)
	}

	// Create CoW disk backed by the base image.
	if createErr := CreateDiskImage(basePath, diskPath); createErr != nil {
		t.Fatalf("CreateDiskImage failed: %v", createErr)
	}

	// Verify file exists.
	if _, statErr := os.Stat(diskPath); statErr != nil {
		t.Fatalf("disk image not created: %v", statErr)
	}

	// Verify backing file with qemu-img info.
	info, err := exec.Command("qemu-img", "info", diskPath).CombinedOutput()
	if err != nil {
		t.Fatalf("qemu-img info failed: %v\n%s", err, info)
	}
	if !strings.Contains(string(info), "backing file") {
		t.Error("expected backing file in qemu-img info output")
	}
}

func TestFormatVMState(t *testing.T) {
	tests := []struct {
		state    libvirt.DomainState
		expected string
	}{
		{libvirt.DOMAIN_NOSTATE, "no state"},
		{libvirt.DOMAIN_RUNNING, "running"},
		{libvirt.DOMAIN_BLOCKED, "blocked"},
		{libvirt.DOMAIN_PAUSED, "paused"},
		{libvirt.DOMAIN_SHUTDOWN, "shutting down"},
		{libvirt.DOMAIN_SHUTOFF, "shut off"},
		{libvirt.DOMAIN_CRASHED, "crashed"},
		{libvirt.DOMAIN_PMSUSPENDED, "pm suspended"},
		{libvirt.DomainState(99), "unknown"},
	}

	for _, tc := range tests {
		result := FormatVMState(tc.state)
		if result != tc.expected {
			t.Errorf("FormatVMState(%d) = %q, want %q", tc.state, result, tc.expected)
		}
	}
}
