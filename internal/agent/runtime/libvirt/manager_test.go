package libvirt

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestDomainName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"myvm", "goship-myvm"},
		{"test-vm", "goship-test-vm"},
		{"", "goship-"},
	}

	for _, tc := range tests {
		result := domainName(tc.input)
		if result != tc.expected {
			t.Errorf("domainName(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestUserFacingName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"goship-myvm", "myvm"},
		{"goship-test-vm", "test-vm"},
		{"goship-", ""},
		{"other-vm", "other-vm"}, // no prefix to strip
	}

	for _, tc := range tests {
		result := userFacingName(tc.input)
		if result != tc.expected {
			t.Errorf("userFacingName(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestVmDir(t *testing.T) {
	result := vmDir("/home/user/.goship", "myvm")
	expected := filepath.Join("/home/user/.goship", "vms", "myvm")
	if result != expected {
		t.Errorf("vmDir() = %q, want %q", result, expected)
	}
}

func TestDiskPath(t *testing.T) {
	result := diskPath("/home/user/.goship", "myvm")
	expected := filepath.Join("/home/user/.goship", "vms", "myvm", "disk.qcow2")
	if result != expected {
		t.Errorf("diskPath() = %q, want %q", result, expected)
	}
}

func TestGenerateUUID(t *testing.T) {
	uuid1, err := GenerateUUID()
	if err != nil {
		t.Fatalf("GenerateUUID failed: %v", err)
	}

	uuid2, err := GenerateUUID()
	if err != nil {
		t.Fatalf("GenerateUUID failed: %v", err)
	}

	// Verify v4 UUID format: 8-4-4-4-12 hex chars
	uuidPattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidPattern.MatchString(uuid1) {
		t.Errorf("UUID %q does not match v4 format", uuid1)
	}
	if !uuidPattern.MatchString(uuid2) {
		t.Errorf("UUID %q does not match v4 format", uuid2)
	}

	// Verify uniqueness
	if uuid1 == uuid2 {
		t.Errorf("two generated UUIDs should be different, got %s both times", uuid1)
	}
}

func TestEnsurePathTraversable(t *testing.T) {
	// Create a temp dir hierarchy with a restricted parent.
	root := t.TempDir()
	restricted := filepath.Join(root, "restricted")
	inner := filepath.Join(restricted, "inner", "deep")

	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	// Remove o+x from the restricted dir.
	if err := os.Chmod(restricted, 0o700); err != nil {
		t.Fatal(err)
	}

	// Verify it's not traversable.
	info, _ := os.Stat(restricted)
	if info.Mode().Perm()&0o001 != 0 {
		t.Fatal("expected restricted dir to not have o+x")
	}

	// Fix it.
	if err := ensurePathTraversable(inner); err != nil {
		t.Fatalf("ensurePathTraversable failed: %v", err)
	}

	// Verify o+x was added.
	info, _ = os.Stat(restricted)
	if info.Mode().Perm()&0o001 == 0 {
		t.Error("expected restricted dir to have o+x after ensurePathTraversable")
	}
}
