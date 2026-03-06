package apiserver

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// CreateProjectRequest is the request body for creating a project.
type CreateProjectRequest struct {
	Name      string             `json:"name"`
	Resources entities.Resources `json:"resources"`
}

// ProjectResponse is a project with its instance and apps attached.
type ProjectResponse struct {
	*entities.Project

	Instance *entities.ProjectInstance `json:"instance,omitempty"`
	Apps     []*entities.AppSpec       `json:"apps,omitempty"`
}

func (s *Server) handleProjectList(w http.ResponseWriter, _ *http.Request) {
	projects := s.store.ListProjects()
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) handleProjectCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireRuntime(w) {
		return
	}

	var req CreateProjectRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	project, err := s.store.CreateProject(req.Name, entities.RuntimeQEMU, req.Resources)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create project: %v", err))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	instance, err := s.rt.CreateInstance(ctx, project)
	if err != nil {
		_ = s.store.DeleteProject(project.ID)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create VM: %v", err))
		return
	}

	project.State = entities.ProjectStateRunning
	if err := s.store.UpdateProject(project); err != nil {
		s.logger.Printf("failed to update project state: %v", err)
	}

	if err := s.store.SetInstance(instance); err != nil {
		s.logger.Printf("failed to save instance: %v", err)
	}

	resp := ProjectResponse{
		Project:  project,
		Instance: instance,
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleProjectGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	resp := ProjectResponse{
		Project:  project,
		Instance: s.store.GetInstance(project.ID),
		Apps:     s.store.GetApps(project.ID),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleProjectDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireRuntime(w) {
		return
	}

	id := r.PathValue("id")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	instance := s.store.GetInstance(project.ID)
	if instance != nil {
		s.rt.LoadInstance(instance)
		if err := s.rt.DestroyInstance(ctx, instance.ID); err != nil {
			s.logger.Printf("failed to destroy VM: %v", err)
		}
		if err := s.store.DeleteInstance(instance.ID); err != nil {
			s.logger.Printf("failed to delete instance from state: %v", err)
		}
	}

	if err := s.store.DeleteProject(project.ID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete project: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleProjectStop(w http.ResponseWriter, r *http.Request) {
	if !s.requireRuntime(w) {
		return
	}

	id := r.PathValue("id")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	instance := s.store.GetInstance(project.ID)
	if instance == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no VM instance for project %s", id))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	s.rt.LoadInstance(instance)

	if err := s.rt.StopInstance(ctx, instance.ID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to stop VM: %v", err))
		return
	}

	instance.State = entities.InstanceStateStopped
	if err := s.store.UpdateInstance(instance); err != nil {
		s.logger.Printf("failed to update instance state: %v", err)
	}

	project.State = entities.ProjectStateStopped
	if err := s.store.UpdateProject(project); err != nil {
		s.logger.Printf("failed to update project state: %v", err)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (s *Server) handleProjectStart(w http.ResponseWriter, r *http.Request) {
	if !s.requireRuntime(w) {
		return
	}

	id := r.PathValue("id")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	instance := s.store.GetInstance(project.ID)
	if instance == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no VM instance for project %s", id))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	s.rt.LoadInstance(instance)

	if err := s.rt.StartInstance(ctx, instance.ID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to start VM: %v", err))
		return
	}

	if err := s.store.UpdateInstance(instance); err != nil {
		s.logger.Printf("failed to update instance state: %v", err)
	}

	project.State = entities.ProjectStateRunning
	if err := s.store.UpdateProject(project); err != nil {
		s.logger.Printf("failed to update project state: %v", err)
	}

	resp := ProjectResponse{
		Project:  project,
		Instance: instance,
	}
	writeJSON(w, http.StatusOK, resp)
}
