package apiserver_test

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mockrt "github.com/guilhermebr/goship/internal/agent/runtime/mock"
	apiserver "github.com/guilhermebr/goship/internal/api"
	"github.com/guilhermebr/goship/internal/client"
	"github.com/guilhermebr/goship/internal/shared/state"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// newIntegrationServer creates a real httptest.Server backed by the API server
// with a mock runtime, and returns a client pointed at it.
func newIntegrationServer(
	t *testing.T, rt *mockrt.Runtime,
) (*client.Client, *state.Store) {
	t.Helper()
	store, err := state.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	logger := log.New(log.Writer(), "integration: ", 0)
	srv := apiserver.New(store, rt, logger)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	cl := client.New(ts.URL)
	return cl, store
}

// seedProjectWithInstance creates a project+instance directly in the store for tests
// that don't need to go through the full CreateProject API flow.
func seedProjectWithInstance(
	t *testing.T,
	store *state.Store,
	name string,
	instID string,
	instState entities.InstanceState,
) *entities.Project {
	t.Helper()
	project, err := store.CreateProject(
		name, entities.RuntimeQEMU,
		entities.Resources{CPU: 1, MemoryMB: 512},
	)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	inst := &entities.ProjectInstance{
		ID:        instID,
		ProjectID: project.ID,
		State:     instState,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if setErr := store.SetInstance(inst); setErr != nil {
		t.Fatalf("set instance: %v", setErr)
	}
	return project
}

func TestIntegration_ProjectLifecycle(t *testing.T) {
	rt := mockrt.New()
	cl, store := newIntegrationServer(t, rt)

	// --- Create project ---
	resp, err := cl.CreateProject(
		"lifecycle-test",
		entities.Resources{CPU: 2, MemoryMB: 1024, DiskMB: 8192},
	)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if resp.Name != "lifecycle-test" {
		t.Fatalf("expected name 'lifecycle-test', got %s", resp.Name)
	}
	if resp.Instance == nil {
		t.Fatal("expected instance after creation")
	}
	projectID := resp.ID

	assertCallCount(t, rt, "CreateInstance", 1)

	// --- List projects ---
	projects, err := cl.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}

	// --- Get project ---
	got, err := cl.GetProject(projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != "lifecycle-test" {
		t.Fatalf("expected name 'lifecycle-test', got %s", got.Name)
	}

	// --- Create & verify app ---
	testAppCRUD(t, cl, rt, projectID)

	// --- Env operations ---
	testEnvOperations(t, cl, projectID)

	// --- Stop/Start/Restart project ---
	testProjectLifecycleOps(t, cl, rt, store, projectID)

	// --- Delete app ---
	rt.Reset()
	deleteErr := cl.DeleteApp(projectID, "web")
	if deleteErr != nil {
		t.Fatalf("DeleteApp: %v", deleteErr)
	}
	assertCallCount(t, rt, "RemoveApp", 1)
	if stored := store.GetApp(projectID, "web"); stored != nil {
		t.Fatal("app should have been deleted from store")
	}

	// --- Delete project ---
	rt.Reset()
	deleteErr = cl.DeleteProject(projectID)
	if deleteErr != nil {
		t.Fatalf("DeleteProject: %v", deleteErr)
	}
	assertCallCount(t, rt, "DestroyInstance", 1)
	projects, err = cl.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects after delete: %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("expected 0 projects after delete, got %d", len(projects))
	}
}

