package client

import (
	"fmt"

	apiserver "github.com/guilhermebr/goship/internal/api"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// ListApps returns all apps for a project.
func (c *Client) ListApps(projectID string) ([]*entities.AppSpec, error) {
	var apps []*entities.AppSpec
	if err := c.get(fmt.Sprintf("/api/v1/projects/%s/apps", projectID), &apps); err != nil {
		return nil, err
	}
	return apps, nil
}

// CreateApp creates a new app in a project.
func (c *Client) CreateApp(projectID string, req apiserver.CreateAppRequest) (*entities.AppSpec, error) {
	var app entities.AppSpec
	if err := c.post(fmt.Sprintf("/api/v1/projects/%s/apps", projectID), req, &app); err != nil {
		return nil, err
	}
	return &app, nil
}

// GetApp returns an app by name within a project.
func (c *Client) GetApp(projectID, name string) (*entities.AppSpec, error) {
	var app entities.AppSpec
	if err := c.get(fmt.Sprintf("/api/v1/projects/%s/apps/%s", projectID, name), &app); err != nil {
		return nil, err
	}
	return &app, nil
}

// DeployApp deploys an app in a project.
func (c *Client) DeployApp(projectID, name string, req *apiserver.DeployAppRequest) (*entities.AppSpec, error) {
	var app entities.AppSpec
	if err := c.post(fmt.Sprintf("/api/v1/projects/%s/apps/%s/deploy", projectID, name), req, &app); err != nil {
		return nil, err
	}
	return &app, nil
}

// StopApp stops a running app.
func (c *Client) StopApp(projectID, name string) error {
	return c.post(fmt.Sprintf("/api/v1/projects/%s/apps/%s/stop", projectID, name), nil, nil)
}

// DeleteApp deletes an app from a project.
func (c *Client) DeleteApp(projectID, name string) error {
	return c.delete(fmt.Sprintf("/api/v1/projects/%s/apps/%s", projectID, name))
}

// GetAppLogs returns logs for an app.
func (c *Client) GetAppLogs(projectID, name string, lines int) (string, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/apps/%s/logs?lines=%d", projectID, name, lines)
	var resp apiserver.AppLogsResponse
	if err := c.get(path, &resp); err != nil {
		return "", err
	}
	return resp.Logs, nil
}
