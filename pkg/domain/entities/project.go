package entities

import "time"

// RuntimeType defines the VM runtime backend.
type RuntimeType string

const (
	// RuntimeQEMU uses QEMU/KVM virtual machines.
	RuntimeQEMU RuntimeType = "qemu"
	// RuntimeKata uses Kata Containers.
	RuntimeKata RuntimeType = "kata"
	// RuntimeFirecracker uses Firecracker microVMs.
	RuntimeFirecracker RuntimeType = "firecracker"
)

// ProjectState represents the lifecycle state of a project.
type ProjectState string

// ProjectState constants define the lifecycle states.
const (
	ProjectStatePending  ProjectState = "pending"
	ProjectStateCreating ProjectState = "creating"
	ProjectStateRunning  ProjectState = "running"
	ProjectStateStopping ProjectState = "stopping"
	ProjectStateStopped  ProjectState = "stopped"
	ProjectStateFailed   ProjectState = "failed"
)

// Resources defines resource limits for a project or app.
type Resources struct {
	// CPU cores (can be fractional, e.g., 0.5)
	CPU float64 `json:"cpu,omitempty"`
	// Memory in megabytes
	MemoryMB int64 `json:"memory_mb,omitempty"`
	// Disk size in megabytes
	DiskMB int64 `json:"disk_mb,omitempty"`
}

// Project represents the isolation boundary in GoShip.
// Each project runs in its own VM(s).
type Project struct {
	// Unique identifier
	ID string `json:"id"`
	// Human-readable name
	Name string `json:"name"`
	// Runtime type (qemu, kata, firecracker)
	Runtime RuntimeType `json:"runtime"`
	// Resource limits for the project VM
	Resources Resources `json:"resources"`
	// Topology defines advanced VM topology (optional, uses defaults if nil)
	Topology *VMTopology `json:"topology,omitempty"`
	// Current state
	State ProjectState `json:"state"`
	// Labels for organization
	Labels map[string]string `json:"labels,omitempty"`
	// Creation timestamp
	CreatedAt time.Time `json:"created_at"`
	// Last update timestamp
	UpdatedAt time.Time `json:"updated_at"`
}
