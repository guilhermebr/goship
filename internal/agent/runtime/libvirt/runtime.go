package libvirt

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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

	if err := os.MkdirAll(config.DataDir, 0o755); err != nil {
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
		_, _ = conn.Close()
		return nil, fmt.Errorf("failed to discover capabilities: %w", err)
	}

	if !r.capabilities.KVMAvailable {
		fmt.Fprint(os.Stderr, "Warning: KVM not available, using QEMU TCG (software emulation)\n")
		fmt.Fprint(os.Stderr, "  CPU mode: host-model (host-passthrough requires KVM)\n")
		fmt.Fprint(os.Stderr, "  Performance will be significantly degraded\n")
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
		_, _ = r.conn.Close()
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
	// Unlock while waiting so other goroutines can proceed, then re-lock
	// before returning so the deferred Unlock is balanced.
	r.mu.Unlock()
	waitErr := r.waitReady(ctx, instanceID, true)
	r.mu.Lock()

	if waitErr != nil {
		// Cleanup on failure.
		delete(r.instances, instanceID)
		// Unlock before potentially slow Destroy, then re-lock for the defer.
		r.mu.Unlock()
		_, _ = mgr.Destroy(project.Name, false)
		r.mu.Lock()
		return nil, fmt.Errorf("VM failed to become ready: %w", waitErr)
	}

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
// It sends a ping first, then optionally streams cloud-init logs while waiting for status.
// Set streamCloudInitLogs to true on first boot (create) when cloud-init runs,
// and false on start/restart where the log contains stale content.
//
//nolint:funlen,gocognit,gocyclo,cyclop // VM boot polling requires multiple sequential checks
func (r *Runtime) waitReady(ctx context.Context, instanceID string, streamCloudInitLogs bool) error {
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
			return errors.New("timed out waiting for VM to become ready")
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
		_ = domain.Free()
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
		if pingErr := comm.Ping(ctx); pingErr != nil {
			_ = comm.Close()
			continue
		}

		if !agentReady {
			agentReady = true
			if streamCloudInitLogs {
				progress("Agent connected, streaming cloud-init logs...")
			} else {
				progress("Agent connected")
			}
		}

		// Fetch cloud-init logs and print new lines (only on first boot).
		if streamCloudInitLogs {
			logResp, logErr := comm.SendCommand(ctx, &v1.InitCommand{
				Action:  v1.ActionLogs,
				LogFile: "/var/log/cloud-init-output.log",
			})
			if logErr == nil && logResp.Status == v1.StatusOK && len(logResp.Logs) > logOffset {
				newContent := logResp.Logs[logOffset:]
				// Print each new line with a prefix.
				lines := strings.SplitSeq(newContent, "\n")
				for line := range lines {
					if line != "" {
						progress("  %s", line)
					}
				}
				logOffset = len(logResp.Logs)
			}
		}

		// Send status to get VM info (IP, hostname).
		resp, err := comm.SendCommand(ctx, &v1.InitCommand{Action: v1.ActionStatus})
		_ = comm.Close()
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
	defer func() { _ = domain.Free() }()

	if err := domain.Shutdown(); err != nil {
		return fmt.Errorf("failed to shutdown domain: %w", err)
	}

	r.mu.Lock()
	info.instance.State = entities.InstanceStateStopping
	info.instance.UpdatedAt = time.Now()
	r.mu.Unlock()

	return nil
}

// StartInstance starts a previously stopped VM instance and waits for it to boot.
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
	defer func() { _ = domain.Free() }()

	if err := domain.Create(); err != nil {
		return fmt.Errorf("failed to start domain: %w", err)
	}

	r.mu.Lock()
	info.instance.State = entities.InstanceStateStarting
	info.instance.UpdatedAt = time.Now()
	r.mu.Unlock()

	// Wait for VM to boot and agent to respond (no cloud-init streaming on restart).
	if err := r.waitReady(ctx, instanceID, false); err != nil {
		return fmt.Errorf("VM failed to become ready: %w", err)
	}

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
	defer func() { _ = comm.Close() }()

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
	defer func() { _ = comm.Close() }()

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
	defer func() { _ = comm.Close() }()

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
		domain, domainErr := conn.LookupDomainByName(info.instance.DomainName)
		if domainErr == nil {
			state, _, stateErr := domain.GetState()
			if stateErr == nil {
				info.instance.State = mapLibvirtState(state)
			}
			_ = domain.Free()
		} else if errors.Is(domainErr, libvirt.ERR_NO_DOMAIN) {
			// Domain no longer exists in libvirt (destroyed externally or host crash).
			info.instance.State = entities.InstanceStateStopped
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

const uploadChunkSize = 512 * 1024 // 512KB

// UploadBinary uploads a binary file into the VM for a specific app via virtio-serial.
func (r *Runtime) UploadBinary(
	ctx context.Context,
	instanceID string,
	appName string,
	fileName string,
	reader io.Reader,
	size int64,
	checksum string,
) error {
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
	defer func() { _ = comm.Close() }()

	// Phase: begin
	resp, err := comm.SendCommand(ctx, &v1.InitCommand{
		Action:   v1.ActionUploadBinary,
		Phase:    "begin",
		AppName:  appName,
		FileName: fileName,
		Size:     size,
		Checksum: checksum,
	})
	if err != nil {
		return fmt.Errorf("upload begin failed: %w", err)
	}
	if resp.Status != v1.StatusOK {
		return fmt.Errorf("upload begin error: %s", resp.Error)
	}

	// Phase: data (chunked transfer)
	buf := make([]byte, uploadChunkSize)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			encoded := base64.StdEncoding.EncodeToString(buf[:n])
			dataResp, sendErr := comm.SendCommand(ctx, &v1.InitCommand{
				Action: v1.ActionUploadBinary,
				Phase:  "data",
				Data:   encoded,
			})
			if sendErr != nil {
				return fmt.Errorf("upload data failed: %w", sendErr)
			}
			if dataResp.Status != v1.StatusOK {
				return fmt.Errorf("upload data error: %s", dataResp.Error)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("failed to read binary: %w", readErr)
		}
	}

	// Phase: finish
	resp, err = comm.SendCommand(ctx, &v1.InitCommand{
		Action: v1.ActionUploadBinary,
		Phase:  "finish",
	})
	if err != nil {
		return fmt.Errorf("upload finish failed: %w", err)
	}
	if resp.Status != v1.StatusOK {
		return fmt.Errorf("upload finish error: %s", resp.Error)
	}

	return nil
}

// UploadImage uploads a Docker image tarball into the VM via virtio-serial.
func (r *Runtime) UploadImage(
	ctx context.Context,
	instanceID string,
	imageRef string,
	reader io.Reader,
	size int64,
	checksum string,
) error {
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
	defer func() { _ = comm.Close() }()

	// Phase: begin
	resp, err := comm.SendCommand(ctx, &v1.InitCommand{
		Action:   v1.ActionUploadImage,
		Phase:    "begin",
		ImageRef: imageRef,
		Size:     size,
		Checksum: checksum,
	})
	if err != nil {
		return fmt.Errorf("image upload begin failed: %w", err)
	}
	if resp.Status != v1.StatusOK {
		return fmt.Errorf("image upload begin error: %s", resp.Error)
	}

	// Phase: data (chunked transfer)
	buf := make([]byte, uploadChunkSize)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			encoded := base64.StdEncoding.EncodeToString(buf[:n])
			dataResp, sendErr := comm.SendCommand(ctx, &v1.InitCommand{
				Action: v1.ActionUploadImage,
				Phase:  "data",
				Data:   encoded,
			})
			if sendErr != nil {
				return fmt.Errorf("image upload data failed: %w", sendErr)
			}
			if dataResp.Status != v1.StatusOK {
				return fmt.Errorf("image upload data error: %s", dataResp.Error)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("failed to read image data: %w", readErr)
		}
	}

	// Phase: finish
	resp, err = comm.SendCommand(ctx, &v1.InitCommand{
		Action: v1.ActionUploadImage,
		Phase:  "finish",
	})
	if err != nil {
		return fmt.Errorf("image upload finish failed: %w", err)
	}
	if resp.Status != v1.StatusOK {
		return fmt.Errorf("image upload finish error: %s", resp.Error)
	}

	return nil
}

