package gsinit

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	v1 "github.com/guilhermebr/goship/pkg/api/v1"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

const (
	containerStateExited       = "exited"
	bytesPerMegabyte     int64 = 1024 * 1024

	// goshipNetworkName is the user-defined Docker bridge network that provides
	// DNS-based service discovery between GoShip-managed containers.
	goshipNetworkName = "goship"
)

// DockerManager manages Docker containers inside the VM.
// Implements the AppExecutor interface.
type DockerManager struct {
	client *client.Client
}

// Ensure DockerManager implements AppExecutor.
var _ AppExecutor = (*DockerManager)(nil)

// NewDockerManager creates a new Docker manager and ensures the GoShip
// bridge network exists for container-to-container DNS resolution.
func NewDockerManager(ctx context.Context) (*DockerManager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	m := &DockerManager{client: cli}

	if err := m.ensureNetwork(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure goship network: %w", err)
	}

	return m, nil
}

// ensureNetwork creates the "goship" bridge network if it doesn't already exist.
// User-defined bridge networks provide automatic DNS resolution between containers.
func (m *DockerManager) ensureNetwork(ctx context.Context) error {
	_, err := m.client.NetworkInspect(ctx, goshipNetworkName, network.InspectOptions{})
	if err == nil {
		return nil // Network already exists.
	}

	_, err = m.client.NetworkCreate(ctx, goshipNetworkName, network.CreateOptions{
		Driver: "bridge",
	})
	if err != nil {
		return fmt.Errorf("failed to create network %s: %w", goshipNetworkName, err)
	}

	return nil
}

// LoadImage loads a Docker image from a tar archive reader (e.g. from docker save).
func (m *DockerManager) LoadImage(ctx context.Context, reader io.Reader) error {
	resp, err := m.client.ImageLoad(ctx, reader)
	if err != nil {
		return fmt.Errorf("failed to load image: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Deploy deploys a container for the given app spec.
//
//nolint:funlen,gocognit,revive // Container deployment requires multiple configuration steps
func (m *DockerManager) Deploy(ctx context.Context, app *entities.AppSpec) error {
	// Build full image reference.
	imageRef := app.Image
	if app.Tag != "" {
		imageRef = fmt.Sprintf("%s:%s", app.Image, app.Tag)
	} else if !strings.Contains(app.Image, ":") {
		imageRef = fmt.Sprintf("%s:latest", app.Image)
	}

	// Check if image exists locally first (e.g. pushed via upload-image).
	_, inspectErr := m.client.ImageInspect(ctx, imageRef)
	if inspectErr != nil {
		// Image not found locally, pull from registry.
		reader, err := m.client.ImagePull(ctx, imageRef, image.PullOptions{})
		if err != nil {
			return fmt.Errorf("failed to pull image %s: %w", imageRef, err)
		}
		defer func() { _ = reader.Close() }()
		_, _ = io.Copy(io.Discard, reader) // Wait for pull to complete.
	}

	// Build container config.
	containerConfig := &container.Config{
		Image: imageRef,
		Env:   buildEnvList(app.Env),
	}

	if len(app.Command) > 0 {
		containerConfig.Cmd = app.Command
	}
	if len(app.Args) > 0 {
		containerConfig.Cmd = append(containerConfig.Cmd, app.Args...)
	}
	if app.WorkingDir != "" {
		containerConfig.WorkingDir = app.WorkingDir
	}

	// Build exposed ports and port bindings.
	exposedPorts := nat.PortSet{}
	portBindings := nat.PortMap{}

	for _, p := range app.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		containerPort := nat.Port(fmt.Sprintf("%d/%s", p.ContainerPort, proto))
		exposedPorts[containerPort] = struct{}{}
		portBindings[containerPort] = []nat.PortBinding{
			{
				HostIP:   "0.0.0.0",
				HostPort: fmt.Sprintf("%d", p.HostPort),
			},
		}
	}
	containerConfig.ExposedPorts = exposedPorts

	// Build host config.
	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		RestartPolicy: container.RestartPolicy{
			Name: toDockerRestartPolicy(app.RestartPolicy),
		},
	}

	// Apply resource limits.
	if app.Resources.MemoryMB > 0 {
		hostConfig.Memory = app.Resources.MemoryMB * bytesPerMegabyte
	}
	if app.Resources.CPU > 0 {
		hostConfig.NanoCPUs = int64(app.Resources.CPU * 1e9)
	}

	// Build volume mounts.
	for _, vol := range app.Volumes {
		hostConfig.Binds = append(hostConfig.Binds, fmt.Sprintf("%s:%s", vol.Source, vol.Destination))
	}

	// Remove existing container if present.
	containerName := fmt.Sprintf("goship-%s", app.Name)
	containers, err := m.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}
	for _, c := range containers {
		for _, name := range c.Names {
			if name == "/"+containerName {
				removeOpts := container.RemoveOptions{Force: true}
				if removeErr := m.client.ContainerRemove(ctx, c.ID, removeOpts); removeErr != nil {
					return fmt.Errorf("failed to remove existing container: %w", removeErr)
				}
				break
			}
		}
	}

	// Attach to goship network with app name as DNS alias.
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			goshipNetworkName: {
				Aliases: []string{app.Name},
			},
		},
	}

	// Create and start container.
	resp, err := m.client.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, containerName)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	if err := m.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	return nil
}