func testAppCRUD(
	t *testing.T, cl *client.Client, rt *mockrt.Runtime, projectID string,
) {
	t.Helper()

	app, err := cl.CreateApp(projectID, apiserver.CreateAppRequest{
		Name:          "web",
		ExecutionMode: entities.ExecutionModeContainer,
		Image:         "nginx:alpine",
		Ports: []entities.PortMapping{
			{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"},
		},
		Replicas: 1,
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if app.Name != "web" {
		t.Fatalf("expected app name 'web', got %s", app.Name)
	}

	apps, err := cl.ListApps(projectID)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}

	gotApp, err := cl.GetApp(projectID, "web")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if gotApp.Image != "nginx:alpine" {
		t.Fatalf("expected image 'nginx:alpine', got %s", gotApp.Image)
	}

	newImage := "nginx:latest"
	updatedApp, err := cl.UpdateApp(
		projectID, "web", apiserver.UpdateAppRequest{Image: &newImage},
	)
	if err != nil {
		t.Fatalf("UpdateApp: %v", err)
	}
	if updatedApp.Image != "nginx:latest" {
		t.Fatalf("expected image 'nginx:latest', got %s", updatedApp.Image)
	}

	// Deploy
	rt.Reset()
	deployed, err := cl.DeployApp(projectID, "web", nil)
	if err != nil {
		t.Fatalf("DeployApp: %v", err)
	}
	if deployed.Name != "web" {
		t.Fatalf("expected deployed app name 'web', got %s", deployed.Name)
	}
	assertCallCount(t, rt, "DeployApp", 1)

	// App logs
	rt.Reset()
	logs, err := cl.GetAppLogs(projectID, "web", 50)
	if err != nil {
		t.Fatalf("GetAppLogs: %v", err)
	}
	if !strings.Contains(logs, "mock app log") {
		t.Fatalf("expected mock app logs, got: %s", logs)
	}
	assertCallCount(t, rt, "GetAppLogs", 1)

	// Stop app
	rt.Reset()
	if stopErr := cl.StopApp(projectID, "web"); stopErr != nil {
		t.Fatalf("StopApp: %v", stopErr)
	}
	assertCallCount(t, rt, "StopApp", 1)
}

func testEnvOperations(t *testing.T, cl *client.Client, projectID string) {
	t.Helper()

	envResp, err := cl.SetEnv(
		projectID, map[string]string{"FOO": "bar", "BAZ": "qux"}, false,
	)
	if err != nil {
		t.Fatalf("SetEnv: %v", err)
	}
	if envResp.Env["FOO"] != "bar" {
		t.Fatalf("expected FOO=bar, got %s", envResp.Env["FOO"])
	}

	envList, err := cl.ListEnv(projectID, false)
	if err != nil {
		t.Fatalf("ListEnv: %v", err)
	}
	if len(envList.Env) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(envList.Env))
	}

	envDel, err := cl.DeleteEnv(projectID, []string{"BAZ"})
	if err != nil {
		t.Fatalf("DeleteEnv: %v", err)
	}
	if _, ok := envDel.Env["BAZ"]; ok {
		t.Fatal("BAZ should have been deleted")
	}
	if envDel.Env["FOO"] != "bar" {
		t.Fatal("FOO should still exist")
	}
}

func testProjectLifecycleOps(
	t *testing.T,
	cl *client.Client,
	rt *mockrt.Runtime,
	store *state.Store,
	projectID string,
) {
	t.Helper()

	// Stop project
	rt.Reset()
	if stopErr := cl.StopProject(projectID); stopErr != nil {
		t.Fatalf("StopProject: %v", stopErr)
	}
	assertCallCount(t, rt, "StopInstance", 1)

	p, _ := store.GetProject(projectID)
	if p.State != entities.ProjectStateStopped {
		t.Fatalf("expected project state stopped, got %s", p.State)
	}

	// Start project
	rt.Reset()
	startResp, err := cl.StartProject(projectID)
	if err != nil {
		t.Fatalf("StartProject: %v", err)
	}
	if startResp.Name != "lifecycle-test" {
		t.Fatalf("expected project name after start, got %s", startResp.Name)
	}
	assertCallCount(t, rt, "StartInstance", 1)

	// Restart project
	rt.Reset()
	restartResp, err := cl.RestartProject(projectID)
	if err != nil {
		t.Fatalf("RestartProject: %v", err)
	}
	if restartResp.Name != "lifecycle-test" {
		t.Fatalf("expected project name after restart, got %s", restartResp.Name)
	}
	assertCallCount(t, rt, "StopInstance", 1)
	assertCallCount(t, rt, "StartInstance", 1)
}

