package apiserver

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// CreateAppRequest is the request body for creating an app.
type CreateAppRequest struct {
	Name          string                 `json:"name"`
	ExecutionMode entities.ExecutionMode `json:"execution_mode"`
	Image         string                 `json:"image,omitempty"`
	Binary        string                 `json:"binary,omitempty"`
	Ports         []entities.PortMapping `json:"ports,omitempty"`
	Env           map[string]string      `json:"env,omitempty"`
	Resources     entities.Resources     `json:"resources"`
	RestartPolicy entities.RestartPolicy `json:"restart_policy,omitempty"`
	Replicas      int                    `json:"replicas,omitempty"`
}

// DeployAppRequest is the request body for deploying an app.
type DeployAppRequest struct {
	Image string `json:"image,omitempty"`
}

// AppLogsResponse wraps log output.
type AppLogsResponse struct {
	Logs string `json:"logs"`
}

func (s *Server) handleAppList(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	apps := s.store.GetApps(project.ID)
	s.logger.Printf("apps listed: project=%s count=%d", project.Name, len(apps))
	writeJSON(w, http.StatusOK, apps)
}

func (s *Server) handleAppCreate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	var req CreateAppRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	mode := req.ExecutionMode
	if mode == "" {
		mode = entities.ExecutionModeContainer
	}
	if mode != entities.ExecutionModeContainer && mode != entities.ExecutionModeProcess {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid execution_mode: %s", mode))
		return
	}

	if mode == entities.ExecutionModeProcess && req.Binary == "" {
		writeError(w, http.StatusBadRequest, "binary is required for process mode")
		return
	}

	if mode == entities.ExecutionModeContainer && len(req.Ports) == 0 {
		writeError(w, http.StatusBadRequest, "ports are required for container mode")
		return
	}

	if existing := s.store.GetApp(project.ID, req.Name); existing != nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("app '%s' already exists in project", req.Name))
		return
	}

	replicas := req.Replicas
	if replicas == 0 {
		replicas = 1
	}

	restartPolicy := req.RestartPolicy
	if restartPolicy == "" {
		restartPolicy = entities.RestartPolicyNever
	}

	app := &entities.AppSpec{
		Name:          req.Name,
		ExecutionMode: mode,
		Image:         req.Image,
		Binary:        req.Binary,
		Replicas:      replicas,
		Ports:         req.Ports,
		Env:           req.Env,
		Resources:     req.Resources,
		RestartPolicy: restartPolicy,
		CreatedAt:     time.Now(),
	}

	if err := s.store.SetApp(project.ID, app); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save app: %v", err))
		return
	}

	s.logger.Printf("app created: project=%s app=%s mode=%s", project.Name, app.Name, app.ExecutionMode)
	writeJSON(w, http.StatusCreated, app)
}

func (s *Server) handleAppGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	name := r.PathValue("name")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	app := s.store.GetApp(project.ID, name)
	if app == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("app '%s' not found in project", name))
		return
	}

	s.logger.Printf("app info: project=%s app=%s", project.Name, name)
	writeJSON(w, http.StatusOK, app)
}

func (s *Server) handleAppDeploy(w http.ResponseWriter, r *http.Request) {
	if !s.requireRuntime(w) {
		return
	}

	id := r.PathValue("id")
	name := r.PathValue("name")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	app := s.store.GetApp(project.ID, name)
	if app == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("app '%s' not found in project", name))
		return
	}

	// Optional image override from request body.
	var req DeployAppRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Image != "" {
			app.Image = req.Image
			if err := s.store.SetApp(project.ID, app); err != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update app: %v", err))
				return
			}
		}
	}

	if app.IsContainerMode() && app.Image == "" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("app '%s' has no image set", name))
		return
	}

	instance := s.store.GetInstance(project.ID)
	if instance == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no VM instance for project %s", id))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()

	s.rt.LoadInstance(instance)

	if err := s.rt.DeployApp(ctx, instance.ID, app); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to deploy app: %v", err))
		return
	}

	s.logger.Printf("app deployed: project=%s app=%s", project.Name, name)
	writeJSON(w, http.StatusOK, app)
}

