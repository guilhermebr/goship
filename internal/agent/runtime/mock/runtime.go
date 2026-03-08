// Package mock provides a thread-safe mock implementation of runtime.ProjectRuntime
// for use in integration and unit tests.
package mock

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// Call records a single method invocation on the mock runtime.
type Call struct {
	Method string
	Args   []any
}

// Runtime is a configurable mock implementing runtime.ProjectRuntime.
// All methods record calls and delegate to configurable function fields.
type Runtime struct {
	mu    sync.Mutex
	Calls []Call

	// LoadedInstances tracks instances registered via LoadInstance.
	LoadedInstances []*entities.ProjectInstance

	// stopped is set by StopInstance so GetInstanceStatus can return stopped.
	stopped bool

	// Per-method function fields — set these to control return values.
	CreateInstanceFn    func(ctx context.Context, project *entities.Project) (*entities.ProjectInstance, error)
	DestroyInstanceFn   func(ctx context.Context, instanceID string) error
	StopInstanceFn      func(ctx context.Context, instanceID string) error
	StartInstanceFn     func(ctx context.Context, instanceID string) error
	DeployAppFn         func(ctx context.Context, instanceID string, app *entities.AppSpec) error
	StopAppFn           func(ctx context.Context, instanceID string, appName string) error
	RemoveAppFn         func(ctx context.Context, instanceID string, appName string) error
	GetInstanceStatusFn func(ctx context.Context, instanceID string) (*entities.ProjectInstance, error)
	ListInstancesFn     func(ctx context.Context) ([]*entities.ProjectInstance, error)
	GetAppLogsFn        func(ctx context.Context, instanceID, appName string, lines int) (string, error)
	GetVMLogsFn         func(ctx context.Context, instanceID, source, logFile string, lines int) (string, error)
	StreamLogsFn        func(ctx context.Context, instanceID, appName string, follow bool) (io.ReadCloser, error)
	ExecCommandFn       func(ctx context.Context, instanceID, appName string, cmd []string) (string, string, int, error)
	ResizeInstanceFn    func(ctx context.Context, instanceID string, project *entities.Project) error

	// Upload function fields use shortened signatures to avoid long lines.
	UploadBinaryFn func(
		ctx context.Context, instanceID, appName, fileName string,
		reader io.Reader, size int64, checksum string,
	) error
	UploadImageFn func(
		ctx context.Context, instanceID, imageRef string,
		reader io.Reader, size int64, checksum string,
	) error
}

// New creates a Runtime with safe defaults that return nil errors and canned data.
func New() *Runtime {
	m := &Runtime{}
	m.setIODefaults()
	m.setLifecycleDefaults()
	return m
}