func TestIntegration_ProjectUpdate(t *testing.T) {
	rt := mockrt.New()
	cl, store := newIntegrationServer(t, rt)

	project := seedProjectWithInstance(
		t, store, "update-test", "inst-update",
		entities.InstanceStateStopped,
	)

	// Also set DiskMB so disk-shrink guard doesn't trigger.
	project.Resources.DiskMB = 4096
	if err := store.UpdateProject(project); err != nil {
		t.Fatalf("update project resources: %v", err)
	}

	cpu := 4.0
	resp, err := cl.UpdateProject(
		project.ID, apiserver.UpdateProjectRequest{CPU: &cpu},
	)
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if resp.Resources.CPU != 4 {
		t.Fatalf("expected CPU=4, got %v", resp.Resources.CPU)
	}

	assertCallCount(t, rt, "ResizeInstance", 1)

	p, _ := store.GetProject(project.ID)
	if p.Resources.CPU != 4 {
		t.Fatalf("expected stored CPU=4, got %v", p.Resources.CPU)
	}
}

func TestIntegration_ProjectLogs(t *testing.T) {
	rt := mockrt.New()
	rt.GetVMLogsFn = func(
		_ context.Context, _, _, _ string, _ int,
	) (string, error) {
		return "goship-init started\nlistening on :8080\n", nil
	}

	cl, store := newIntegrationServer(t, rt)
	_ = seedProjectWithInstance(
		t, store, "logs-test", "inst-logs",
		entities.InstanceStateRunning,
	)

	project, _ := store.GetProject("logs-test")
	logs, err := cl.GetProjectLogs(project.ID, "goship-init", "", 100)
	if err != nil {
		t.Fatalf("GetProjectLogs: %v", err)
	}
	if !strings.Contains(logs, "goship-init started") {
		t.Fatalf("expected log content, got: %s", logs)
	}
	if !strings.Contains(logs, "listening on :8080") {
		t.Fatalf("expected log content, got: %s", logs)
	}
	assertCallCount(t, rt, "GetVMLogs", 1)
}

func TestIntegration_UploadHandlers(t *testing.T) {
	rt := mockrt.New()
	cl, store := newIntegrationServer(t, rt)

	project := seedProjectWithInstance(
		t, store, "upload-test", "inst-upload",
		entities.InstanceStateRunning,
	)

	// --- PushImage ---
	imageData := bytes.NewReader([]byte("fake-image-tar-data"))
	pushErr := cl.PushImage(
		project.ID, "myapp:v1", imageData, int64(imageData.Len()),
	)
	if pushErr != nil {
		t.Fatalf("PushImage: %v", pushErr)
	}
	assertCallCount(t, rt, "UploadImage", 1)

	// --- UpdateInit (no restart) ---
	rt.Reset()
	initData := bytes.NewReader([]byte("fake-init-binary"))
	initErr := cl.UpdateInit(
		project.ID, initData, int64(initData.Len()), false,
	)
	if initErr != nil {
		t.Fatalf("UpdateInit (no restart): %v", initErr)
	}
	assertCallCount(t, rt, "UploadBinary", 1)
	assertCallCount(t, rt, "StopInstance", 0)

	// --- UpdateInit (with restart) ---
	rt.Reset()
	initData2 := bytes.NewReader([]byte("fake-init-binary-v2"))
	initErr = cl.UpdateInit(
		project.ID, initData2, int64(initData2.Len()), true,
	)
	if initErr != nil {
		t.Fatalf("UpdateInit (with restart): %v", initErr)
	}
	assertCallCount(t, rt, "UploadBinary", 1)
	assertCallCount(t, rt, "StopInstance", 1)
	assertCallCount(t, rt, "StartInstance", 1)
}

