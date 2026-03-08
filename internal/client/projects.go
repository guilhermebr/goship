package client

import (
	"fmt"

	apiserver "github.com/guilhermebr/goship/internal/api"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// ListProjects returns all projects from the API.
func (c *Client) ListProjects() ([]*entities.Project, error) {
	var projects []*entities.Project
	if err := c.get("/api/v1/projects", &projects); err != nil {
		return nil, err
	}
	return projects, nil
}

// CreateProject creates a new project via the API.
func (c *Client) CreateProject(name string, resources entities.Resources) (*apiserver.ProjectResponse, error) {
	req := apiserver.CreateProjectRequest{
		Name:      name,
		Resources: resources,
	}
	var resp apiserver.ProjectResponse
	if err := c.post("/api/v1/projects", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetProject returns a project with its instance and apps by ID or name.
func (c *Client) GetProject(idOrName string) (*apiserver.ProjectResponse, error) {
	var resp apiserver.ProjectResponse
	if err := c.get(fmt.Sprintf("/api/v1/projects/%s", idOrName), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteProject deletes a project by ID or name.
func (c *Client) DeleteProject(idOrName string) error {
	return c.delete(fmt.Sprintf("/api/v1/projects/%s", idOrName))
}

// StopProject stops a project's VM.
func (c *Client) StopProject(idOrName string) error {
	return c.post(fmt.Sprintf("/api/v1/projects/%s/stop", idOrName), nil, nil)
}

// StartProject starts a stopped project's VM.
func (c *Client) StartProject(idOrName string) (*apiserver.ProjectResponse, error) {
	var resp apiserver.ProjectResponse
	if err := c.post(fmt.Sprintf("/api/v1/projects/%s/start", idOrName), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RestartProject restarts a project's VM (stop then start in a single call).
func (c *Client) RestartProject(idOrName string) (*apiserver.ProjectResponse, error) {
	var resp apiserver.ProjectResponse
	if err := c.post(fmt.Sprintf("/api/v1/projects/%s/restart", idOrName), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UpdateProject updates a project's resource allocation.
func (c *Client) UpdateProject(
	idOrName string, req apiserver.UpdateProjectRequest,
) (*apiserver.ProjectResponse, error) {
	var resp apiserver.ProjectResponse
	if err := c.put(fmt.Sprintf("/api/v1/projects/%s", idOrName), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetProjectLogs returns VM logs for a project.
func (c *Client) GetProjectLogs(idOrName, source, logFile string, lines int) (string, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/logs?lines=%d", idOrName, lines)
	if source != "" {
		path += "&source=" + source
	}
	if logFile != "" {
		path += "&file=" + logFile
	}
	var resp apiserver.ProjectLogsResponse
	if err := c.get(path, &resp); err != nil {
		return "", err
	}
	return resp.Logs, nil
}
