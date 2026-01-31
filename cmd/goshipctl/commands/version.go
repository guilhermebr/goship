package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"libvirt.org/go/libvirt"
)

// versionCmd prints version information and libvirt/QEMU details.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version and libvirt information",
	Run:   runVersion,
}

func runVersion(cmd *cobra.Command, args []string) {
	fmt.Printf("goshipctl %s\n", version)
	fmt.Printf("  commit: %s\n", commit)
	fmt.Printf("  built:  %s\n", buildTime)

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nLibvirt: unavailable (%v)\n", err)
		return
	}
	defer conn.Close()

	// GetLibVersion returns the libvirt library version (the client library itself).
	libVer, err := conn.GetLibVersion()
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nLibvirt version: error (%v)\n", err)
		return
	}
	fmt.Printf("\nLibvirt version: %s\n", formatVersion(libVer))

	// GetVersion returns the version of the hypervisor running behind this connection.
	// Since we connect to "qemu:///system", the hypervisor is QEMU, so this returns
	// the QEMU version. For a Xen connection it would return the Xen version, etc.
	hvVer, err := conn.GetVersion()
	if err != nil {
		fmt.Fprintf(os.Stderr, "QEMU version:    error (%v)\n", err)
		return
	}
	fmt.Printf("QEMU version:    %s\n", formatVersion(hvVer))
}

// formatVersion converts a libvirt encoded version (major*1000000 + minor*1000 + patch)
// into a "major.minor.patch" string.
func formatVersion(v uint32) string {
	major := v / 1000000
	minor := (v % 1000000) / 1000
	patch := v % 1000
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}
