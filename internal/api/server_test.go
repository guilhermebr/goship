package apiserver_test

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apiserver "github.com/guilhermebr/goship/internal/api"
	"github.com/guilhermebr/goship/internal/shared/state"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

func newTestServer(t *testing.T) *apiserver.Server {
	t.Helper()
	store, err := state.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	logger := log.New(log.Writer(), "test: ", 0)
	return apiserver.New(store, nil, logger)
}

func newTestServerWithStore(t *testing.T) (*apiserver.Server, *state.Store) {
	t.Helper()
	store, err := state.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	logger := log.New(log.Writer(), "test: ", 0)
	return apiserver.New(store, nil, logger), store
}

// newTestServerWithLog creates a server that captures log output for assertions.
func newTestServerWithLog(t *testing.T) (*apiserver.Server, *state.Store, *bytes.Buffer) {
	t.Helper()
	store, err := state.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	return apiserver.New(store, nil, logger), store, &buf
}

func doRequest(srv *apiserver.Server, method, path string, body any) *httptest.ResponseRecorder {
	var req *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		req = httptest.NewRequest(method, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t)
	w := doRequest(srv, http.MethodGet, "/healthz", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("expected status ok, got %s", resp["status"])
	}
}

func TestProjectList_Empty(t *testing.T) {
	srv := newTestServer(t)
	w := doRequest(srv, http.MethodGet, "/api/v1/projects", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var projects []*entities.Project
	if err := json.NewDecoder(w.Body).Decode(&projects); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("expected empty list, got %d projects", len(projects))
	}
}

func TestProjectList_WithProjects(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	_, err := store.CreateProject("test-project", entities.RuntimeQEMU, entities.Resources{
		CPU:      1,
		MemoryMB: 512,
		DiskMB:   8192,
	})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	w := doRequest(srv, http.MethodGet, "/api/v1/projects", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var projects []*entities.Project
	if err := json.NewDecoder(w.Body).Decode(&projects); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].Name != "test-project" {
		t.Fatalf("expected project name 'test-project', got %s", projects[0].Name)
	}
}

