package commands

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	lvrt "github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
	apiserver "github.com/guilhermebr/goship/internal/api"
	"github.com/guilhermebr/goship/internal/compose"
	"github.com/guilhermebr/goship/internal/shared/size"
	"github.com/guilhermebr/goship/internal/vault"
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
	appMode          string
	appImage         string
	appBinary        string
	appPorts         []string
	appReplicas      int
	appEnv           []string
	appEnvFile       string
	appCPU           float64
	appMemory        string
	appDescription   string
	appTags          []string
	appRestartPolicy string
)

// Flags for app edit.
var (
	appEditImage         string
	appEditBinary        string
	appEditPorts         []string
	appEditEnv           []string
	appEditEnvFile       string
	appEditDescription   string
	appEditTags          []string
	appEditRestartPolicy string
	appEditCPU           float64
	appEditMemory        string
)

// Flags for app deploy.
var appLocalImage bool

// Flags for app logs.
var (
	appLogLines  int
	appLogFollow bool
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

var appLogsCmd = &cobra.Command{
	Use:   "logs <project> <appname>",
	Short: "View application logs",
	Args:  cobra.ExactArgs(2),
	RunE:  runAppLogs,
}

var appEditCmd = &cobra.Command{
	Use:   "edit <project> <appname>",
	Short: "Edit an application definition",
	Long:  `Modify an application's configuration without deploying. Changes take effect on next deploy.`,
	Args:  cobra.ExactArgs(2),
	RunE:  runAppEdit,
}

var appPushImageCmd = &cobra.Command{
	Use:   "push-image <project> <image>",
	Short: "Push a local Docker image to the project VM",
	Long:  `Export a local Docker image and transfer it to the project VM via virtio-serial. No registry needed.`,
	Args:  cobra.ExactArgs(2),
	RunE:  runAppPushImage,
}

func init() {
	appCreateCmd.Flags().StringVarP(&appMode, "mode", "m", "container", "Execution mode: container or process")
	appCreateCmd.Flags().StringVarP(&appImage, "image", "i", "", "Container image (required for container mode)")
	appCreateCmd.Flags().StringVarP(&appBinary, "binary", "b", "", "Binary path inside VM (required for process mode)")
	appCreateCmd.Flags().StringArrayVarP(&appPorts, "port", "p", nil, "Port mapping host:container (repeatable)")
	appCreateCmd.Flags().IntVarP(&appReplicas, "replicas", "r", 1, "Number of replicas")
	appCreateCmd.Flags().StringArrayVarP(&appEnv, "env", "e", nil, "Environment variable KEY=VALUE (repeatable)")
	appCreateCmd.Flags().StringVar(&appEnvFile, "env-file", "", "Load environment variables from a file")
	appCreateCmd.Flags().Float64Var(&appCPU, "cpu", 0, "CPU limit (cores)")
	appCreateCmd.Flags().StringVar(&appMemory, "memory", "", "Memory limit (e.g., 512M, 2G)")
	appCreateCmd.Flags().StringVarP(&appDescription, "description", "d", "", "App description")
	appCreateCmd.Flags().StringArrayVarP(&appTags, "tag", "g", nil, "Tags (repeatable)")
	appCreateCmd.Flags().
		StringVar(&appRestartPolicy, "restart-policy", "never", "Restart policy: never, always, or on-failure")

	appDeployCmd.Flags().StringVarP(&appImage, "image", "i", "", "Container image (overrides app spec)")
	appDeployCmd.Flags().BoolVar(&appLocalImage, "local-image", false, "Push local Docker image to VM before deploying")

	appEditCmd.Flags().StringVarP(&appEditImage, "image", "i", "", "Container image")
	appEditCmd.Flags().StringVarP(&appEditBinary, "binary", "b", "", "Binary path (process mode)")
	appEditCmd.Flags().
		StringArrayVarP(&appEditPorts, "port", "p", nil, "Port mapping host:container (repeatable, replaces all)")
	appEditCmd.Flags().StringArrayVarP(&appEditEnv, "env", "e", nil, "Set env var KEY=VALUE (repeatable)")
	appEditCmd.Flags().StringVar(&appEditEnvFile, "env-file", "", "Load environment variables from a file")
	appEditCmd.Flags().StringVarP(&appEditDescription, "description", "d", "", "App description")
	appEditCmd.Flags().StringArrayVarP(&appEditTags, "tag", "g", nil, "Tags (repeatable, replaces all)")
	appEditCmd.Flags().
		StringVar(&appEditRestartPolicy, "restart-policy", "", "Restart policy: never, always, on-failure")
	appEditCmd.Flags().Float64Var(&appEditCPU, "cpu", 0, "CPU limit (cores)")
	appEditCmd.Flags().StringVar(&appEditMemory, "memory", "", "Memory limit (e.g., 512M, 2G)")

	appLogsCmd.Flags().IntVarP(&appLogLines, "lines", "n", 100, "Number of log lines to show")
	appLogsCmd.Flags().BoolVarP(&appLogFollow, "follow", "f", false, "Follow log output (poll every 2s)")

	appCmd.AddCommand(appCreateCmd)
	appCmd.AddCommand(appEditCmd)
	appCmd.AddCommand(appDeployCmd)
	appCmd.AddCommand(appListCmd)
	appCmd.AddCommand(appInfoCmd)
	appCmd.AddCommand(appStopCmd)
	appCmd.AddCommand(appDeleteCmd)
	appCmd.AddCommand(appLogsCmd)
	appCmd.AddCommand(appPushImageCmd)
}

// getProjectIDViaAPI resolves a project name to its ID using the API client.
func getProjectIDViaAPI(nameOrID string) (string, error) {
	resp, err := apiClient.GetProject(nameOrID)
	if err != nil {
		return "", fmt.Errorf("project not found: %s", nameOrID)
	}
	return resp.ID, nil
}

func runAppCreate(cmd *cobra.Command, args []string) error {
	projectName, appName := args[0], args[1]

	if apiClient != nil {
		return runAppCreateAPI(cmd, projectName, appName)
	}

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	// Validate execution mode.
	mode := entities.ExecutionMode(appMode)
	if mode != entities.ExecutionModeContainer && mode != entities.ExecutionModeProcess {
		return fmt.Errorf("invalid mode: %s (must be 'container' or 'process')", appMode)
	}

	if mode == entities.ExecutionModeProcess && appBinary == "" {
		return errors.New("--binary is required for process mode")
	}

	if mode == entities.ExecutionModeContainer && len(appPorts) == 0 {
		return errors.New("--port is required for container mode (e.g., -p 8080:80)")
	}

	// Check if app already exists.
	if existing := store.GetApp(project.ID, appName); existing != nil {
		return fmt.Errorf("app '%s' already exists in project '%s'", appName, projectName)
	}

	// Validate restart policy.
	restartPolicy := entities.RestartPolicy(appRestartPolicy)
	switch restartPolicy {
	case entities.RestartPolicyNever, entities.RestartPolicyAlways, entities.RestartPolicyOnFailure:
		// valid
	default:
		return fmt.Errorf("invalid restart policy: %s (must be 'never', 'always', or 'on-failure')", appRestartPolicy)
	}

	// Parse port mappings.
	ports, err := parsePorts(appPorts)
	if err != nil {
		return err
	}

	// Parse environment variables: --env-file is the base, --env overrides.
	env, err := mergeEnvFlags(appEnvFile, appEnv)
	if err != nil {
		return err
	}

	var memoryMB int64
	if appMemory != "" {
		memoryMB, err = size.ParseSizeMB(appMemory)
		if err != nil {
			return fmt.Errorf("invalid --memory value: %w", err)
		}
	}

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
			MemoryMB: memoryMB,
		},
		RestartPolicy: restartPolicy,
		Description:   appDescription,
		Tags:          appTags,
		CreatedAt:     time.Now(),
	}

	if err := store.SetApp(project.ID, app); err != nil {
		return fmt.Errorf("failed to save app: %w", err)
	}

	printAppCreated(cmd.OutOrStdout(), app, projectName)
	return nil
}

