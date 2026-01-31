package libvirt

import (
	"strings"
	"testing"

	"github.com/guilhermebr/goship/pkg/domain/entities"
)

func TestGenerateDomainXML_BasicKVM(t *testing.T) {
	config := &DomainConfig{
		Name:      "test-vm",
		UUID:      "12345678-1234-1234-1234-123456789abc",
		MemoryMB:  512,
		EnableKVM: true,
		CPU: entities.CPUTopology{
			Sockets: 1,
			Cores:   2,
			Threads: 1,
		},
		Disks: []entities.DiskDevice{
			{
				Path:   "/var/lib/goship/test.qcow2",
				Format: "qcow2",
				Bus:    "virtio",
			},
		},
		Networks: []entities.NetworkDevice{
			{
				Type:   "network",
				Source: "default",
				Model:  "virtio",
			},
		},
	}

	xml, err := GenerateDomainXML(config)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	checks := []string{
		"<domain type='kvm'>",
		"<name>test-vm</name>",
		"<uuid>12345678-1234-1234-1234-123456789abc</uuid>",
		"<memory unit='MiB'>512</memory>",
		"<vcpu placement='static'>2</vcpu>",
		"sockets='1' cores='2' threads='1'",
		"<target dev='vda' bus='virtio'/>",
		"<source network='default'/>",
		"<model type='virtio'/>",
		"<type arch='x86_64' machine='q35'>hvm</type>",
		"<cpu mode='host-passthrough'>",
	}

	for _, check := range checks {
		if !strings.Contains(xml, check) {
			t.Errorf("expected XML to contain %q", check)
		}
	}
}

func TestGenerateDomainXML_QEMUMode(t *testing.T) {
	config := &DomainConfig{
		Name:      "qemu-vm",
		UUID:      "00000000-0000-0000-0000-000000000000",
		MemoryMB:  256,
		EnableKVM: false,
		CPU: entities.CPUTopology{
			Sockets: 1,
			Cores:   1,
			Threads: 1,
		},
		Disks: []entities.DiskDevice{
			{
				Path:   "/tmp/disk.qcow2",
				Format: "qcow2",
				Bus:    "virtio",
			},
		},
	}

	xml, err := GenerateDomainXML(config)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	if !strings.Contains(xml, "<domain type='qemu'>") {
		t.Error("expected domain type 'qemu' when EnableKVM is false")
	}
	if strings.Contains(xml, "<domain type='kvm'>") {
		t.Error("unexpected domain type 'kvm' when EnableKVM is false")
	}
}

func TestGenerateDomainXML_MultipleDisks(t *testing.T) {
	config := &DomainConfig{
		Name:      "multi-disk-vm",
		UUID:      "11111111-1111-1111-1111-111111111111",
		MemoryMB:  1024,
		EnableKVM: true,
		CPU: entities.CPUTopology{
			Sockets: 1,
			Cores:   1,
			Threads: 1,
		},
		Disks: []entities.DiskDevice{
			{Path: "/disk0.qcow2", Format: "qcow2", Bus: "virtio"},
			{Path: "/disk1.qcow2", Format: "qcow2", Bus: "virtio"},
			{Path: "/disk2.raw", Format: "raw", Bus: "virtio"},
		},
	}

	xml, err := GenerateDomainXML(config)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	if !strings.Contains(xml, "<target dev='vda' bus='virtio'/>") {
		t.Error("expected disk vda")
	}
	if !strings.Contains(xml, "<target dev='vdb' bus='virtio'/>") {
		t.Error("expected disk vdb")
	}
	if !strings.Contains(xml, "<target dev='vdc' bus='virtio'/>") {
		t.Error("expected disk vdc")
	}
}

