// Package libvirt implements VM runtime operations using libvirt.
package libvirt

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"time"

	"libvirt.org/go/libvirt"

	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// libvirtCapabilities represents parsed libvirt capabilities XML.
type libvirtCapabilities struct {
	XMLName xml.Name `xml:"capabilities"`
	Host    struct {
		UUID string `xml:"uuid"`
		CPU  struct {
			Arch     string `xml:"arch"`
			Model    string `xml:"model"`
			Vendor   string `xml:"vendor"`
			Topology struct {
				Sockets int `xml:"sockets,attr"`
				Cores   int `xml:"cores,attr"`
				Threads int `xml:"threads,attr"`
			} `xml:"topology"`
		} `xml:"cpu"`
	} `xml:"host"`
}

// DiscoverCapabilities queries libvirt for host capabilities.
func DiscoverCapabilities(conn *libvirt.Connect) (*entities.HostCapabilities, error) {
	// Get capabilities XML
	capsXML, err := conn.GetCapabilities()
	if err != nil {
		return nil, fmt.Errorf("failed to get capabilities: %w", err)
	}

	// Parse XML
	var caps libvirtCapabilities
	if err := xml.Unmarshal([]byte(capsXML), &caps); err != nil {
		return nil, fmt.Errorf("failed to parse capabilities: %w", err)
	}

	// Check KVM availability
	kvmAvailable := checkKVMAvailable()

	// Check hugepages
	hugepagesAvailable, hugepageSizes := checkHugepages()

	// Detect confidential computing capabilities
	ccCaps := detectConfidentialComputing()

	info, err := GetVersionInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get version info: %w", err)
	}

	// Get total memory
	nodeInfo, err := conn.GetNodeInfo()
	totalMemoryMB := int64(0)
	if err == nil {
		totalMemoryMB = int64(nodeInfo.Memory) / 1024 // KB to MB
	}

	// Build CPU topology
	cpuTopology := entities.CPUTopology{
		Sockets: caps.Host.CPU.Topology.Sockets,
		Cores:   caps.Host.CPU.Topology.Cores,
		Threads: caps.Host.CPU.Topology.Threads,
	}
	// Ensure non-zero defaults
	if cpuTopology.Sockets == 0 {
		cpuTopology.Sockets = 1
	}
	if cpuTopology.Cores == 0 {
		cpuTopology.Cores = 1
	}
	if cpuTopology.Threads == 0 {
		cpuTopology.Threads = 1
	}

	return &entities.HostCapabilities{
		Hypervisor:            "kvm",
		Arch:                  caps.Host.CPU.Arch,
		KVMAvailable:          kvmAvailable,
		CPUModel:              caps.Host.CPU.Model,
		CPUVendor:             caps.Host.CPU.Vendor,
		CPUTopology:           cpuTopology,
		TotalMemoryMB:         totalMemoryMB,
		HugepagesAvailable:    hugepagesAvailable,
		HugepageSizes:         hugepageSizes,
		ConfidentialComputing: ccCaps,
		LibvirtVersion:        info.LibvirtVersion,
		QEMUVersion:           info.QEMUVersion,
		CollectedAt:           time.Now(),
	}, nil
}

// parseCapabilitiesXML parses libvirt capabilities XML into the internal struct.
// Exported for testing.
func parseCapabilitiesXML(xmlData string) (*libvirtCapabilities, error) {
	var caps libvirtCapabilities
	if err := xml.Unmarshal([]byte(xmlData), &caps); err != nil {
		return nil, fmt.Errorf("failed to parse capabilities: %w", err)
	}
	return &caps, nil
}

// checkKVMAvailable checks if /dev/kvm is accessible.
func checkKVMAvailable() bool {
	_, err := os.Stat("/dev/kvm")
	return err == nil
}

// checkHugepages checks for available hugepages and their sizes.
func checkHugepages() (bool, []int64) {
	entries, err := os.ReadDir("/sys/kernel/mm/hugepages")
	if err != nil {
		return false, nil
	}

	var sizes []int64
	for _, entry := range entries {
		// Parse hugepages-2048kB, hugepages-1048576kB, etc.
		name := entry.Name()
		if strings.HasPrefix(name, "hugepages-") && strings.HasSuffix(name, "kB") {
			sizeStr := strings.TrimPrefix(name, "hugepages-")
			sizeStr = strings.TrimSuffix(sizeStr, "kB")
			var size int64
			fmt.Sscanf(sizeStr, "%d", &size)
			if size > 0 {
				sizes = append(sizes, size)
			}
		}
	}

	return len(sizes) > 0, sizes
}

// detectConfidentialComputing detects available confidential computing capabilities.
func detectConfidentialComputing() []entities.ConfidentialComputingCapability {
	var caps []entities.ConfidentialComputingCapability

	// Check AMD SEV
	if sevInfo := checkSEV(); sevInfo != nil {
		caps = append(caps, *sevInfo)
	}

	// Check Intel TDX
	if tdxInfo := checkTDX(); tdxInfo != nil {
		caps = append(caps, *tdxInfo)
	}

	return caps
}

// checkSEV checks for AMD SEV support.
func checkSEV() *entities.ConfidentialComputingCapability {
	data, err := os.ReadFile("/sys/module/kvm_amd/parameters/sev")
	if err != nil {
		return nil
	}

	enabled := strings.TrimSpace(string(data))
	if enabled != "Y" && enabled != "1" {
		return nil
	}

	sevType := entities.CCTypeSEV

	// Check for SEV-ES
	if data, err := os.ReadFile("/sys/module/kvm_amd/parameters/sev_es"); err == nil {
		if strings.TrimSpace(string(data)) == "Y" || strings.TrimSpace(string(data)) == "1" {
			sevType = entities.CCTypeSEVES
		}
	}

	// Check for SEV-SNP (highest level, overrides ES)
	if data, err := os.ReadFile("/sys/module/kvm_amd/parameters/sev_snp"); err == nil {
		if strings.TrimSpace(string(data)) == "Y" || strings.TrimSpace(string(data)) == "1" {
			sevType = entities.CCTypeSEVSNP
		}
	}

	return &entities.ConfidentialComputingCapability{
		Type:      sevType,
		Available: true,
	}
}

// checkTDX checks for Intel TDX support.
func checkTDX() *entities.ConfidentialComputingCapability {
	data, err := os.ReadFile("/sys/module/kvm_intel/parameters/tdx")
	if err != nil {
		return nil
	}

	enabled := strings.TrimSpace(string(data))
	if enabled != "Y" && enabled != "1" {
		return nil
	}

	return &entities.ConfidentialComputingCapability{
		Type:      entities.CCTypeTDX,
		Available: true,
	}
}
