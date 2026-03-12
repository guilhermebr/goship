package commands

import (
	"context"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	apiserver "github.com/guilhermebr/goship/internal/api"
	"github.com/guilhermebr/goship/internal/compose"
	"github.com/guilhermebr/goship/internal/manifest"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

var (
	deployFile   string
	deployEnv    string
	deployDryRun bool
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy a project from a goship.yml manifest",
	Long: `Deploy a project using a goship.yml deployment manifest.

The manifest defines project resources, domains, environment variables,
secrets, and references a docker-compose.yml for service definitions.

If the project doesn't exist, it will be created automatically.`,
	RunE: runDeploy,
}

func init() {
	deployCmd.Flags().StringVarP(&deployFile, "file", "f", "goship.yml", "Path to goship.yml manifest")
	deployCmd.Flags().StringVar(&deployEnv, "env", "", "Environment overlay to apply (e.g., dev, prod)")
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "Print steps without executing")
}

func runDeploy(cmd *cobra.Command, args []string) error {
	// 1. Load & resolve manifest.
	m, err := manifest.Load(deployFile)
	if err != nil {
		return err
	}

	m, err = m.Resolve(deployEnv)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	if deployEnv != "" {
		fmt.Fprintf(out, "Deploying project '%s' (env: %s)\n\n", m.Project, deployEnv)
	} else {
		fmt.Fprintf(out, "Deploying project '%s'\n\n", m.Project)
	}

	if apiClient != nil {
		return runDeployAPI(cmd, m)
	}

	return runDeployDirect(cmd, m)
}

