package commands

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	lvrt "github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
	"github.com/guilhermebr/goship/internal/shared/size"
	"github.com/guilhermebr/goship/internal/vault"
	v1 "github.com/guilhermebr/goship/pkg/api/v1"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage projects",
	Long:  `Create, list, and delete projects. Each project runs in its own isolated VM.`,
}

// Flags for project create.
var (
	projectCPU           float64
	projectMemory        string
	projectDisk          string
	projectNetworkType   string
	projectNetworkSource string
)

var projectCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new project",
	Long:  `Create a new project with its own isolated VM.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectCreate,
}

var projectListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all projects",
	Aliases: []string{"ls"},
	RunE:    runProjectList,
}

var projectDeleteCmd = &cobra.Command{
	Use:     "delete <name>",
	Short:   "Delete a project and its VM",
	Aliases: []string{"rm"},
	Args:    cobra.ExactArgs(1),
	RunE:    runProjectDelete,
}

var projectInfoCmd = &cobra.Command{
	Use:   "info <name>",
	Short: "Show project details",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectInfo,
}

var projectConsoleCmd = &cobra.Command{
	Use:   "console <name>",
	Short: "Attach to the VM console",
	Long:  `Opens an interactive console to the project VM via virsh.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectConsole,
}

// Flags for project logs.
var (
	logsLines  int
	logsFollow bool
	logsFile   string
)

// logAliases maps well-known names to log file paths inside the VM.
var logAliases = map[string]string{
	"cloud-init":  "/var/log/cloud-init-output.log",
	"goship-init": "/var/log/goship-init.log",
}

var projectLogsCmd = &cobra.Command{
	Use:   "logs <name> [source]",
	Short: "Show logs from the VM",
	Long: `Show logs from the project VM.

Log sources (positional argument):
  goship-init   /var/log/goship-init.log (default)
  cloud-init    /var/log/cloud-init-output.log

Use --file to read an arbitrary log path under /var/log/.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runProjectLogs,
}

var projectStopCmd = &cobra.Command{
	Use:   "stop <name>",
	Short: "Stop a project VM (graceful ACPI shutdown)",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectStop,
}

var projectStartCmd = &cobra.Command{
	Use:   "start <name>",
	Short: "Start a stopped project VM",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectStart,
}

var projectRestartCmd = &cobra.Command{
	Use:   "restart <name>",
	Short: "Restart a project VM (stop then start)",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectRestart,
}

// Flags for project edit.
var (
	projectEditCPU    float64
	projectEditMemory string
	projectEditDisk   string
)

var projectEditCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Edit project VM resources (CPU, memory, disk)",
	Long: `Edit a project's VM resource allocation. The VM must be stopped before editing.
Disk size can only be increased, not decreased.`,
	Args: cobra.ExactArgs(1),
	RunE: runProjectEdit,
}

// Flags for project update-init.
var (
	updateInitBinary  string
	updateInitRestart bool
)

var projectUpdateInitCmd = &cobra.Command{
	Use:   "update-init <name>",
	Short: "Push a new goship-init binary into a running VM",
	Long:  `Transfers a new goship-init binary to the VM over virtio-serial using a chunked transfer protocol.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectUpdateInit,
}