// GetAppLogs retrieves log output from an application inside a VM instance.
func (r *Runtime) GetAppLogs(ctx context.Context, instanceID string, appName string, lines int) (string, error) {
	r.mu.RLock()
	info, ok := r.instances[instanceID]
	r.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("instance not found: %s", instanceID)
	}

	comm, err := NewVMCommunicator(info.socketPath)
	if err != nil {
		return "", fmt.Errorf("failed to connect to VM: %w", err)
	}
	defer func() { _ = comm.Close() }()

	logFile := fmt.Sprintf("/var/log/goship-%s.log", appName)
	resp, err := comm.SendCommand(ctx, &v1.InitCommand{
		Action:  v1.ActionLogs,
		LogFile: logFile,
		Lines:   lines,
	})
	if err != nil {
		return "", fmt.Errorf("logs command failed: %w", err)
	}
	if resp.Status != v1.StatusOK {
		return "", fmt.Errorf("logs error: %s", resp.Error)
	}

	return resp.Logs, nil
}

// StreamLogs streams logs from an application inside a VM instance.
func (r *Runtime) StreamLogs(
	ctx context.Context,
	instanceID string,
	appName string,
	follow bool,
) (io.ReadCloser, error) {
	return nil, errors.New("log streaming not implemented")
}

// ExecCommand executes a command inside a VM instance.
//
//nolint:revive // Four return values required by interface
func (r *Runtime) ExecCommand(
	ctx context.Context,
	instanceID string,
	appName string,
	cmd []string,
) (stdout, stderr string, exitCode int, err error) {
	return "", "", -1, errors.New("exec not implemented")
}

