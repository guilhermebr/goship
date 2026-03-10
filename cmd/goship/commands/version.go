package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	lvrt "github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
)

// versionCmd prints version information and libvirt/QEMU details.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version and libvirt information",
	Run:   runVersion,
}

func runVersion(cmd *cobra.Command, args []string) {
	fmt.Printf("goship %s\n", version)
	fmt.Printf("  commit: %s\n", commit)
	fmt.Printf("  built:  %s\n", buildTime)

	info, err := lvrt.GetVersionInfo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nLibvirt: unavailable (%v)\n", err)
		return
	}

	fmt.Printf("\nLibvirt version: %s\n", info.LibvirtVersion)
	fmt.Printf("QEMU version:    %s\n", info.QEMUVersion)
}
