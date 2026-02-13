// Package runtime defines the ProjectRuntime interface and related types.
// This interface abstracts VM lifecycle management, allowing different
// runtime backends (QEMU, Kata, Firecracker) to be plugged in.
package runtime

import (
	"context"
	"io"
	"time"

	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// ProjectRuntime defines the interface for managing project VMs.
// All runtime-specific logic must implement this interface.
type ProjectRuntime interface {
	// CreateInstance creates a new VM instance for a project.
	CreateInstance(ctx context.Context, project *entities.Project) (*entities.ProjectInstance, error)

	// DestroyInstance stops and removes a VM instance.
	// This should be idempotent - destroying a non-existent instance should not error.
	DestroyInstance(ctx context.Context, instanceID string) error

	// StopInstance gracefully shuts down a VM instance (ACPI shutdown).
	StopInstance(ctx context.Context, instanceID string) error

	// StartInstance starts a previously stopped VM instance.
	StartInstance(ctx context.Context, instanceID string) error

	// DeployApp deploys an application inside a VM instance.
	DeployApp(ctx context.Context, instanceID string, app *entities.AppSpec) error

	// StopApp stops a running application inside a VM instance.
	StopApp(ctx context.Context, instanceID string, appName string) error

	// RemoveApp removes an application from a VM instance.
	RemoveApp(ctx context.Context, instanceID string, appName string) error

	// GetInstanceStatus returns the current status of a VM instance.
	GetInstanceStatus(ctx context.Context, instanceID string) (*entities.ProjectInstance, error)

	// ListInstances returns all VM instances managed by this runtime.
	ListInstances(ctx context.Context) ([]*entities.ProjectInstance, error)

	// StreamLogs streams logs from an application inside a VM instance.
	StreamLogs(ctx context.Context, instanceID string, appName string, follow bool) (io.ReadCloser, error)

	// ExecCommand executes a command inside a VM instance.
	ExecCommand(ctx context.Context, instanceID string, appName string, cmd []string) (stdout, stderr string, exitCode int, err error)
}

// CapabilityProvider defines methods for discovering host capabilities.
type CapabilityProvider interface {
	// GetHostCapabilities returns the host's virtualization capabilities.
	GetHostCapabilities(ctx context.Context) (*entities.HostCapabilities, error)

	// RefreshCapabilities forces a refresh of cached capabilities.
	RefreshCapabilities(ctx context.Context) error
}

// RuntimeWithCapabilities combines ProjectRuntime with capability discovery.
type RuntimeWithCapabilities interface {
	ProjectRuntime
	CapabilityProvider
}

// RuntimeConfig contains configuration for a runtime backend.
type RuntimeConfig struct {
	// DataDir is the directory for runtime data (images, state, etc.)
	DataDir string
	// VMImagePath is the path to the base VM image
	VMImagePath string
	// DefaultCPU is the default number of CPU cores for VMs
	DefaultCPU int
	// DefaultMemoryMB is the default memory in MB for VMs
	DefaultMemoryMB int64
	// DefaultDiskMB is the default disk size in MB for VMs
	DefaultDiskMB int64
	// NetworkType is the network type (network, bridge, user)
	NetworkType string
	// NetworkSource is the network source (bridge name or network name)
	NetworkSource string
	// EnableKVM enables KVM acceleration
	EnableKVM bool
	// SSHKeyPath is the path to the SSH public key
	SSHKeyPath string
	// LibvirtURI is the libvirt connection URI (e.g., qemu:///system)
	LibvirtURI string
	// ConnectionTimeout is the timeout for runtime operations
	ConnectionTimeout time.Duration
	// InitBinaryPath is the path to the goship-init binary
	InitBinaryPath string
	// ProvisionGuest enables guest disk provisioning via virt-customize
	ProvisionGuest bool
	// InstallDocker installs Docker inside the VM during provisioning
	InstallDocker bool
}

// DefaultConfig returns the default runtime configuration.
func DefaultConfig() *RuntimeConfig {
	return &RuntimeConfig{
		DataDir:           "~/.goship",
		VMImagePath:       "~/.goship/images/goship-vm.qcow2",
		DefaultCPU:        1,
		DefaultMemoryMB:   512,
		DefaultDiskMB:     4096,
		NetworkType:       "network",
		NetworkSource:     "default",
		EnableKVM:         true,
		LibvirtURI:        "qemu:///system",
		ConnectionTimeout: 30 * time.Second,
		ProvisionGuest:    true,
		InstallDocker:     true,
	}
}

// RuntimeOption is a function that configures a RuntimeConfig.
type RuntimeOption func(*RuntimeConfig)

// WithDataDir sets the data directory.
func WithDataDir(dir string) RuntimeOption {
	return func(c *RuntimeConfig) { c.DataDir = dir }
}

// WithVMImage sets the VM image path.
func WithVMImage(path string) RuntimeOption {
	return func(c *RuntimeConfig) { c.VMImagePath = path }
}

// WithDefaultResources sets default resource limits.
func WithDefaultResources(cpu int, memoryMB, diskMB int64) RuntimeOption {
	return func(c *RuntimeConfig) {
		c.DefaultCPU = cpu
		c.DefaultMemoryMB = memoryMB
		c.DefaultDiskMB = diskMB
	}
}

// WithNetwork sets the network type and source.
func WithNetwork(netType, source string) RuntimeOption {
	return func(c *RuntimeConfig) {
		c.NetworkType = netType
		c.NetworkSource = source
	}
}

// WithKVM enables or disables KVM acceleration.
func WithKVM(enable bool) RuntimeOption {
	return func(c *RuntimeConfig) { c.EnableKVM = enable }
}

// WithSSHKey sets the SSH public key path.
func WithSSHKey(path string) RuntimeOption {
	return func(c *RuntimeConfig) { c.SSHKeyPath = path }
}

// WithLibvirtURI sets the libvirt connection URI.
func WithLibvirtURI(uri string) RuntimeOption {
	return func(c *RuntimeConfig) { c.LibvirtURI = uri }
}

// WithInitBinary sets the goship-init binary path.
func WithInitBinary(path string) RuntimeOption {
	return func(c *RuntimeConfig) { c.InitBinaryPath = path }
}

// WithProvisionGuest enables or disables guest disk provisioning.
func WithProvisionGuest(enable bool) RuntimeOption {
	return func(c *RuntimeConfig) { c.ProvisionGuest = enable }
}

// WithInstallDocker enables or disables Docker installation during provisioning.
func WithInstallDocker(enable bool) RuntimeOption {
	return func(c *RuntimeConfig) { c.InstallDocker = enable }
}