func (m *Runtime) setLifecycleDefaults() {
	m.CreateInstanceFn = func(_ context.Context, project *entities.Project) (*entities.ProjectInstance, error) {
		return &entities.ProjectInstance{
			ID:        uuid.New().String(),
			ProjectID: project.ID,
			State:     entities.InstanceStateRunning,
			IPAddress: "10.0.0.42",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, nil
	}
	m.DestroyInstanceFn = func(_ context.Context, _ string) error { return nil }
	m.StopInstanceFn = func(_ context.Context, _ string) error {
		m.mu.Lock()
		m.stopped = true
		m.mu.Unlock()
		return nil
	}
	m.StartInstanceFn = func(_ context.Context, _ string) error {
		m.mu.Lock()
		m.stopped = false
		m.mu.Unlock()
		return nil
	}
	m.DeployAppFn = func(_ context.Context, _ string, _ *entities.AppSpec) error { return nil }
	m.StopAppFn = func(_ context.Context, _ string, _ string) error { return nil }
	m.RemoveAppFn = func(_ context.Context, _ string, _ string) error { return nil }
	m.GetInstanceStatusFn = func(_ context.Context, instanceID string) (*entities.ProjectInstance, error) {
		m.mu.Lock()
		stopped := m.stopped
		m.mu.Unlock()
		st := entities.InstanceStateRunning
		if stopped {
			st = entities.InstanceStateStopped
		}
		return &entities.ProjectInstance{ID: instanceID, State: st}, nil
	}
	m.ListInstancesFn = func(_ context.Context) ([]*entities.ProjectInstance, error) {
		return nil, nil
	}
	m.ResizeInstanceFn = func(_ context.Context, _ string, _ *entities.Project) error { return nil }
}

func (m *Runtime) setIODefaults() {
	m.UploadBinaryFn = func(
		_ context.Context, _, _, _ string, r io.Reader, _ int64, _ string,
	) error {
		_, _ = io.Copy(io.Discard, r)
		return nil
	}
	m.UploadImageFn = func(
		_ context.Context, _, _ string, r io.Reader, _ int64, _ string,
	) error {
		_, _ = io.Copy(io.Discard, r)
		return nil
	}
	m.GetAppLogsFn = func(_ context.Context, _, _ string, _ int) (string, error) {
		return "mock app log line 1\nmock app log line 2\n", nil
	}
	m.GetVMLogsFn = func(_ context.Context, _, _, _ string, _ int) (string, error) {
		return "mock vm log line 1\nmock vm log line 2\n", nil
	}
	m.StreamLogsFn = func(_ context.Context, _, _ string, _ bool) (io.ReadCloser, error) {
		return io.NopCloser(io.LimitReader(nil, 0)), nil
	}
	m.ExecCommandFn = func(
		_ context.Context, _, _ string, _ []string,
	) (string, string, int, error) {
		return "", "", 0, nil
	}
}

// record stores a call under the mutex.
func (m *Runtime) record(method string, args ...any) {
	m.mu.Lock()
	m.Calls = append(m.Calls, Call{Method: method, Args: args})
	m.mu.Unlock()
}

// CallsFor returns all recorded calls for the given method name.
func (m *Runtime) CallsFor(method string) []Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Call
	for _, c := range m.Calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// Reset clears all recorded calls and resets internal state.
func (m *Runtime) Reset() {
	m.mu.Lock()
	m.Calls = nil
	m.LoadedInstances = nil
	m.stopped = false
	m.mu.Unlock()
}

// --- ProjectRuntime interface implementation ---

// CreateInstance implements runtime.ProjectRuntime.
func (m *Runtime) CreateInstance(
	ctx context.Context, project *entities.Project,
) (*entities.ProjectInstance, error) {
	m.record("CreateInstance", project)
	return m.CreateInstanceFn(ctx, project)
}

// DestroyInstance implements runtime.ProjectRuntime.
func (m *Runtime) DestroyInstance(ctx context.Context, instanceID string) error {
	m.record("DestroyInstance", instanceID)
	return m.DestroyInstanceFn(ctx, instanceID)
}

// StopInstance implements runtime.ProjectRuntime.
func (m *Runtime) StopInstance(ctx context.Context, instanceID string) error {
	m.record("StopInstance", instanceID)
	return m.StopInstanceFn(ctx, instanceID)
}

// StartInstance implements runtime.ProjectRuntime.
func (m *Runtime) StartInstance(ctx context.Context, instanceID string) error {
	m.record("StartInstance", instanceID)
	return m.StartInstanceFn(ctx, instanceID)
}

// DeployApp implements runtime.ProjectRuntime.
func (m *Runtime) DeployApp(
	ctx context.Context, instanceID string, app *entities.AppSpec,
) error {
	m.record("DeployApp", instanceID, app)
	return m.DeployAppFn(ctx, instanceID, app)
}

// StopApp implements runtime.ProjectRuntime.
func (m *Runtime) StopApp(ctx context.Context, instanceID string, appName string) error {
	m.record("StopApp", instanceID, appName)
	return m.StopAppFn(ctx, instanceID, appName)
}

// RemoveApp implements runtime.ProjectRuntime.
func (m *Runtime) RemoveApp(ctx context.Context, instanceID string, appName string) error {
	m.record("RemoveApp", instanceID, appName)
	return m.RemoveAppFn(ctx, instanceID, appName)
}

// GetInstanceStatus implements runtime.ProjectRuntime.
func (m *Runtime) GetInstanceStatus(
	ctx context.Context, instanceID string,
) (*entities.ProjectInstance, error) {
	m.record("GetInstanceStatus", instanceID)
	return m.GetInstanceStatusFn(ctx, instanceID)
}

// ListInstances implements runtime.ProjectRuntime.
func (m *Runtime) ListInstances(ctx context.Context) ([]*entities.ProjectInstance, error) {
	m.record("ListInstances")
	return m.ListInstancesFn(ctx)
}

// UploadBinary implements runtime.ProjectRuntime.
func (m *Runtime) UploadBinary(
	ctx context.Context, instanceID, appName, fileName string,
	reader io.Reader, size int64, checksum string,
) error {
	m.record("UploadBinary", instanceID, appName, fileName)
	return m.UploadBinaryFn(ctx, instanceID, appName, fileName, reader, size, checksum)
}

// UploadImage implements runtime.ProjectRuntime.
func (m *Runtime) UploadImage(
	ctx context.Context, instanceID, imageRef string,
	reader io.Reader, size int64, checksum string,
) error {
	m.record("UploadImage", instanceID, imageRef)
	return m.UploadImageFn(ctx, instanceID, imageRef, reader, size, checksum)
}

// GetAppLogs implements runtime.ProjectRuntime.
func (m *Runtime) GetAppLogs(
	ctx context.Context, instanceID, appName string, lines int,
) (string, error) {
	m.record("GetAppLogs", instanceID, appName, lines)
	return m.GetAppLogsFn(ctx, instanceID, appName, lines)
}

// GetVMLogs implements runtime.ProjectRuntime.
func (m *Runtime) GetVMLogs(
	ctx context.Context, instanceID, source, logFile string, lines int,
) (string, error) {
	m.record("GetVMLogs", instanceID, source, logFile, lines)
	return m.GetVMLogsFn(ctx, instanceID, source, logFile, lines)
}

// StreamLogs implements runtime.ProjectRuntime.
func (m *Runtime) StreamLogs(
	ctx context.Context, instanceID, appName string, follow bool,
) (io.ReadCloser, error) {
	m.record("StreamLogs", instanceID, appName, follow)
	return m.StreamLogsFn(ctx, instanceID, appName, follow)
}

// ExecCommand implements runtime.ProjectRuntime.
//
//nolint:revive // Signature matches the ProjectRuntime interface.
func (m *Runtime) ExecCommand(
	ctx context.Context, instanceID, appName string, cmd []string,
) (stdout, stderr string, exitCode int, err error) {
	m.record("ExecCommand", instanceID, appName, cmd)
	return m.ExecCommandFn(ctx, instanceID, appName, cmd)
}

// ResizeInstance implements runtime.ProjectRuntime.
func (m *Runtime) ResizeInstance(
	ctx context.Context, instanceID string, project *entities.Project,
) error {
	m.record("ResizeInstance", instanceID, project)
	return m.ResizeInstanceFn(ctx, instanceID, project)
}

// LoadInstance implements runtime.ProjectRuntime.
func (m *Runtime) LoadInstance(instance *entities.ProjectInstance) {
	m.record("LoadInstance", instance)
	m.mu.Lock()
	m.LoadedInstances = append(m.LoadedInstances, instance)
	m.mu.Unlock()
}
