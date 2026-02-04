package v1

// Action constants for the GoShip Init protocol.
const (
	ActionDeploy = "deploy"
	ActionStop   = "stop"
	ActionRemove = "remove"
	ActionStatus = "status"
	ActionPing   = "ping"
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
}

// InitResponse is the reply from the GoShip Init agent back to the host.
type InitResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}