//nolint:funlen,gocognit,gocyclo,cyclop,nestif,maintidx // Direct deploy orchestrates multiple steps
func runDeployDirect(cmd *cobra.Command, m *manifest.Manifest) error {
	out := cmd.OutOrStdout()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Step 1: Ensure project exists.
	resources, err := m.Resources.ToEntityResources()
	if err != nil {
		return fmt.Errorf("invalid resources: %w", err)
	}

	project, err := store.GetProject(m.Project)
	if err != nil {
		// Project doesn't exist — create it.
		if deployDryRun {
			fmt.Fprintf(out, "[dry-run] Create project '%s' (cpu=%.0f, memory=%s, disk=%s)\n",
				m.Project, resources.CPU, formatMB(resources.MemoryMB), formatMB(resources.DiskMB))
		} else {
			fmt.Fprintf(out, "Creating project '%s'...\n", m.Project)

			project, err = store.CreateProject(m.Project, entities.RuntimeQEMU, resources)
			if err != nil {
				return fmt.Errorf("failed to create project: %w", err)
			}

			instance, err := rt.CreateInstance(ctx, project)
			if err != nil {
				_ = store.DeleteProject(project.ID)
				return fmt.Errorf("failed to create VM: %w", err)
			}

			project.State = entities.ProjectStateRunning
			_ = store.UpdateProject(project)
			_ = store.SetInstance(instance)

			fmt.Fprintf(out, "  Project created (ID: %s)\n\n", project.ID)
		}
	} else {
		fmt.Fprintf(out, "Project '%s' already exists (ID: %s)\n\n", m.Project, project.ID)
	}

	// Step 2: Set environment variables.
	if len(m.Env) > 0 {
		if deployDryRun {
			fmt.Fprintf(out, "[dry-run] Set %d env var(s): %s\n", len(m.Env), formatKeys(m.Env))
		} else {
			if project.Env == nil {
				project.Env = make(map[string]string)
			}
			maps.Copy(project.Env, m.Env)
			fmt.Fprintf(out, "Set %d env var(s)\n", len(m.Env))
		}
	}

	// Step 3: Set secrets.
	if len(m.Secrets) > 0 {
		if deployDryRun {
			fmt.Fprintf(out, "[dry-run] Set %d secret(s): %s\n", len(m.Secrets), formatKeys(m.Secrets))
		} else {
			v, vErr := initVault()
			if vErr != nil {
				return vErr
			}

			if project.Env == nil {
				project.Env = make(map[string]string)
			}

			for k, val := range m.Secrets {
				encrypted, encErr := v.Encrypt(val)
				if encErr != nil {
					return fmt.Errorf("failed to encrypt %s: %w", k, encErr)
				}
				project.Env[k] = encrypted
			}
			fmt.Fprintf(out, "Set %d secret(s) (encrypted)\n", len(m.Secrets))
		}
	}

	// Step 4: Set domains.
	if len(m.Domains) > 0 {
		defaultDomain := m.DefaultDomain
		if defaultDomain == "" {
			defaultDomain = m.Domains[0]
		}

		if deployDryRun {
			fmt.Fprintf(out, "[dry-run] Set domains: %s (default: %s)\n",
				strings.Join(m.Domains, ", "), defaultDomain)
		} else {
			project.Domains = m.Domains
			project.DefaultDomain = defaultDomain
			fmt.Fprintf(out, "Set %d domain(s) (default: %s)\n", len(m.Domains), defaultDomain)
		}
	}

	// Persist project changes (env, secrets, domains).
	if !deployDryRun && project != nil {
		if err := store.UpdateProject(project); err != nil {
			return fmt.Errorf("failed to update project: %w", err)
		}
	}

	// Step 5: Compose up.
	if m.Compose != nil && m.Compose.File != "" {
		composeFilePath := m.Compose.File
		if !filepath.IsAbs(composeFilePath) {
			baseDir := filepath.Dir(deployFile)
			absBase, absErr := filepath.Abs(baseDir)
			if absErr == nil {
				composeFilePath = filepath.Join(absBase, composeFilePath)
			}
		}

		if deployDryRun {
			build := true
			if m.Compose.Build != nil {
				build = *m.Compose.Build
			}
			fmt.Fprintf(out, "[dry-run] Compose up: %s (build=%v)\n", m.Compose.File, build)
			fmt.Fprint(out, "\nDry run complete.\n")
			return nil
		}

		apps, builds, warnings, parseErr := compose.Parse(composeFilePath)
		if parseErr != nil {
			return fmt.Errorf("failed to parse compose file: %w", parseErr)
		}

		for _, w := range warnings {
			fmt.Fprintf(out, "Warning: %s\n", w)
		}

		instance := store.GetInstance(project.ID)
		if instance == nil {
			return fmt.Errorf("no VM instance found for project '%s'", m.Project)
		}

		rt.LoadInstance(instance)

		shouldBuild := true
		if m.Compose.Build != nil {
			shouldBuild = *m.Compose.Build
		}

		// Build and push images.
		if shouldBuild && len(builds) > 0 {
			fmt.Fprintf(out, "\nBuilding %d image(s)...\n\n", len(builds))
			for svcName, bc := range builds {
				fmt.Fprintf(out, "  Building %s (%s)... ", svcName, bc.ImageName)

				buildArgs := []string{"build", "-t", bc.ImageName}
				if bc.Dockerfile != "" {
					buildArgs = append(buildArgs, "-f", bc.Dockerfile)
				}
				buildArgs = append(buildArgs, bc.Context)

				buildCmd := exec.CommandContext(ctx, "docker", buildArgs...)
				if output, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
					fmt.Fprintf(out, "FAILED\n%s\n", string(output))
					return fmt.Errorf("docker build failed for service %q: %w", svcName, buildErr)
				}
				fmt.Fprint(out, "OK\n")

				fmt.Fprintf(out, "  Pushing %s to VM... ", bc.ImageName)
				if pushErr := pushLocalImage(ctx, out, instance.ID, bc.ImageName); pushErr != nil {
					return fmt.Errorf("failed to push image for service %q: %w", svcName, pushErr)
				}
				fmt.Fprint(out, "OK\n")
			}
			fmt.Fprintln(out)
		} else if !shouldBuild && len(builds) > 0 {
			for svcName := range builds {
				fmt.Fprintf(out, "Warning: service %q: requires build but build=false, skipping\n", svcName)
			}
			var filtered []entities.AppSpec
			for _, app := range apps {
				if _, needsBuild := builds[app.Name]; !needsBuild {
					filtered = append(filtered, app)
				}
			}
			apps = filtered
		}

		// Cap per-service CPU limits.
		vmCPUs := project.Resources.CPU
		if vmCPUs > 0 {
			for i := range apps {
				if apps[i].Resources.CPU > vmCPUs {
					fmt.Fprintf(out, "Warning: service %q: cpus %.2f exceeds VM limit (%.0f), capping\n",
						apps[i].Name, apps[i].Resources.CPU, vmCPUs)
					apps[i].Resources.CPU = vmCPUs
				}
			}
		}

		// Merge project-level env vars into each app.
		for i := range apps {
			if mergeErr := mergeProjectEnv(project.Env, &apps[i]); mergeErr != nil {
				return mergeErr
			}
		}

		fmt.Fprintf(out, "\nDeploying %d service(s)...\n\n", len(apps))

		var deployed int
		for _, app := range apps {
			fmt.Fprintf(out, "  %s (%s)... ", app.Name, app.Image)

			if setErr := store.SetApp(project.ID, &app); setErr != nil {
				fmt.Fprintf(out, "FAILED (state: %v)\n", setErr)
				continue
			}

			if deployErr := rt.DeployApp(ctx, instance.ID, &app); deployErr != nil {
				fmt.Fprintf(out, "FAILED (deploy: %v)\n", deployErr)
				continue
			}

			deployed++
			fmt.Fprint(out, "OK\n")
		}

		fmt.Fprintf(out, "\n%d/%d service(s) deployed.\n", deployed, len(apps))
	} else if deployDryRun {
		fmt.Fprint(out, "\nDry run complete.\n")
	}

	if !deployDryRun {
		fmt.Fprintf(out, "\nDeploy complete for project '%s'.\n", m.Project)
	}

	return nil
}