func printAppCreated(out io.Writer, app *entities.AppSpec, projectName string) {
	fmt.Fprintf(out, "App '%s' created in project '%s'\n", app.Name, projectName)
	fmt.Fprintf(out, "  Mode:  %s\n", app.ExecutionMode)
	if app.IsContainerMode() && app.Image != "" {
		fmt.Fprintf(out, "  Image: %s\n", app.Image)
	} else if app.IsProcessMode() {
		fmt.Fprintf(out, "  Binary: %s\n", app.Binary)
	}
	if len(app.Ports) > 0 {
		fmt.Fprintf(out, "  Ports: %s\n", formatPorts(app.Ports))
	}
	fmt.Fprintf(out, "\nUse 'goship app deploy %s %s' to start it.\n", projectName, app.Name)
}

func runAppCreateAPI(cmd *cobra.Command, projectName, appName string) error {
	projectID, err := getProjectIDViaAPI(projectName)
	if err != nil {
		return err
	}

	mode := entities.ExecutionMode(appMode)

	var memoryMB int64
	if appMemory != "" {
		memoryMB, err = size.ParseSizeMB(appMemory)
		if err != nil {
			return fmt.Errorf("invalid --memory value: %w", err)
		}
	}

	ports, err := parsePorts(appPorts)
	if err != nil {
		return err
	}

	env, err := mergeEnvFlags(appEnvFile, appEnv)
	if err != nil {
		return err
	}

	req := apiserver.CreateAppRequest{
		Name:          appName,
		ExecutionMode: mode,
		Image:         appImage,
		Binary:        appBinary,
		Ports:         ports,
		Env:           env,
		Resources: entities.Resources{
			CPU:      appCPU,
			MemoryMB: memoryMB,
		},
		RestartPolicy: entities.RestartPolicy(appRestartPolicy),
		Replicas:      appReplicas,
	}

	app, createErr := apiClient.CreateApp(projectID, req)
	if createErr != nil {
		return createErr
	}

	printAppCreated(cmd.OutOrStdout(), app, projectName)
	return nil
}