// Stop stops a container by app name.
func (m *DockerManager) Stop(ctx context.Context, appName string) error {
	containerName := fmt.Sprintf("goship-%s", appName)
	timeout := 30

	if err := m.client.ContainerStop(ctx, containerName, container.StopOptions{Timeout: &timeout}); err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to stop container: %w", err)
	}
	return nil
}

// Remove removes a container by app name.
func (m *DockerManager) Remove(ctx context.Context, appName string) error {
	containerName := fmt.Sprintf("goship-%s", appName)

	if err := m.client.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true}); err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to remove container: %w", err)
	}
	return nil
}

// GetStatus returns the status of all GoShip-managed containers.
//
//nolint:gocognit,nestif,revive // Container status extraction requires parsing Docker API responses
func (m *DockerManager) GetStatus(ctx context.Context) ([]v1.AppStatus, error) {
	containers, err := m.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var statuses []v1.AppStatus
	for _, c := range containers {
		for _, name := range c.Names {
			if after, ok := strings.CutPrefix(name, "/goship-"); ok {
				appName := after
				status := v1.AppStatus{
					Name:          appName,
					ExecutionMode: entities.ExecutionModeContainer,
					ID:            c.ID[:12],
					Image:         c.Image,
					State:         toAppState(c.State),
					Status:        c.Status,
				}

				for _, p := range c.Ports {
					status.Ports = append(status.Ports, entities.PortMapping{
						HostPort:      int(p.PublicPort),
						ContainerPort: int(p.PrivatePort),
						Protocol:      p.Type,
					})
				}

				if c.State == "running" {
					inspect, err := m.client.ContainerInspect(ctx, c.ID)
					if err == nil {
						if startedAt, err := time.Parse(time.RFC3339Nano, inspect.State.StartedAt); err == nil {
							status.StartedAt = &startedAt
						}
					}
				}

				statuses = append(statuses, status)
				break
			}
		}
	}
	return statuses, nil
}

// GetDockerVersion returns the Docker daemon version.
func (m *DockerManager) GetDockerVersion(ctx context.Context) (string, error) {
	version, err := m.client.ServerVersion(ctx)
	if err != nil {
		return "", err
	}
	return version.Version, nil
}

// Close closes the Docker client.
func (m *DockerManager) Close() error {
	return m.client.Close()
}

// buildEnvList converts a map of environment variables to a slice of "KEY=VALUE" strings.
func buildEnvList(env map[string]string) []string {
	var result []string
	for k, v := range env {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}
	return result
}

// toDockerRestartPolicy converts a GoShip restart policy to Docker's format.
func toDockerRestartPolicy(policy entities.RestartPolicy) container.RestartPolicyMode {
	switch policy {
	case entities.RestartPolicyAlways:
		return container.RestartPolicyAlways
	case entities.RestartPolicyOnFailure:
		return container.RestartPolicyOnFailure
	case entities.RestartPolicyNever:
		return container.RestartPolicyDisabled
	default:
		return container.RestartPolicyUnlessStopped
	}
}

// toAppState converts a Docker state string to a GoShip AppState.
func toAppState(state string) entities.AppState {
	switch state {
	case "running":
		return entities.AppStateRunning
	case containerStateExited, "dead":
		return entities.AppStateStopped
	case "created", "restarting":
		return entities.AppStatePending
	default:
		return entities.AppStateFailed
	}
}
