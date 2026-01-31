package commands

import (
	"regexp"
	"strings"
	"testing"
)

func TestGenerateXML_DefaultConfig(t *testing.T) {
	cmd := generateXMLCmd
	// Reset flags to defaults
	cmd.Flags().Set("name", "goship-vm")
	cmd.Flags().Set("memory", "512")
	cmd.Flags().Set("cpus", "1")
	cmd.Flags().Set("enable-kvm", "true")
	cmd.Flags().Set("disk-path", "/var/lib/goship/disk.qcow2")
	cmd.Flags().Set("disk-format", "qcow2")
	cmd.Flags().Set("network-type", "network")
	cmd.Flags().Set("network-source", "default")

	// Execute via root command to capture output
	buf := new(strings.Builder)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"generate-xml", "--name", "goship-vm"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("generate-xml command failed: %v", err)
	}
}

func TestGenerateXML_CustomConfig(t *testing.T) {
	rootCmd.SetArgs([]string{
		"generate-xml",
		"--name", "custom-vm",
		"--memory", "2048",
		"--cpus", "4",
		"--enable-kvm=false",
		"--disk-path", "/custom/disk.raw",
		"--disk-format", "raw",
		"--network-type", "bridge",
		"--network-source", "br0",
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("generate-xml command with custom flags failed: %v", err)
	}
}

func TestGenerateUUID(t *testing.T) {
	uuid1, err := generateUUID()
	if err != nil {
		t.Fatalf("generateUUID failed: %v", err)
	}

	uuid2, err := generateUUID()
	if err != nil {
		t.Fatalf("generateUUID failed: %v", err)
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