func TestGenerateDomainXML_WithMemoryBacking(t *testing.T) {
	config := &DomainConfig{
		Name:      "hugepages-vm",
		UUID:      "22222222-2222-2222-2222-222222222222",
		MemoryMB:  2048,
		EnableKVM: true,
		CPU: entities.CPUTopology{
			Sockets: 1,
			Cores:   2,
			Threads: 2,
		},
		Disks: []entities.DiskDevice{
			{Path: "/disk.qcow2", Format: "qcow2", Bus: "virtio"},
		},
		MemoryBacking: &entities.MemoryBacking{
			Hugepages: true,
			Locked:    true,
		},
	}

	xml, err := GenerateDomainXML(config)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	if !strings.Contains(xml, "<memoryBacking>") {
		t.Error("expected memoryBacking element")
	}
	if !strings.Contains(xml, "<hugepages/>") {
		t.Error("expected hugepages element")
	}
	if !strings.Contains(xml, "<locked/>") {
		t.Error("expected locked element")
	}
}

func TestGenerateDomainXML_WithSerial(t *testing.T) {
	config := &DomainConfig{
		Name:      "serial-vm",
		UUID:      "33333333-3333-3333-3333-333333333333",
		MemoryMB:  512,
		EnableKVM: true,
		CPU: entities.CPUTopology{
			Sockets: 1,
			Cores:   1,
			Threads: 1,
		},
		Disks: []entities.DiskDevice{
			{Path: "/disk.qcow2", Format: "qcow2", Bus: "virtio"},
		},
		Serials: []entities.SerialDevice{
			{
				Type:       "virtio-serial",
				SocketPath: "/var/lib/goship/instance1/goship.sock",
				PortName:   "goship.0",
			},
		},
	}

	xml, err := GenerateDomainXML(config)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	if !strings.Contains(xml, "<channel type='unix'>") {
		t.Error("expected channel element for serial device")
	}
	if !strings.Contains(xml, "path='/var/lib/goship/instance1/goship.sock'") {
		t.Error("expected socket path in channel")
	}
	if !strings.Contains(xml, "name='goship.0'") {
		t.Error("expected port name in channel")
	}
}

func TestGenerateDomainXML_BridgeNetwork(t *testing.T) {
	config := &DomainConfig{
		Name:      "bridge-vm",
		UUID:      "44444444-4444-4444-4444-444444444444",
		MemoryMB:  512,
		EnableKVM: true,
		CPU: entities.CPUTopology{
			Sockets: 1,
			Cores:   1,
			Threads: 1,
		},
		Disks: []entities.DiskDevice{
			{Path: "/disk.qcow2", Format: "qcow2", Bus: "virtio"},
		},
		Networks: []entities.NetworkDevice{
			{
				Type:   "bridge",
				Source: "br0",
				Model:  "virtio",
			},
		},
	}

	xml, err := GenerateDomainXML(config)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	if !strings.Contains(xml, "<interface type='bridge'>") {
		t.Error("expected bridge interface")
	}
	if !strings.Contains(xml, "<source bridge='br0'/>") {
		t.Error("expected bridge source br0")
	}
}

func TestGenerateDomainXML_WithCDROM(t *testing.T) {
	config := &DomainConfig{
		Name:      "cdrom-vm",
		UUID:      "55555555-5555-5555-5555-555555555555",
		MemoryMB:  512,
		EnableKVM: true,
		CPU: entities.CPUTopology{
			Sockets: 1,
			Cores:   1,
			Threads: 1,
		},
		Disks: []entities.DiskDevice{
			{Path: "/disk.qcow2", Format: "qcow2", Bus: "virtio"},
		},
		CDROMs: []CDROMDevice{
			{Path: "/cloud-init.iso", Format: "raw"},
		},
	}

	xml, err := GenerateDomainXML(config)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	if !strings.Contains(xml, "device='cdrom'") {
		t.Error("expected cdrom device")
	}
	if !strings.Contains(xml, "<source file='/cloud-init.iso'/>") {
		t.Error("expected cdrom source path")
	}
	if !strings.Contains(xml, "<readonly/>") {
		t.Error("expected readonly on cdrom")
	}
	if !strings.Contains(xml, "bus='sata'") {
		t.Error("expected sata bus for cdrom")
	}
}

func TestDiskLetter(t *testing.T) {
	tests := []struct {
		index    int
		expected string
	}{
		{0, "a"},
		{1, "b"},
		{2, "c"},
		{25, "z"},
	}

	for _, tc := range tests {
		result := diskLetter(tc.index)
		if result != tc.expected {
			t.Errorf("diskLetter(%d) = %s, want %s", tc.index, result, tc.expected)
		}
	}
}
