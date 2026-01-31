package commands

import "github.com/spf13/cobra"

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

// SetVersion sets the version information from ldflags.
func SetVersion(v, c, b string) {
	version = v
	commit = c
	buildTime = b
}

// rootCmd is the base command.
var rootCmd = &cobra.Command{
	Use:   "goshipctl",
	Short: "GoShip CLI - Self-hosted application platform",
	Long: `GoShip is a self-hosted, project-based application platform.

Each project runs in its own VM, providing strong isolation.
Apps are deployed inside project VMs.`,
	SilenceUsage: true,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(capabilitiesCmd)
}
