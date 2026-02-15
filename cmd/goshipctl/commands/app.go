package commands

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	lvrt "github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
	v1 "github.com/guilhermebr/goship/pkg/api/v1"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

var appCmd = &cobra.Command{
	Use:   "app",
	Short: "Manage applications inside project VMs",
	Long:  `Create, deploy, list, stop, and remove applications running inside project VMs.`,
}

// Flags for app create.
var (
	appMode        string
	appImage       string
	appBinary      string
	appPorts       []string
	appReplicas    int
	appEnv         []string
	appCPU         float64
	appMemory      int64
	appDescription string
	appTags        []string
)

var appCreateCmd = &cobra.Command{
	Use:   "create <project> <appname>",
	Short: "Create an application definition",
	Long:  `Create an application definition in the project. Use 'app deploy' to start it.`,
	Args:  cobra.ExactArgs(2),
	RunE:  runAppCreate,
}

var appDeployCmd = &cobra.Command{
	Use:   "deploy <project> <appname>",
	Short: "Deploy an application to the project VM",
	Args:  cobra.ExactArgs(2),
	RunE:  runAppDeploy,
}

var appListCmd = &cobra.Command{
	Use:     "list <project>",
	Short:   "List applications in a project",
	Aliases: []string{"ls"},
	Args:    cobra.ExactArgs(1),
	RunE:    runAppList,
}

var appInfoCmd = &cobra.Command{
	Use:   "info <project> <appname>",
	Short: "Show application details",
	Args:  cobra.ExactArgs(2),
	RunE:  runAppInfo,
}

var appStopCmd = &cobra.Command{
	Use:   "stop <project> <appname>",
	Short: "Stop a running application",
	Args:  cobra.ExactArgs(2),
	RunE:  runAppStop,
}

var appDeleteCmd = &cobra.Command{
	Use:     "delete <project> <appname>",
	Short:   "Remove an application from the VM and delete from state",
	Aliases: []string{"rm"},
	Args:    cobra.ExactArgs(2),
	RunE:    runAppDelete,
}

func init() {
	appCreateCmd.Flags().StringVarP(&appMode, "mode", "m", "container", "Execution mode: container or process")
	appCreateCmd.Flags().StringVarP(&appImage, "image", "i", "", "Container image (required for container mode)")
	appCreateCmd.Flags().StringVarP(&appBinary, "binary", "b", "", "Binary path inside VM (required for process mode)")
	appCreateCmd.Flags().StringArrayVarP(&appPorts, "port", "p", nil, "Port mapping host:container (repeatable)")
	appCreateCmd.Flags().IntVarP(&appReplicas, "replicas", "r", 1, "Number of replicas")
	appCreateCmd.Flags().StringArrayVarP(&appEnv, "env", "e", nil, "Environment variable KEY=VALUE (repeatable)")
	appCreateCmd.Flags().Float64Var(&appCPU, "cpu", 0, "CPU limit (cores)")
	appCreateCmd.Flags().Int64Var(&appMemory, "memory", 0, "Memory limit in MB")
	appCreateCmd.Flags().StringVarP(&appDescription, "description", "d", "", "App description")
	appCreateCmd.Flags().StringArrayVarP(&appTags, "tag", "g", nil, "Tags (repeatable)")

	appCmd.AddCommand(appCreateCmd)
	appCmd.AddCommand(appDeployCmd)
	appCmd.AddCommand(appListCmd)
	appCmd.AddCommand(appInfoCmd)
	appCmd.AddCommand(appStopCmd)
	appCmd.AddCommand(appDeleteCmd)
}

