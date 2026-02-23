package v1

import (
	"encoding/json"
	"testing"
)

func TestInitCommand_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		cmd  InitCommand
	}{
		{
			name: "ping",
			cmd:  InitCommand{Action: ActionPing},
		},
		{
			name: "deploy with app",
			cmd:  InitCommand{Action: ActionDeploy, AppName: "web"},
		},
		{
			name: "stop with app",
			cmd:  InitCommand{Action: ActionStop, AppName: "api-server"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.cmd)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var got InitCommand
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if got.Action != tc.cmd.Action {
				t.Errorf("Action = %q, want %q", got.Action, tc.cmd.Action)
			}
			if got.AppName != tc.cmd.AppName {
				t.Errorf("AppName = %q, want %q", got.AppName, tc.cmd.AppName)
			}
		})
	}
}

func TestInitCommand_OmitEmptyAppName(t *testing.T) {
	cmd := InitCommand{Action: ActionPing}
	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// app_name should be omitted when empty.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}
	if _, exists := raw["app_name"]; exists {
		t.Error("expected app_name to be omitted when empty")
	}
}

func TestInitResponse_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		resp InitResponse
	}{
		{
			name: "ok",
			resp: InitResponse{Status: StatusOK},
		},
		{
			name: "error with message",
			resp: InitResponse{Status: StatusError, Error: "container not found"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.resp)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var got InitResponse
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if got.Status != tc.resp.Status {
				t.Errorf("Status = %q, want %q", got.Status, tc.resp.Status)
			}
			if got.Error != tc.resp.Error {
				t.Errorf("Error = %q, want %q", got.Error, tc.resp.Error)
			}
		})
	}
}

func TestInitResponse_OmitEmptyError(t *testing.T) {
	resp := InitResponse{Status: StatusOK}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}
	if _, exists := raw["error"]; exists {
		t.Error("expected error to be omitted when empty")
	}
}