func (s *Server) handleAppStop(w http.ResponseWriter, r *http.Request) {
	if !s.requireRuntime(w) {
		return
	}

	id := r.PathValue("id")
	name := r.PathValue("name")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	if app := s.store.GetApp(project.ID, name); app == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("app '%s' not found in project", name))
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

	if err := s.rt.StopApp(ctx, instance.ID, name); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to stop app: %v", err))
		return
	}

	s.logger.Printf("app stopped: project=%s app=%s", project.Name, name)
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (s *Server) handleAppDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireRuntime(w) {
		return
	}

	id := r.PathValue("id")
	name := r.PathValue("name")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	if app := s.store.GetApp(project.ID, name); app == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("app '%s' not found in project", name))
		return
	}

	instance := s.store.GetInstance(project.ID)
	if instance != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()

		s.rt.LoadInstance(instance)

		if err := s.rt.RemoveApp(ctx, instance.ID, name); err != nil {
			s.logger.Printf("failed to remove app from VM (best effort): %v", err)
		}
	}

	if err := s.store.DeleteApp(project.ID, name); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete app: %v", err))
		return
	}

	s.logger.Printf("app deleted: project=%s app=%s", project.Name, name)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleAppLogs(w http.ResponseWriter, r *http.Request) {
	if !s.requireRuntime(w) {
		return
	}

	id := r.PathValue("id")
	name := r.PathValue("name")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	if app := s.store.GetApp(project.ID, name); app == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("app '%s' not found in project", name))
		return
	}

	instance := s.store.GetInstance(project.ID)
	if instance == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no VM instance for project %s", id))
		return
	}

	lines := 100
	if q := r.URL.Query().Get("lines"); q != "" {
		if n, parseErr := strconv.Atoi(q); parseErr == nil && n > 0 {
			lines = n
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	s.rt.LoadInstance(instance)

	logs, err := s.rt.GetAppLogs(ctx, instance.ID, name, lines)
	if err != nil {
		// Trim verbose error wrapping for cleaner API responses.
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get logs: %v", err))
		return
	}

	s.logger.Printf("app logs: project=%s app=%s lines=%d", project.Name, name, lines)
	writeJSON(w, http.StatusOK, AppLogsResponse{Logs: strings.TrimRight(logs, "\n")})
}

// UpdateAppRequest is the request body for updating an app.
// All fields are pointers to distinguish between "not provided" and "zero value".
type UpdateAppRequest struct {
	Image         *string                `json:"image,omitempty"`
	Binary        *string                `json:"binary,omitempty"`
	Ports         []entities.PortMapping `json:"ports,omitempty"`
	Env           map[string]string      `json:"env,omitempty"`
	Description   *string                `json:"description,omitempty"`
	Tags          []string               `json:"tags,omitempty"`
	RestartPolicy *string                `json:"restart_policy,omitempty"`
	CPU           *float64               `json:"cpu,omitempty"`
	MemoryMB      *int64                 `json:"memory_mb,omitempty"`
}

//nolint:funlen // Handler applies many optional fields from request
func (s *Server) handleAppUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	name := r.PathValue("name")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	app := s.store.GetApp(project.ID, name)
	if app == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("app '%s' not found in project", name))
		return
	}

	var req UpdateAppRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	hasChanges := false

	if req.Image != nil {
		app.Image = *req.Image
		hasChanges = true
	}
	if req.Binary != nil {
		app.Binary = *req.Binary
		hasChanges = true
	}
	if req.Ports != nil {
		app.Ports = req.Ports
		hasChanges = true
	}
	if req.Env != nil {
		if app.Env == nil {
			app.Env = make(map[string]string)
		}
		maps.Copy(app.Env, req.Env)
		hasChanges = true
	}
	if req.Description != nil {
		app.Description = *req.Description
		hasChanges = true
	}
	if req.Tags != nil {
		app.Tags = req.Tags
		hasChanges = true
	}
	if req.RestartPolicy != nil {
		rp := entities.RestartPolicy(*req.RestartPolicy)
		switch rp {
		case entities.RestartPolicyNever, entities.RestartPolicyAlways, entities.RestartPolicyOnFailure:
			app.RestartPolicy = rp
			hasChanges = true
		default:
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid restart_policy: %s", *req.RestartPolicy))
			return
		}
	}
	if req.CPU != nil {
		app.Resources.CPU = *req.CPU
		hasChanges = true
	}
	if req.MemoryMB != nil {
		app.Resources.MemoryMB = *req.MemoryMB
		hasChanges = true
	}

	if !hasChanges {
		writeError(w, http.StatusBadRequest, "no changes specified")
		return
	}

	if err := s.store.SetApp(project.ID, app); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save app: %v", err))
		return
	}

	s.logger.Printf("app updated: project=%s app=%s", project.Name, name)
	writeJSON(w, http.StatusOK, app)
}
