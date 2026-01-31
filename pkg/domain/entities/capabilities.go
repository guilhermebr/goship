// Package entities defines the core domain types for GoShip.
// These types are shared between the CLI, Agent, and Control Plane.
package entities

import "time"

// =============================================================================
// Confidential Computing Types
// =============================================================================

// ConfidentialComputingType defines the type of confidential computing.
type ConfidentialComputingType string

const (
	// CCTypeNone indicates no confidential computing.
	CCTypeNone ConfidentialComputingType = "none"
	// CCTypeSEV indicates AMD SEV (Secure Encrypted Virtualization).
	CCTypeSEV ConfidentialComputingType = "sev"
	// CCTypeSEVES indicates AMD SEV-ES (SEV with Encrypted State).
	CCTypeSEVES ConfidentialComputingType = "sev-es"
	// CCTypeSEVSNP indicates AMD SEV-SNP (SEV with Secure Nested Paging).
	CCTypeSEVSNP ConfidentialComputingType = "sev-snp"
	// CCTypeTDX indicates Intel TDX (Trust Domain Extensions).
	CCTypeTDX ConfidentialComputingType = "tdx"
)

// ConfidentialComputingCapability describes CC capabilities.
type ConfidentialComputingCapability struct {
	// Type of confidential computing
	Type ConfidentialComputingType `json:"type"`
	// Available indicates if this type is available
	Available bool `json:"available"`
	// MaxGuests is the maximum number of guests (for SEV)
	MaxGuests int `json:"max_guests,omitempty"`
	// MinASID is the minimum ASID value (for SEV)
	MinASID int `json:"min_asid,omitempty"`
}

// =============================================================================
// CPU Topology Types
// =============================================================================

// CPUTopology defines the CPU topology for a VM.
type CPUTopology struct {
	// Sockets is the number of CPU sockets
	Sockets int `json:"sockets,omitempty"`
	// Cores is the number of cores per socket
	Cores int `json:"cores,omitempty"`
	// Threads is the number of threads per core (hyperthreading)
	Threads int `json:"threads,omitempty"`
	// Model is the CPU model (host-passthrough, host-model, or specific model)
	Model string `json:"model,omitempty"`
	// Features are additional CPU feature flags
	Features []string `json:"features,omitempty"`
}

// TotalVCPUs returns the total number of virtual CPUs.
func (t CPUTopology) TotalVCPUs() int {
	sockets := t.Sockets
	if sockets == 0 {
		sockets = 1
	}
	cores := t.Cores
	if cores == 0 {
		cores = 1
	}
	threads := t.Threads
	if threads == 0 {
		threads = 1
	}
	return sockets * cores * threads
}

// =============================================================================
// Host Capabilities Types
// =============================================================================

// HostCapabilities describes the host's virtualization capabilities.
type HostCapabilities struct {
	// Hypervisor is the hypervisor type (kvm, qemu)
	Hypervisor string `json:"hypervisor"`
	// Arch is the host architecture (x86_64, aarch64)
	Arch string `json:"arch"`
	// KVMAvailable indicates if /dev/kvm is accessible
	KVMAvailable bool `json:"kvm_available"`
	// CPUModel is the host CPU model
	CPUModel string `json:"cpu_model"`
	// CPUVendor is the CPU vendor (Intel, AMD, etc.)
	CPUVendor string `json:"cpu_vendor"`
	// CPUTopology is the host CPU topology
	CPUTopology CPUTopology `json:"cpu_topology"`
	// TotalMemoryMB is the total host memory in MB
	TotalMemoryMB int64 `json:"total_memory_mb"`
	// HugepagesAvailable indicates if hugepages are available
	HugepagesAvailable bool `json:"hugepages_available"`
	// HugepageSizes lists available hugepage sizes in KB
	HugepageSizes []int64 `json:"hugepage_sizes,omitempty"`
	// ConfidentialComputing lists available CC capabilities
	ConfidentialComputing []ConfidentialComputingCapability `json:"confidential_computing,omitempty"`
	// LibvirtVersion is the libvirt version
	LibvirtVersion string `json:"libvirt_version"`
	// QEMUVersion is the QEMU version
	QEMUVersion string `json:"qemu_version"`
	// CollectedAt is when capabilities were collected
	CollectedAt time.Time `json:"collected_at"`
}
