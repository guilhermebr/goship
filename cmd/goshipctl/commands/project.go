package commands

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

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

func init() {
	projectCreateCmd.Flags().Float64Var(&projectCPU, "cpu", 1, "Number of CPU cores")
	projectCreateCmd.Flags().Int64Var(&projectMemory, "memory", 512, "Memory in MB")
	projectCreateCmd.Flags().Int64Var(&projectDisk, "disk", 4096, "Disk size in MB")
	projectCreateCmd.Flags().StringVar(&projectNetworkType, "network-type", "", "Network type (network, bridge, user)")
	projectCreateCmd.Flags().StringVar(&projectNetworkSource, "network-source", "", "Network source (network name or bridge name)")

	projectCmd.AddCommand(projectCreateCmd)
	projectCmd.AddCommand(projectListCmd)
	projectCmd.AddCommand(projectDeleteCmd)
	projectCmd.AddCommand(projectInfoCmd)
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
