package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"libvirt.org/go/libvirt"

	"github.com/guilhermebr/goship/pkg/domain/entities"

	libvirtRuntime "github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
)

// capabilitiesCmd prints host virtualization capabilities.
var capabilitiesCmd = &cobra.Command{
	Use:   "capabilities",
	Short: "Show host virtualization capabilities",
	Long:  `Connects to libvirt, discovers host capabilities (CPU, memory, KVM, hugepages, confidential computing), and prints a human-readable summary.`,
	Run:   runCapabilities,
}

func runCapabilities(cmd *cobra.Command, args []string) {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot connect to libvirt: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	caps, err := libvirtRuntime.DiscoverCapabilities(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot discover capabilities: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(formatCapabilities(caps))
}

// formatCapabilities formats HostCapabilities.
func formatCapabilities(caps *entities.HostCapabilities) string {
	var b strings.Builder

	b.WriteString("Host Capabilities\n")
	b.WriteString("=================\n\n")

	// Hypervisor
	b.WriteString(fmt.Sprintf("Hypervisor:    %s\n", caps.Hypervisor))
	b.WriteString(fmt.Sprintf("Architecture:  %s\n", caps.Arch))

	kvmStatus := "yes"
	if !caps.KVMAvailable {
		kvmStatus = "no"
	}
	b.WriteString(fmt.Sprintf("KVM:           %s\n", kvmStatus))

	// CPU
	b.WriteString(fmt.Sprintf("\nCPU Model:     %s\n", caps.CPUModel))
	b.WriteString(fmt.Sprintf("CPU Vendor:    %s\n", caps.CPUVendor))
	b.WriteString(fmt.Sprintf("Topology:      %d socket(s), %d core(s), %d thread(s) [%d vCPUs]\n",
		caps.CPUTopology.Sockets,
		caps.CPUTopology.Cores,
		caps.CPUTopology.Threads,
		caps.CPUTopology.TotalVCPUs()))

	// Memory
	b.WriteString(fmt.Sprintf("\nMemory:        %d MB (%.1f GB)\n",
		caps.TotalMemoryMB,
		float64(caps.TotalMemoryMB)/1024.0))

	// Hugepages
	if caps.HugepagesAvailable {
		sizeStrs := make([]string, len(caps.HugepageSizes))
		for i, s := range caps.HugepageSizes {
			sizeStrs[i] = fmt.Sprintf("%dkB", s)
		}
		b.WriteString(fmt.Sprintf("Hugepages:     yes (%s)\n", strings.Join(sizeStrs, ", ")))
	} else {
		b.WriteString("Hugepages:     no\n")
	}

	// Confidential computing
	if len(caps.ConfidentialComputing) > 0 {
		for _, cc := range caps.ConfidentialComputing {
			b.WriteString(fmt.Sprintf("Confidential:  %s (available)\n", cc.Type))
		}
	} else {
		b.WriteString("Confidential:  none detected\n")
	}

	// Versions
	b.WriteString(fmt.Sprintf("\nLibvirt:       %s\n", caps.LibvirtVersion))
	b.WriteString(fmt.Sprintf("QEMU:          %s\n", caps.QEMUVersion))

	return b.String()
}
