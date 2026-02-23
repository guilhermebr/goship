package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/guilhermebr/goship/internal/agent/runtime"
	"github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
	"github.com/guilhermebr/goship/internal/shared/state"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

var (
	// Global flags
	dataDir            string
	verbose            bool
	initBinaryPath     string
	skipGuestProvision bool
	installDocker      bool

	// Shared resources (initialized lazily by PersistentPreRunE)
	store *state.Store
	rt    *libvirt.Runtime
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
	PersistentPreRunE:  initResources,
	PersistentPostRunE: cleanupResources,
	SilenceUsage:       true,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "~/.goship", "Data directory for GoShip")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", true, "Enable verbose output")
	rootCmd.PersistentFlags().
		StringVar(&initBinaryPath, "goship-init", "~/.goship/bin/goship-init", "Path to goship-init binary")
	rootCmd.PersistentFlags().
		BoolVar(&skipGuestProvision, "skip-guest-provision", false, "Skip guest disk provisioning")
	rootCmd.PersistentFlags().
		BoolVar(&installDocker, "install-docker", true, "Install Docker during guest provisioning")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(capabilitiesCmd)
	rootCmd.AddCommand(generateXMLCmd)
	rootCmd.AddCommand(vmCmd)
	rootCmd.AddCommand(imageCmd)
	rootCmd.AddCommand(projectCmd)
	rootCmd.AddCommand(appCmd)
	rootCmd.AddCommand(composeCmd)
}

// needsStore returns true if the command requires the state store.
func needsStore(cmd *cobra.Command) bool {
	parent := ""
	if cmd.Parent() != nil {
		parent = cmd.Parent().Name()
	}
	return parent == "project" || parent == "app" || parent == "compose"
}

// needsRuntime returns true if the command requires the libvirt runtime.
func needsRuntime(cmd *cobra.Command) bool {
	parent := ""
	if cmd.Parent() != nil {
		parent = cmd.Parent().Name()
	}
	name := cmd.Name()

	// App subcommands that talk to the VM need the runtime.
	if parent == "app" {
		switch name {
		case "deploy", "stop", "delete", "logs", "push-image":
			return true
		}
	}

	// Project subcommands that need the runtime for VM operations.
	if parent == "project" {
		switch name {
		case "create", "delete", "edit", "stop", "start", "restart", "update-init":
			return true
		}
	}

	// Compose subcommands that need the runtime for VM operations.
	if parent == "compose" {
		switch name {
		case "up", "down":
			return true
		}
	}

	return false
}

// initResources initializes shared resources based on command needs.
func initResources(cmd *cobra.Command, args []string) error {
	if !needsStore(cmd) {
		return nil
	}

	var err error
	store, err = state.NewStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to initialize state store: %w", err)
	}

	if !needsRuntime(cmd) {
		return nil
	}

	opts := []runtime.RuntimeOption{
		runtime.WithDataDir(dataDir),
		runtime.WithVMImage(dataDir + "/images/goship-vm.qcow2"),
		runtime.WithInitBinary(initBinaryPath),
		runtime.WithProvisionGuest(!skipGuestProvision),
		runtime.WithInstallDocker(installDocker),
		runtime.WithProgressWriter(os.Stdout),
	}
	if projectNetworkType != "" {
		source := projectNetworkSource
		if projectNetworkType == "network" && source == "" {
			source = "default"
		}
		opts = append(opts, runtime.WithNetwork(projectNetworkType, source))
	}

	rt, err = libvirt.New(opts...)
	if err != nil {
		return fmt.Errorf("failed to initialize libvirt runtime: %w", err)
	}

	return nil
}

// cleanupResources cleans up shared resources.
func cleanupResources(cmd *cobra.Command, args []string) error {
	if rt != nil {
		return rt.Close()
	}
	return nil
}

// printVerbose prints a message if verbose mode is enabled.
func printVerbose(format string, args ...any) {
	if verbose {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

// printError prints an error message to stderr.
func printError(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
}
