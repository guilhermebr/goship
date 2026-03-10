package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"libvirt.org/go/libvirt"

	lvrt "github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

const (
	statusYes = "yes"
	statusNo  = "no"
)

// capabilitiesCmd prints host virtualization capabilities.
var capabilitiesCmd = &cobra.Command{
	Use:   "capabilities",
	Short: "Show host virtualization capabilities",
	Long: `Connects to libvirt, discovers host capabilities (CPU, memory, KVM, hugepages, confidential computing),
and prints a human-readable summary.`,
	RunE: runCapabilities,
}

func runCapabilities(cmd *cobra.Command, args []string) error {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("cannot connect to libvirt: %w", err)
	}
	defer func() { _, _ = conn.Close() }()

	caps, err := lvrt.DiscoverCapabilities(conn)
	if err != nil {
		return fmt.Errorf("cannot discover capabilities: %w", err)
	}

	fmt.Print(formatCapabilities(caps))
	return nil
}

// formatCapabilities formats HostCapabilities.
func formatCapabilities(caps *entities.HostCapabilities) string {
	var b strings.Builder

	b.WriteString("Host Capabilities\n")
	b.WriteString("=================\n\n")

	// Hypervisor
	fmt.Fprintf(&b, "Hypervisor:    %s\n", caps.Hypervisor)
	fmt.Fprintf(&b, "Architecture:  %s\n", caps.Arch)

	kvmStatus := statusYes
	if !caps.KVMAvailable {
		kvmStatus = statusNo
	}
	fmt.Fprintf(&b, "KVM:           %s\n", kvmStatus)

	cpuMode := "host-passthrough"
	if !caps.KVMAvailable {
		cpuMode = "host-model (KVM not available)"
	}
	fmt.Fprintf(&b, "CPU Mode:      %s\n", cpuMode)

	// CPU
	fmt.Fprintf(&b, "\nCPU Model:     %s\n", caps.CPUModel)
	fmt.Fprintf(&b, "CPU Vendor:    %s\n", caps.CPUVendor)
	fmt.Fprintf(&b, "Topology:      %d socket(s), %d core(s), %d thread(s) [%d vCPUs]\n",
		caps.CPUTopology.Sockets,
		caps.CPUTopology.Cores,
		caps.CPUTopology.Threads,
		caps.CPUTopology.TotalVCPUs())

	// Memory
	fmt.Fprintf(&b, "\nMemory:        %d MB (%.1f GB)\n",
		caps.TotalMemoryMB,
		float64(caps.TotalMemoryMB)/1024.0)

	// Hugepages
	if caps.HugepagesAvailable {
		sizeStrs := make([]string, len(caps.HugepageSizes))
		for i, s := range caps.HugepageSizes {
			sizeStrs[i] = fmt.Sprintf("%dkB", s)
		}
		fmt.Fprintf(&b, "Hugepages:     yes (%s)\n", strings.Join(sizeStrs, ", "))
	} else {
		b.WriteString("Hugepages:     no\n")
	}

	// Confidential computing
	if len(caps.ConfidentialComputing) > 0 {
		for _, cc := range caps.ConfidentialComputing {
			fmt.Fprintf(&b, "Confidential:  %s (available)\n", cc.Type)
		}
	} else {
		b.WriteString("Confidential:  none detected\n")
	}

	// Versions
	fmt.Fprintf(&b, "\nLibvirt:       %s\n", caps.LibvirtVersion)
	fmt.Fprintf(&b, "QEMU:          %s\n", caps.QEMUVersion)

	return b.String()
}
