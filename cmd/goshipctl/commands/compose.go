package commands

import (
	"context"
	"fmt"
	"os/exec"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/guilhermebr/goship/internal/compose"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

var composeFile string
var composeBuild bool

var composeCmd = &cobra.Command{
	Use:   "compose",
	Short: "Manage multi-service apps via docker-compose.yml",
	Long:  `Deploy, stop, and inspect multi-service applications defined in a docker-compose.yml file.`,
}

var composeUpCmd = &cobra.Command{
	Use:   "up <project>",
	Short: "Deploy services from a compose file to a project VM",
	Args:  cobra.ExactArgs(1),
	RunE:  runComposeUp,
}

var composeDownCmd = &cobra.Command{
	Use:   "down <project>",
	Short: "Remove services defined in a compose file from a project VM",
	Args:  cobra.ExactArgs(1),
	RunE:  runComposeDown,
}

var composePsCmd = &cobra.Command{
	Use:   "ps <project>",
	Short: "List compose services and their status",
	Args:  cobra.ExactArgs(1),
	RunE:  runComposePs,
}

func init() {
	composeUpCmd.Flags().StringVarP(&composeFile, "file", "f", "docker-compose.yml", "Path to compose file")
	composeUpCmd.Flags().BoolVar(&composeBuild, "build", true, "Build images for services with build context")
	composeDownCmd.Flags().StringVarP(&composeFile, "file", "f", "docker-compose.yml", "Path to compose file")

	composeCmd.AddCommand(composeUpCmd)
	composeCmd.AddCommand(composeDownCmd)
	composeCmd.AddCommand(composePsCmd)
}

func runComposeUp(cmd *cobra.Command, args []string) error {
	projectName := args[0]
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

	apps, builds, warnings, err := compose.Parse(composeFile)
	if err != nil {
		return fmt.Errorf("failed to parse compose file: %w", err)
	}

	out := cmd.OutOrStdout()

	// Print warnings for unsupported fields.
	for _, w := range warnings {
		fmt.Fprintf(out, "Warning: %s\n", w)
	}

	rt.LoadInstance(instance)

	// Build and push images for services with build context.
	if composeBuild && len(builds) > 0 {
		fmt.Fprintf(out, "Building %d image(s)...\n\n", len(builds))

		for svcName, bc := range builds {
			fmt.Fprintf(out, "  Building %s (%s)... ", svcName, bc.ImageName)

			buildArgs := []string{"build", "-t", bc.ImageName}
			if bc.Dockerfile != "" {
				buildArgs = append(buildArgs, "-f", bc.Dockerfile)
			}
			buildArgs = append(buildArgs, bc.Context)

			buildCmd := exec.CommandContext(ctx, "docker", buildArgs...)
			if output, err := buildCmd.CombinedOutput(); err != nil {
				fmt.Fprintf(out, "FAILED\n%s\n", string(output))
				return fmt.Errorf("docker build failed for service %q: %w", svcName, err)
			}
			fmt.Fprintf(out, "OK\n")

			fmt.Fprintf(out, "  Pushing %s to VM... ", bc.ImageName)
			if err := pushLocalImage(ctx, out, instance.ID, bc.ImageName); err != nil {
				return fmt.Errorf("failed to push image for service %q: %w", svcName, err)
			}
			fmt.Fprintf(out, "OK\n")
		}
		fmt.Fprintln(out)
	} else if !composeBuild && len(builds) > 0 {
		for svcName := range builds {
			fmt.Fprintf(out, "Warning: service %q: requires build but --build=false, skipping\n", svcName)
		}
		// Filter out apps that need building.
		var filtered []entities.AppSpec
		for _, app := range apps {
			if _, needsBuild := builds[app.Name]; !needsBuild {
				filtered = append(filtered, app)
			}
		}
		apps = filtered
	}

	// Cap per-service CPU limits to the VM's available vCPUs.
	vmCPUs := project.Resources.CPU
	if vmCPUs > 0 {
		for i := range apps {
			if apps[i].Resources.CPU > vmCPUs {
				fmt.Fprintf(out, "Warning: service %q: cpus %.2f exceeds VM limit (%.0f), capping\n", apps[i].Name, apps[i].Resources.CPU, vmCPUs)
				apps[i].Resources.CPU = vmCPUs
			}
		}
	}

	fmt.Fprintf(out, "Deploying %d service(s) to project '%s'...\n\n", len(apps), projectName)

	var deployed int
	for _, app := range apps {
		fmt.Fprintf(out, "  %s (%s)... ", app.Name, app.Image)

		if err := store.SetApp(project.ID, &app); err != nil {
			fmt.Fprintf(out, "FAILED (state: %v)\n", err)
			continue
		}

		if err := rt.DeployApp(ctx, instance.ID, &app); err != nil {
			fmt.Fprintf(out, "FAILED (deploy: %v)\n", err)
			continue
		}

		deployed++
		fmt.Fprintf(out, "OK\n")
	}

	fmt.Fprintf(out, "\n%d/%d service(s) deployed.\n", deployed, len(apps))

	return nil
}

func runComposeDown(cmd *cobra.Command, args []string) error {
	projectName := args[0]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	instance := store.GetInstance(project.ID)
	if instance == nil {
		return fmt.Errorf("no VM instance found for project '%s'", projectName)
	}

	apps, _, _, err := compose.Parse(composeFile)
	if err != nil {
		return fmt.Errorf("failed to parse compose file: %w", err)
	}

	rt.LoadInstance(instance)

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Removing %d service(s) from project '%s'...\n\n", len(apps), projectName)

	var removed int
	for _, app := range apps {
		fmt.Fprintf(out, "  %s... ", app.Name)

		// Best-effort removal from VM.
		if err := rt.RemoveApp(ctx, instance.ID, app.Name); err != nil {
			printError("failed to remove from VM: %v", err)
		}

		if err := store.DeleteApp(project.ID, app.Name); err != nil {
			fmt.Fprintf(out, "FAILED (state: %v)\n", err)
			continue
		}

		removed++
		fmt.Fprintf(out, "OK\n")
	}

	fmt.Fprintf(out, "\n%d/%d service(s) removed.\n", removed, len(apps))

	return nil
}

func runComposePs(cmd *cobra.Command, args []string) error {
	projectName := args[0]

	project, err := store.GetProject(projectName)
	if err != nil {
		return fmt.Errorf("project not found: %s", projectName)
	}

	apps := store.GetApps(project.ID)
	if len(apps) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No services found in project '%s'.\n", projectName)
		return nil
	}

	// Get live statuses.
	liveStatuses := getLiveAppStatuses(project)

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SERVICE\tIMAGE\tSTATUS\tPORTS")

	for _, app := range apps {
		// Only show container-mode apps in compose ps.
		if !app.IsContainerMode() {
			continue
		}

		status := "created"
		if s, ok := liveStatuses[app.Name]; ok {
			status = string(s.State)
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			app.Name,
			app.Image,
			status,
			formatPorts(app.Ports),
		)
	}

	return w.Flush()
}
