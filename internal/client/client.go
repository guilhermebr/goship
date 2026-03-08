// Package client provides an HTTP client for the GoShip REST API.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	apiserver "github.com/guilhermebr/goship/internal/api"
)

// Client is an HTTP client for the GoShip API server.
type Client struct {
	baseURL string
	http    *http.Client
}

// New creates a new API client pointing at the given base URL.
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// doJSON performs an HTTP request and decodes the JSON response.
// If reqBody is non-nil it is encoded as JSON in the request body.
// If respBody is non-nil the response body is decoded into it.
func (c *Client) doJSON(method, path string, reqBody, respBody any) error {
	var body io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("failed to encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req) //nolint:gosec // baseURL is user-configured, not tainted
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		var errResp apiserver.ErrorResponse
		if decErr := json.NewDecoder(resp.Body).Decode(&errResp); decErr == nil && errResp.Error != "" {
			return fmt.Errorf("API error (%d): %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("API error: %s", resp.Status)
	}

	if respBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

func (c *Client) get(path string, respBody any) error {
	return c.doJSON(http.MethodGet, path, nil, respBody)
}

func (c *Client) post(path string, reqBody, respBody any) error {
	return c.doJSON(http.MethodPost, path, reqBody, respBody)
}

func (c *Client) put(path string, reqBody, respBody any) error {
	return c.doJSON(http.MethodPut, path, reqBody, respBody)
}

func (c *Client) delete(path string) error {
	return c.doJSON(http.MethodDelete, path, nil, nil)
}

func (c *Client) deleteWithBody(path string, reqBody, respBody any) error {
	return c.doJSON(http.MethodDelete, path, reqBody, respBody)
}