func runAppCreate(cmd *cobra.Command, args []string) error {
	projectName, appName := args[0], args[1]

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	// Validate execution mode.
	mode := entities.ExecutionMode(appMode)
	if mode != entities.ExecutionModeContainer && mode != entities.ExecutionModeProcess {
		return fmt.Errorf("invalid mode: %s (must be 'container' or 'process')", appMode)
	}

	if mode == entities.ExecutionModeContainer && appImage == "" {
		return fmt.Errorf("--image is required for container mode")
	}
	if mode == entities.ExecutionModeProcess && appBinary == "" {
		return fmt.Errorf("--binary is required for process mode")
	}

	// Check if app already exists.
	if existing := store.GetApp(project.ID, appName); existing != nil {
		return fmt.Errorf("app '%s' already exists in project '%s'", appName, projectName)
	}

	// Parse port mappings.
	ports, err := parsePorts(appPorts)
	if err != nil {
		return err
	}

	// Parse environment variables.
	env := parseEnv(appEnv)

	app := &entities.AppSpec{
		Name:          appName,
		ExecutionMode: mode,
		Image:         appImage,
		Binary:        appBinary,
		Replicas:      appReplicas,
		Ports:         ports,
		Env:           env,
		Resources: entities.Resources{
			CPU:      appCPU,
			MemoryMB: appMemory,
		},
		Description: appDescription,
		Tags:        appTags,
		CreatedAt:   time.Now(),
	}

	if err := store.SetApp(project.ID, app); err != nil {
		return fmt.Errorf("failed to save app: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "App '%s' created in project '%s'\n", appName, projectName)
	fmt.Fprintf(out, "  Mode:  %s\n", mode)
	if mode == entities.ExecutionModeContainer {
		fmt.Fprintf(out, "  Image: %s\n", appImage)
	} else {
		fmt.Fprintf(out, "  Binary: %s\n", appBinary)
	}
	if len(ports) > 0 {
		fmt.Fprintf(out, "  Ports: %s\n", formatPorts(ports))
	}
	fmt.Fprintf(out, "\nUse 'goshipctl app deploy %s %s' to start it.\n", projectName, appName)

	return nil
}

func runAppDeploy(cmd *cobra.Command, args []string) error {
	projectName, appName := args[0], args[1]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	app := store.GetApp(project.ID, appName)
	if app == nil {
		return fmt.Errorf("app '%s' not found in project '%s'", appName, projectName)
	}

	instance := store.GetInstance(project.ID)
	if instance == nil {
		return fmt.Errorf("no VM instance found for project '%s'", projectName)
	}

	rt.LoadInstance(instance)

	printVerbose("Deploying app '%s' to project '%s'...", appName, projectName)

	if err := rt.DeployApp(ctx, instance.ID, app); err != nil {
		return fmt.Errorf("failed to deploy app: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "App '%s' deployed successfully in project '%s'\n", appName, projectName)
	if app.IsContainerMode() {
		fmt.Fprintf(out, "  Image: %s\n", app.Image)
	} else {
		fmt.Fprintf(out, "  Binary: %s\n", app.Binary)
	}
	if len(app.Ports) > 0 {
		fmt.Fprintf(out, "  Ports: %s\n", formatPorts(app.Ports))
	}

	return nil
}

func runAppList(cmd *cobra.Command, args []string) error {
	projectName := args[0]

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	apps := store.GetApps(project.ID)
	if len(apps) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No apps found in project '%s'.\n", projectName)
		return nil
	}

	// Try to get live status from VM.
	liveStatuses := getLiveAppStatuses(project)

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tMODE\tIMAGE/BINARY\tSTATUS\tPORTS")

	for _, app := range apps {
		ref := app.Image
		if app.IsProcessMode() {
			ref = app.Binary
		}

		status := "created"
		if s, ok := liveStatuses[app.Name]; ok {
			status = string(s.State)
		}

		ports := formatPorts(app.Ports)

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			app.Name,
			app.ExecutionMode,
			ref,
			status,
			ports,
		)
	}

	return w.Flush()
}

func runAppInfo(cmd *cobra.Command, args []string) error {
	projectName, appName := args[0], args[1]

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	app := store.GetApp(project.ID, appName)
	if app == nil {
		return fmt.Errorf("app '%s' not found in project '%s'", appName, projectName)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "App: %s\n", app.Name)
	fmt.Fprintf(out, "  Project:  %s\n", projectName)
	fmt.Fprintf(out, "  Mode:     %s\n", app.ExecutionMode)

	if app.IsContainerMode() {
		fmt.Fprintf(out, "  Image:    %s\n", app.Image)
		if app.Tag != "" {
			fmt.Fprintf(out, "  Tag:      %s\n", app.Tag)
		}
	} else {
		fmt.Fprintf(out, "  Binary:   %s\n", app.Binary)
	}

	fmt.Fprintf(out, "  Replicas: %d\n", app.Replicas)

	if len(app.Ports) > 0 {
		fmt.Fprintf(out, "  Ports:    %s\n", formatPorts(app.Ports))
	}

	if len(app.Env) > 0 {
		fmt.Fprintf(out, "  Env:\n")
		for k, v := range app.Env {
			fmt.Fprintf(out, "    %s=%s\n", k, v)
		}
	}

	if app.Resources.CPU > 0 || app.Resources.MemoryMB > 0 {
		fmt.Fprintf(out, "  Resources:\n")
		if app.Resources.CPU > 0 {
			fmt.Fprintf(out, "    CPU:    %.1f cores\n", app.Resources.CPU)
		}
		if app.Resources.MemoryMB > 0 {
			fmt.Fprintf(out, "    Memory: %d MB\n", app.Resources.MemoryMB)
		}
	}

	if app.Description != "" {
		fmt.Fprintf(out, "  Description: %s\n", app.Description)
	}
	if len(app.Tags) > 0 {
		fmt.Fprintf(out, "  Tags:     %s\n", strings.Join(app.Tags, ", "))
	}
	fmt.Fprintf(out, "  Created:  %s\n", app.CreatedAt.Format(time.RFC3339))

	// Try to get live status.
	liveStatuses := getLiveAppStatuses(project)
	if s, ok := liveStatuses[appName]; ok {
		fmt.Fprintf(out, "\nLive Status:\n")
		fmt.Fprintf(out, "  State:  %s\n", s.State)
		fmt.Fprintf(out, "  ID:     %s\n", s.ID)
		fmt.Fprintf(out, "  Status: %s\n", s.Status)
		if s.StartedAt != nil {
			fmt.Fprintf(out, "  Started: %s\n", s.StartedAt.Format(time.RFC3339))
		}
	}

	return nil
}

func runAppStop(cmd *cobra.Command, args []string) error {
	projectName, appName := args[0], args[1]
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	if app := store.GetApp(project.ID, appName); app == nil {
		return fmt.Errorf("app '%s' not found in project '%s'", appName, projectName)
	}

	instance := store.GetInstance(project.ID)
	if instance == nil {
		return fmt.Errorf("no VM instance found for project '%s'", projectName)
	}

	rt.LoadInstance(instance)

	printVerbose("Stopping app '%s' in project '%s'...", appName, projectName)

	if err := rt.StopApp(ctx, instance.ID, appName); err != nil {
		return fmt.Errorf("failed to stop app: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "App '%s' stopped in project '%s'\n", appName, projectName)
	return nil
}

func runAppDelete(cmd *cobra.Command, args []string) error {
	projectName, appName := args[0], args[1]
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	if app := store.GetApp(project.ID, appName); app == nil {
		return fmt.Errorf("app '%s' not found in project '%s'", appName, projectName)
	}

	// Try to remove from VM (best effort).
	instance := store.GetInstance(project.ID)
	if instance != nil {
		rt.LoadInstance(instance)

		if err := rt.RemoveApp(ctx, instance.ID, appName); err != nil {
			printError("failed to remove app from VM: %v (continuing with state cleanup)", err)
		}
	}

	// Always remove from state.
	if err := store.DeleteApp(project.ID, appName); err != nil {
		return fmt.Errorf("failed to delete app from state: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "App '%s' deleted from project '%s'\n", appName, projectName)
	return nil
}

// getLiveAppStatuses queries the VM for live app statuses via virtio-serial.
func getLiveAppStatuses(project *entities.Project) map[string]v1.AppStatus {
	statuses := make(map[string]v1.AppStatus)

	instance := store.GetInstance(project.ID)
	if instance == nil || instance.DomainName == "" {
		return statuses
	}

	vmName := strings.TrimPrefix(instance.DomainName, lvrt.DomainPrefix)
	socketPath := filepath.Join(expandDataDir(dataDir), "vms", vmName, "goship.sock")

	comm, err := lvrt.NewVMCommunicator(socketPath)
	if err != nil {
		return statuses
	}
	defer comm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := comm.SendCommand(ctx, &v1.InitCommand{Action: v1.ActionStatus})
	if err != nil || resp.Status != v1.StatusOK {
		return statuses
	}

	for _, app := range resp.Apps {
		statuses[app.Name] = app
	}
	return statuses
}

// parsePorts parses port mapping strings like "8080:80" or "8080".
func parsePorts(portStrs []string) ([]entities.PortMapping, error) {
	var ports []entities.PortMapping
	for _, s := range portStrs {
		parts := strings.SplitN(s, ":", 2)
		hostPort, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid port: %s", s)
		}

		containerPort := hostPort
		if len(parts) == 2 {
			containerPort, err = strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid port: %s", s)
			}
		}

		ports = append(ports, entities.PortMapping{
			HostPort:      hostPort,
			ContainerPort: containerPort,
			Protocol:      "tcp",
		})
	}
	return ports, nil
}

// parseEnv parses environment variable strings like "KEY=VALUE".
func parseEnv(envStrs []string) map[string]string {
	env := make(map[string]string)
	for _, s := range envStrs {
		parts := strings.SplitN(s, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	return env
}

// formatPorts formats port mappings for display.
func formatPorts(ports []entities.PortMapping) string {
	var parts []string
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf("%d:%d", p.HostPort, p.ContainerPort))
	}
	return strings.Join(parts, ", ")
}
