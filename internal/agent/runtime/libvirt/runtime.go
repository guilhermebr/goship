package libvirt

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"libvirt.org/go/libvirt"

	"github.com/guilhermebr/goship/internal/agent/runtime"
	v1 "github.com/guilhermebr/goship/pkg/api/v1"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// Ensure Runtime implements the required interfaces at compile time.
var _ runtime.RuntimeWithCapabilities = (*Runtime)(nil)

// Runtime implements runtime.RuntimeWithCapabilities using libvirt.
// It wraps the existing VMManager and VMCommunicator, exposing them
// through the ProjectRuntime interface.
type Runtime struct {
	config       *runtime.RuntimeConfig
	conn         *libvirt.Connect
	capabilities *entities.HostCapabilities
	instances    map[string]*instanceInfo
	mu           sync.RWMutex
	connMu       sync.Mutex
}

// instanceInfo tracks a running VM instance.
type instanceInfo struct {
	instance   *entities.ProjectInstance
	socketPath string
}

// New creates a new libvirt Runtime.
func New(opts ...runtime.RuntimeOption) (*Runtime, error) {
	config := runtime.DefaultConfig()
	for _, opt := range opts {
		opt(config)
	}

	if config.LibvirtURI == "" {
		config.LibvirtURI = "qemu:///system"
	}

	config.DataDir = expandPath(config.DataDir)
	config.VMImagePath = expandPath(config.VMImagePath)
	if config.InitBinaryPath != "" {
		config.InitBinaryPath = expandPath(config.InitBinaryPath)
	}

	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	conn, err := libvirt.NewConnect(config.LibvirtURI)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to libvirt at %s: %w", config.LibvirtURI, err)
	}

	r := &Runtime{
		config:    config,
		conn:      conn,
		instances: make(map[string]*instanceInfo),
	}

	// Discover host capabilities.
	ctx, cancel := context.WithTimeout(context.Background(), config.ConnectionTimeout)
	defer cancel()
	if err := r.RefreshCapabilities(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to discover capabilities: %w", err)
	}

	if !r.capabilities.KVMAvailable {
		fmt.Fprintf(os.Stderr, "Warning: KVM not available, VM performance will be degraded\n")
		config.EnableKVM = false
	}

	return r, nil
}

// getConnection returns the libvirt connection, reconnecting if needed.
func (r *Runtime) getConnection() (*libvirt.Connect, error) {
	r.connMu.Lock()
	defer r.connMu.Unlock()

	if r.conn != nil {
		alive, err := r.conn.IsAlive()
		if err == nil && alive {
			return r.conn, nil
		}
		r.conn.Close()
	}

	conn, err := libvirt.NewConnect(r.config.LibvirtURI)
	if err != nil {
		return nil, fmt.Errorf("failed to reconnect to libvirt: %w", err)
	}
	r.conn = conn
	return conn, nil
}

