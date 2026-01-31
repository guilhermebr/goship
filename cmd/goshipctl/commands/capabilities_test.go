package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/goship/pkg/domain/entities"
)

func TestFormatCapabilities_Full(t *testing.T) {
	caps := &entities.HostCapabilities{
		Hypervisor:   "kvm",
		Arch:         "x86_64",
		KVMAvailable: true,
		CPUModel:     "Skylake-Server-IBRS",
		CPUVendor:    "Intel",
		CPUTopology: entities.CPUTopology{
			Sockets: 1,
			Cores:   8,
			Threads: 2,
		},
		TotalMemoryMB:      32768,
		HugepagesAvailable: true,
		HugepageSizes:      []int64{2048, 1048576},
		ConfidentialComputing: []entities.ConfidentialComputingCapability{
			{Type: entities.CCTypeSEV, Available: true},
		},
		LibvirtVersion: "10.3.0",
		QEMUVersion:    "9.0.2",
		CollectedAt:    time.Now(),
	}

	output := formatCapabilities(caps)

	expected := []string{
		"Host Capabilities",
		"Hypervisor:    kvm",
		"Architecture:  x86_64",
		"KVM:           yes",
		"CPU Model:     Skylake-Server-IBRS",
		"CPU Vendor:    Intel",
		"1 socket(s), 8 core(s), 2 thread(s) [16 vCPUs]",
		"Memory:        32768 MB (32.0 GB)",
		"Hugepages:     yes (2048kB, 1048576kB)",
		"sev (available)",
		"Libvirt:       10.3.0",
		"QEMU:          9.0.2",
	}

	for _, s := range expected {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\ngot:\n%s", s, output)
		}
	}
}

func TestFormatCapabilities_Minimal(t *testing.T) {
	caps := &entities.HostCapabilities{
		Hypervisor:            "kvm",
		Arch:                  "aarch64",
		KVMAvailable:          false,
		CPUModel:              "cortex-a72",
		CPUVendor:             "ARM",
		CPUTopology:           entities.CPUTopology{Sockets: 1, Cores: 4, Threads: 1},
		TotalMemoryMB:         4096,
		HugepagesAvailable:    false,
		ConfidentialComputing: nil,
		LibvirtVersion:        "8.0.0",
		QEMUVersion:           "unknown",
		CollectedAt:           time.Now(),
	}

	output := formatCapabilities(caps)

	expected := []string{
		"KVM:           no",
		"cortex-a72",
		"1 socket(s), 4 core(s), 1 thread(s) [4 vCPUs]",
		"Memory:        4096 MB (4.0 GB)",
		"Hugepages:     no",
		"Confidential:  none detected",
		"QEMU:          unknown",
	}

	for _, s := range expected {
		if !strings.Contains(output, s) {
			t.Errorf("output missing %q\ngot:\n%s", s, output)
		}
	}

	// Ensure no "available" CC line appears
	if strings.Contains(output, "(available)") {
		t.Errorf("should not contain CC available line, got:\n%s", output)
	}
}
