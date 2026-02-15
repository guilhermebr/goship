package v1

import (
	"time"

	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// Action constants for the GoShip Init protocol.
const (
	ActionDeploy     = "deploy"
	ActionStop       = "stop"
	ActionRemove     = "remove"
	ActionStatus     = "status"
	ActionPing       = "ping"
	ActionLogs       = "logs"
	ActionUpdateInit = "update-init"
)

// Response status constants.
const (
	StatusOK    = "ok"
	StatusError = "error"
)

// InitCommand is a message sent from the host to the GoShip Init agent inside a VM.
type InitCommand struct {
	Action  string             `json:"action"`
	App     *entities.AppSpec  `json:"app,omitempty"`      // App spec for deploy action.
	AppName string             `json:"app_name,omitempty"`
	Lines   int                `json:"lines,omitempty"`
	LogFile string             `json:"log_file,omitempty"` // Path to log file (default: goship-init.log)

	// Fields for update-init chunked transfer protocol.
	Phase    string `json:"phase,omitempty"`    // "begin", "data", "finish"
	Data     string `json:"data,omitempty"`     // base64-encoded chunk (data phase)
	Size     int64  `json:"size,omitempty"`     // total binary size in bytes (begin phase)
	Checksum string `json:"checksum,omitempty"` // sha256 hex digest (begin phase)
}

// InitResponse is the reply from the GoShip Init agent back to the host.
type InitResponse struct {
	Status     string            `json:"status"`
	Error      string            `json:"error,omitempty"`
	VMInfo     *VMInfo           `json:"vm_info,omitempty"`
	Logs       string            `json:"logs,omitempty"`
	Apps       []AppStatus       `json:"apps,omitempty"`
	Containers []ContainerStatus `json:"containers,omitempty"` // Deprecated: use Apps.

	// Fields for update-init progress.
	BytesReceived int64 `json:"bytes_received,omitempty"`
}

// VMInfo carries VM identity and network information from the guest agent.
type VMInfo struct {
	Hostname      string   `json:"hostname,omitempty"`
	IPAddresses   []string `json:"ip_addresses,omitempty"`
	DockerVersion string   `json:"docker_version,omitempty"`
	UptimeSeconds int64    `json:"uptime_seconds,omitempty"`
}

// ContainerState represents the lifecycle state of a container.
// Deprecated: Use entities.AppState instead for new code.
type ContainerState string

const (
	ContainerStatePending ContainerState = "pending"
	ContainerStateRunning ContainerState = "running"
	ContainerStateStopped ContainerState = "stopped"
	ContainerStateFailed  ContainerState = "failed"
)

// ContainerStatus represents the status of a container inside a VM.
// Deprecated: Use AppStatus instead for new code.
type ContainerStatus struct {
	Name      string                 `json:"name"`
	ID        string                 `json:"id"`
	Image     string                 `json:"image"`
	State     ContainerState         `json:"state"`
	Status    string                 `json:"status"`
	Ports     []entities.PortMapping `json:"ports,omitempty"`
	StartedAt *time.Time             `json:"started_at,omitempty"`
}

// AppStatus represents the status of an app (container or process) inside a VM.
type AppStatus struct {
	Name          string                  `json:"name"`
	ExecutionMode entities.ExecutionMode  `json:"execution_mode"`
	ID            string                  `json:"id"`
	Image         string                  `json:"image,omitempty"`
	Binary        string                  `json:"binary,omitempty"`
	State         entities.AppState       `json:"state"`
	Status        string                  `json:"status"`
	Ports         []entities.PortMapping  `json:"ports,omitempty"`
	StartedAt     *time.Time              `json:"started_at,omitempty"`
}

// ToContainerStatus converts AppStatus to ContainerStatus for backwards compatibility.
func (s *AppStatus) ToContainerStatus() ContainerStatus {
	return ContainerStatus{
		Name:      s.Name,
		ID:        s.ID,
		Image:     s.Image,
		State:     ContainerState(s.State),
		Status:    s.Status,
		Ports:     s.Ports,
		StartedAt: s.StartedAt,
	}
}