// CreateInstance creates a new VM instance for a project.
func (r *Runtime) CreateInstance(ctx context.Context, project *entities.Project) (*entities.ProjectInstance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check for existing instance.
	for _, info := range r.instances {
		if info.instance.ProjectID == project.ID {
			return nil, fmt.Errorf("instance already exists for project %s", project.ID)
		}
	}

	instanceID := uuid.New().String()

	// Resolve resource defaults.
	cpus := r.config.DefaultCPU
	if project.Resources.CPU > 0 {
		cpus = int(project.Resources.CPU)
	}
	memoryMB := r.config.DefaultMemoryMB
	if project.Resources.MemoryMB > 0 {
		memoryMB = project.Resources.MemoryMB
	}
	diskMB := r.config.DefaultDiskMB
	if project.Resources.DiskMB > 0 {
		diskMB = project.Resources.DiskMB
	}

	// Read SSH key if configured.
	var sshKey string
	if r.config.SSHKeyPath != "" {
		keyBytes, err := os.ReadFile(expandPath(r.config.SSHKeyPath))
		if err == nil {
			sshKey = strings.TrimSpace(string(keyBytes))
		}
	}

	// Create VM via VMManager.
	mgr := &VMManager{conn: r.conn, dataDir: r.config.DataDir}

	vmInfo, err := mgr.Create(CreateVMOptions{
		Name:           project.Name,
		BaseImage:      r.config.VMImagePath,
		MemoryMB:       memoryMB,
		DiskMB:         diskMB,
		CPUs:           cpus,
		EnableKVM:      r.config.EnableKVM,
		SecurityNone:   true,
		NetworkType:    r.config.NetworkType,
		NetworkSource:  r.config.NetworkSource,
		Hostname:       project.Name,
		SSHKey:         sshKey,
		InitBinaryPath: r.config.InitBinaryPath,
		ProvisionGuest: r.config.ProvisionGuest,
		InstallDocker:  r.config.InstallDocker,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create VM: %w", err)
	}

	socketPath := filepath.Join(r.config.DataDir, "vms", project.Name, "goship.sock")

	instance := &entities.ProjectInstance{
		ID:         instanceID,
		ProjectID:  project.ID,
		NodeID:     "local",
		State:      entities.InstanceStateStarting,
		DomainName: vmInfo.Domain,
		DomainUUID: vmInfo.UUID,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	r.instances[instanceID] = &instanceInfo{
		instance:   instance,
		socketPath: socketPath,
	}

	// Wait for VM to boot and become ready.
	r.mu.Unlock()
	if err := r.waitReady(ctx, instanceID); err != nil {
		// Cleanup on failure.
		r.mu.Lock()
		delete(r.instances, instanceID)
		r.mu.Unlock()
		mgr.Destroy(project.Name, false)
		return nil, fmt.Errorf("VM failed to become ready: %w", err)
	}
	r.mu.Lock()

	return instance, nil
}

// LoadInstance registers a persisted instance into the in-memory map.
// This is needed because each CLI invocation creates a fresh Runtime with
// an empty instance map. Commands like delete must call this first.
func (r *Runtime) LoadInstance(instance *entities.ProjectInstance) {
	r.mu.Lock()
	defer r.mu.Unlock()

	vmName := strings.TrimPrefix(instance.DomainName, DomainPrefix)
	socketPath := filepath.Join(r.config.DataDir, "vms", vmName, "goship.sock")

	r.instances[instance.ID] = &instanceInfo{
		instance:   instance,
		socketPath: socketPath,
	}
}

// waitReady polls the VM until it boots and the goship-init agent responds.
// It sends a ping first, then streams cloud-init logs while waiting for status.
func (r *Runtime) waitReady(ctx context.Context, instanceID string) error {
	r.mu.RLock()
	info, ok := r.instances[instanceID]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	pw := r.config.ProgressWriter
	progress := func(format string, args ...any) {
		if pw != nil {
			fmt.Fprintf(pw, format+"\n", args...)
		}
	}

	progress("Waiting for VM to boot...")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	domainReady := false
	agentReady := false
	logOffset := 0 // bytes of cloud-init log already printed

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for VM to become ready")
		case <-ticker.C:
		}

		// Check libvirt domain is running.
		conn, err := r.getConnection()
		if err != nil {
			continue
		}
		domain, err := conn.LookupDomainByName(info.instance.DomainName)
		if err != nil {
			continue
		}
		state, _, err := domain.GetState()
		domain.Free()
		if err != nil || state != libvirt.DOMAIN_RUNNING {
			continue
		}

		if !domainReady {
			domainReady = true
			progress("Waiting for agent...")
		}

		// Try connecting to the virtio-serial socket.
		comm, err := NewVMCommunicator(info.socketPath)
		if err != nil {
			continue
		}

		// Send ping first (fast healthcheck).
		if err := comm.Ping(ctx); err != nil {
			comm.Close()
			continue
		}

		if !agentReady {
			agentReady = true
			progress("Agent connected, streaming cloud-init logs...")
		}

		// Fetch cloud-init logs and print new lines.
		logResp, err := comm.SendCommand(ctx, &v1.InitCommand{
			Action:  v1.ActionLogs,
			LogFile: "/var/log/cloud-init-output.log",
		})
		if err == nil && logResp.Status == v1.StatusOK && len(logResp.Logs) > logOffset {
			newContent := logResp.Logs[logOffset:]
			// Print each new line with a prefix.
			lines := strings.Split(newContent, "\n")
			for _, line := range lines {
				if line != "" {
					progress("  %s", line)
				}
			}
			logOffset = len(logResp.Logs)
		}

		// Send status to get VM info (IP, hostname).
		resp, err := comm.SendCommand(ctx, &v1.InitCommand{Action: v1.ActionStatus})
		comm.Close()
		if err != nil {
			continue
		}

		// Update instance with IP and state.
		r.mu.Lock()
		info.instance.State = entities.InstanceStateRunning
		info.instance.UpdatedAt = time.Now()
		if resp.VMInfo != nil && len(resp.VMInfo.IPAddresses) > 0 {
			info.instance.IPAddress = resp.VMInfo.IPAddresses[0]
		}
		r.mu.Unlock()

		progress("VM ready (IP: %s)", info.instance.IPAddress)
		return nil
	}
}