//nolint:funlen,gocognit,gocyclo,cyclop // CLI handler with complex flag parsing and validation
func runAppEdit(cmd *cobra.Command, args []string) error {
	projectName, appName := args[0], args[1]

	if apiClient != nil {
		return runAppEditAPI(cmd, projectName, appName)
	}

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	app := store.GetApp(project.ID, appName)
	if app == nil {
		return fmt.Errorf("app '%s' not found in project '%s'", appName, projectName)
	}

	var changes []string

	if cmd.Flags().Changed("image") {
		if app.IsProcessMode() {
			return errors.New("--image is not valid for process mode apps")
		}
		app.Image = appEditImage
		changes = append(changes, fmt.Sprintf("  Image: %s", appEditImage))
	}

	if cmd.Flags().Changed("binary") {
		if app.IsContainerMode() {
			return errors.New("--binary is not valid for container mode apps")
		}
		app.Binary = appEditBinary
		changes = append(changes, fmt.Sprintf("  Binary: %s", appEditBinary))
	}

	if cmd.Flags().Changed("port") {
		ports, err := parsePorts(appEditPorts)
		if err != nil {
			return err
		}
		app.Ports = ports
		changes = append(changes, fmt.Sprintf("  Ports: %s", formatPorts(ports)))
	}

	if cmd.Flags().Changed("env-file") || cmd.Flags().Changed("env") {
		if err := applyEnvEdits(app, cmd, appEditEnvFile, appEditEnv); err != nil {
			return err
		}
		changes = append(changes, fmt.Sprintf("  Env: %d var(s) total", len(app.Env)))
	}

	if cmd.Flags().Changed("description") {
		app.Description = appEditDescription
		changes = append(changes, fmt.Sprintf("  Description: %s", appEditDescription))
	}

	if cmd.Flags().Changed("tag") {
		app.Tags = appEditTags
		changes = append(changes, fmt.Sprintf("  Tags: %s", strings.Join(appEditTags, ", ")))
	}

	if cmd.Flags().Changed("restart-policy") {
		rp := entities.RestartPolicy(appEditRestartPolicy)
		switch rp {
		case entities.RestartPolicyNever, entities.RestartPolicyAlways, entities.RestartPolicyOnFailure:
			// valid
		default:
			return fmt.Errorf(
				"invalid restart policy: %s (must be 'never', 'always', or 'on-failure')",
				appEditRestartPolicy,
			)
		}
		app.RestartPolicy = rp
		changes = append(changes, fmt.Sprintf("  Restart policy: %s", rp))
	}

	if cmd.Flags().Changed("cpu") {
		app.Resources.CPU = appEditCPU
		changes = append(changes, fmt.Sprintf("  CPU: %.1f cores", appEditCPU))
	}

	if cmd.Flags().Changed("memory") {
		memMB, memErr := size.ParseSizeMB(appEditMemory)
		if memErr != nil {
			return fmt.Errorf("invalid --memory value: %w", memErr)
		}
		app.Resources.MemoryMB = memMB
		changes = append(changes, fmt.Sprintf("  Memory: %d MB", memMB))
	}

	if len(changes) == 0 {
		return errors.New("no changes specified; use flags to modify app fields (see --help)")
	}

	if err := store.SetApp(project.ID, app); err != nil {
		return fmt.Errorf("failed to save app: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "App '%s' updated in project '%s':\n", appName, projectName)
	for _, c := range changes {
		fmt.Fprintln(out, c)
	}
	fmt.Fprint(out, "\nNote: changes take effect on next deploy.\n")

	return nil
}

//nolint:funlen // CLI handler with many optional flag checks
func runAppEditAPI(cmd *cobra.Command, projectName, appName string) error {
	projectID, err := getProjectIDViaAPI(projectName)
	if err != nil {
		return err
	}

	req := apiserver.UpdateAppRequest{}
	hasChanges := false

	if cmd.Flags().Changed("image") {
		req.Image = &appEditImage
		hasChanges = true
	}
	if cmd.Flags().Changed("binary") {
		req.Binary = &appEditBinary
		hasChanges = true
	}
	if cmd.Flags().Changed("port") {
		ports, portErr := parsePorts(appEditPorts)
		if portErr != nil {
			return portErr
		}
		req.Ports = ports
		hasChanges = true
	}
	if cmd.Flags().Changed("env-file") || cmd.Flags().Changed("env") {
		env, envErr := mergeEnvFlags(appEditEnvFile, appEditEnv)
		if envErr != nil {
			return envErr
		}
		req.Env = env
		hasChanges = true
	}
	if cmd.Flags().Changed("description") {
		req.Description = &appEditDescription
		hasChanges = true
	}
	if cmd.Flags().Changed("tag") {
		req.Tags = appEditTags
		hasChanges = true
	}
	if cmd.Flags().Changed("restart-policy") {
		req.RestartPolicy = &appEditRestartPolicy
		hasChanges = true
	}
	if cmd.Flags().Changed("cpu") {
		req.CPU = &appEditCPU
		hasChanges = true
	}
	if cmd.Flags().Changed("memory") {
		memMB, memErr := size.ParseSizeMB(appEditMemory)
		if memErr != nil {
			return fmt.Errorf("invalid --memory value: %w", memErr)
		}
		req.MemoryMB = &memMB
		hasChanges = true
	}

	if !hasChanges {
		return errors.New("no changes specified; use flags to modify app fields (see --help)")
	}

	_, err = apiClient.UpdateApp(projectID, appName, req)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "App '%s' updated in project '%s'\n", appName, projectName)
	fmt.Fprint(cmd.OutOrStdout(), "Note: changes take effect on next deploy.\n")
	return nil
}

//nolint:funlen,gocognit,gocyclo,cyclop,nestif // CLI handler with complex deployment logic and binary upload
func runAppDeploy(cmd *cobra.Command, args []string) error {
	projectName, appName := args[0], args[1]

	if apiClient != nil {
		projectID, err := getProjectIDViaAPI(projectName)
		if err != nil {
			return err
		}
		req := &apiserver.DeployAppRequest{Image: appImage}
		app, deployErr := apiClient.DeployApp(projectID, appName, req)
		if deployErr != nil {
			return deployErr
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "App '%s' deployed successfully in project '%s'\n", appName, projectName)
		if app.IsContainerMode() {
			fmt.Fprintf(out, "  Image: %s\n", app.Image)
		} else {
			fmt.Fprintf(out, "  Binary: %s\n", app.Binary)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	app := store.GetApp(project.ID, appName)
	if app == nil {
		return fmt.Errorf("app '%s' not found in project '%s'", appName, projectName)
	}

	// If --image flag is provided, update the app's image.
	if appImage != "" {
		app.Image = appImage
		if err := store.SetApp(project.ID, app); err != nil {
			return fmt.Errorf("failed to update app image: %w", err)
		}
	}

	// Validate: container mode must have an image by deploy time.
	if app.IsContainerMode() && app.Image == "" {
		return fmt.Errorf("--image is required: app '%s' has no image set", appName)
	}

	instance := store.GetInstance(project.ID)
	if instance == nil {
		return fmt.Errorf("no VM instance found for project '%s'", projectName)
	}

	rt.LoadInstance(instance)

	out := cmd.OutOrStdout()

	// If --local-image is set, push the local Docker image to the VM first.
	if appLocalImage && app.IsContainerMode() {
		imageRef := app.Image
		if app.Tag != "" {
			imageRef = fmt.Sprintf("%s:%s", app.Image, app.Tag)
		} else if !strings.Contains(app.Image, ":") {
			imageRef = fmt.Sprintf("%s:latest", app.Image)
		}
		if err := pushLocalImage(ctx, out, instance.ID, imageRef); err != nil {
			return fmt.Errorf("failed to push local image: %w", err)
		}
	}

	// For process mode, auto-upload local binary if it exists on the host.
	if app.IsProcessMode() && app.Binary != "" {
		if _, statErr := os.Stat(app.Binary); statErr == nil {
			fmt.Fprintf(out, "Uploading binary '%s' to VM...\n", app.Binary)

			f, openErr := os.Open(app.Binary)
			if openErr != nil {
				return fmt.Errorf("failed to open binary %s: %w", app.Binary, openErr)
			}
			defer func() { _ = f.Close() }()

			stat, statErr := f.Stat()
			if statErr != nil {
				return fmt.Errorf("failed to stat binary: %w", statErr)
			}

			// Compute SHA256 checksum.
			h := sha256.New()
			if _, copyErr := io.Copy(h, f); copyErr != nil {
				return fmt.Errorf("failed to compute checksum: %w", copyErr)
			}
			checksum := fmt.Sprintf("%x", h.Sum(nil))

			// Seek back to beginning.
			if _, seekErr := f.Seek(0, io.SeekStart); seekErr != nil {
				return fmt.Errorf("failed to seek binary: %w", seekErr)
			}

			fileName := filepath.Base(app.Binary)
			if uploadErr := rt.UploadBinary(
				ctx,
				instance.ID,
				appName,
				fileName,
				f,
				stat.Size(),
				checksum,
			); uploadErr != nil {
				return fmt.Errorf("failed to upload binary: %w", uploadErr)
			}

			// Update binary path to the VM-side location.
			app.Binary = fmt.Sprintf("/opt/goship/binaries/%s/%s", appName, fileName)
			if err := store.SetApp(project.ID, app); err != nil {
				return fmt.Errorf("failed to update app state: %w", err)
			}

			fmt.Fprintf(out, "  Uploaded: %s (%d bytes, sha256: %s)\n", fileName, stat.Size(), checksum)
		}
	}

	// Merge project-level env vars as base, app env overrides.
	if err := mergeProjectEnv(project.Env, app); err != nil {
		return err
	}

	printVerbose("Deploying app '%s' to project '%s'...", appName, projectName)

	if err := rt.DeployApp(ctx, instance.ID, app); err != nil {
		return fmt.Errorf("failed to deploy app: %w", err)
	}

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

	if apiClient != nil {
		projectID, err := getProjectIDViaAPI(projectName)
		if err != nil {
			return err
		}
		apps, listErr := apiClient.ListApps(projectID)
		if listErr != nil {
			return listErr
		}
		if len(apps) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "No apps found in project '%s'.\n", projectName)
			return nil
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tMODE\tIMAGE/BINARY\tPORTS")
		for _, app := range apps {
			ref := app.Image
			if app.IsProcessMode() {
				ref = app.Binary
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", app.Name, app.ExecutionMode, ref, formatPorts(app.Ports))
		}
		return w.Flush()
	}

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

//nolint:funlen,gocognit // CLI handler with detailed app info display
func runAppInfo(cmd *cobra.Command, args []string) error {
	projectName, appName := args[0], args[1]

	if apiClient != nil {
		return runAppInfoAPI(cmd, projectName, appName)
	}

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

	if app.RestartPolicy != "" {
		fmt.Fprintf(out, "  Restart:  %s\n", app.RestartPolicy)
	}

	if len(app.Ports) > 0 {
		fmt.Fprintf(out, "  Ports:    %s\n", formatPorts(app.Ports))
	}

	if len(app.Env) > 0 {
		fmt.Fprint(out, "  Env:\n")
		for _, k := range slices.Sorted(maps.Keys(app.Env)) {
			v := app.Env[k]
			if vault.IsEncrypted(v) {
				v = maskedValue
			}
			fmt.Fprintf(out, "    %s=%s\n", k, v)
		}
	}

	if app.Resources.CPU > 0 || app.Resources.MemoryMB > 0 {
		fmt.Fprint(out, "  Resources:\n")
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
		fmt.Fprint(out, "\nLive Status:\n")
		fmt.Fprintf(out, "  State:  %s\n", s.State)
		fmt.Fprintf(out, "  ID:     %s\n", s.ID)
		fmt.Fprintf(out, "  Status: %s\n", s.Status)
		if s.RestartCount > 0 {
			fmt.Fprintf(out, "  Restarts: %d\n", s.RestartCount)
		}
		if s.StartedAt != nil {
			fmt.Fprintf(out, "  Started: %s\n", s.StartedAt.Format(time.RFC3339))
		}
	}

	return nil
}

func runAppInfoAPI(cmd *cobra.Command, projectName, appName string) error {
	projectID, err := getProjectIDViaAPI(projectName)
	if err != nil {
		return err
	}

	app, err := apiClient.GetApp(projectID, appName)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "App: %s\n", app.Name)
	fmt.Fprintf(out, "  Project:  %s\n", projectName)
	fmt.Fprintf(out, "  Mode:     %s\n", app.ExecutionMode)
	if app.IsContainerMode() {
		fmt.Fprintf(out, "  Image:    %s\n", app.Image)
	} else {
		fmt.Fprintf(out, "  Binary:   %s\n", app.Binary)
	}
	fmt.Fprintf(out, "  Replicas: %d\n", app.Replicas)
	if len(app.Ports) > 0 {
		fmt.Fprintf(out, "  Ports:    %s\n", formatPorts(app.Ports))
	}
	fmt.Fprintf(out, "  Created:  %s\n", app.CreatedAt.Format(time.RFC3339))
	return nil
}

func runAppStop(cmd *cobra.Command, args []string) error {
	projectName, appName := args[0], args[1]

	if apiClient != nil {
		projectID, err := getProjectIDViaAPI(projectName)
		if err != nil {
			return err
		}
		if err := apiClient.StopApp(projectID, appName); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "App '%s' stopped in project '%s'\n", appName, projectName)
		return nil
	}

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

	if apiClient != nil {
		projectID, err := getProjectIDViaAPI(projectName)
		if err != nil {
			return err
		}
		if err := apiClient.DeleteApp(projectID, appName); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "App '%s' deleted from project '%s'\n", appName, projectName)
		return nil
	}

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
	socketPath := filepath.Join(expandDataDir(cfg.DataDir), "vms", vmName, "goship.sock")

	comm, err := lvrt.NewVMCommunicator(socketPath)
	if err != nil {
		return statuses
	}
	defer func() { _ = comm.Close() }()

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
		const partsLen = 2
		if len(parts) == partsLen {
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
	const partsLen = 2
	for _, s := range envStrs {
		parts := strings.SplitN(s, "=", 2)
		if len(parts) == partsLen {
			env[parts[0]] = parts[1]
		}
	}
	return env
}

func runAppLogs(cmd *cobra.Command, args []string) error {
	projectName, appName := args[0], args[1]

	if apiClient != nil {
		return runAppLogsAPI(cmd, projectName, appName)
	}

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

	out := cmd.OutOrStdout()

	logs, err := rt.GetAppLogs(ctx, instance.ID, appName, appLogLines)
	if err != nil {
		return fmt.Errorf("failed to get app logs: %w", err)
	}
	fmt.Fprint(out, logs)
	if logs != "" && !strings.HasSuffix(logs, "\n") {
		fmt.Fprintln(out)
	}

	if !appLogFollow {
		return nil
	}

	// Follow mode: poll every 2s, print new content.
	previousLen := len(logs)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			newLogs, err := rt.GetAppLogs(ctx, instance.ID, appName, appLogLines)
			if err != nil {
				continue
			}
			if len(newLogs) > previousLen {
				newContent := newLogs[previousLen:]
				fmt.Fprint(out, newContent)
				if !strings.HasSuffix(newContent, "\n") {
					fmt.Fprintln(out)
				}
				previousLen = len(newLogs)
			}
		}
	}
}

