package apiserver

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/guilhermebr/goship/internal/shared/size"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// CreateProjectRequest is the request body for creating a project.
type CreateProjectRequest struct {
	Name          string             `json:"name"`
	Resources     entities.Resources `json:"resources"`
	Domains       []string           `json:"domains,omitempty"`
	DefaultDomain string             `json:"default_domain,omitempty"`
}

// ProjectResponse is a project with its instance and apps attached.
type ProjectResponse struct {
	*entities.Project

	Instance *entities.ProjectInstance `json:"instance,omitempty"`
	Apps     []*entities.AppSpec       `json:"apps,omitempty"`
}

func (s *Server) handleProjectList(w http.ResponseWriter, _ *http.Request) {
	projects := s.store.ListProjects()
	s.logger.Printf("projects listed: count=%d", len(projects))
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

	if len(req.Domains) > 0 {
		project.Domains = req.Domains
		if req.DefaultDomain != "" {
			project.DefaultDomain = req.DefaultDomain
		} else {
			project.DefaultDomain = req.Domains[0]
		}
		if updateErr := s.store.UpdateProject(project); updateErr != nil {
			s.logger.Printf("failed to save project domains: %v", updateErr)
		}
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

	s.logger.Printf("project created: name=%s id=%s", project.Name, project.ID)

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

	s.logger.Printf("project info: name=%s id=%s", project.Name, project.ID)

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

	// Remove proxy routes for all apps in this project.
	for _, app := range s.store.GetApps(project.ID) {
		s.removeAppRoutes(project, app)
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

	// Clean up registry storage for this project's namespace.
	if s.registry != nil {
		if regErr := s.registry.CleanupProject(project.Name); regErr != nil {
			s.logger.Printf("failed to clean up registry for project %s: %v", project.Name, regErr)
		}
	}

	if err := s.store.DeleteProject(project.ID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete project: %v", err))
		return
	}

	s.logger.Printf("project deleted: name=%s id=%s", project.Name, project.ID)
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

	s.logger.Printf("project stopped: name=%s id=%s", project.Name, project.ID)
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

	s.logger.Printf("project started: name=%s id=%s", project.Name, project.ID)

	resp := ProjectResponse{
		Project:  project,
		Instance: instance,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleProjectRestart(w http.ResponseWriter, r *http.Request) {
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

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	s.rt.LoadInstance(instance)

	// Stop the VM.
	if err := s.rt.StopInstance(ctx, instance.ID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to stop VM: %v", err))
		return
	}

	// Wait for the domain to reach shut off state.
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			writeError(w, http.StatusGatewayTimeout, "timed out waiting for VM to stop")
			return
		case <-ticker.C:
			status, err := s.rt.GetInstanceStatus(ctx, instance.ID)
			if err != nil {
				continue
			}
			if status.State == entities.InstanceStateStopped {
				goto start
			}
		}
	}

start:
	// Start the VM.
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

	s.logger.Printf("project restarted: name=%s id=%s", project.Name, project.ID)

	resp := ProjectResponse{
		Project:  project,
		Instance: instance,
	}
	writeJSON(w, http.StatusOK, resp)
}

// UpdateProjectRequest is the request body for updating a project's resources.
type UpdateProjectRequest struct {
	CPU      *float64 `json:"cpu,omitempty"`
	MemoryMB *int64   `json:"memory_mb,omitempty"`
	Memory   *string  `json:"memory,omitempty"`
	DiskMB   *int64   `json:"disk_mb,omitempty"`
	Disk     *string  `json:"disk,omitempty"`
}

//nolint:funlen // Handler applies many optional resource fields
func (s *Server) handleProjectUpdate(w http.ResponseWriter, r *http.Request) {
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

	if instance.State != entities.InstanceStateStopped {
		writeError(w, http.StatusConflict,
			fmt.Sprintf("VM must be stopped before editing (current state: %s)", instance.State))
		return
	}

	var req UpdateProjectRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	hasChanges := false

	if req.CPU != nil {
		project.Resources.CPU = *req.CPU
		hasChanges = true
	}

	if req.MemoryMB != nil {
		project.Resources.MemoryMB = *req.MemoryMB
		hasChanges = true
	} else if req.Memory != nil {
		memMB, err := size.ParseSizeMB(*req.Memory)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid memory value: %v", err))
			return
		}
		project.Resources.MemoryMB = memMB
		hasChanges = true
	}

	if req.DiskMB != nil {
		if *req.DiskMB < project.Resources.DiskMB {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("disk size can only grow: current %d MB, requested %d MB",
					project.Resources.DiskMB, *req.DiskMB))
			return
		}
		project.Resources.DiskMB = *req.DiskMB
		hasChanges = true
	} else if req.Disk != nil {
		diskMB, err := size.ParseSizeMB(*req.Disk)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid disk value: %v", err))
			return
		}
		if diskMB < project.Resources.DiskMB {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("disk size can only grow: current %d MB, requested %d MB",
					project.Resources.DiskMB, diskMB))
			return
		}
		project.Resources.DiskMB = diskMB
		hasChanges = true
	}

	if !hasChanges {
		writeError(w, http.StatusBadRequest, "no changes specified")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	s.rt.LoadInstance(instance)

	if err := s.rt.ResizeInstance(ctx, instance.ID, project); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to resize VM: %v", err))
		return
	}

	if err := s.store.UpdateProject(project); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save project: %v", err))
		return
	}

	s.logger.Printf("project updated: name=%s id=%s", project.Name, project.ID)

	resp := ProjectResponse{
		Project:  project,
		Instance: instance,
	}
	writeJSON(w, http.StatusOK, resp)
}