// DestroyInstance stops and removes a VM instance.
func (r *Runtime) DestroyInstance(ctx context.Context, instanceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	info, ok := r.instances[instanceID]
	if !ok {
		return nil // Instance already destroyed.
	}

	// Extract project name from domain name.
	vmName := strings.TrimPrefix(info.instance.DomainName, DomainPrefix)

	mgr := &VMManager{conn: r.conn, dataDir: r.config.DataDir}
	if _, err := mgr.Destroy(vmName, false); err != nil {
		return fmt.Errorf("failed to destroy VM: %w", err)
	}

	delete(r.instances, instanceID)
	return nil
}

// StopInstance gracefully shuts down a VM instance via ACPI.
func (r *Runtime) StopInstance(ctx context.Context, instanceID string) error {
	r.mu.RLock()
	info, ok := r.instances[instanceID]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	conn, err := r.getConnection()
	if err != nil {
		return fmt.Errorf("failed to get libvirt connection: %w", err)
	}

	domain, err := conn.LookupDomainByName(info.instance.DomainName)
	if err != nil {
		return fmt.Errorf("domain not found: %w", err)
	}
	defer domain.Free()

	if err := domain.Shutdown(); err != nil {
		return fmt.Errorf("failed to shutdown domain: %w", err)
	}

	r.mu.Lock()
	info.instance.State = entities.InstanceStateStopping
	info.instance.UpdatedAt = time.Now()
	r.mu.Unlock()

	return nil
}

// StartInstance starts a previously stopped VM instance.
func (r *Runtime) StartInstance(ctx context.Context, instanceID string) error {
	r.mu.RLock()
	info, ok := r.instances[instanceID]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	conn, err := r.getConnection()
	if err != nil {
		return fmt.Errorf("failed to get libvirt connection: %w", err)
	}

	domain, err := conn.LookupDomainByName(info.instance.DomainName)
	if err != nil {
		return fmt.Errorf("domain not found: %w", err)
	}
	defer domain.Free()

	if err := domain.Create(); err != nil {
		return fmt.Errorf("failed to start domain: %w", err)
	}

	r.mu.Lock()
	info.instance.State = entities.InstanceStateRunning
	info.instance.UpdatedAt = time.Now()
	r.mu.Unlock()

	return nil
}

// DeployApp deploys an application inside a VM instance via virtio-serial.
func (r *Runtime) DeployApp(ctx context.Context, instanceID string, app *entities.AppSpec) error {
	r.mu.RLock()
	info, ok := r.instances[instanceID]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	comm, err := NewVMCommunicator(info.socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to VM: %w", err)
	}
	defer comm.Close()

	resp, err := comm.SendCommand(ctx, &v1.InitCommand{
		Action: v1.ActionDeploy,
		App:    app,
	})
	if err != nil {
		return fmt.Errorf("deploy command failed: %w", err)
	}
	if resp.Status != v1.StatusOK {
		return fmt.Errorf("deploy failed: %s", resp.Error)
	}
	return nil
}

// StopApp stops a running application inside a VM instance via virtio-serial.
func (r *Runtime) StopApp(ctx context.Context, instanceID string, appName string) error {
	r.mu.RLock()
	info, ok := r.instances[instanceID]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	comm, err := NewVMCommunicator(info.socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to VM: %w", err)
	}
	defer comm.Close()

	resp, err := comm.SendCommand(ctx, &v1.InitCommand{
		Action:  v1.ActionStop,
		AppName: appName,
	})
	if err != nil {
		return fmt.Errorf("stop command failed: %w", err)
	}
	if resp.Status != v1.StatusOK {
		return fmt.Errorf("stop failed: %s", resp.Error)
	}
	return nil
}

