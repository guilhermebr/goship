package v1

// Action constants for the GoShip Init protocol.
const (
	ActionDeploy = "deploy"
	ActionStop   = "stop"
	ActionRemove = "remove"
	ActionStatus = "status"
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
	Action  string `json:"action"`
	AppName string `json:"app_name,omitempty"`
	Lines   int    `json:"lines,omitempty"`

	// Fields for update-init chunked transfer protocol.
	Phase    string `json:"phase,omitempty"`    // "begin", "data", "finish"
	Data     string `json:"data,omitempty"`     // base64-encoded chunk (data phase)
	Size     int64  `json:"size,omitempty"`     // total binary size in bytes (begin phase)
	Checksum string `json:"checksum,omitempty"` // sha256 hex digest (begin phase)
}

// InitResponse is the reply from the GoShip Init agent back to the host.
type InitResponse struct {
	Status string  `json:"status"`
	Error  string  `json:"error,omitempty"`
	VMInfo *VMInfo `json:"vm_info,omitempty"`
	Logs   string  `json:"logs,omitempty"`

	// Fields for update-init progress.
	BytesReceived int64 `json:"bytes_received,omitempty"`
}

// VMInfo carries VM identity and network information from the guest agent.
type VMInfo struct {
	Hostname    string   `json:"hostname,omitempty"`
	IPAddresses []string `json:"ip_addresses,omitempty"`
}
