package entities

import "time"

// NodeStatus represents the lifecycle state of a node.
type NodeStatus string

// NodeStatus constants define the lifecycle states of a node.
const (
	NodeStatusOnline   NodeStatus = "online"
	NodeStatusOffline  NodeStatus = "offline"
	NodeStatusDraining NodeStatus = "draining"
)

// Node represents a compute node in the GoShip cluster.
type Node struct {
	// Unique identifier
	ID string `json:"id"`
	// Human-readable hostname
	Hostname string `json:"hostname"`
	// Endpoint is the agent's address (ip:port)
	Endpoint string `json:"endpoint"`
	// Current status
	Status NodeStatus `json:"status"`
	// Available resources on this node
	Resources Resources `json:"resources"`
	// Labels for scheduling and organization
	Labels map[string]string `json:"labels,omitempty"`
	// LastHeartbeat is the last time this node reported in
	LastHeartbeat time.Time `json:"last_heartbeat"`
	// Creation timestamp
	CreatedAt time.Time `json:"created_at"`
	// Last update timestamp
	UpdatedAt time.Time `json:"updated_at"`
}
