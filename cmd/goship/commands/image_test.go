package commands

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{10485760, "10.00 MB"},
		{1073741824, "1.00 GB"},
		{2684354560, "2.50 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatBytes(tt.input)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestImagePull_AlreadyExists(t *testing.T) {
	// Create a temp directory with a fake image file.
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "goship-vm.qcow2")

	if err := os.WriteFile(imagePath, []byte("fake-image"), 0o644); err != nil {
		t.Fatalf("creating fake image: %v", err)
	}

	// Build a fresh command with all required flags.
	cmd := &cobra.Command{Use: "pull", RunE: runImagePull}
	cmd.Flags().String("output", imagePath, "")
	cmd.Flags().Bool("force", false, "")

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when image already exists, got nil")
	}

	want := "image already exists"
	if got := err.Error(); !strings.Contains(got, want) {
		t.Errorf("error = %q, want it to contain %q", got, want)
	}
}

func TestImageBuild_MissingInitBinary(t *testing.T) {
	if imageBuildCmd.Flags().Lookup("goship-init") != nil {
		t.Fatal("image build should not expose --goship-init flag anymore")
	}
}

func TestOpenRCServiceScript(t *testing.T) {
	if strings.Contains(defaultImageURL, "github.com/guilhermebr/goship/releases") {
		t.Fatal("default image URL should point to Alpine NoCloud source")
	}
}

func TestCheckExistingVMs_NoVMs(t *testing.T) {
	// When no ~/.goship/vms/ directory exists, checkExistingVMs should
	// return an empty list (no error, just no VMs found).
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "goship-vm.qcow2")

	// Create a fake image file so the path is valid.
	if err := os.WriteFile(imagePath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	vms := checkExistingVMs(imagePath)
	if len(vms) != 0 {
		t.Errorf("expected no VMs, got %v", vms)
	}
}

func TestCheckExistingVMs_WithBackedVM(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img not available, skipping")
	}

	dir := t.TempDir()

	// Create a fake base image (real qcow2 so qemu-img info works).
	basePath := filepath.Join(dir, "goship-vm.qcow2")
	out, err := exec.Command("qemu-img", "create", "-f", "qcow2", basePath, "64M").CombinedOutput()
	if err != nil {
		t.Fatalf("creating base image: %v\n%s", err, out)
	}

	// Create a VM directory with a CoW disk backed by the base image.
	vmDir := filepath.Join(dir, "vms", "test-vm")
	if mkdirErr := os.MkdirAll(vmDir, 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}

	diskPath := filepath.Join(vmDir, "disk.qcow2")
	absBase, _ := filepath.Abs(basePath)
	out, err = exec.Command("qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", absBase, diskPath).
		CombinedOutput()
	if err != nil {
		t.Fatalf("creating CoW disk: %v\n%s", err, out)
	}

	// checkExistingVMs scans ~/.goship/vms, but we need it to scan our temp dir.
	// We can verify getBackingFile works correctly instead, and that the
	// matching logic is correct by testing the pieces.

	// Verify getBackingFile returns the correct backing file.
	backing := getBackingFile(diskPath)
	if backing != absBase {
		t.Errorf("getBackingFile() = %q, want %q", backing, absBase)
	}
}

func TestGetBackingFile_NoBacking(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img not available, skipping")
	}

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "standalone.qcow2")

	out, err := exec.Command("qemu-img", "create", "-f", "qcow2", imgPath, "64M").CombinedOutput()
	if err != nil {
		t.Fatalf("creating image: %v\n%s", err, out)
	}

	backing := getBackingFile(imgPath)
	if backing != "" {
		t.Errorf("expected empty backing file, got %q", backing)
	}
}

func TestGetBackingFile_InvalidPath(t *testing.T) {
	backing := getBackingFile("/nonexistent/path/disk.qcow2")
	if backing != "" {
		t.Errorf("expected empty backing file for nonexistent path, got %q", backing)
	}
}

func TestQemuImgInfoJSON(t *testing.T) {
	// Test that our JSON struct correctly parses qemu-img info output.
	sample := `{"backing-filename": "/path/to/base.qcow2", "format": "qcow2"}`
	var info qemuImgInfo
	if err := json.Unmarshal([]byte(sample), &info); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if info.BackingFilename != "/path/to/base.qcow2" {
		t.Errorf("BackingFilename = %q, want %q", info.BackingFilename, "/path/to/base.qcow2")
	}

	// Test with no backing file.
	sample2 := `{"format": "qcow2"}`
	var info2 qemuImgInfo
	if err := json.Unmarshal([]byte(sample2), &info2); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if info2.BackingFilename != "" {
		t.Errorf("BackingFilename = %q, want empty", info2.BackingFilename)
	}
}
