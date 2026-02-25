package gsinit

import (
	"context"
	"errors"

	v1 "github.com/guilhermebr/goship/pkg/api/v1"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// AppExecutor defines the interface for executing applications inside a VM.
// Implementations: DockerManager (containers) and ProcessManager (direct processes).
type AppExecutor interface {
	Deploy(ctx context.Context, app *entities.AppSpec) error
	Stop(ctx context.Context, appName string) error
	Remove(ctx context.Context, appName string) error
	GetStatus(ctx context.Context) ([]v1.AppStatus, error)
	GetLogs(ctx context.Context, appName string, lines int) (string, error)
	Close() error
}

// ExecutorManager routes app operations to the correct executor based on execution mode.
type ExecutorManager struct {
	docker  *DockerManager
	process *ProcessManager
}

// NewExecutorManager creates a new executor manager.
func NewExecutorManager(docker *DockerManager, process *ProcessManager) *ExecutorManager {
	return &ExecutorManager{
		docker:  docker,
		process: process,
	}
}

// GetExecutor returns the appropriate executor for the given app.
func (m *ExecutorManager) GetExecutor(app *entities.AppSpec) (AppExecutor, error) {
	if app.IsProcessMode() {
		if m.process == nil {
			return nil, errors.New("process executor is not available")
		}
		return m.process, nil
	}
	if m.docker == nil {
		return nil, errors.New("docker executor is not available")
	}
	return m.docker, nil
}

// Deploy deploys an app using the appropriate executor.
func (m *ExecutorManager) Deploy(ctx context.Context, app *entities.AppSpec) error {
	executor, err := m.GetExecutor(app)
	if err != nil {
		return err
	}
	return executor.Deploy(ctx, app)
}

// Stop stops an app. Tries both executors since mode is unknown from name alone.
func (m *ExecutorManager) Stop(ctx context.Context, appName string) error {
	if m.docker != nil {
		if err := m.docker.Stop(ctx, appName); err == nil {
			return nil
		}
	}
	if m.process != nil {
		return m.process.Stop(ctx, appName)
	}
	return nil
}

// Remove removes an app from both executors.
func (m *ExecutorManager) Remove(ctx context.Context, appName string) error {
	if m.docker != nil {
		_ = m.docker.Remove(ctx, appName)
	}
	if m.process != nil {
		_ = m.process.Remove(ctx, appName)
	}
	return nil
}

// GetLogs retrieves logs for an app. Tries docker first, falls back to process.
func (m *ExecutorManager) GetLogs(ctx context.Context, appName string, lines int) (string, error) {
	if m.docker != nil {
		logs, err := m.docker.GetLogs(ctx, appName, lines)
		if err == nil {
			return logs, nil
		}
	}
	if m.process != nil {
		return m.process.GetLogs(ctx, appName, lines)
	}
	return "", errors.New("no executor available for logs")
}

// GetAllStatus returns the status of all apps from both executors.
func (m *ExecutorManager) GetAllStatus(ctx context.Context) ([]v1.AppStatus, error) {
	var allStatuses []v1.AppStatus

	if m.docker != nil {
		statuses, err := m.docker.GetStatus(ctx)
		if err == nil {
			allStatuses = append(allStatuses, statuses...)
		}
	}

	if m.process != nil {
		statuses, err := m.process.GetStatus(ctx)
		if err == nil {
			allStatuses = append(allStatuses, statuses...)
		}
	}

	return allStatuses, nil
}

// Close closes all executors.
func (m *ExecutorManager) Close() error {
	var lastErr error
	if m.docker != nil {
		if err := m.docker.Close(); err != nil {
			lastErr = err
		}
	}
	if m.process != nil {
		if err := m.process.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}
