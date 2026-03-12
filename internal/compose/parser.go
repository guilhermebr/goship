package compose

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/guilhermebr/goship/internal/shared/size"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// parseConfig holds optional configuration for parsing.
type parseConfig struct {
	baseDir string // base directory for resolving relative env_file paths
}

// ParseOption configures the compose parser.
type ParseOption func(*parseConfig)

// WithBaseDir sets the base directory for resolving relative env_file paths.
func WithBaseDir(dir string) ParseOption {
	return func(c *parseConfig) {
		c.baseDir = dir
	}
}

// Parse reads a docker-compose.yml file and returns a list of AppSpecs, build contexts, and warnings.
// It automatically resolves env_file paths relative to the compose file's directory.
func Parse(
	path string,
) (apps []entities.AppSpec, builds map[string]BuildContext, warnings []string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to read compose file: %w", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to resolve compose file path: %w", err)
	}
	return ParseBytes(data, WithBaseDir(filepath.Dir(absPath)))
}

// ParseBytes parses docker-compose.yml content and returns a list of AppSpecs, build contexts, and warnings.
// Use WithBaseDir option to enable env_file resolution relative to the compose file directory.
//
//nolint:funlen,gocognit,gocyclo,cyclop // Compose parsing inherently complex with multiple field transformations
func ParseBytes(
	data []byte, opts ...ParseOption,
) (apps []entities.AppSpec, builds map[string]BuildContext, warnings []string, err error) {
	var cfg parseConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	var cf ComposeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid compose file: %w", err)
	}

	if len(cf.Services) == 0 {
		return nil, nil, nil, errors.New("no services defined in compose file")
	}

	var appList []entities.AppSpec
	buildMap := make(map[string]BuildContext)

	// Sort service names for deterministic order.
	names := make([]string, 0, len(cf.Services))
	for name := range cf.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		svc := cf.Services[name]

		app := entities.AppSpec{
			Name:          name,
			ExecutionMode: entities.ExecutionModeContainer,
			Image:         svc.Image,
			Replicas:      1,
			CreatedAt:     time.Now(),
		}

		// Parse command.
		if svc.Command != nil {
			cmd, err := parseCommand(svc.Command)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("service %q: %w", name, err)
			}
			app.Command = cmd
		}

		// Parse ports.
		if len(svc.Ports) > 0 {
			ports, err := parsePorts(svc.Ports)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("service %q: %w", name, err)
			}
			app.Ports = ports
		}

		// Resolve environment: load env_file(s), parse environment section, substitute variables, merge.
		env, envWarnings, err := resolveServiceEnv(name, &svc, cfg.baseDir)
		if err != nil {
			return nil, nil, nil, err
		}
		warnings = append(warnings, envWarnings...)
		if len(env) > 0 {
			app.Env = env
		}

		// Parse volumes.
		if len(svc.Volumes) > 0 {
			vols, err := parseVolumes(svc.Volumes)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("service %q: %w", name, err)
			}
			app.Volumes = vols
		}

		// Parse restart policy.
		if svc.Restart != "" {
			app.RestartPolicy = mapRestartPolicy(svc.Restart)
		}

		// Parse resource limits.
		if svc.Deploy != nil && svc.Deploy.Resources != nil && svc.Deploy.Resources.Limits != nil {
			res, err := parseResources(svc.Deploy.Resources.Limits)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("service %q: %w", name, err)
			}
			app.Resources = res
		}

		// Handle services with build context.
		if svc.Build != nil && svc.Image == "" {
			bc, err := parseBuild(svc.Build)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("service %q: %w", name, err)
			}
			bc.ImageName = fmt.Sprintf("goship-%s:latest", name)
			app.Image = bc.ImageName
			buildMap[name] = *bc
		}

		if svc.Image == "" && svc.Build == nil {
			warnings = append(warnings, fmt.Sprintf("service %q: no image specified, skipping", name))
			continue
		}

		// Collect warnings for unsupported fields.
		if svc.DependsOn != nil {
			warnings = append(warnings, fmt.Sprintf("service %q: depends_on is not supported (ignored)", name))
		}
		if svc.Networks != nil {
			warnings = append(warnings, fmt.Sprintf("service %q: networks is not supported (ignored)", name))
		}

		appList = append(appList, app)
	}

	return appList, buildMap, warnings, nil
}

// resolveServiceEnv loads env_file(s), parses the environment section, resolves ${VAR} references, and merges.
// Resolution order: env_file values are the base, environment section overrides, ${VAR} resolved from both.
func resolveServiceEnv(name string, svc *ComposeService, baseDir string) (map[string]string, []string, error) {
	var warnings []string

	// Step 1: Load env_file(s) into base vars.
	baseVars := make(map[string]string)
	if svc.EnvFile != nil {
		paths, err := parseEnvFileField(svc.EnvFile)
		if err != nil {
			return nil, nil, fmt.Errorf("service %q: %w", name, err)
		}
		for _, p := range paths {
			// Resolve relative paths against the compose file's directory.
			if !filepath.IsAbs(p) && baseDir != "" {
				p = filepath.Join(baseDir, p)
			}
			vars, err := LoadEnvFile(p)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("service %q: env_file %s: %v (skipped)", name, p, err))
				continue
			}
			maps.Copy(baseVars, vars)
		}
	}

	// Step 2: Parse the environment section (raw, may contain ${VAR} references).
	var rawEnv map[string]string
	if svc.Environment != nil {
		var err error
		rawEnv, err = parseEnvironment(svc.Environment)
		if err != nil {
			return nil, nil, fmt.Errorf("service %q: %w", name, err)
		}
	}

	// Step 3: Build substitution context = env_file vars (for resolving ${VAR}).
	// substituteVars also falls back to os.Environ() for host env vars.
	var resolvedEnv map[string]string
	if rawEnv != nil {
		resolvedEnv = substituteVars(rawEnv, baseVars)
	}

	// Step 4: Merge — env_file is the base, resolved environment overrides.
	merged := make(map[string]string, len(baseVars)+len(resolvedEnv))
	maps.Copy(merged, baseVars)
	maps.Copy(merged, resolvedEnv)

	return merged, warnings, nil
}