func init() {
	projectCreateCmd.Flags().Float64Var(&projectCPU, "cpu", 1, "Number of CPU cores")
	projectCreateCmd.Flags().StringVar(&projectMemory, "memory", "512M", "Memory (e.g., 512M, 8G)")
	projectCreateCmd.Flags().StringVar(&projectDisk, "disk", "8G", "Disk size (e.g., 4G, 8192M)")
	projectCreateCmd.Flags().StringVar(&projectNetworkType, "network-type", "", "Network type (network, bridge, user)")
	projectCreateCmd.Flags().
		StringVar(&projectNetworkSource, "network-source", "", "Network source (network name or bridge name)")

	projectLogsCmd.Flags().IntVarP(&logsLines, "lines", "n", 100, "Number of log lines to show")
	projectLogsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output (poll every 2s)")
	projectLogsCmd.Flags().
		StringVar(&logsFile, "file", "", "Arbitrary log file path inside the VM (must be under /var/log/)")

	projectEditCmd.Flags().Float64Var(&projectEditCPU, "cpu", 0, "Number of CPU cores")
	projectEditCmd.Flags().StringVar(&projectEditMemory, "memory", "", "Memory (e.g., 512M, 8G)")
	projectEditCmd.Flags().StringVar(&projectEditDisk, "disk", "", "Disk size (e.g., 4G, 8192M; can only grow)")

	projectUpdateInitCmd.Flags().
		StringVar(&updateInitBinary, "binary", "", "Path to goship-init binary (default: --goship-init flag value)")
	projectUpdateInitCmd.Flags().BoolVar(&updateInitRestart, "restart", false, "Restart the VM after successful update")

	projectCmd.AddCommand(projectCreateCmd)
	projectCmd.AddCommand(projectListCmd)
	projectCmd.AddCommand(projectDeleteCmd)
	projectCmd.AddCommand(projectInfoCmd)
	projectCmd.AddCommand(projectConsoleCmd)
	projectCmd.AddCommand(projectLogsCmd)
	projectCmd.AddCommand(projectEditCmd)
	projectCmd.AddCommand(projectStopCmd)
	projectCmd.AddCommand(projectStartCmd)
	projectCmd.AddCommand(projectRestartCmd)
	projectCmd.AddCommand(projectUpdateInitCmd)
}

func runProjectCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	printVerbose("Creating project: %s", name)

	memoryMB, err := size.ParseSizeMB(projectMemory)
	if err != nil {
		return fmt.Errorf("invalid --memory value: %w", err)
	}

	diskMB, err := size.ParseSizeMB(projectDisk)
	if err != nil {
		return fmt.Errorf("invalid --disk value: %w", err)
	}

	resources := entities.Resources{
		CPU:      projectCPU,
		MemoryMB: memoryMB,
		DiskMB:   diskMB,
	}

	// Create project in state store.
	project, err := store.CreateProject(name, entities.RuntimeQEMU, resources)
	if err != nil {
		return fmt.Errorf("failed to create project: %w", err)
	}

	printVerbose("Project created with ID: %s", project.ID)

	// Create VM instance via runtime.
	instance, err := rt.CreateInstance(ctx, project)
	if err != nil {
		// Cleanup on failure.
		_ = store.DeleteProject(project.ID)
		return fmt.Errorf("failed to create VM: %w", err)
	}

	// Update project state to running.
	project.State = entities.ProjectStateRunning
	if err := store.UpdateProject(project); err != nil {
		printError("failed to update project state: %v", err)
	}

	// Persist instance.
	if err := store.SetInstance(instance); err != nil {
		printError("failed to save instance: %v", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Project '%s' created successfully\n", name)
	fmt.Fprintf(cmd.OutOrStdout(), "  ID:       %s\n", project.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  VM State: %s\n", instance.State)
	if instance.IPAddress != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  IP:       %s\n", instance.IPAddress)
	}

	return nil
}

func runProjectList(cmd *cobra.Command, args []string) error {
	projects := store.ListProjects()

	if len(projects) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No projects found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tID\tSTATE\tRUNTIME\tCPU\tMEMORY\tCREATED")

	const shortIDLen = 8
	for _, p := range projects {
		shortID := p.ID
		if len(shortID) > shortIDLen {
			shortID = shortID[:shortIDLen]
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%.0f\t%dMB\t%s\n",
			p.Name,
			shortID,
			p.State,
			p.Runtime,
			p.Resources.CPU,
			p.Resources.MemoryMB,
			p.CreatedAt.Format("2006-01-02 15:04"),
		)
	}

	return w.Flush()
}

func runProjectDelete(cmd *cobra.Command, args []string) error {
	name := args[0]
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	project, err := store.GetProject(name)
	if err != nil {
		return fmt.Errorf("project not found: %s", name)
	}

	printVerbose("Deleting project: %s", project.Name)

	// Destroy the VM instance if one exists.
	instance := store.GetInstance(project.ID)
	if instance != nil {
		printVerbose("Destroying VM: %s", instance.ID)

		// Load persisted instance into runtime so DestroyInstance can find it.
		rt.LoadInstance(instance)

		if err := rt.DestroyInstance(ctx, instance.ID); err != nil {
			printError("failed to destroy VM: %v", err)
		}

		if err := store.DeleteInstance(instance.ID); err != nil {
			printError("failed to delete instance from state: %v", err)
		}
	}

	// Delete project from state.
	if err := store.DeleteProject(project.ID); err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Project '%s' deleted successfully\n", name)
	return nil
}

func runProjectInfo(cmd *cobra.Command, args []string) error {
	name := args[0]

	project, err := store.GetProject(name)
	if err != nil {
		return fmt.Errorf("project not found: %s", name)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Project: %s\n", project.Name)
	fmt.Fprintf(out, "  ID:       %s\n", project.ID)
	fmt.Fprintf(out, "  State:    %s\n", project.State)
	fmt.Fprintf(out, "  Runtime:  %s\n", project.Runtime)
	fmt.Fprintf(out, "  CPU:      %.0f cores\n", project.Resources.CPU)
	fmt.Fprintf(out, "  Memory:   %d MB\n", project.Resources.MemoryMB)
	fmt.Fprintf(out, "  Disk:     %d MB\n", project.Resources.DiskMB)
	fmt.Fprintf(out, "  Created:  %s\n", project.CreatedAt.Format(time.RFC3339))

	// Show environment variables.
	if len(project.Env) > 0 {
		fmt.Fprint(out, "\nEnvironment:\n")
		for _, k := range slices.Sorted(maps.Keys(project.Env)) {
			v := project.Env[k]
			if vault.IsEncrypted(v) {
				v = maskedValue
			}
			fmt.Fprintf(out, "  %s=%s\n", k, v)
		}
	}

	// Show instance info if available.
	instance := store.GetInstance(project.ID)
	if instance != nil {
		fmt.Fprint(out, "\nVM Instance:\n")
		fmt.Fprintf(out, "  ID:       %s\n", instance.ID)
		fmt.Fprintf(out, "  State:    %s\n", instance.State)
		if instance.IPAddress != "" {
			fmt.Fprintf(out, "  IP:       %s\n", instance.IPAddress)
		}
		if instance.DomainName != "" {
			fmt.Fprintf(out, "  Domain:   %s\n", instance.DomainName)
		}
	}

	// Show apps if any.
	apps := store.GetApps(project.ID)
	if len(apps) > 0 {
		fmt.Fprint(out, "\nApps:\n")
		for _, app := range apps {
			if app.IsContainerMode() {
				fmt.Fprintf(out, "  - %s (image: %s)\n", app.Name, app.Image)
			} else {
				fmt.Fprintf(out, "  - %s (binary: %s)\n", app.Name, app.Binary)
			}
		}
	}

	return nil
}

func runProjectConsole(cmd *cobra.Command, args []string) error {
	name := args[0]

	project, err := store.GetProject(name)
	if err != nil {
		return fmt.Errorf("project not found: %s", name)
	}

	instance := store.GetInstance(project.ID)
	if instance == nil {
		return fmt.Errorf("no VM instance found for project %s", name)
	}

	if instance.DomainName == "" {
		return fmt.Errorf("no domain name for instance %s", instance.ID)
	}

	virshPath, err := exec.LookPath("virsh")
	if err != nil {
		return fmt.Errorf("virsh not found in PATH: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Connecting to console of %s...\n", instance.DomainName)
	fmt.Fprint(cmd.OutOrStdout(), "Use Ctrl+] to exit the console.\n\n")

	// Replace current process with virsh console for full TTY control.
	return syscall.Exec(virshPath, []string{"virsh", "console", instance.DomainName}, os.Environ())
}

//nolint:funlen // CLI handler with streaming logs and polling logic
func runProjectLogs(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Resolve log file: --file flag > positional alias > default (empty = agent default).
	logFile := logsFile
	if logFile == "" && len(args) > 1 {
		alias := args[1]
		path, ok := logAliases[alias]
		if !ok {
			return fmt.Errorf("unknown log source %q (known: cloud-init, goship-init)", alias)
		}
		logFile = path
	}

	project, err := store.GetProject(name)
	if err != nil {
		return fmt.Errorf("project not found: %s", name)
	}

	instance := store.GetInstance(project.ID)
	if instance == nil {
		return fmt.Errorf("no VM instance found for project %s", name)
	}

	// Derive the socket path from the domain name.
	vmName := strings.TrimPrefix(instance.DomainName, lvrt.DomainPrefix)
	socketPath := filepath.Join(expandDataDir(dataDir), "vms", vmName, "goship.sock")

	fetchLogs := func() (string, error) {
		comm, commErr := lvrt.NewVMCommunicator(socketPath)
		if commErr != nil {
			return "", fmt.Errorf("failed to connect to VM: %w", commErr)
		}
		defer func() { _ = comm.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, cmdErr := comm.SendCommand(ctx, &v1.InitCommand{
			Action:  v1.ActionLogs,
			Lines:   logsLines,
			LogFile: logFile,
		})
		if cmdErr != nil {
			return "", fmt.Errorf("failed to get logs: %w", cmdErr)
		}
		if resp.Status != v1.StatusOK {
			return "", fmt.Errorf("agent error: %s", resp.Error)
		}
		return resp.Logs, nil
	}

	logs, err := fetchLogs()
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), logs)

	if logsFollow {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		lastLogs := logs
		for range ticker.C {
			logs, err := fetchLogs()
			if err != nil {
				printError("failed to fetch logs: %v", err)
				continue
			}
			// Only print new content.
			if logs != lastLogs {
				fmt.Fprintln(cmd.OutOrStdout(), logs)
				lastLogs = logs
			}
		}
	}

	return nil
}

// expandDataDir expands ~ in the data directory path.
func expandDataDir(dir string) string {
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, dir[2:])
		}
	}
	return dir
}

