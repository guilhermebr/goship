package libvirt

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isoToolAvailable returns true if genisoimage or mkisofs is installed.
func isoToolAvailable() bool {
	_, err := findISOTool()
	return err == nil
}

func TestGenerateCloudInitISO(t *testing.T) {
	if !isoToolAvailable() {
		t.Skip("skipping: neither genisoimage nor mkisofs available")
	}

	tmpDir := t.TempDir()
	isoPath := filepath.Join(tmpDir, "cloud-init.iso")

	err := GenerateCloudInitISO(&CloudInitConfig{
		InstanceID: "test-instance-001",
		Hostname:   "test-vm",
		SSHKey:     "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@host",
	}, isoPath)
	if err != nil {
		t.Fatalf("GenerateCloudInitISO() error: %v", err)
	}

	info, err := os.Stat(isoPath)
	if err != nil {
		t.Fatalf("ISO file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("ISO file is empty")
	}
}

func TestGenerateCloudInitISO_NoSSHKey(t *testing.T) {
	if !isoToolAvailable() {
		t.Skip("skipping: neither genisoimage nor mkisofs available")
	}

	tmpDir := t.TempDir()
	isoPath := filepath.Join(tmpDir, "cloud-init.iso")

	err := GenerateCloudInitISO(&CloudInitConfig{
		InstanceID: "test-instance-002",
		Hostname:   "no-key-vm",
	}, isoPath)
	if err != nil {
		t.Fatalf("GenerateCloudInitISO() error: %v", err)
	}

	info, err := os.Stat(isoPath)
	if err != nil {
		t.Fatalf("ISO file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("ISO file is empty")
	}
}

func TestCloudInitMetaDataContent(t *testing.T) {
	if !isoToolAvailable() {
		t.Skip("skipping: neither genisoimage nor mkisofs available")
	}

	tmpDir := t.TempDir()
	isoPath := filepath.Join(tmpDir, "cloud-init.iso")

	config := &CloudInitConfig{
		InstanceID: "meta-test-uuid",
		Hostname:   "meta-host",
		SSHKey:     "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@host",
	}

	if err := GenerateCloudInitISO(config, isoPath); err != nil {
		t.Fatalf("GenerateCloudInitISO() error: %v", err)
	}

	// Mount the ISO and read meta-data to verify content.
	mountDir := filepath.Join(tmpDir, "mount")
	if err := os.MkdirAll(mountDir, 0755); err != nil {
		t.Fatalf("failed to create mount dir: %v", err)
	}

	// Use isoinfo to extract meta-data content (doesn't require root).
	if _, err := exec.LookPath("isoinfo"); err != nil {
		t.Skip("skipping content verification: isoinfo not available")
	}

	out, err := exec.Command("isoinfo", "-i", isoPath, "-R", "-x", "/meta-data").CombinedOutput()
	if err != nil {
		t.Fatalf("isoinfo failed: %v\n%s", err, out)
	}

	metaData := string(out)
	if !strings.Contains(metaData, "instance-id: meta-test-uuid") {
		t.Errorf("meta-data missing instance-id, got:\n%s", metaData)
	}
	if !strings.Contains(metaData, "local-hostname: meta-host") {
		t.Errorf("meta-data missing local-hostname, got:\n%s", metaData)
	}
}

func TestCloudInitUserDataContent(t *testing.T) {
	if !isoToolAvailable() {
		t.Skip("skipping: neither genisoimage nor mkisofs available")
	}

	tmpDir := t.TempDir()
	isoPath := filepath.Join(tmpDir, "cloud-init.iso")

	sshKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@host"
	config := &CloudInitConfig{
		InstanceID: "user-test-uuid",
		Hostname:   "user-host",
		SSHKey:     sshKey,
	}

	if err := GenerateCloudInitISO(config, isoPath); err != nil {
		t.Fatalf("GenerateCloudInitISO() error: %v", err)
	}

	// Use isoinfo to extract user-data content (doesn't require root).
	if _, err := exec.LookPath("isoinfo"); err != nil {
		t.Skip("skipping content verification: isoinfo not available")
	}

	out, err := exec.Command("isoinfo", "-i", isoPath, "-R", "-x", "/user-data").CombinedOutput()
	if err != nil {
		t.Fatalf("isoinfo failed: %v\n%s", err, out)
	}

	userData := string(out)
	if !strings.HasPrefix(userData, "#cloud-config") {
		t.Errorf("user-data should start with #cloud-config, got:\n%s", userData)
	}
	if !strings.Contains(userData, "hostname: user-host") {
		t.Errorf("user-data missing hostname, got:\n%s", userData)
	}
	if !strings.Contains(userData, "users:") {
		t.Errorf("user-data missing users directive, got:\n%s", userData)
	}
	if !strings.Contains(userData, "- default") {
		t.Errorf("user-data missing default user entry, got:\n%s", userData)
	}
	if !strings.Contains(userData, "name: goship") {
		t.Errorf("user-data missing goship user, got:\n%s", userData)
	}
	if !strings.Contains(userData, "sudo: ALL=(ALL) NOPASSWD:ALL") {
		t.Errorf("user-data missing sudo directive, got:\n%s", userData)
	}
	if !strings.Contains(userData, "lock_passwd: false") {
		t.Errorf("user-data missing lock_passwd directive, got:\n%s", userData)
	}
	if !strings.Contains(userData, sshKey) {
		t.Errorf("user-data missing SSH key, got:\n%s", userData)
	}
	// Top-level ssh_authorized_keys must exist for the default user fallback.
	if !strings.Contains(userData, "ssh_authorized_keys:\n  - "+sshKey) {
		t.Errorf("user-data missing top-level ssh_authorized_keys, got:\n%s", userData)
	}
}

func TestFindISOTool(t *testing.T) {
	tool, err := findISOTool()
	if err != nil {
		t.Skip("skipping: no ISO tool available")
	}
	if tool == "" {
		t.Fatal("findISOTool returned empty path")
	}
}