func runAppLogsAPI(cmd *cobra.Command, projectName, appName string) error {
	projectID, err := getProjectIDViaAPI(projectName)
	if err != nil {
		return err
	}

	logs, err := apiClient.GetAppLogs(projectID, appName, appLogLines)
	if err != nil {
		return err
	}

	fmt.Fprint(cmd.OutOrStdout(), logs)
	if logs != "" && !strings.HasSuffix(logs, "\n") {
		fmt.Fprintln(cmd.OutOrStdout())
	}
	return nil
}

func runAppPushImage(cmd *cobra.Command, args []string) error {
	projectName, imageRef := args[0], args[1]

	if apiClient != nil {
		return runAppPushImageAPI(cmd, projectName, imageRef)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	instance := store.GetInstance(project.ID)
	if instance == nil {
		return fmt.Errorf("no VM instance found for project '%s'", projectName)
	}

	rt.LoadInstance(instance)

	out := cmd.OutOrStdout()
	if err := pushLocalImage(ctx, out, instance.ID, imageRef); err != nil {
		return err
	}

	fmt.Fprintf(out, "Image '%s' pushed to project '%s'\n", imageRef, projectName)
	return nil
}

func runAppPushImageAPI(cmd *cobra.Command, projectName, imageRef string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Exporting image '%s'...\n", imageRef)

	// Create temp file for the compressed image.
	tmpFile, err := os.CreateTemp("", "goship-image-*.tar.gz")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	// Run docker save and pipe through gzip to temp file.
	dockerSave := exec.CommandContext(ctx, "docker", "save", imageRef)
	saveOut, err := dockerSave.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create pipe: %w", err)
	}
	if startErr := dockerSave.Start(); startErr != nil {
		return fmt.Errorf("failed to run 'docker save %s': %w", imageRef, startErr)
	}

	gzWriter := gzip.NewWriter(tmpFile)
	if _, copyErr := io.Copy(gzWriter, saveOut); copyErr != nil {
		_ = dockerSave.Wait()
		return fmt.Errorf("failed to compress image: %w", copyErr)
	}
	if closeErr := gzWriter.Close(); closeErr != nil {
		_ = dockerSave.Wait()
		return fmt.Errorf("failed to finalize compression: %w", closeErr)
	}
	if waitErr := dockerSave.Wait(); waitErr != nil {
		return fmt.Errorf("docker save failed: %w", waitErr)
	}

	stat, err := tmpFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat compressed image: %w", err)
	}
	if _, seekErr := tmpFile.Seek(0, io.SeekStart); seekErr != nil {
		return fmt.Errorf("failed to seek: %w", seekErr)
	}

	const mbDivisor = 1024 * 1024
	fmt.Fprintf(out, "  Compressed size: %.1f MB\n", float64(stat.Size())/mbDivisor)
	fmt.Fprint(out, "Pushing image to project via API...\n")

	if err := apiClient.PushImage(projectName, imageRef, tmpFile, stat.Size()); err != nil {
		return err
	}

	fmt.Fprintf(out, "Image '%s' pushed to project '%s'\n", imageRef, projectName)
	return nil
}