func TestIntegration_ErrorPropagation(t *testing.T) {
	rt := mockrt.New()
	cl, _ := newIntegrationServer(t, rt)

	// --- Runtime error → client error ---
	rt.CreateInstanceFn = func(
		_ context.Context, _ *entities.Project,
	) (*entities.ProjectInstance, error) {
		return nil, errors.New("libvirt connection refused")
	}
	_, err := cl.CreateProject(
		"fail-project", entities.Resources{CPU: 1, MemoryMB: 512},
	)
	if err == nil {
		t.Fatal("expected error from CreateProject")
	}
	if !strings.Contains(err.Error(), "libvirt connection refused") {
		t.Fatalf("expected 'libvirt connection refused', got: %v", err)
	}
	// Project should be cleaned up after runtime failure.
	projects, listErr := cl.ListProjects()
	if listErr != nil {
		t.Fatalf("ListProjects: %v", listErr)
	}
	if len(projects) != 0 {
		t.Fatalf("expected 0 projects after failure, got %d", len(projects))
	}

	// --- 404 for unknown project ---
	_, err = cl.GetProject("nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got: %v", err)
	}

	// --- 409 for duplicate project name ---
	testDuplicateProject(t)

	// --- Runtime errors on deploy and logs ---
	testRuntimeErrors(t)
}

func testDuplicateProject(t *testing.T) {
	t.Helper()
	rt := mockrt.New()
	cl, _ := newIntegrationServer(t, rt)

	_, err := cl.CreateProject(
		"dup-name", entities.Resources{CPU: 1, MemoryMB: 512},
	)
	if err != nil {
		t.Fatalf("first CreateProject: %v", err)
	}
	_, err = cl.CreateProject(
		"dup-name", entities.Resources{CPU: 1, MemoryMB: 512},
	)
	if err == nil {
		t.Fatal("expected error for duplicate project name")
	}
	if !strings.Contains(err.Error(), "409") {
		t.Fatalf("expected 409 error, got: %v", err)
	}
}

func testRuntimeErrors(t *testing.T) {
	t.Helper()
	rt := mockrt.New()
	cl, store := newIntegrationServer(t, rt)

	p := seedProjectWithInstance(
		t, store, "deploy-fail", "inst-df",
		entities.InstanceStateRunning,
	)
	setErr := store.SetApp(p.ID, &entities.AppSpec{
		Name:          "web",
		ExecutionMode: entities.ExecutionModeContainer,
		Image:         "nginx",
		Ports: []entities.PortMapping{
			{HostPort: 80, ContainerPort: 80},
		},
		Replicas:  1,
		CreatedAt: time.Now(),
	})
	if setErr != nil {
		t.Fatalf("set app: %v", setErr)
	}

	rt.DeployAppFn = func(
		_ context.Context, _ string, _ *entities.AppSpec,
	) error {
		return errors.New("docker pull failed")
	}
	_, err := cl.DeployApp(p.ID, "web", nil)
	if err == nil {
		t.Fatal("expected error from DeployApp")
	}
	if !strings.Contains(err.Error(), "docker pull failed") {
		t.Fatalf("expected 'docker pull failed', got: %v", err)
	}

	rt.GetAppLogsFn = func(
		_ context.Context, _, _ string, _ int,
	) (string, error) {
		return "", errors.New("virtio channel timeout")
	}
	_, err = cl.GetAppLogs(p.ID, "web", 50)
	if err == nil {
		t.Fatal("expected error from GetAppLogs")
	}
	if !strings.Contains(err.Error(), "virtio channel timeout") {
		t.Fatalf("expected error message, got: %v", err)
	}
}