// ProjectLogsResponse wraps VM log output.
type ProjectLogsResponse struct {
	Logs string `json:"logs"`
}

func (s *Server) handleProjectLogs(w http.ResponseWriter, r *http.Request) {
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

	source := r.URL.Query().Get("source")
	lines := 100
	if q := r.URL.Query().Get("lines"); q != "" {
		if n, parseErr := strconv.Atoi(q); parseErr == nil && n > 0 {
			lines = n
		}
	}

	logFile := r.URL.Query().Get("file")

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	s.rt.LoadInstance(instance)

	logs, err := s.rt.GetVMLogs(ctx, instance.ID, source, logFile, lines)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get logs: %v", err))
		return
	}

	s.logger.Printf("project logs: name=%s id=%s source=%s lines=%d", project.Name, project.ID, source, lines)
	writeJSON(w, http.StatusOK, ProjectLogsResponse{Logs: strings.TrimRight(logs, "\n")})
}

// UpdateDomainsRequest is the request body for updating project domains.
type UpdateDomainsRequest struct {
	Domains       []string `json:"domains"`
	DefaultDomain string   `json:"default_domain,omitempty"`
}

// handleDomainsUpdate updates the domains assigned to a project.
func (s *Server) handleDomainsUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	project, err := s.store.GetProject(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project not found: %s", id))
		return
	}

	var req UpdateDomainsRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Remove routes for old domains before updating.
	oldDomains := project.Domains

	project.Domains = req.Domains
	switch {
	case req.DefaultDomain != "":
		project.DefaultDomain = req.DefaultDomain
	case len(req.Domains) > 0:
		project.DefaultDomain = req.Domains[0]
	default:
		project.DefaultDomain = ""
	}

	if err := s.store.UpdateProject(project); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save project: %v", err))
		return
	}

	// Reconcile proxy routes: remove old domain routes, add new ones.
	s.reconcileRoutesOnDomainChange(project, oldDomains)

	s.logger.Printf("project domains updated: name=%s domains=%v", project.Name, project.Domains)
	writeJSON(w, http.StatusOK, project)
}

// reconcileRoutesOnDomainChange removes routes for old domains and re-registers
// routes for new domains across all apps in the project.
func (s *Server) reconcileRoutesOnDomainChange(project *entities.Project, oldDomains []string) {
	if s.proxyRoutes == nil {
		return
	}
	apps := s.store.GetApps(project.ID)
	// Remove routes using old domains.
	for _, app := range apps {
		hostname := app.GetHostname()
		for _, domain := range oldDomains {
			route := fmt.Sprintf("%s.%s", hostname, domain)
			s.proxyRoutes.Delete(route)
		}
	}
	// Re-register routes with new domains if instance is running.
	instance := s.store.GetInstance(project.ID)
	if instance == nil || instance.IPAddress == "" {
		return
	}
	for _, app := range apps {
		s.registerAppRoutes(project, app, instance.IPAddress)
	}
}
