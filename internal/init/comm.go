// Package gsinit implements the GoShip Init agent that runs as PID 1 inside project VMs.
package gsinit

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	v1 "github.com/guilhermebr/goship/pkg/api/v1"
)

const (
	// DefaultSerialDevice is the primary virtio-serial device path inside the VM.
	// This path matches the libvirt channel target name "goship.0".
	DefaultSerialDevice = "/dev/virtio-ports/goship.0"
	// LegacySerialDevice is an alternate virtio-serial path used by some guests.
	LegacySerialDevice = "/dev/vport0p1"
	// FallbackSerialDevice is a last-resort serial fallback path.
	FallbackSerialDevice = "/dev/ttyS1"
	// DefaultSerialWaitTimeout is the bounded wait for serial device readiness.
	DefaultSerialWaitTimeout = 30 * time.Second
	// DefaultSerialRetryInterval is the retry interval while waiting for device creation.
	DefaultSerialRetryInterval = 500 * time.Millisecond
)

// CommandHandler is a function that processes an InitCommand and returns an InitResponse.
type CommandHandler func(*v1.InitCommand) *v1.InitResponse

// Communicator handles guest-side communication with the host over a virtio-serial device.
type Communicator struct {
	device   io.ReadWriteCloser
	reader   *bufio.Reader
	mu       sync.Mutex
	handlers map[string]CommandHandler
}

// NewCommunicator opens the serial device and returns a ready communicator.
// It tries the primary device first, then falls back to the secondary.
func NewCommunicator(device string) (*Communicator, error) {
	waitTimeout := durationFromEnv("GOSHIP_SERIAL_WAIT_TIMEOUT", DefaultSerialWaitTimeout)
	retryInterval := durationFromEnv("GOSHIP_SERIAL_RETRY_INTERVAL", DefaultSerialRetryInterval)
	candidates := []string{device}
	if device == DefaultSerialDevice {
		candidates = append(candidates, LegacySerialDevice, FallbackSerialDevice)
	}
	f, err := openDeviceWithRetry(candidates, waitTimeout, retryInterval)
	if err != nil {
		return nil, err
	}
	return newCommunicatorFromReadWriteCloser(f), nil
}

// newCommunicatorFromReadWriteCloser creates a Communicator from an existing io.ReadWriteCloser.
// Used internally and by tests.
func newCommunicatorFromReadWriteCloser(rwc io.ReadWriteCloser) *Communicator {
	return &Communicator{
		device:   rwc,
		reader:   bufio.NewReader(rwc),
		handlers: make(map[string]CommandHandler),
	}
}

// openDeviceWithRetry waits for each candidate serial device and tries them in order.
func openDeviceWithRetry(candidates []string, timeout, retryInterval time.Duration) (*os.File, error) {
	if timeout <= 0 {
		timeout = DefaultSerialWaitTimeout
	}
	if retryInterval <= 0 {
		retryInterval = DefaultSerialRetryInterval
	}

	start := time.Now()
	seen := make(map[string]struct{})
	var attempted []string
	for _, device := range candidates {
		if device == "" {
			continue
		}
		if _, exists := seen[device]; exists {
			continue
		}
		seen[device] = struct{}{}
		if len(attempted) > 0 {
			log.Printf("warn: failed to open serial device %s after %s, trying %s", attempted[len(attempted)-1], timeout, device)
		}
		attempted = append(attempted, device)

		if f, err := tryOpenUntil(device, timeout, retryInterval); err == nil {
			return f, nil
		}
	}

	return nil, fmt.Errorf("failed to open serial device (tried %v, elapsed %s)", attempted, time.Since(start).Round(time.Millisecond))
}

func tryOpenUntil(device string, timeout, retryInterval time.Duration) (*os.File, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error

	for {
		f, err := os.OpenFile(device, os.O_RDWR, 0)
		if err == nil {
			return f, nil
		}
		lastErr = err

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("failed to open %s within %s: %w", device, timeout, lastErr)
		}
		time.Sleep(retryInterval)
	}
}

func durationFromEnv(env string, fallback time.Duration) time.Duration {
	raw := os.Getenv(env)
	if raw == "" {
		return fallback
	}

	if d, err := time.ParseDuration(raw); err == nil {
		return d
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	log.Printf("warn: invalid %s=%q, using default %s", env, raw, fallback)
	return fallback
}

// RegisterHandler registers a handler for the given action.
func (c *Communicator) RegisterHandler(action string, handler CommandHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[action] = handler
}

// Listen reads commands from the serial device, dispatches them to registered
// handlers, and writes responses back. It blocks until the device is closed
// or an unrecoverable error occurs.
func (c *Communicator) Listen() error {
	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			if isTransientDeviceReadError(c.device, err) {
				time.Sleep(200 * time.Millisecond)
				continue
			}
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("failed to read from device: %w", err)
		}

		var cmd v1.InitCommand
		if err := json.Unmarshal(line, &cmd); err != nil {
			log.Printf("warn: invalid JSON: %s", string(line))
			c.sendResponse(&v1.InitResponse{
				Status: v1.StatusError,
				Error:  "invalid JSON",
			})
			continue
		}

		c.mu.Lock()
		handler, ok := c.handlers[cmd.Action]
		c.mu.Unlock()

		var resp *v1.InitResponse
		if !ok {
			resp = &v1.InitResponse{
				Status: v1.StatusError,
				Error:  fmt.Sprintf("unknown action: %s", cmd.Action),
			}
		} else {
			resp = handler(&cmd)
		}

		c.sendResponse(resp)
	}
}

func isTransientDeviceReadError(device io.ReadWriteCloser, err error) bool {
	if err == nil {
		return false
	}
	if err != io.EOF && !errors.Is(err, syscall.EIO) {
		return false
	}

	f, ok := device.(*os.File)
	if !ok {
		return false
	}
	info, statErr := f.Stat()
	if statErr != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// sendResponse marshals the response and writes it to the device.
func (c *Communicator) sendResponse(resp *v1.InitResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("error: failed to marshal response: %v", err)
		return
	}
	data = append(data, '\n')

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.device.Write(data); err != nil {
		log.Printf("error: failed to write response: %v", err)
	}
}

// Close closes the underlying device.
func (c *Communicator) Close() error {
	return c.device.Close()
}
