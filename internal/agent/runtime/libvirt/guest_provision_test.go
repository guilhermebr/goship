package libvirt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckGuestProvisionDependencies_MissingVirtCustomize(t *testing.T) {
	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })

	lookPath = func(file string) (string, error) {
		if file == "virt-customize" {
			return "", fmt.Errorf("not found")
		}
		return "/usr/bin/" + file, nil
	}

	err := CheckGuestProvisionDependencies()
	if err == nil {
		t.Fatal("expected dependency error, got nil")
	}
	if !strings.Contains(err.Error(), "virt-customize not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildVirtCustomizeArgs_ContainsInitAndServiceWiring(t *testing.T) {
	args := buildVirtCustomizeArgs(GuestProvisionOptions{
		DiskPath:       "/tmp/disk.qcow2",
		InitBinaryPath: "/tmp/goship-init",
		InstallDocker:  false,
	}, "/tmp/goship-init-service-123", "/tmp/goship-resolv-123")

	joined := strings.Join(args, " ")
	for _, expected := range []string{
		"/tmp/goship-init:/opt/goship/",
		"0755:/opt/goship/goship-init",
		"/etc/init.d/goship-init",
		"rc-update add goship-init default",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("args missing %q: %s", expected, joined)
		}
	}
}

func TestProvisionGuestDisk_InvokesVirtCustomize(t *testing.T) {
	dir := t.TempDir()
	disk := filepath.Join(dir, "disk.qcow2")
	initBin := filepath.Join(dir, "goship-init")

	if err := os.WriteFile(disk, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(initBin, []byte("#!/bin/sh\necho test\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	origLookPath := lookPath
	origRunCmd := runCmd
	t.Cleanup(func() {
		lookPath = origLookPath
		runCmd = origRunCmd
	})

	lookPath = func(file string) (string, error) {
		if file == "virt-customize" {
			return "/usr/bin/virt-customize", nil
		}
		return "", fmt.Errorf("unexpected lookup: %s", file)
	}

	called := false
	runCmd = func(name string, args ...string) ([]byte, error) {
		if name != "virt-customize" {
			return nil, fmt.Errorf("unexpected command: %s", name)
		}
		called = true
		return []byte("ok"), nil
	}

	if err := ProvisionGuestDisk(GuestProvisionOptions{
		DiskPath:       disk,
		InitBinaryPath: initBin,
		InstallDocker:  true,
	}); err != nil {
		t.Fatalf("ProvisionGuestDisk error: %v", err)
	}
	if !called {
		t.Fatal("expected virt-customize command to be called")
	}
}