// RemoveApp removes an application from a VM instance via virtio-serial.
func (r *Runtime) RemoveApp(ctx context.Context, instanceID string, appName string) error {
	r.mu.RLock()
	info, ok := r.instances[instanceID]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	comm, err := NewVMCommunicator(info.socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to VM: %w", err)
	}
	defer comm.Close()

	resp, err := comm.SendCommand(ctx, &v1.InitCommand{
		Action:  v1.ActionRemove,
		AppName: appName,
	})
	if err != nil {
		return fmt.Errorf("remove command failed: %w", err)
	}
	if resp.Status != v1.StatusOK {
		return fmt.Errorf("remove failed: %s", resp.Error)
	}
	return nil
}

// GetInstanceStatus returns the current status of a VM instance.
func (r *Runtime) GetInstanceStatus(ctx context.Context, instanceID string) (*entities.ProjectInstance, error) {
	r.mu.RLock()
	info, ok := r.instances[instanceID]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}

	// Try to refresh state from libvirt.
	conn, err := r.getConnection()
	if err == nil {
		domain, err := conn.LookupDomainByName(info.instance.DomainName)
		if err == nil {
			state, _, err := domain.GetState()
			if err == nil {
				info.instance.State = mapLibvirtState(state)
			}
			domain.Free()
		}
	}

	info.instance.UpdatedAt = time.Now()
	return info.instance, nil
}

// ListInstances returns all VM instances managed by this runtime.
func (r *Runtime) ListInstances(ctx context.Context) ([]*entities.ProjectInstance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	instances := make([]*entities.ProjectInstance, 0, len(r.instances))
	for _, info := range r.instances {
		instances = append(instances, info.instance)
	}
	return instances, nil
}

// StreamLogs streams logs from an application inside a VM instance.
func (r *Runtime) StreamLogs(ctx context.Context, instanceID string, appName string, follow bool) (io.ReadCloser, error) {
	return nil, fmt.Errorf("log streaming not implemented")
}

// ExecCommand executes a command inside a VM instance.
func (r *Runtime) ExecCommand(ctx context.Context, instanceID string, appName string, cmd []string) (stdout, stderr string, exitCode int, err error) {
	return "", "", -1, fmt.Errorf("exec not implemented")
}

// GetHostCapabilities returns host capabilities.
func (r *Runtime) GetHostCapabilities(ctx context.Context) (*entities.HostCapabilities, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.capabilities == nil {
		return nil, fmt.Errorf("capabilities not yet discovered")
	}
	return r.capabilities, nil
}

// RefreshCapabilities refreshes host capabilities from libvirt.
func (r *Runtime) RefreshCapabilities(ctx context.Context) error {
	conn, err := r.getConnection()
	if err != nil {
		return err
	}

	caps, err := DiscoverCapabilities(conn)
	if err != nil {
		return err
	}

	r.mu.Lock()
	r.capabilities = caps
	r.mu.Unlock()
	return nil
}

// Close closes the runtime and its libvirt connection.
func (r *Runtime) Close() error {
	r.connMu.Lock()
	defer r.connMu.Unlock()

	if r.conn != nil {
		_, err := r.conn.Close()
		r.conn = nil
		return err
	}
	return nil
}

// mapLibvirtState maps libvirt domain state to entities.InstanceState.
func mapLibvirtState(state libvirt.DomainState) entities.InstanceState {
	switch state {
	case libvirt.DOMAIN_RUNNING, libvirt.DOMAIN_BLOCKED:
		return entities.InstanceStateRunning
	case libvirt.DOMAIN_PAUSED, libvirt.DOMAIN_SHUTOFF, libvirt.DOMAIN_PMSUSPENDED:
		return entities.InstanceStateStopped
	case libvirt.DOMAIN_SHUTDOWN:
		return entities.InstanceStateStopping
	case libvirt.DOMAIN_CRASHED:
		return entities.InstanceStateFailed
	default:
		return entities.InstanceStatePending
	}
}

// expandPath expands ~ to the user's home directory.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