// pushLocalImage exports a local Docker image, compresses it, and transfers it to the VM.
//
//nolint:funlen // Image export and transfer logic requires multiple steps
func pushLocalImage(ctx context.Context, out io.Writer, instanceID string, imageRef string) error {
	fmt.Fprintf(out, "Exporting image '%s'...\n", imageRef)

	// Create temp file for the compressed image.
	tmpFile, err := os.CreateTemp("", "goship-image-*.tar.gz")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	// Run docker save and pipe through gzip to temp file.
	dockerSave := exec.CommandContext(ctx, "docker", "save", imageRef)
	saveOut, err := dockerSave.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create pipe: %w", err)
	}

	startErr := dockerSave.Start()
	if startErr != nil {
		return fmt.Errorf("failed to run 'docker save %s': %w", imageRef, startErr)
	}

	gzWriter := gzip.NewWriter(tmpFile)
	if _, copyErr := io.Copy(gzWriter, saveOut); copyErr != nil {
		_ = dockerSave.Wait()
		return fmt.Errorf("failed to compress image: %w", copyErr)
	}
	if closeErr := gzWriter.Close(); closeErr != nil {
		_ = dockerSave.Wait()
		return fmt.Errorf("failed to finalize compression: %w", closeErr)
	}

	if waitErr := dockerSave.Wait(); waitErr != nil {
		return fmt.Errorf("docker save failed: %w", waitErr)
	}

	// Get compressed file size.
	stat, err := tmpFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat compressed image: %w", err)
	}
	compressedSize := stat.Size()

	// Compute SHA256 of compressed file.
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek: %w", err)
	}

	h := sha256.New()
	if _, err := io.Copy(h, tmpFile); err != nil {
		return fmt.Errorf("failed to compute checksum: %w", err)
	}
	checksum := fmt.Sprintf("%x", h.Sum(nil))

	// Seek back to beginning for transfer.
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek: %w", err)
	}

	const mbDivisor = 1024 * 1024
	fmt.Fprintf(out, "  Compressed size: %.1f MB\n", float64(compressedSize)/mbDivisor)
	fmt.Fprint(out, "Pushing image to VM...\n")

	if err := rt.UploadImage(ctx, instanceID, imageRef, tmpFile, compressedSize, checksum); err != nil {
		return fmt.Errorf("failed to upload image: %w", err)
	}

	fmt.Fprint(out, "  Image loaded successfully\n")
	return nil
}

