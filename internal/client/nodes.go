package client

import (
	"fmt"

	apiserver "github.com/guilhermebr/goship/internal/api"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// RegisterNode registers a new node via the API.
func (c *Client) RegisterNode(req apiserver.RegisterNodeRequest) (*entities.Node, error) {
	var node entities.Node
	if err := c.post("/api/v1/nodes", req, &node); err != nil {
		return nil, err
	}
	return &node, nil
}

// ListNodes returns all nodes from the API.
func (c *Client) ListNodes() ([]*entities.Node, error) {
	var nodes []*entities.Node
	if err := c.get("/api/v1/nodes", &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

// GetNode returns a node by ID or hostname.
func (c *Client) GetNode(idOrHostname string) (*entities.Node, error) {
	var node entities.Node
	if err := c.get(fmt.Sprintf("/api/v1/nodes/%s", idOrHostname), &node); err != nil {
		return nil, err
	}
	return &node, nil
}

// DeleteNode deletes a node by ID or hostname.
func (c *Client) DeleteNode(idOrHostname string) error {
	return c.delete(fmt.Sprintf("/api/v1/nodes/%s", idOrHostname))
}

// DrainNode sets a node to draining status.
func (c *Client) DrainNode(idOrHostname string) error {
	return c.post(fmt.Sprintf("/api/v1/nodes/%s/drain", idOrHostname), nil, nil)
}

// Heartbeat sends a heartbeat for the given node.
func (c *Client) Heartbeat(idOrHostname string) error {
	return c.post(fmt.Sprintf("/api/v1/nodes/%s/heartbeat", idOrHostname), nil, nil)
}

// GetDesiredState returns the desired state for the given node.
func (c *Client) GetDesiredState(idOrHostname string) (*apiserver.DesiredState, error) {
	var ds apiserver.DesiredState
	if err := c.get(fmt.Sprintf("/api/v1/nodes/%s/desired-state", idOrHostname), &ds); err != nil {
		return nil, err
	}
	return &ds, nil
}