//nolint:funlen // CLI handler with complex flag parsing and VM reconfiguration
func runProjectEdit(cmd *cobra.Command, args []string) error {
	name := args[0]
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	project, err := store.GetProject(name)
	if err != nil {
		return fmt.Errorf("project not found: %s", name)
	}

	instance := store.GetInstance(project.ID)
	if instance == nil {
		return fmt.Errorf("no VM instance found for project %s", name)
	}

	// Reconcile state from libvirt in case persisted state is stale.
	if rt != nil {
		rt.LoadInstance(instance)
		if status, err := rt.GetInstanceStatus(ctx, instance.ID); err == nil {
			if instance.State != status.State {
				instance.State = status.State
				_ = store.UpdateInstance(instance)
			}
		}
	}

	// Check VM is stopped.
	if instance.State != entities.InstanceStateStopped {
		return fmt.Errorf(
			"VM must be stopped before editing (current state: %s); run 'goshipctl project stop %s' first",
			instance.State,
			name,
		)
	}

	var changes []string

	if cmd.Flags().Changed("cpu") {
		project.Resources.CPU = projectEditCPU
		changes = append(changes, fmt.Sprintf("  CPU: %.0f cores", projectEditCPU))
	}

	if cmd.Flags().Changed("memory") {
		memMB, err := size.ParseSizeMB(projectEditMemory)
		if err != nil {
			return fmt.Errorf("invalid --memory value: %w", err)
		}
		project.Resources.MemoryMB = memMB
		changes = append(changes, fmt.Sprintf("  Memory: %d MB", memMB))
	}

	if cmd.Flags().Changed("disk") {
		diskMB, err := size.ParseSizeMB(projectEditDisk)
		if err != nil {
			return fmt.Errorf("invalid --disk value: %w", err)
		}
		if diskMB < project.Resources.DiskMB {
			return fmt.Errorf(
				"disk size can only grow: current %d MB, requested %d MB",
				project.Resources.DiskMB,
				diskMB,
			)
		}
		project.Resources.DiskMB = diskMB
		changes = append(changes, fmt.Sprintf("  Disk: %d MB", diskMB))
	}

	if len(changes) == 0 {
		return errors.New("no changes specified; use --cpu, --memory, or --disk flags (see --help)")
	}

	// Load the instance into the runtime and apply changes.
	rt.LoadInstance(instance)

	if err := rt.ResizeInstance(ctx, instance.ID, project); err != nil {
		return fmt.Errorf("failed to resize VM: %w", err)
	}

	// Persist updated project.
	if err := store.UpdateProject(project); err != nil {
		return fmt.Errorf("failed to save project: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Project '%s' updated:\n", name)
	for _, c := range changes {
		fmt.Fprintln(out, c)
	}
	fmt.Fprintf(out, "\nStart the VM to apply changes: goshipctl project start %s\n", name)

	return nil
}

func runProjectStop(cmd *cobra.Command, args []string) error {
	name := args[0]
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	project, err := store.GetProject(name)
	if err != nil {
		return fmt.Errorf("project not found: %s", name)
	}

	instance := store.GetInstance(project.ID)
	if instance == nil {
		return fmt.Errorf("no VM instance found for project %s", name)
	}

	rt.LoadInstance(instance)

	printVerbose("Stopping VM for project %s...", name)

	if err := rt.StopInstance(ctx, instance.ID); err != nil {
		return fmt.Errorf("failed to stop VM: %w", err)
	}

	instance.State = entities.InstanceStateStopping
	if err := store.UpdateInstance(instance); err != nil {
		printError("failed to update instance state: %v", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Project '%s' VM stopping (ACPI shutdown sent)\n", name)

	// Wait for the domain to reach shut off state.
	waitCtx, waitCancel := context.WithTimeout(ctx, 60*time.Second)
	defer waitCancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			printError("timed out waiting for VM to stop; state may still be 'stopping'")
			return nil
		case <-ticker.C:
			status, err := rt.GetInstanceStatus(waitCtx, instance.ID)
			if err != nil {
				continue
			}
			if status.State == entities.InstanceStateStopped {
				instance.State = entities.InstanceStateStopped
				if err := store.UpdateInstance(instance); err != nil {
					printError("failed to update instance state: %v", err)
				}

				project.State = entities.ProjectStateStopped
				if err := store.UpdateProject(project); err != nil {
					printError("failed to update project state: %v", err)
				}

				fmt.Fprintf(cmd.OutOrStdout(), "Project '%s' VM stopped\n", name)
				return nil
			}
		}
	}
}

func runProjectStart(cmd *cobra.Command, args []string) error {
	name := args[0]
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	project, err := store.GetProject(name)
	if err != nil {
		return fmt.Errorf("project not found: %s", name)
	}

	instance := store.GetInstance(project.ID)
	if instance == nil {
		return fmt.Errorf("no VM instance found for project %s", name)
	}

	rt.LoadInstance(instance)

	printVerbose("Starting VM for project %s...", name)

	if err := rt.StartInstance(ctx, instance.ID); err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	instance.State = entities.InstanceStateRunning
	if err := store.UpdateInstance(instance); err != nil {
		printError("failed to update instance state: %v", err)
	}

	project.State = entities.ProjectStateRunning
	if err := store.UpdateProject(project); err != nil {
		printError("failed to update project state: %v", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Project '%s' VM started\n", name)
	return nil
}

func runProjectRestart(cmd *cobra.Command, args []string) error {
	name := args[0]
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	project, err := store.GetProject(name)
	if err != nil {
		return fmt.Errorf("project not found: %s", name)
	}

	instance := store.GetInstance(project.ID)
	if instance == nil {
		return fmt.Errorf("no VM instance found for project %s", name)
	}

	rt.LoadInstance(instance)

	printVerbose("Restarting VM for project %s...", name)

	// Stop the VM.
	if err := rt.StopInstance(ctx, instance.ID); err != nil {
		return fmt.Errorf("failed to stop VM: %w", err)
	}

	// Wait for the domain to reach shut off state before starting.
	printVerbose("Waiting for VM to shut down...")
	waitCtx, waitCancel := context.WithTimeout(ctx, 60*time.Second)
	defer waitCancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			return errors.New("timed out waiting for VM to stop")
		case <-ticker.C:
			status, err := rt.GetInstanceStatus(waitCtx, instance.ID)
			if err != nil {
				continue
			}
			if status.State == entities.InstanceStateStopped {
				goto start
			}
		}
	}

start:
	// Start the VM.
	if err := rt.StartInstance(ctx, instance.ID); err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	instance.State = entities.InstanceStateRunning
	if err := store.UpdateInstance(instance); err != nil {
		printError("failed to update instance state: %v", err)
	}

	project.State = entities.ProjectStateRunning
	if err := store.UpdateProject(project); err != nil {
		printError("failed to update project state: %v", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Project '%s' VM restarted\n", name)
	return nil
}

const updateInitChunkSize = 512 * 1024 // 512KB

//nolint:funlen,gocognit // CLI handler with multi-phase chunked binary transfer
func runProjectUpdateInit(cmd *cobra.Command, args []string) error {
	name := args[0]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	project, err := store.GetProject(name)
	if err != nil {
		return fmt.Errorf("project not found: %s", name)
	}

	instance := store.GetInstance(project.ID)
	if instance == nil {
		return fmt.Errorf("no VM instance found for project %s", name)
	}

	// Resolve binary path.
	binaryPath := updateInitBinary
	if binaryPath == "" {
		binaryPath = initBinaryPath // global --goship-init flag
	}
	binaryPath = expandDataDir(binaryPath)

	// Read binary and compute checksum.
	f, err := os.Open(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to open binary %s: %w", binaryPath, err)
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat binary: %w", err)
	}
	totalSize := stat.Size()

	h := sha256.New()
	if _, copyErr := io.Copy(h, f); copyErr != nil {
		return fmt.Errorf("failed to compute checksum: %w", copyErr)
	}
	checksum := fmt.Sprintf("%x", h.Sum(nil))

	// Seek back to beginning for chunked reading.
	if _, seekErr := f.Seek(0, io.SeekStart); seekErr != nil {
		return fmt.Errorf("failed to seek binary: %w", seekErr)
	}

	// Connect to VM via virtio-serial.
	vmName := strings.TrimPrefix(instance.DomainName, lvrt.DomainPrefix)
	socketPath := filepath.Join(expandDataDir(dataDir), "vms", vmName, "goship.sock")

	comm, err := lvrt.NewVMCommunicator(socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to VM: %w", err)
	}
	defer func() { _ = comm.Close() }()

	// Phase: begin
	fmt.Fprintf(cmd.OutOrStdout(), "Updating goship-init in project '%s'...\n", name)
	fmt.Fprintf(cmd.OutOrStdout(), "  Binary: %s (%d bytes)\n", binaryPath, totalSize)
	fmt.Fprintf(cmd.OutOrStdout(), "  SHA256: %s\n", checksum)

	resp, err := comm.SendCommand(ctx, &v1.InitCommand{
		Action:   v1.ActionUpdateInit,
		Phase:    "begin",
		Size:     totalSize,
		Checksum: checksum,
	})
	if err != nil {
		return fmt.Errorf("begin phase failed: %w", err)
	}
	if resp.Status != v1.StatusOK {
		return fmt.Errorf("begin phase error: %s", resp.Error)
	}

	// Phase: data (chunked transfer)
	buf := make([]byte, updateInitChunkSize)
	var sent int64
	chunkNum := 0

	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			chunkNum++
			encoded := base64.StdEncoding.EncodeToString(buf[:n])

			resp, err = comm.SendCommand(ctx, &v1.InitCommand{
				Action: v1.ActionUpdateInit,
				Phase:  "data",
				Data:   encoded,
			})
			if err != nil {
				return fmt.Errorf("data phase chunk %d failed: %w", chunkNum, err)
			}
			if resp.Status != v1.StatusOK {
				return fmt.Errorf("data phase chunk %d error: %s", chunkNum, resp.Error)
			}

			sent += int64(n)
			const percentMultiplier = 100
			pct := float64(sent) / float64(totalSize) * percentMultiplier
			fmt.Fprintf(cmd.OutOrStdout(), "  Sent chunk %d: %d/%d bytes (%.0f%%)\n", chunkNum, sent, totalSize, pct)
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("failed to read binary: %w", readErr)
		}
	}

	// Phase: finish
	resp, err = comm.SendCommand(ctx, &v1.InitCommand{
		Action: v1.ActionUpdateInit,
		Phase:  "finish",
	})
	if err != nil {
		return fmt.Errorf("finish phase failed: %w", err)
	}
	if resp.Status != v1.StatusOK {
		return fmt.Errorf("finish phase error: %s", resp.Error)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "goship-init updated successfully in project '%s'\n", name)

	// Optionally restart the VM.
	if updateInitRestart {
		fmt.Fprint(cmd.OutOrStdout(), "Restarting VM...\n")
		return runProjectRestart(cmd, args)
	}

	return nil
}
