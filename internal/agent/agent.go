// Package agent implements the node agent that sends heartbeats
// and reconciles desired state from the control plane.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/guilhermebr/goship/internal/agent/runtime"
	apiserver "github.com/guilhermebr/goship/internal/api"
)

// Agent is the node agent that sends heartbeats and pulls desired state.
type Agent struct {
	nodeID    string
	serverURL string
	rt        runtime.ProjectRuntime
	http      *http.Client
	interval  time.Duration
	logger    *log.Logger
}

// New creates a new Agent.
func New(nodeID, serverURL string, rt runtime.ProjectRuntime, interval time.Duration, logger *log.Logger) *Agent {
	return &Agent{
		nodeID:    nodeID,
		serverURL: serverURL,
		rt:        rt,
		http:      &http.Client{Timeout: 10 * time.Second},
		interval:  interval,
		logger:    logger,
	}
}

// Run starts the agent loop. It sends heartbeats and reconciles desired state
// at the configured interval until the context is canceled.
func (a *Agent) Run(ctx context.Context) error {
	a.logger.Printf("agent started: node=%s server=%s interval=%s", a.nodeID, a.serverURL, a.interval)

	// Run immediately on start, then on ticker.
	a.tick(ctx)

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			a.logger.Print("agent stopping...")
			return nil
		case <-ticker.C:
			a.tick(ctx)
		}
	}
}

func (a *Agent) tick(ctx context.Context) {
	if err := a.heartbeat(); err != nil {
		a.logger.Printf("heartbeat failed: %v", err)
	}

	if err := a.pullAndReconcile(ctx); err != nil {
		a.logger.Printf("reconcile failed: %v", err)
	}
}

func (a *Agent) heartbeat() error {
	url := fmt.Sprintf("%s/api/v1/nodes/%s/heartbeat", a.serverURL, a.nodeID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create heartbeat request: %w", err)
	}

	resp, err := a.http.Do(req) //nolint:gosec // serverURL is user-configured
	if err != nil {
		return fmt.Errorf("heartbeat request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		var errResp apiserver.ErrorResponse
		if decErr := json.NewDecoder(resp.Body).Decode(&errResp); decErr == nil && errResp.Error != "" {
			return fmt.Errorf("heartbeat error (%d): %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("heartbeat error: %s", resp.Status)
	}

	return nil
}

func (a *Agent) pullAndReconcile(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/v1/nodes/%s/desired-state", a.serverURL, a.nodeID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create desired-state request: %w", err)
	}

	resp, err := a.http.Do(req) //nolint:gosec // serverURL is user-configured
	if err != nil {
		return fmt.Errorf("desired-state request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		var errResp apiserver.ErrorResponse
		if decErr := json.NewDecoder(resp.Body).Decode(&errResp); decErr == nil && errResp.Error != "" {
			return fmt.Errorf("desired-state error (%d): %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("desired-state error: %s", resp.Status)
	}

	var desired apiserver.DesiredState
	if err := json.NewDecoder(resp.Body).Decode(&desired); err != nil {
		return fmt.Errorf("failed to decode desired state: %w", err)
	}

	reconciler := &Reconciler{rt: a.rt, logger: a.logger}
	return reconciler.Reconcile(ctx, &desired)
}

// Register sends a one-time registration to the control plane.
func (a *Agent) Register(hostname, endpoint string) error {
	body := apiserver.RegisterNodeRequest{
		Hostname: hostname,
		Endpoint: endpoint,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to encode register request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/nodes", a.serverURL)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("failed to create register request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.http.Do(req) //nolint:gosec // serverURL is user-configured
	if err != nil {
		return fmt.Errorf("register request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		var errResp apiserver.ErrorResponse
		if decErr := json.NewDecoder(resp.Body).Decode(&errResp); decErr == nil && errResp.Error != "" {
			return fmt.Errorf("register error (%d): %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("register error: %s", resp.Status)
	}

	a.logger.Printf("node registered: %s", hostname)
	return nil
}
