package commands

import (
	"strings"
	"testing"
)

func TestGenerateXML_DefaultConfig(t *testing.T) {
	cmd := generateXMLCmd
	// Reset flags to defaults
	_ = cmd.Flags().Set("name", "goship-vm")
	_ = cmd.Flags().Set("memory", "512")
	_ = cmd.Flags().Set("cpus", "1")
	_ = cmd.Flags().Set("enable-kvm", "true")
	_ = cmd.Flags().Set("disk-path", "/var/lib/goship/disk.qcow2")
	_ = cmd.Flags().Set("disk-format", "qcow2")
	_ = cmd.Flags().Set("network-type", "network")
	_ = cmd.Flags().Set("network-source", "default")

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
