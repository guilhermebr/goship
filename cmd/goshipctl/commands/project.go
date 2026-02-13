package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	lvrt "github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
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
	projectMemory        int64
	projectDisk          int64
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
)

var projectLogsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "Show goship-init logs from the VM",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectLogs,
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

func init() {
	projectCreateCmd.Flags().Float64Var(&projectCPU, "cpu", 1, "Number of CPU cores")
	projectCreateCmd.Flags().Int64Var(&projectMemory, "memory", 512, "Memory in MB")
	projectCreateCmd.Flags().Int64Var(&projectDisk, "disk", 4096, "Disk size in MB")
	projectCreateCmd.Flags().StringVar(&projectNetworkType, "network-type", "", "Network type (network, bridge, user)")
	projectCreateCmd.Flags().StringVar(&projectNetworkSource, "network-source", "", "Network source (network name or bridge name)")

	projectLogsCmd.Flags().IntVarP(&logsLines, "lines", "n", 100, "Number of log lines to show")
	projectLogsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output (poll every 2s)")

	projectCmd.AddCommand(projectCreateCmd)
	projectCmd.AddCommand(projectListCmd)
	projectCmd.AddCommand(projectDeleteCmd)
	projectCmd.AddCommand(projectInfoCmd)
	projectCmd.AddCommand(projectConsoleCmd)
	projectCmd.AddCommand(projectLogsCmd)
	projectCmd.AddCommand(projectStopCmd)
	projectCmd.AddCommand(projectStartCmd)
	projectCmd.AddCommand(projectRestartCmd)
}

func runProjectCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	printVerbose("Creating project: %s", name)

	resources := entities.Resources{
		CPU:      projectCPU,
		MemoryMB: projectMemory,
		DiskMB:   projectDisk,
	}

	// Create project in state store.
	project, err := store.CreateProject(name, entities.RuntimeQEMU, resources)
	if err != nil {
		return fmt.Errorf("failed to create project: %w", err)
	}

	printVerbose("Project created with ID: %s", project.ID)
	printVerbose("Starting VM...")

	// Create VM instance via runtime.
	instance, err := rt.CreateInstance(ctx, project)
	if err != nil {
		// Cleanup on failure.
		store.DeleteProject(project.ID)
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

	for _, p := range projects {
		shortID := p.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
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

	// Show instance info if available.
	instance := store.GetInstance(project.ID)
	if instance != nil {
		fmt.Fprintf(out, "\nVM Instance:\n")
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
		fmt.Fprintf(out, "\nApps:\n")
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
	fmt.Fprintf(cmd.OutOrStdout(), "Use Ctrl+] to exit the console.\n\n")

	// Replace current process with virsh console for full TTY control.
	return syscall.Exec(virshPath, []string{"virsh", "console", instance.DomainName}, os.Environ())
}

func runProjectLogs(cmd *cobra.Command, args []string) error {
	name := args[0]

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
		comm, err := lvrt.NewVMCommunicator(socketPath)
		if err != nil {
			return "", fmt.Errorf("failed to connect to VM: %w", err)
		}
		defer comm.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := comm.SendCommand(ctx, &v1.InitCommand{
			Action: v1.ActionLogs,
			Lines:  logsLines,
		})
		if err != nil {
			return "", fmt.Errorf("failed to get logs: %w", err)
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

	project.State = entities.ProjectStateStopped
	if err := store.UpdateProject(project); err != nil {
		printError("failed to update project state: %v", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Project '%s' VM stopping (ACPI shutdown sent)\n", name)
	return nil
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
			return fmt.Errorf("timed out waiting for VM to stop")
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
