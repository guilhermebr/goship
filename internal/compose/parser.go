package compose

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// Parse reads a docker-compose.yml file and returns a list of AppSpecs, build contexts, and warnings.
func Parse(path string) ([]entities.AppSpec, map[string]BuildContext, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to read compose file: %w", err)
	}
	return ParseBytes(data)
}

// ParseBytes parses docker-compose.yml content and returns a list of AppSpecs, build contexts, and warnings.
func ParseBytes(data []byte) ([]entities.AppSpec, map[string]BuildContext, []string, error) {
	var cf ComposeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid compose file: %w", err)
	}

	if len(cf.Services) == 0 {
		return nil, nil, nil, fmt.Errorf("no services defined in compose file")
	}

	var warnings []string
	var apps []entities.AppSpec
	builds := make(map[string]BuildContext)

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

		// Parse environment.
		if svc.Environment != nil {
			env, err := parseEnvironment(svc.Environment)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("service %q: %w", name, err)
			}
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
			builds[name] = *bc
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

		apps = append(apps, app)
	}

	return apps, builds, warnings, nil
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
		case 2:
			// hostPort:containerPort
		case 3:
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
			parts := strings.SplitN(s, "=", 2)
			if len(parts) == 2 {
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
	var vols []entities.VolumeMount
	for _, s := range volStrs {
		parts := strings.SplitN(s, ":", 3)
		if len(parts) < 2 {
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
	case "no":
		return entities.RestartPolicyNever
	default:
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
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return 0, fmt.Errorf("empty memory value")
	}

	suffix := strings.ToLower(s[len(s)-1:])
	numStr := s[:len(s)-1]

	switch suffix {
	case "m":
		val, err := strconv.ParseInt(numStr, 10, 64)
		if err != nil {
			return 0, err
		}
		return val, nil
	case "g":
		val, err := strconv.ParseInt(numStr, 10, 64)
		if err != nil {
			return 0, err
		}
		return val * 1024, nil
	default:
		return 0, fmt.Errorf("unsupported memory suffix %q (use m or g)", suffix)
	}
}
