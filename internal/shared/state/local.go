// Package state provides local state management for GoShip.
// In Phase 0, state is stored as a JSON file in ~/.goship/state.json
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/guilhermebr/goship/pkg/domain/entities"
)

const (
	// DefaultStateDir is the default directory for GoShip state.
	DefaultStateDir = "~/.goship"
	// StateFileName is the name of the state file.
	StateFileName = "state.json"
)

// Store manages local state persistence.
type Store struct {
	path  string
	state *entities.LocalState
	mu    sync.RWMutex
}

// NewStore creates a new local state store.
func NewStore(dataDir string) (*Store, error) {
	// Expand home directory
	if len(dataDir) > 0 && dataDir[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dataDir = filepath.Join(home, dataDir[1:])
	}

	// Ensure directory exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	statePath := filepath.Join(dataDir, StateFileName)

	store := &Store{
		path:  statePath,
		state: entities.NewLocalState(),
	}

	// Load existing state if present
	if err := store.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}

	return store, nil
}

// load reads state from disk.
func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}

	var state entities.LocalState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("failed to parse state file: %w", err)
	}

	// Initialize maps if nil
	if state.Projects == nil {
		state.Projects = make(map[string]*entities.Project)
	}
	if state.Instances == nil {
		state.Instances = make(map[string]*entities.ProjectInstance)
	}
	if state.Apps == nil {
		state.Apps = make(map[string]map[string]*entities.AppSpec)
	}

	s.state = &state
	return nil
}

// save writes state to disk.
func (s *Store) save() error {
	s.state.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// CreateProject creates a new project.
func (s *Store) CreateProject(name string, runtime entities.RuntimeType, resources entities.Resources) (*entities.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if project already exists
	for _, p := range s.state.Projects {
		if p.Name == name {
			return nil, fmt.Errorf("project already exists: %s", name)
		}
	}

	project := &entities.Project{
		ID:        uuid.New().String(),
		Name:      name,
		Runtime:   runtime,
		Resources: resources,
		State:     entities.ProjectStatePending,
		Labels:    make(map[string]string),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	s.state.Projects[project.ID] = project

	if err := s.save(); err != nil {
		delete(s.state.Projects, project.ID)
		return nil, err
	}

	return project, nil
}

// GetProject returns a project by ID or name.
func (s *Store) GetProject(idOrName string) (*entities.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Try by ID first
	if project, ok := s.state.Projects[idOrName]; ok {
		return project, nil
	}

	// Try by name
	for _, project := range s.state.Projects {
		if project.Name == idOrName {
			return project, nil
		}
	}

	return nil, fmt.Errorf("project not found: %s", idOrName)
}

// ListProjects returns all projects.
func (s *Store) ListProjects() []*entities.Project {
	s.mu.RLock()
	defer s.mu.RUnlock()

	projects := make([]*entities.Project, 0, len(s.state.Projects))
	for _, p := range s.state.Projects {
		projects = append(projects, p)
	}

	return projects
}

// UpdateProject updates a project.
func (s *Store) UpdateProject(project *entities.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.state.Projects[project.ID]; !ok {
		return fmt.Errorf("project not found: %s", project.ID)
	}

	project.UpdatedAt = time.Now()
	s.state.Projects[project.ID] = project

	return s.save()
}

// DeleteProject deletes a project.
func (s *Store) DeleteProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get project to find by name if needed
	var projectID string
	if _, ok := s.state.Projects[id]; ok {
		projectID = id
	} else {
		for _, p := range s.state.Projects {
			if p.Name == id {
				projectID = p.ID
				break
			}
		}
	}

	if projectID == "" {
		return fmt.Errorf("project not found: %s", id)
	}

	delete(s.state.Projects, projectID)
	delete(s.state.Instances, projectID)
	delete(s.state.Apps, projectID)

	return s.save()
}

// SetInstance sets the instance for a project.
func (s *Store) SetInstance(instance *entities.ProjectInstance) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Instances[instance.ID] = instance

	return s.save()
}

// GetInstance returns the instance for a project.
func (s *Store) GetInstance(projectID string) *entities.ProjectInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.state.GetInstance(projectID)
}

// GetInstanceByID returns an instance by ID.
func (s *Store) GetInstanceByID(instanceID string) *entities.ProjectInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.state.Instances[instanceID]
}

// DeleteInstance deletes an instance.
func (s *Store) DeleteInstance(instanceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.state.Instances, instanceID)

	return s.save()
}

// SetApp sets an app for a project.
func (s *Store) SetApp(projectID string, app *entities.AppSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.SetApp(projectID, app)

	return s.save()
}

// GetApp returns an app by project ID and app name.
func (s *Store) GetApp(projectID, appName string) *entities.AppSpec {
	s.mu.RLock()
	defer s.mu.RUnlock()

	apps := s.state.GetProjectApps(projectID)
	return apps[appName]
}

// GetApps returns all apps for a project.
func (s *Store) GetApps(projectID string) []*entities.AppSpec {
	s.mu.RLock()
	defer s.mu.RUnlock()

	apps := s.state.GetProjectApps(projectID)
	result := make([]*entities.AppSpec, 0, len(apps))
	for _, app := range apps {
		result = append(result, app)
	}

	return result
}

// DeleteApp deletes an app from a project.
func (s *Store) DeleteApp(projectID, appName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.RemoveApp(projectID, appName)

	return s.save()
}

// Path returns the path to the state file.
func (s *Store) Path() string {
	return s.path
}
