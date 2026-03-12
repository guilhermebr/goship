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

	// UploadBinary uploads a binary file into the VM for a specific app.
	UploadBinary(
		ctx context.Context,
		instanceID string,
		appName string,
		fileName string,
		reader io.Reader,
		size int64,
		checksum string,
	) error

	// UploadImage uploads a Docker image tarball into the VM and loads it into Docker.
	UploadImage(
		ctx context.Context,
		instanceID string,
		imageRef string,
		reader io.Reader,
		size int64,
		checksum string,
	) error

	// GetAppLogs retrieves log output from an application inside a VM instance.
	GetAppLogs(ctx context.Context, instanceID string, appName string, lines int) (string, error)

	// GetVMLogs retrieves log output from a VM instance (e.g., goship-init, cloud-init).
	// source is a well-known alias (goship-init, cloud-init), logFile is an explicit path.
	GetVMLogs(ctx context.Context, instanceID string, source string, logFile string, lines int) (string, error)

	// StreamLogs streams logs from an application inside a VM instance.
	StreamLogs(ctx context.Context, instanceID string, appName string, follow bool) (io.ReadCloser, error)

	// ExecCommand executes a command inside a VM instance.
	ExecCommand(
		ctx context.Context,
		instanceID string,
		appName string,
		cmd []string,
	) (stdout, stderr string, exitCode int, err error)

	// ResizeInstance updates the VM definition with new resource values from the project.
	// The VM must be stopped before calling this method.
	ResizeInstance(ctx context.Context, instanceID string, project *entities.Project) error

	// LoadInstance registers a persisted instance into the runtime's in-memory state.
	// This is needed when a runtime starts fresh and must recover previously created instances.
	LoadInstance(instance *entities.ProjectInstance)
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
	// RegistryAddr is the host registry address for VM insecure-registries config (e.g., "192.168.122.1:5000")
	RegistryAddr string
	// ProgressWriter, if set, receives boot progress and cloud-init log output
	ProgressWriter io.Writer
}

// DefaultConfig returns the default runtime configuration.
func DefaultConfig() *RuntimeConfig {
	return &RuntimeConfig{
		DataDir:           "~/.goship",
		VMImagePath:       "~/.goship/images/goship-vm.qcow2",
		DefaultCPU:        1,
		DefaultMemoryMB:   512,
		DefaultDiskMB:     8192,
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

// WithRegistryAddr sets the host registry address for VM insecure-registries configuration.
func WithRegistryAddr(addr string) RuntimeOption {
	return func(c *RuntimeConfig) { c.RegistryAddr = addr }
}

// WithProgressWriter sets a writer for boot progress and cloud-init log output.
func WithProgressWriter(w io.Writer) RuntimeOption {
	return func(c *RuntimeConfig) { c.ProgressWriter = w }
}