// applyEnvEdits merges --env-file and --env flags into an existing app's env map.
func applyEnvEdits(app *entities.AppSpec, cmd *cobra.Command, envFile string, envFlags []string) error {
	if app.Env == nil {
		app.Env = make(map[string]string)
	}
	if envFile != "" {
		fileEnv, err := compose.LoadEnvFile(envFile)
		if err != nil {
			return fmt.Errorf("failed to load --env-file: %w", err)
		}
		maps.Copy(app.Env, fileEnv)
	}
	if cmd.Flags().Changed("env") {
		maps.Copy(app.Env, parseEnv(envFlags))
	}
	return nil
}

// mergeEnvFlags loads --env-file (base) and applies --env overrides.
func mergeEnvFlags(envFile string, envFlags []string) (map[string]string, error) {
	env := make(map[string]string)
	if envFile != "" {
		fileEnv, err := compose.LoadEnvFile(envFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load --env-file: %w", err)
		}
		maps.Copy(env, fileEnv)
	}
	maps.Copy(env, parseEnv(envFlags))
	return env, nil
}

// formatPorts formats port mappings for display.
func formatPorts(ports []entities.PortMapping) string {
	var parts []string
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf("%d:%d", p.HostPort, p.ContainerPort))
	}
	return strings.Join(parts, ", ")
}
