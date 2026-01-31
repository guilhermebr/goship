package libvirt

import (
	"testing"
)

const sampleCapabilitiesXML = `<capabilities>
  <host>
    <uuid>44454c4c-3200-1042-8033-b6c04f525631</uuid>
    <cpu>
      <arch>x86_64</arch>
      <model>Skylake-Server-IBRS</model>
      <vendor>Intel</vendor>
      <topology sockets='1' cores='8' threads='2'/>
    </cpu>
  </host>
</capabilities>`

const minimalCapabilitiesXML = `<capabilities>
  <host>
    <uuid>00000000-0000-0000-0000-000000000000</uuid>
    <cpu>
      <arch>aarch64</arch>
      <model>cortex-a72</model>
      <vendor>ARM</vendor>
    </cpu>
  </host>
</capabilities>`

func TestParseCapabilitiesXML(t *testing.T) {
	caps, err := parseCapabilitiesXML(sampleCapabilitiesXML)
	if err != nil {
		t.Fatalf("parseCapabilitiesXML failed: %v", err)
	}

	if caps.Host.CPU.Arch != "x86_64" {
		t.Errorf("expected arch x86_64, got %s", caps.Host.CPU.Arch)
	}
	if caps.Host.CPU.Model != "Skylake-Server-IBRS" {
		t.Errorf("expected model Skylake-Server-IBRS, got %s", caps.Host.CPU.Model)
	}
	if caps.Host.CPU.Vendor != "Intel" {
		t.Errorf("expected vendor Intel, got %s", caps.Host.CPU.Vendor)
	}
	if caps.Host.CPU.Topology.Sockets != 1 {
		t.Errorf("expected 1 socket, got %d", caps.Host.CPU.Topology.Sockets)
	}
	if caps.Host.CPU.Topology.Cores != 8 {
		t.Errorf("expected 8 cores, got %d", caps.Host.CPU.Topology.Cores)
	}
	if caps.Host.CPU.Topology.Threads != 2 {
		t.Errorf("expected 2 threads, got %d", caps.Host.CPU.Topology.Threads)
	}
	if caps.Host.UUID != "44454c4c-3200-1042-8033-b6c04f525631" {
		t.Errorf("unexpected UUID: %s", caps.Host.UUID)
	}
}

func TestParseCapabilitiesXML_Minimal(t *testing.T) {
	caps, err := parseCapabilitiesXML(minimalCapabilitiesXML)
	if err != nil {
		t.Fatalf("parseCapabilitiesXML failed: %v", err)
	}

	if caps.Host.CPU.Arch != "aarch64" {
		t.Errorf("expected arch aarch64, got %s", caps.Host.CPU.Arch)
	}
	if caps.Host.CPU.Model != "cortex-a72" {
		t.Errorf("expected model cortex-a72, got %s", caps.Host.CPU.Model)
	}
	if caps.Host.CPU.Vendor != "ARM" {
		t.Errorf("expected vendor ARM, got %s", caps.Host.CPU.Vendor)
	}
	// No topology element — defaults to zero values
	if caps.Host.CPU.Topology.Sockets != 0 {
		t.Errorf("expected 0 sockets (missing topology), got %d", caps.Host.CPU.Topology.Sockets)
	}
	if caps.Host.CPU.Topology.Cores != 0 {
		t.Errorf("expected 0 cores (missing topology), got %d", caps.Host.CPU.Topology.Cores)
	}
}

func TestParseCapabilitiesXML_InvalidXML(t *testing.T) {
	_, err := parseCapabilitiesXML("not xml at all")
	if err == nil {
		t.Fatal("expected error for invalid XML, got nil")
	}
}

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		input    uint32
		expected string
	}{
		{10003002, "10.3.2"},
		{9000000, "9.0.0"},
		{8006000, "8.6.0"},
		{0, "0.0.0"},
		{1001001, "1.1.1"},
	}

	for _, tc := range tests {
		result := formatVersion(tc.input)
		if result != tc.expected {
			t.Errorf("formatVersion(%d) = %s, want %s", tc.input, result, tc.expected)
		}
	}
}

func TestCheckKVMAvailable(t *testing.T) {
	// This test just verifies the function runs without panic.
	// The result depends on the host environment.
	_ = checkKVMAvailable()
}