func TestIntegration_NameLookup(t *testing.T) {
	rt := mockrt.New()
	cl, _ := newIntegrationServer(t, rt)

	resp, err := cl.CreateProject(
		"name-lookup", entities.Resources{CPU: 1, MemoryMB: 512},
	)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	projectID := resp.ID

	_, err = cl.CreateApp(projectID, apiserver.CreateAppRequest{
		Name:          "api",
		ExecutionMode: entities.ExecutionModeContainer,
		Image:         "myapi:latest",
		Ports: []entities.PortMapping{
			{HostPort: 3000, ContainerPort: 3000, Protocol: "tcp"},
		},
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	// Verify read operations by name.
	testNameLookupReads(t, cl, projectID)

	// Verify write operations by name.
	testNameLookupWrites(t, cl, rt)
}

func testNameLookupReads(
	t *testing.T, cl *client.Client, projectID string,
) {
	t.Helper()

	got, err := cl.GetProject("name-lookup")
	if err != nil {
		t.Fatalf("GetProject by name: %v", err)
	}
	if got.ID != projectID {
		t.Fatalf("expected ID %s, got %s", projectID, got.ID)
	}

	apps, err := cl.ListApps("name-lookup")
	if err != nil {
		t.Fatalf("ListApps by name: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}

	app, err := cl.GetApp("name-lookup", "api")
	if err != nil {
		t.Fatalf("GetApp by name: %v", err)
	}
	if app.Name != "api" {
		t.Fatalf("expected app name 'api', got %s", app.Name)
	}

	envResp, err := cl.SetEnv(
		"name-lookup", map[string]string{"KEY": "val"}, false,
	)
	if err != nil {
		t.Fatalf("SetEnv by name: %v", err)
	}
	if envResp.Env["KEY"] != "val" {
		t.Fatalf("expected KEY=val, got %s", envResp.Env["KEY"])
	}

	envList, err := cl.ListEnv("name-lookup", false)
	if err != nil {
		t.Fatalf("ListEnv by name: %v", err)
	}
	if len(envList.Env) != 1 {
		t.Fatalf("expected 1 env var, got %d", len(envList.Env))
	}

	_, err = cl.DeleteEnv("name-lookup", []string{"KEY"})
	if err != nil {
		t.Fatalf("DeleteEnv by name: %v", err)
	}
}

func testNameLookupWrites(
	t *testing.T, cl *client.Client, rt *mockrt.Runtime,
) {
	t.Helper()

	rt.Reset()
	_, err := cl.DeployApp("name-lookup", "api", nil)
	if err != nil {
		t.Fatalf("DeployApp by name: %v", err)
	}
	assertCallCount(t, rt, "DeployApp", 1)

	_, err = cl.GetAppLogs("name-lookup", "api", 20)
	if err != nil {
		t.Fatalf("GetAppLogs by name: %v", err)
	}

	if stopErr := cl.StopApp("name-lookup", "api"); stopErr != nil {
		t.Fatalf("StopApp by name: %v", stopErr)
	}

	_, err = cl.GetProjectLogs("name-lookup", "goship-init", "", 50)
	if err != nil {
		t.Fatalf("GetProjectLogs by name: %v", err)
	}

	if stopErr := cl.StopProject("name-lookup"); stopErr != nil {
		t.Fatalf("StopProject by name: %v", stopErr)
	}

	_, err = cl.StartProject("name-lookup")
	if err != nil {
		t.Fatalf("StartProject by name: %v", err)
	}

	if delErr := cl.DeleteApp("name-lookup", "api"); delErr != nil {
		t.Fatalf("DeleteApp by name: %v", delErr)
	}

	if delErr := cl.DeleteProject("name-lookup"); delErr != nil {
		t.Fatalf("DeleteProject by name: %v", delErr)
	}

	projects, listErr := cl.ListProjects()
	if listErr != nil {
		t.Fatalf("ListProjects: %v", listErr)
	}
	if len(projects) != 0 {
		t.Fatalf("expected 0 projects, got %d", len(projects))
	}
}

// assertCallCount is a test helper that verifies the mock recorded
// exactly the expected number of calls for a given method.
func assertCallCount(
	t *testing.T, rt *mockrt.Runtime, method string, expected int,
) {
	t.Helper()
	if got := len(rt.CallsFor(method)); got != expected {
		t.Fatalf("expected %d %s calls, got %d", expected, method, got)
	}
}