// ResizeInstance updates the VM definition with new resource values from the project.
// The VM must be stopped (shut off) before calling this method.
//
//nolint:funlen // VM resize requires regenerating complete domain XML
func (r *Runtime) ResizeInstance(ctx context.Context, instanceID string, project *entities.Project) error {
	r.mu.RLock()
	info, ok := r.instances[instanceID]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	// Verify the domain is shut off.
	conn, err := r.getConnection()
	if err != nil {
		return fmt.Errorf("failed to get libvirt connection: %w", err)
	}

	domain, err := conn.LookupDomainByName(info.instance.DomainName)
	if err != nil {
		return fmt.Errorf("domain not found: %w", err)
	}
	defer func() { _ = domain.Free() }()

	state, _, err := domain.GetState()
	if err != nil {
		return fmt.Errorf("failed to get domain state: %w", err)
	}
	if state != libvirt.DOMAIN_SHUTOFF {
		return fmt.Errorf("VM must be stopped before resizing (current state: %s)", FormatVMState(state))
	}

	// Resolve resource values (same defaults logic as CreateInstance).
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

	// Derive paths from the domain name.
	vmName := strings.TrimPrefix(info.instance.DomainName, DomainPrefix)
	disk := diskPath(r.config.DataDir, vmName)
	dir := vmDir(r.config.DataDir, vmName)
	socketPath := filepath.Join(dir, "goship.sock")

	// Build the cloud-init CDROMs list (preserve existing cloud-init ISO if present).
	var cdroms []CDROMDevice
	isoPath := filepath.Join(dir, "cloud-init.iso")
	if _, statErr := os.Stat(isoPath); statErr == nil {
		cdroms = append(cdroms, CDROMDevice{Path: isoPath, Format: "raw"})
	}

	// Regenerate domain XML with updated resources.
	config := &DomainConfig{
		Name:         info.instance.DomainName,
		UUID:         info.instance.DomainUUID,
		MemoryMB:     memoryMB,
		EnableKVM:    r.config.EnableKVM,
		SecurityNone: true,
		DACLabel:     fmt.Sprintf("+%d:+%d", os.Getuid(), os.Getgid()),
		CPU: entities.CPUTopology{
			Sockets: 1,
			Cores:   cpus,
			Threads: 1,
		},
		Disks: []entities.DiskDevice{
			{
				Path:   disk,
				Format: "qcow2",
				Bus:    "virtio",
			},
		},
		CDROMs: cdroms,
		Networks: []entities.NetworkDevice{
			{
				Type:   r.config.NetworkType,
				Source: r.config.NetworkSource,
				Model:  "virtio",
			},
		},
		Serials: []entities.SerialDevice{
			{
				Type:       "virtio-serial",
				SocketPath: socketPath,
				PortName:   "goship.0",
			},
		},
	}

	domainXML, err := GenerateDomainXML(config)
	if err != nil {
		return fmt.Errorf("failed to generate domain XML: %w", err)
	}

	// Redefine the domain with updated XML.
	newDomain, err := conn.DomainDefineXML(domainXML)
	if err != nil {
		return fmt.Errorf("failed to redefine domain: %w", err)
	}
	_ = newDomain.Free()

	// Resize disk if it grew.
	if diskMB > 0 {
		size := fmt.Sprintf("%dM", diskMB)
		cmd := exec.Command("qemu-img", "resize", disk, size)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to resize disk to %s: %w\n%s", size, err, string(out))
		}
	}

	return nil
}

// GetHostCapabilities returns host capabilities.
func (r *Runtime) GetHostCapabilities(ctx context.Context) (*entities.HostCapabilities, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.capabilities == nil {
		return nil, errors.New("capabilities not yet discovered")
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
