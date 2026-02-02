package commands

import (
	"os"
	"path/filepath"
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

	// Build a fresh command to avoid shared state with other tests.
	cmd := &cobra.Command{Use: "pull", RunE: runImagePull}
	cmd.Flags().String("output", imagePath, "")
	cmd.Flags().Bool("force", false, "")

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when image already exists, got nil")
	}

	want := "image already exists"
	if got := err.Error(); !contains(got, want) {
		t.Errorf("error = %q, want it to contain %q", got, want)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
