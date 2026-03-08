package commands

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ardanlabs/conf/v3"
	"github.com/spf13/cobra"

	"github.com/guilhermebr/goship/internal/agent/runtime"
	"github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
	"github.com/guilhermebr/goship/internal/client"
	"github.com/guilhermebr/goship/internal/shared/state"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

// cfg holds parsed configuration (env vars → struct defaults, then CLI flags override).
var cfg Config

var (
	// Shared resources (initialized lazily by PersistentPreRunE)
	store     *state.Store
	rt        *libvirt.Runtime
	apiClient *client.Client
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
	// Parse environment variables into cfg (ignores unknown flags at this stage).
	// Errors are intentionally swallowed — CLI flags below provide final defaults.
	_, _ = conf.Parse("GOSHIP", &cfg)

	// Register CLI flags bound to cfg fields. The current cfg value (from env
	// or struct default) becomes the Cobra default so env vars take effect.
	rootCmd.PersistentFlags().
		StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "Data directory for GoShip (env: GOSHIP_DATA_DIR)")
	rootCmd.PersistentFlags().
		BoolVarP(&cfg.Verbose, "verbose", "v", cfg.Verbose, "Enable verbose output (env: GOSHIP_VERBOSE)")
	rootCmd.PersistentFlags().
		StringVar(&cfg.InitBinaryPath, "goship-init", cfg.InitBinaryPath,
			"Path to goship-init binary (env: GOSHIP_INIT_BINARY_PATH)")
	rootCmd.PersistentFlags().
		BoolVar(&cfg.SkipGuestProvision, "skip-guest-provision", cfg.SkipGuestProvision,
			"Skip guest disk provisioning (env: GOSHIP_SKIP_GUEST_PROVISION)")
	rootCmd.PersistentFlags().
		BoolVar(&cfg.InstallDocker, "install-docker", cfg.InstallDocker,
			"Install Docker during guest provisioning (env: GOSHIP_INSTALL_DOCKER)")
	rootCmd.PersistentFlags().
		StringVar(&cfg.ApiUrl, "api-url", cfg.ApiUrl,
			"GoShip API server URL (env: GOSHIP_API_URL)")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(capabilitiesCmd)
	rootCmd.AddCommand(generateXMLCmd)
	rootCmd.AddCommand(vmCmd)
	rootCmd.AddCommand(imageCmd)
	rootCmd.AddCommand(projectCmd)
	rootCmd.AddCommand(appCmd)
	rootCmd.AddCommand(composeCmd)
	rootCmd.AddCommand(envCmd)
}

const (
	cmdProject = "project"
	cmdApp     = "app"
	cmdCompose = "compose"
	cmdEnv     = "env"
)

// needsStore returns true if the command requires the state store.
func needsStore(cmd *cobra.Command) bool {
	parent := ""
	if cmd.Parent() != nil {
		parent = cmd.Parent().Name()
	}
	return parent == cmdProject || parent == cmdApp || parent == cmdCompose || parent == cmdEnv
}

// needsRuntime returns true if the command requires the libvirt runtime.
func needsRuntime(cmd *cobra.Command) bool {
	parent := ""
	if cmd.Parent() != nil {
		parent = cmd.Parent().Name()
	}
	name := cmd.Name()

	// App subcommands that talk to the VM need the runtime.
	if parent == cmdApp {
		switch name {
		case "deploy", "stop", "delete", "logs", "push-image":
			return true
		}
	}

	// Project subcommands that need the runtime for VM operations.
	if parent == cmdProject {
		switch name {
		case "create", "delete", "edit", "stop", "start", "restart", "update-init":
			return true
		}
	}

	// Compose subcommands that need the runtime for VM operations.
	if parent == cmdCompose {
		switch name {
		case "up", "down":
			return true
		}
	}

	return false
}

// needsRuntimeOptional returns true if the command benefits from the runtime
// for state reconciliation but should not fail without it (e.g. libvirt down).
func needsRuntimeOptional(cmd *cobra.Command) bool {
	parent := ""
	if cmd.Parent() != nil {
		parent = cmd.Parent().Name()
	}
	name := cmd.Name()

	if parent == cmdProject {
		switch name {
		case "list", "info":
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

	// API mode: use HTTP client instead of local store/runtime.
	if cfg.ApiUrl != "" {
		apiClient = client.New(cfg.ApiUrl)
		return nil
	}

	var err error
	store, err = state.NewStore(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("failed to initialize state store: %w", err)
	}

	wantRuntime := needsRuntime(cmd)
	wantOptional := needsRuntimeOptional(cmd)

	if !wantRuntime && !wantOptional {
		return nil
	}

	opts := []runtime.RuntimeOption{
		runtime.WithDataDir(cfg.DataDir),
		runtime.WithVMImage(cfg.DataDir + "/images/goship-vm.qcow2"),
		runtime.WithInitBinary(cfg.InitBinaryPath),
		runtime.WithProvisionGuest(!cfg.SkipGuestProvision),
		runtime.WithInstallDocker(cfg.InstallDocker),
		runtime.WithProgressWriter(os.Stdout),
	}

	// Resolve network: per-project flags take precedence, then global config.
	netType := projectNetworkType
	if netType == "" {
		netType = cfg.NetworkType
	}
	netSource := projectNetworkSource
	if netSource == "" {
		netSource = cfg.NetworkSource
	}

	if netType != "" {
		if netType == "network" && netSource == "" {
			netSource = "default"
		}
		opts = append(opts, runtime.WithNetwork(netType, netSource))
	}

	if cfg.LibvirtURI != "" {
		opts = append(opts, runtime.WithLibvirtURI(cfg.LibvirtURI))
	}

	rt, err = libvirt.New(opts...)
	if err != nil {
		if wantRuntime {
			return fmt.Errorf("failed to initialize libvirt runtime: %w", err)
		}
		// Optional: swallow error, commands will show store data as-is.
		rt = nil
		return nil
	}

	reconcileState()

	return nil
}

// reconcileState synchronizes the state store with actual libvirt domain states.
// It checks all projects with active instances and updates their state if libvirt
// reports a different status (e.g. VM was stopped externally via virsh destroy).
func reconcileState() {
	if store == nil || rt == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	projects := store.ListProjects()
	for _, project := range projects {
		instance := store.GetInstance(project.ID)
		if instance == nil {
			continue
		}

		// Only reconcile instances in active states that might have drifted.
		switch instance.State {
		case entities.InstanceStateRunning, entities.InstanceStateStarting, entities.InstanceStateStopping:
		default:
			continue
		}

		oldState := instance.State
		rt.LoadInstance(instance)

		status, err := rt.GetInstanceStatus(ctx, instance.ID)
		if err != nil {
			continue
		}

		if status.State == oldState {
			continue
		}

		// Instance state drifted — update both instance and project.
		instance.State = status.State
		_ = store.UpdateInstance(instance)

		newProjectState := mapInstanceToProjectState(status.State)
		if newProjectState != project.State {
			project.State = newProjectState
			_ = store.UpdateProject(project)
		}
	}
}

// mapInstanceToProjectState derives the project state from an instance state.
func mapInstanceToProjectState(is entities.InstanceState) entities.ProjectState {
	switch is {
	case entities.InstanceStateRunning:
		return entities.ProjectStateRunning
	case entities.InstanceStateStopped:
		return entities.ProjectStateStopped
	case entities.InstanceStateStopping:
		return entities.ProjectStateStopping
	case entities.InstanceStateFailed:
		return entities.ProjectStateFailed
	default:
		return entities.ProjectStatePending
	}
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
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

// printError prints an error message to stderr.
func printError(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
}
