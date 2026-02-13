package entities

import "time"

// InstanceState represents the lifecycle state of a VM instance.
type InstanceState string

const (
	InstanceStatePending  InstanceState = "pending"
	InstanceStateStarting InstanceState = "starting"
	InstanceStateRunning  InstanceState = "running"
	InstanceStateStopping InstanceState = "stopping"
	InstanceStateStopped  InstanceState = "stopped"
	InstanceStateFailed   InstanceState = "failed"
)

// ProjectInstance represents a running VM instance for a project on a node.
type ProjectInstance struct {
	// Unique identifier
	ID string `json:"id"`
	// Project this instance belongs to
	ProjectID string `json:"project_id"`
	// Node this instance runs on
	NodeID string `json:"node_id"`
	// Current state
	State InstanceState `json:"state"`
	// VM IP address (for networking)
	IPAddress string `json:"ip_address,omitempty"`
	// SSH port (if available)
	SSHPort int `json:"ssh_port,omitempty"`
	// DomainName is the libvirt domain name
	DomainName string `json:"domain_name,omitempty"`
	// DomainUUID is the libvirt domain UUID
	DomainUUID string `json:"domain_uuid,omitempty"`
	// Topology is the actual VM topology in use
	Topology *VMTopology `json:"topology,omitempty"`
	// Error message (if failed)
	Error string `json:"error,omitempty"`
	// Creation timestamp
	CreatedAt time.Time `json:"created_at"`
	// Last update timestamp
	UpdatedAt time.Time `json:"updated_at"`
}
