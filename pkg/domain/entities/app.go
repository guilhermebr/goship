package entities

import "time"

// ExecutionMode defines how an app runs inside the VM.
type ExecutionMode string

const (
	// ExecutionModeContainer runs the app as a container (Docker, Podman, containerd).
	ExecutionModeContainer ExecutionMode = "container"
	// ExecutionModeProcess runs the app as a direct process (systemd-managed).
	ExecutionModeProcess ExecutionMode = "process"
)

// AppState represents the lifecycle state of an app (container or process).
type AppState string

// AppState constants define the lifecycle states.
const (
	AppStatePending AppState = "pending"
	AppStateRunning AppState = "running"
	AppStateStopped AppState = "stopped"
	AppStateFailed  AppState = "failed"
)

// RestartPolicy defines the container restart behavior.
type RestartPolicy string

// RestartPolicy constants define the restart behaviors.
const (
	RestartPolicyAlways    RestartPolicy = "always"
	RestartPolicyOnFailure RestartPolicy = "on-failure"
	RestartPolicyNever     RestartPolicy = "never"
)

// PortMapping defines a port mapping from host to container.
type PortMapping struct {
	// Host port (external)
	HostPort int `json:"host_port"`
	// Container port (internal)
	ContainerPort int `json:"container_port"`
	// Protocol (tcp or udp)
	Protocol string `json:"protocol,omitempty"`
}

// VolumeMount defines a volume mount inside a container.
type VolumeMount struct {
	// Source path on the VM
	Source string `json:"source"`
	// Destination path in the container
	Destination string `json:"destination"`
	// Read-only flag
	ReadOnly bool `json:"read_only,omitempty"`
}

// HealthCheck defines health check configuration for an app.
type HealthCheck struct {
	// HTTP path to check (e.g., /health)
	Path string `json:"path,omitempty"`
	// Port to check
	Port int `json:"port,omitempty"`
	// Interval between checks
	IntervalSeconds int `json:"interval_seconds,omitempty"`
	// Timeout for each check
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// Number of retries before marking unhealthy
	Retries int `json:"retries,omitempty"`
}

// AppSpec defines an application to run inside a project VM.
// Apps can run as containers or direct processes based on ExecutionMode.
type AppSpec struct {
	// Application name (unique within project)
	Name string `json:"name"`

	// ExecutionMode determines how the app runs: "container" or "process"
	// Defaults to "container" if not specified
	ExecutionMode ExecutionMode `json:"execution_mode,omitempty"`

	// ==========================================================================
	// Container-specific fields (used when ExecutionMode = "container")
	// ==========================================================================

	// Container image (e.g., nginx:alpine) - required for container mode
	Image string `json:"image,omitempty"`
	// Image tag (optional, defaults to latest)
	Tag string `json:"tag,omitempty"`

	// ==========================================================================
	// Process-specific fields (used when ExecutionMode = "process")
	// ==========================================================================

	// Binary is the path to the executable inside the VM - required for process mode
	Binary string `json:"binary,omitempty"`

	// ==========================================================================
	// Common fields (used by both container and process modes)
	// ==========================================================================

	// Number of replicas per VM
	Replicas int `json:"replicas"`
	// Command to run (overrides image CMD for containers, or arguments to binary for processes)
	Command []string `json:"command,omitempty"`
	// Arguments to the command
	Args []string `json:"args,omitempty"`
	// Environment variables
	Env map[string]string `json:"env,omitempty"`
	// Port mappings
	Ports []PortMapping `json:"ports,omitempty"`
	// Volume mounts (for containers) or bind mounts (for processes)
	Volumes []VolumeMount `json:"volumes,omitempty"`
	// Resource limits
	Resources Resources `json:"resources"`
	// Health check configuration
	HealthCheck *HealthCheck `json:"health_check,omitempty"`
	// Restart policy
	RestartPolicy RestartPolicy `json:"restart_policy,omitempty"`
	// Working directory
	WorkingDir string `json:"working_dir,omitempty"`
	// Hostname for reverse proxy routing (defaults to app name)
	Hostname string `json:"hostname,omitempty"`
	// Available controls whether the app gets external domain routes (default true)
	Available *bool `json:"available,omitempty"`

	// ==========================================================================
	// Metadata fields
	// ==========================================================================

	// Description of the application
	Description string `json:"description,omitempty"`
	// Tags for organization and filtering
	Tags []string `json:"tags,omitempty"`
	// CreatedAt timestamp when the app was created
	CreatedAt time.Time `json:"created_at,omitzero"`
}

// IsAvailable returns true if the app should get external domain routes.
// Defaults to true when not explicitly set.
func (a *AppSpec) IsAvailable() bool {
	return a.Available == nil || *a.Available
}

// GetHostname returns the hostname for reverse proxy routing.
// Defaults to the app name if not explicitly set.
func (a *AppSpec) GetHostname() string {
	if a.Hostname != "" {
		return a.Hostname
	}
	return a.Name
}

// IsContainerMode returns true if the app runs as a container.
func (a *AppSpec) IsContainerMode() bool {
	return a.ExecutionMode == "" || a.ExecutionMode == ExecutionModeContainer
}

// IsProcessMode returns true if the app runs as a direct process.
func (a *AppSpec) IsProcessMode() bool {
	return a.ExecutionMode == ExecutionModeProcess
}
