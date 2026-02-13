package entities

import "time"

// LocalState represents the complete local state for Phase 0.
// This is stored in ~/.goship/state.json
type LocalState struct {
	// Version of the state format
	Version string `json:"version"`
	// Projects defined
	Projects map[string]*Project `json:"projects"`
	// Project instances (VMs)
	Instances map[string]*ProjectInstance `json:"instances"`
	// Apps per project (project_id -> app_name -> AppSpec)
	Apps map[string]map[string]*AppSpec `json:"apps"`
	// Last update timestamp
	UpdatedAt time.Time `json:"updated_at"`
}

// NewLocalState creates a new empty local state.
func NewLocalState() *LocalState {
	return &LocalState{
		Version:   "v1",
		Projects:  make(map[string]*Project),
		Instances: make(map[string]*ProjectInstance),
		Apps:      make(map[string]map[string]*AppSpec),
		UpdatedAt: time.Now(),
	}
}

// GetProjectApps returns all apps for a project.
func (s *LocalState) GetProjectApps(projectID string) map[string]*AppSpec {
	if apps, ok := s.Apps[projectID]; ok {
		return apps
	}
	return make(map[string]*AppSpec)
}

// SetApp sets an app for a project.
func (s *LocalState) SetApp(projectID string, app *AppSpec) {
	if s.Apps[projectID] == nil {
		s.Apps[projectID] = make(map[string]*AppSpec)
	}
	s.Apps[projectID][app.Name] = app
	s.UpdatedAt = time.Now()
}

// RemoveApp removes an app from a project.
func (s *LocalState) RemoveApp(projectID, appName string) {
	if s.Apps[projectID] != nil {
		delete(s.Apps[projectID], appName)
	}
	s.UpdatedAt = time.Now()
}

// GetInstance returns the instance for a project.
func (s *LocalState) GetInstance(projectID string) *ProjectInstance {
	for _, instance := range s.Instances {
		if instance.ProjectID == projectID {
			return instance
		}
	}
	return nil
}
