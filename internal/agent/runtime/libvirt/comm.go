package libvirt

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	v1 "github.com/guilhermebr/goship/pkg/api/v1"
)

const defaultTimeout = 30 * time.Second

// VMCommunicator handles host-side communication with a GoShip Init agent
// running inside a VM, over a virtio-serial Unix socket.
type VMCommunicator struct {
	socketPath string
	conn       net.Conn
	reader     *bufio.Reader
	mu         sync.Mutex
}

// NewVMCommunicator dials the Unix socket at socketPath and returns a ready communicator.
func NewVMCommunicator(socketPath string) (*VMCommunicator, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to VM socket %s: %w", socketPath, err)
	}

	return &VMCommunicator{
		socketPath: socketPath,
		conn:       conn,
		reader:     bufio.NewReader(conn),
	}, nil
}

// SendCommand sends a command to the VM agent and reads the response.
// The context controls the deadline; if no deadline is set, a default of 30s is used.
func (c *VMCommunicator) SendCommand(ctx context.Context, cmd *v1.InitCommand) (*v1.InitResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Set deadline from context, or use default.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(defaultTimeout)
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("failed to set deadline: %w", err)
	}

	// Encode and write newline-delimited JSON.
	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command: %w", err)
	}
	data = append(data, '\n')

	if _, err := c.conn.Write(data); err != nil {
		return nil, fmt.Errorf("failed to write command: %w", err)
	}

	// Read response line.
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp v1.InitResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &resp, nil
}

// Ping sends a ping command and verifies the agent responds with StatusOK.
func (c *VMCommunicator) Ping(ctx context.Context) error {
	resp, err := c.SendCommand(ctx, &v1.InitCommand{Action: v1.ActionPing})
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}
	if resp.Status != v1.StatusOK {
		return fmt.Errorf("ping returned status %q: %s", resp.Status, resp.Error)
	}
	return nil
}

// Close closes the underlying connection.
func (c *VMCommunicator) Close() error {
	return c.conn.Close()
}