func TestProjectGet_Found(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	project, err := store.CreateProject("my-project", entities.RuntimeQEMU, entities.Resources{
		CPU: 2, MemoryMB: 1024,
	})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	instance := &entities.ProjectInstance{
		ID:        "inst-1",
		ProjectID: project.ID,
		State:     entities.InstanceStateRunning,
		IPAddress: "192.168.1.100",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.SetInstance(instance); err != nil {
		t.Fatalf("failed to set instance: %v", err)
	}

	app := &entities.AppSpec{
		Name:          "web",
		ExecutionMode: entities.ExecutionModeContainer,
		Image:         "nginx:alpine",
		Ports:         []entities.PortMapping{{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"}},
		Replicas:      1,
		CreatedAt:     time.Now(),
	}
	if err := store.SetApp(project.ID, app); err != nil {
		t.Fatalf("failed to set app: %v", err)
	}

	w := doRequest(srv, http.MethodGet, "/api/v1/projects/"+project.ID, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiserver.ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Name != "my-project" {
		t.Fatalf("expected name 'my-project', got %s", resp.Name)
	}
	if resp.Instance == nil {
		t.Fatal("expected instance to be present")
	}
	if resp.Instance.IPAddress != "192.168.1.100" {
		t.Fatalf("expected IP 192.168.1.100, got %s", resp.Instance.IPAddress)
	}
	if len(resp.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(resp.Apps))
	}
	if resp.Apps[0].Name != "web" {
		t.Fatalf("expected app name 'web', got %s", resp.Apps[0].Name)
	}
}

func TestProjectGet_NotFound(t *testing.T) {
	srv := newTestServer(t)
	w := doRequest(srv, http.MethodGet, "/api/v1/projects/nonexistent", nil)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}

	var resp apiserver.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error == "" {
		t.Fatal("expected error message")
	}
}

func TestProjectCreate_NoRuntime(t *testing.T) {
	srv := newTestServer(t) // no runtime (nil)

	body := apiserver.CreateProjectRequest{
		Name: "my-project",
		Resources: entities.Resources{
			CPU:      1,
			MemoryMB: 512,
		},
	}
	w := doRequest(srv, http.MethodPost, "/api/v1/projects", body)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAppCreate(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	project, err := store.CreateProject("test-project", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	body := apiserver.CreateAppRequest{
		Name:          "web",
		ExecutionMode: entities.ExecutionModeContainer,
		Image:         "nginx:alpine",
		Ports:         []entities.PortMapping{{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"}},
		Replicas:      1,
	}

	w := doRequest(srv, http.MethodPost, "/api/v1/projects/"+project.ID+"/apps", body)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var app entities.AppSpec
	if err := json.NewDecoder(w.Body).Decode(&app); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if app.Name != "web" {
		t.Fatalf("expected app name 'web', got %s", app.Name)
	}
	if app.Image != "nginx:alpine" {
		t.Fatalf("expected image 'nginx:alpine', got %s", app.Image)
	}

	// Verify it's in the store.
	stored := store.GetApp(project.ID, "web")
	if stored == nil {
		t.Fatal("app not found in store after creation")
	}
}

func TestAppCreate_InvalidMode(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	project, err := store.CreateProject("test-project", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	body := apiserver.CreateAppRequest{
		Name:          "web",
		ExecutionMode: "invalid-mode",
	}

	w := doRequest(srv, http.MethodPost, "/api/v1/projects/"+project.ID+"/apps", body)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAppCreate_Duplicate(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	project, err := store.CreateProject("test-project", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	body := apiserver.CreateAppRequest{
		Name:          "web",
		ExecutionMode: entities.ExecutionModeContainer,
		Image:         "nginx:alpine",
		Ports:         []entities.PortMapping{{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"}},
	}

	// First create should succeed.
	w := doRequest(srv, http.MethodPost, "/api/v1/projects/"+project.ID+"/apps", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	// Second create should conflict.
	w = doRequest(srv, http.MethodPost, "/api/v1/projects/"+project.ID+"/apps", body)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAppList(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	project, err := store.CreateProject("test-project", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	if err := store.SetApp(project.ID, &entities.AppSpec{
		Name:          "web",
		ExecutionMode: entities.ExecutionModeContainer,
		Image:         "nginx:alpine",
		Replicas:      1,
		CreatedAt:     time.Now(),
	}); err != nil {
		t.Fatalf("failed to set app: %v", err)
	}

	if err := store.SetApp(project.ID, &entities.AppSpec{
		Name:          "api",
		ExecutionMode: entities.ExecutionModeContainer,
		Image:         "myapp:latest",
		Replicas:      1,
		CreatedAt:     time.Now(),
	}); err != nil {
		t.Fatalf("failed to set app: %v", err)
	}

	w := doRequest(srv, http.MethodGet, "/api/v1/projects/"+project.ID+"/apps", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var apps []*entities.AppSpec
	if err := json.NewDecoder(w.Body).Decode(&apps); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(apps))
	}
}

func TestAppGet_Found(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	project, err := store.CreateProject("test-project", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	if err := store.SetApp(project.ID, &entities.AppSpec{
		Name:          "web",
		ExecutionMode: entities.ExecutionModeContainer,
		Image:         "nginx:alpine",
		Replicas:      1,
		CreatedAt:     time.Now(),
	}); err != nil {
		t.Fatalf("failed to set app: %v", err)
	}

	w := doRequest(srv, http.MethodGet, "/api/v1/projects/"+project.ID+"/apps/web", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var app entities.AppSpec
	if err := json.NewDecoder(w.Body).Decode(&app); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if app.Name != "web" {
		t.Fatalf("expected app name 'web', got %s", app.Name)
	}
}

func TestAppGet_NotFound(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	project, err := store.CreateProject("test-project", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	w := doRequest(srv, http.MethodGet, "/api/v1/projects/"+project.ID+"/apps/nonexistent", nil)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestProjectGet_ByName(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	_, err := store.CreateProject("my-project", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	w := doRequest(srv, http.MethodGet, "/api/v1/projects/my-project", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiserver.ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Name != "my-project" {
		t.Fatalf("expected name 'my-project', got %s", resp.Name)
	}
}

func TestAppDeploy_NoRuntime(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	project, err := store.CreateProject("test-project", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	if err := store.SetApp(project.ID, &entities.AppSpec{
		Name:          "web",
		ExecutionMode: entities.ExecutionModeContainer,
		Image:         "nginx:alpine",
		Replicas:      1,
		CreatedAt:     time.Now(),
	}); err != nil {
		t.Fatalf("failed to set app: %v", err)
	}

	w := doRequest(srv, http.MethodPost, "/api/v1/projects/"+project.ID+"/apps/web/deploy", nil)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Logging tests ---

func TestLogMiddleware_IncludesRemoteAddr(t *testing.T) {
	srv, _, logBuf := newTestServerWithLog(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "10.0.0.1:54321") {
		t.Fatalf("expected log to contain remote addr, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "GET") {
		t.Fatalf("expected log to contain method, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "/healthz") {
		t.Fatalf("expected log to contain path, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "200") {
		t.Fatalf("expected log to contain status 200, got: %s", logOutput)
	}
}

func TestLogMiddleware_IncludesQueryString(t *testing.T) {
	srv, store, logBuf := newTestServerWithLog(t)

	project, err := store.CreateProject("test-project", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	if err := store.SetApp(project.ID, &entities.AppSpec{
		Name:          "web",
		ExecutionMode: entities.ExecutionModeContainer,
		Image:         "nginx:alpine",
		Replicas:      1,
		CreatedAt:     time.Now(),
	}); err != nil {
		t.Fatalf("failed to set app: %v", err)
	}

	// Request app logs with query string (no runtime, so will 503).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+project.ID+"/apps/web/logs?lines=50", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "?lines=50") {
		t.Fatalf("expected log to contain query string, got: %s", logOutput)
	}
}

func TestLogMiddleware_NoQueryString(t *testing.T) {
	srv, _, logBuf := newTestServerWithLog(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	logOutput := logBuf.String()
	if strings.Contains(logOutput, "?") {
		t.Fatalf("expected no query string in log, got: %s", logOutput)
	}
}

func TestActionLog_ProjectList(t *testing.T) {
	srv, store, logBuf := newTestServerWithLog(t)

	if _, err := store.CreateProject(
		"p1",
		entities.RuntimeQEMU,
		entities.Resources{CPU: 1, MemoryMB: 512},
	); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "projects listed: count=1") {
		t.Fatalf("expected action log 'projects listed: count=1', got: %s", logOutput)
	}
}

func TestActionLog_ProjectGet(t *testing.T) {
	srv, store, logBuf := newTestServerWithLog(t)

	project, err := store.CreateProject("my-project", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+project.ID, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	logOutput := logBuf.String()
	expected := "project info: name=my-project id=" + project.ID
	if !strings.Contains(logOutput, expected) {
		t.Fatalf("expected action log %q, got: %s", expected, logOutput)
	}
}

func TestActionLog_AppCreate(t *testing.T) {
	srv, store, logBuf := newTestServerWithLog(t)

	project, err := store.CreateProject("test-project", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	body := apiserver.CreateAppRequest{
		Name:          "web",
		ExecutionMode: entities.ExecutionModeContainer,
		Image:         "nginx:alpine",
		Ports:         []entities.PortMapping{{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"}},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+project.ID+"/apps", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "app created: project=test-project app=web mode=container") {
		t.Fatalf("expected action log for app create, got: %s", logOutput)
	}
}

func TestActionLog_AppList(t *testing.T) {
	srv, store, logBuf := newTestServerWithLog(t)

	project, err := store.CreateProject("test-project", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	if err := store.SetApp(project.ID, &entities.AppSpec{
		Name: "web", ExecutionMode: entities.ExecutionModeContainer, Image: "nginx", Replicas: 1, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to set app: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+project.ID+"/apps", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "apps listed: project=test-project count=1") {
		t.Fatalf("expected action log for app list, got: %s", logOutput)
	}
}

func TestActionLog_AppGet(t *testing.T) {
	srv, store, logBuf := newTestServerWithLog(t)

	project, err := store.CreateProject("test-project", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	if err := store.SetApp(project.ID, &entities.AppSpec{
		Name: "api", ExecutionMode: entities.ExecutionModeContainer, Image: "myapp", Replicas: 1, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to set app: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+project.ID+"/apps/api", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "app info: project=test-project app=api") {
		t.Fatalf("expected action log for app info, got: %s", logOutput)
	}
}

func TestActionLog_NotOnError(t *testing.T) {
	srv, _, logBuf := newTestServerWithLog(t)

	// Request a nonexistent project — should NOT produce an action log.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}

	logOutput := logBuf.String()
	if strings.Contains(logOutput, "project info:") {
		t.Fatalf("expected no action log on error, got: %s", logOutput)
	}
}