// parseCommand parses a compose command field which can be a string or list of strings.
func parseCommand(v any) ([]string, error) {
	switch cmd := v.(type) {
	case string:
		return strings.Fields(cmd), nil
	case []any:
		var parts []string
		for _, item := range cmd {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("invalid command element: %v", item)
			}
			parts = append(parts, s)
		}
		return parts, nil
	default:
		return nil, fmt.Errorf("invalid command type: %T", v)
	}
}

// parsePorts parses compose port strings like "8080:80", "8080:80/udp", "127.0.0.1:8080:80".
func parsePorts(portStrs []string) ([]entities.PortMapping, error) {
	const (
		portPartsHostContainer   = 2
		portPartsIPHostContainer = 3
	)

	var ports []entities.PortMapping
	for _, s := range portStrs {
		protocol := "tcp"

		// Check for protocol suffix.
		if idx := strings.Index(s, "/"); idx != -1 {
			protocol = s[idx+1:]
			s = s[:idx]
		}

		// Handle ip:hostPort:containerPort format by stripping the IP bind address.
		parts := strings.Split(s, ":")
		switch len(parts) {
		case portPartsHostContainer:
			// hostPort:containerPort
		case portPartsIPHostContainer:
			// ip:hostPort:containerPort — drop the IP
			parts = parts[1:]
		default:
			return nil, fmt.Errorf("invalid port mapping %q", s)
		}

		hostPort, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid host port in %q: %w", s, err)
		}

		containerPort, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid container port in %q: %w", s, err)
		}

		ports = append(ports, entities.PortMapping{
			HostPort:      hostPort,
			ContainerPort: containerPort,
			Protocol:      protocol,
		})
	}
	return ports, nil
}

// parseEnvironment parses compose environment which can be a map or list of "KEY=VALUE" strings.
func parseEnvironment(v any) (map[string]string, error) {
	env := make(map[string]string)

	switch e := v.(type) {
	case map[string]any:
		for key, val := range e {
			env[key] = fmt.Sprintf("%v", val)
		}
	case []any:
		for _, item := range e {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("invalid environment element: %v", item)
			}
			const envKeyValueParts = 2
			parts := strings.SplitN(s, "=", envKeyValueParts)
			if len(parts) == envKeyValueParts {
				env[parts[0]] = parts[1]
			} else {
				env[parts[0]] = ""
			}
		}
	default:
		return nil, fmt.Errorf("invalid environment type: %T", v)
	}

	return env, nil
}

// parseVolumes parses compose volume strings like "source:dest" or "source:dest:ro".
func parseVolumes(volStrs []string) ([]entities.VolumeMount, error) {
	const minVolumeParts = 2

	var vols []entities.VolumeMount
	for _, s := range volStrs {
		parts := strings.SplitN(s, ":", 3)
		if len(parts) < minVolumeParts {
			return nil, fmt.Errorf("invalid volume %q: expected source:destination", s)
		}

		vol := entities.VolumeMount{
			Source:      parts[0],
			Destination: parts[1],
		}

		if len(parts) == 3 && parts[2] == "ro" {
			vol.ReadOnly = true
		}

		vols = append(vols, vol)
	}
	return vols, nil
}

// mapRestartPolicy converts compose restart policy to GoShip RestartPolicy.
func mapRestartPolicy(policy string) entities.RestartPolicy {
	switch policy {
	case "always", "unless-stopped":
		return entities.RestartPolicyAlways
	case "on-failure":
		return entities.RestartPolicyOnFailure
	default:
		// "no" and any unknown policy map to never
		return entities.RestartPolicyNever
	}
}

// parseResources parses compose resource limits.
func parseResources(spec *ComposeResourceSpec) (entities.Resources, error) {
	var res entities.Resources

	if spec.CPUs != "" {
		cpu, err := strconv.ParseFloat(spec.CPUs, 64)
		if err != nil {
			return res, fmt.Errorf("invalid cpus value %q: %w", spec.CPUs, err)
		}
		res.CPU = cpu
	}

	if spec.Memory != "" {
		mem, err := parseMemory(spec.Memory)
		if err != nil {
			return res, fmt.Errorf("invalid memory value %q: %w", spec.Memory, err)
		}
		res.MemoryMB = mem
	}

	return res, nil
}

// parseBuild parses a compose build field which can be a string (context path) or an object.
func parseBuild(v any) (*BuildContext, error) {
	switch b := v.(type) {
	case string:
		return &BuildContext{Context: b}, nil
	case map[string]any:
		bc := &BuildContext{}
		if ctx, ok := b["context"]; ok {
			s, ok := ctx.(string)
			if !ok {
				return nil, fmt.Errorf("invalid build context type: %T", ctx)
			}
			bc.Context = s
		}
		if df, ok := b["dockerfile"]; ok {
			s, ok := df.(string)
			if !ok {
				return nil, fmt.Errorf("invalid build dockerfile type: %T", df)
			}
			bc.Dockerfile = s
		}
		if bc.Context == "" {
			bc.Context = "."
		}
		return bc, nil
	default:
		return nil, fmt.Errorf("invalid build type: %T", v)
	}
}

// parseMemory parses a memory string like "512m", "1g", "256M", "2G" into megabytes.
func parseMemory(s string) (int64, error) {
	return size.ParseSizeMB(s)
}