//nolint:funlen,gocognit,gocyclo,cyclop,nestif,maintidx // API deploy orchestrates multiple steps
func runDeployAPI(cmd *cobra.Command, m *manifest.Manifest) error {
	out := cmd.OutOrStdout()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Step 1: Ensure project exists.
	resources, err := m.Resources.ToEntityResources()
	if err != nil {
		return fmt.Errorf("invalid resources: %w", err)
	}

	var projectID string

	resp, getErr := apiClient.GetProject(m.Project)
	if getErr != nil {
		// Project doesn't exist — create it.
		if deployDryRun {
			fmt.Fprintf(out, "[dry-run] Create project '%s' (cpu=%.0f, memory=%s, disk=%s)\n",
				m.Project, resources.CPU, formatMB(resources.MemoryMB), formatMB(resources.DiskMB))
			projectID = "<dry-run>"
		} else {
			fmt.Fprintf(out, "Creating project '%s'...\n", m.Project)

			createResp, createErr := apiClient.CreateProject(m.Project, resources)
			if createErr != nil {
				return fmt.Errorf("failed to create project: %w", createErr)
			}

			projectID = createResp.ID
			fmt.Fprintf(out, "  Project created (ID: %s)\n\n", projectID)
		}
	} else {
		projectID = resp.ID
		fmt.Fprintf(out, "Project '%s' already exists (ID: %s)\n\n", m.Project, projectID)
	}

	// Step 2: Set environment variables.
	if len(m.Env) > 0 {
		if deployDryRun {
			fmt.Fprintf(out, "[dry-run] Set %d env var(s): %s\n", len(m.Env), formatKeys(m.Env))
		} else {
			if _, setErr := apiClient.SetEnv(m.Project, m.Env, false); setErr != nil {
				return fmt.Errorf("failed to set env vars: %w", setErr)
			}
			fmt.Fprintf(out, "Set %d env var(s)\n", len(m.Env))
		}
	}

	// Step 3: Set secrets.
	if len(m.Secrets) > 0 {
		if deployDryRun {
			fmt.Fprintf(out, "[dry-run] Set %d secret(s): %s\n", len(m.Secrets), formatKeys(m.Secrets))
		} else {
			if _, setErr := apiClient.SetEnv(m.Project, m.Secrets, true); setErr != nil {
				return fmt.Errorf("failed to set secrets: %w", setErr)
			}
			fmt.Fprintf(out, "Set %d secret(s) (encrypted)\n", len(m.Secrets))
		}
	}

	// Step 4: Set domains.
	if len(m.Domains) > 0 {
		defaultDomain := m.DefaultDomain
		if defaultDomain == "" {
			defaultDomain = m.Domains[0]
		}

		if deployDryRun {
			fmt.Fprintf(out, "[dry-run] Set domains: %s (default: %s)\n",
				strings.Join(m.Domains, ", "), defaultDomain)
		} else {
			if _, domErr := apiClient.UpdateDomains(m.Project, apiserver.UpdateDomainsRequest{
				Domains:       m.Domains,
				DefaultDomain: defaultDomain,
			}); domErr != nil {
				return fmt.Errorf("failed to set domains: %w", domErr)
			}
			fmt.Fprintf(out, "Set %d domain(s) (default: %s)\n", len(m.Domains), defaultDomain)
		}
	}

	// Step 5: Compose up.
	if m.Compose != nil && m.Compose.File != "" {
		composeFilePath := m.Compose.File
		if !filepath.IsAbs(composeFilePath) {
			baseDir := filepath.Dir(deployFile)
			absBase, absErr := filepath.Abs(baseDir)
			if absErr == nil {
				composeFilePath = filepath.Join(absBase, composeFilePath)
			}
		}

		if deployDryRun {
			build := true
			if m.Compose.Build != nil {
				build = *m.Compose.Build
			}
			fmt.Fprintf(out, "[dry-run] Compose up: %s (build=%v)\n", m.Compose.File, build)
			fmt.Fprint(out, "\nDry run complete.\n")
			return nil
		}

		apps, builds, warnings, parseErr := compose.Parse(composeFilePath)
		if parseErr != nil {
			return fmt.Errorf("failed to parse compose file: %w", parseErr)
		}

		for _, w := range warnings {
			fmt.Fprintf(out, "Warning: %s\n", w)
		}

		shouldBuild := true
		if m.Compose.Build != nil {
			shouldBuild = *m.Compose.Build
		}

		// Build and push images.
		if shouldBuild && len(builds) > 0 {
			fmt.Fprintf(out, "\nBuilding %d image(s)...\n\n", len(builds))
			for svcName, bc := range builds {
				fmt.Fprintf(out, "  Building %s (%s)... ", svcName, bc.ImageName)
				buildArgs := []string{"build", "-t", bc.ImageName}
				if bc.Dockerfile != "" {
					buildArgs = append(buildArgs, "-f", bc.Dockerfile)
				}
				buildArgs = append(buildArgs, bc.Context)

				buildCmd := exec.CommandContext(ctx, "docker", buildArgs...)
				if output, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
					fmt.Fprintf(out, "FAILED\n%s\n", string(output))
					return fmt.Errorf("docker build failed for service %q: %w", svcName, buildErr)
				}
				fmt.Fprint(out, "OK\n")

				fmt.Fprintf(out, "  Pushing %s to VM via API... ", bc.ImageName)
				if pushErr := runAppPushImageAPI(cmd, m.Project, bc.ImageName); pushErr != nil {
					return fmt.Errorf("failed to push image for service %q: %w", svcName, pushErr)
				}
				fmt.Fprint(out, "OK\n")
			}
			fmt.Fprintln(out)
		} else if !shouldBuild && len(builds) > 0 {
			for svcName := range builds {
				fmt.Fprintf(out, "Warning: service %q: requires build but build=false, skipping\n", svcName)
			}
			var filtered []entities.AppSpec
			for _, app := range apps {
				if _, needsBuild := builds[app.Name]; !needsBuild {
					filtered = append(filtered, app)
				}
			}
			apps = filtered
		}

		fmt.Fprintf(out, "\nDeploying %d service(s)...\n\n", len(apps))

		var deployed int
		for _, app := range apps {
			fmt.Fprintf(out, "  %s (%s)... ", app.Name, app.Image)

			req := apiserver.CreateAppRequest{
				Name:          app.Name,
				ExecutionMode: app.ExecutionMode,
				Image:         app.Image,
				Binary:        app.Binary,
				Ports:         app.Ports,
				Env:           app.Env,
				Resources:     app.Resources,
				RestartPolicy: app.RestartPolicy,
				Replicas:      app.Replicas,
			}

			if _, createErr := apiClient.CreateApp(projectID, req); createErr != nil {
				if !strings.Contains(createErr.Error(), "already exists") {
					fmt.Fprintf(out, "FAILED (create: %v)\n", createErr)
					continue
				}
			}

			deployReq := &apiserver.DeployAppRequest{Image: app.Image}
			if _, deployErr := apiClient.DeployApp(projectID, app.Name, deployReq); deployErr != nil {
				fmt.Fprintf(out, "FAILED (deploy: %v)\n", deployErr)
				continue
			}

			deployed++
			fmt.Fprint(out, "OK\n")
		}

		fmt.Fprintf(out, "\n%d/%d service(s) deployed.\n", deployed, len(apps))
	} else if deployDryRun {
		fmt.Fprint(out, "\nDry run complete.\n")
	}

	if !deployDryRun {
		fmt.Fprintf(out, "\nDeploy complete for project '%s'.\n", m.Project)
	}

	return nil
}

// formatMB formats megabytes as a human-readable string.
func formatMB(mb int64) string {
	if mb == 0 {
		return "default"
	}
	if mb >= 1024 && mb%1024 == 0 {
		return fmt.Sprintf("%dG", mb/1024)
	}
	return fmt.Sprintf("%dM", mb)
}

// formatKeys returns a sorted comma-separated list of map keys.
func formatKeys(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
