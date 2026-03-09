package client

import (
	"fmt"

	apiserver "github.com/guilhermebr/goship/internal/api"
)

// SetEnv sets environment variables for a project.
func (c *Client) SetEnv(idOrName string, env map[string]string, secret bool) (*apiserver.EnvResponse, error) {
	req := apiserver.SetEnvRequest{
		Env:    env,
		Secret: secret,
	}
	var resp apiserver.EnvResponse
	if err := c.put(fmt.Sprintf("/api/v1/projects/%s/env", idOrName), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListEnv returns environment variables for a project.
//
//nolint:revive // showValues is a query param toggle
func (c *Client) ListEnv(idOrName string, showValues bool) (*apiserver.EnvResponse, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/env", idOrName)
	if showValues {
		path += "?show-values=true"
	}
	var resp apiserver.EnvResponse
	if err := c.get(path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteEnv deletes environment variables from a project.
func (c *Client) DeleteEnv(idOrName string, keys []string) (*apiserver.EnvResponse, error) {
	req := apiserver.DeleteEnvRequest{Keys: keys}
	var resp apiserver.EnvResponse
	if err := c.deleteWithBody(fmt.Sprintf("/api/v1/projects/%s/env", idOrName), &req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
