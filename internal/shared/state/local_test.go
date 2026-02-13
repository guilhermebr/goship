package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/guilhermebr/goship/pkg/domain/entities"
)

func setupTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestNewStore_CreatesStateFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	expected := filepath.Join(dir, StateFileName)
	if store.Path() != expected {
		t.Errorf("Path() = %q, want %q", store.Path(), expected)
	}
}

func TestNewStore_LoadsExistingState(t *testing.T) {
	dir := t.TempDir()

	// Create a store and add a project
	store1, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	_, err = store1.CreateProject("test-project", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// Create a new store from the same directory
	store2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore (reload): %v", err)
	}

	projects := store2.ListProjects()
	if len(projects) != 1 {
		t.Fatalf("ListProjects: got %d, want 1", len(projects))
	}
	if projects[0].Name != "test-project" {
		t.Errorf("Name = %q, want %q", projects[0].Name, "test-project")
	}
}

func TestNewStore_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	_, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}
}

func TestCreateProject(t *testing.T) {
	store := setupTestStore(t)

	project, err := store.CreateProject("my-project", entities.RuntimeQEMU, entities.Resources{
		CPU:      2,
		MemoryMB: 1024,
		DiskMB:   10240,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	if project.ID == "" {
		t.Error("expected non-empty ID")
	}
	if project.Name != "my-project" {
		t.Errorf("Name = %q, want %q", project.Name, "my-project")
	}
	if project.Runtime != entities.RuntimeQEMU {
		t.Errorf("Runtime = %q, want %q", project.Runtime, entities.RuntimeQEMU)
	}
	if project.State != entities.ProjectStatePending {
		t.Errorf("State = %q, want %q", project.State, entities.ProjectStatePending)
	}
	if project.Resources.CPU != 2 {
		t.Errorf("CPU = %v, want 2", project.Resources.CPU)
	}
}

func TestCreateProject_DuplicateName(t *testing.T) {
	store := setupTestStore(t)

	_, err := store.CreateProject("dup", entities.RuntimeQEMU, entities.Resources{})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	_, err = store.CreateProject("dup", entities.RuntimeQEMU, entities.Resources{})
	if err == nil {
		t.Fatal("expected error for duplicate project name")
	}
}

func TestGetProject_ByID(t *testing.T) {
	store := setupTestStore(t)

	created, _ := store.CreateProject("proj", entities.RuntimeQEMU, entities.Resources{})

	got, err := store.GetProject(created.ID)
	if err != nil {
		t.Fatalf("GetProject by ID: %v", err)
	}
	if got.Name != "proj" {
		t.Errorf("Name = %q, want %q", got.Name, "proj")
	}
}

func TestGetProject_ByName(t *testing.T) {
	store := setupTestStore(t)

	store.CreateProject("findme", entities.RuntimeQEMU, entities.Resources{})

	got, err := store.GetProject("findme")
	if err != nil {
		t.Fatalf("GetProject by name: %v", err)
	}
	if got.Name != "findme" {
		t.Errorf("Name = %q, want %q", got.Name, "findme")
	}
}

func TestGetProject_NotFound(t *testing.T) {
	store := setupTestStore(t)

	_, err := store.GetProject("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
}

func TestListProjects(t *testing.T) {
	store := setupTestStore(t)

	store.CreateProject("a", entities.RuntimeQEMU, entities.Resources{})
	store.CreateProject("b", entities.RuntimeQEMU, entities.Resources{})

	projects := store.ListProjects()
	if len(projects) != 2 {
		t.Fatalf("ListProjects: got %d, want 2", len(projects))
	}
}

func TestListProjects_Empty(t *testing.T) {
	store := setupTestStore(t)

	projects := store.ListProjects()
	if len(projects) != 0 {
		t.Fatalf("ListProjects: got %d, want 0", len(projects))
	}
}

func TestUpdateProject(t *testing.T) {
	store := setupTestStore(t)

	project, _ := store.CreateProject("update-me", entities.RuntimeQEMU, entities.Resources{})

	project.State = entities.ProjectStateRunning
	project.Resources.CPU = 4
	err := store.UpdateProject(project)
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}

	got, _ := store.GetProject(project.ID)
	if got.State != entities.ProjectStateRunning {
		t.Errorf("State = %q, want %q", got.State, entities.ProjectStateRunning)
	}
	if got.Resources.CPU != 4 {
		t.Errorf("CPU = %v, want 4", got.Resources.CPU)
	}
}

func TestUpdateProject_NotFound(t *testing.T) {
	store := setupTestStore(t)

	err := store.UpdateProject(&entities.Project{ID: "nope"})
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
}

func TestDeleteProject_ByID(t *testing.T) {
	store := setupTestStore(t)

	project, _ := store.CreateProject("del-me", entities.RuntimeQEMU, entities.Resources{})

	err := store.DeleteProject(project.ID)
	if err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	_, err = store.GetProject(project.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestDeleteProject_ByName(t *testing.T) {
	store := setupTestStore(t)

	store.CreateProject("del-by-name", entities.RuntimeQEMU, entities.Resources{})

	err := store.DeleteProject("del-by-name")
	if err != nil {
		t.Fatalf("DeleteProject by name: %v", err)
	}

	projects := store.ListProjects()
	if len(projects) != 0 {
		t.Fatalf("ListProjects: got %d, want 0", len(projects))
	}
}

func TestDeleteProject_NotFound(t *testing.T) {
	store := setupTestStore(t)

	err := store.DeleteProject("ghost")
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
}

func TestDeleteProject_CleansUpInstancesAndApps(t *testing.T) {
	store := setupTestStore(t)

	project, _ := store.CreateProject("cleanup", entities.RuntimeQEMU, entities.Resources{})

	// Add an instance
	store.SetInstance(&entities.ProjectInstance{
		ID:        project.ID,
		ProjectID: project.ID,
		State:     entities.InstanceStateRunning,
	})

	// Add an app
	store.SetApp(project.ID, &entities.AppSpec{Name: "web"})

	// Delete the project
	store.DeleteProject(project.ID)

	if inst := store.GetInstance(project.ID); inst != nil {
		t.Error("expected instance to be cleaned up")
	}
	if apps := store.GetApps(project.ID); len(apps) != 0 {
		t.Errorf("expected apps to be cleaned up, got %d", len(apps))
	}
}

func TestSetAndGetInstance(t *testing.T) {
	store := setupTestStore(t)

	project, _ := store.CreateProject("inst-test", entities.RuntimeQEMU, entities.Resources{})

	instance := &entities.ProjectInstance{
		ID:         "inst-1",
		ProjectID:  project.ID,
		State:      entities.InstanceStateRunning,
		DomainName: "goship-inst-test",
		DomainUUID: "some-uuid",
	}

	err := store.SetInstance(instance)
	if err != nil {
		t.Fatalf("SetInstance: %v", err)
	}

	got := store.GetInstance(project.ID)
	if got == nil {
		t.Fatal("GetInstance returned nil")
	}
	if got.ID != "inst-1" {
		t.Errorf("ID = %q, want %q", got.ID, "inst-1")
	}
	if got.DomainName != "goship-inst-test" {
		t.Errorf("DomainName = %q, want %q", got.DomainName, "goship-inst-test")
	}
}

func TestGetInstanceByID(t *testing.T) {
	store := setupTestStore(t)

	instance := &entities.ProjectInstance{
		ID:        "lookup-me",
		ProjectID: "some-project",
		State:     entities.InstanceStateRunning,
	}
	store.SetInstance(instance)

	got := store.GetInstanceByID("lookup-me")
	if got == nil {
		t.Fatal("GetInstanceByID returned nil")
	}
	if got.ProjectID != "some-project" {
		t.Errorf("ProjectID = %q, want %q", got.ProjectID, "some-project")
	}
}

func TestGetInstanceByID_NotFound(t *testing.T) {
	store := setupTestStore(t)

	got := store.GetInstanceByID("missing")
	if got != nil {
		t.Fatal("expected nil for missing instance")
	}
}

func TestUpdateInstance(t *testing.T) {
	store := setupTestStore(t)

	instance := &entities.ProjectInstance{
		ID:        "update-inst",
		ProjectID: "proj",
		State:     entities.InstanceStateRunning,
	}
	store.SetInstance(instance)

	instance.State = entities.InstanceStateStopped
	instance.IPAddress = "10.0.0.1"
	err := store.UpdateInstance(instance)
	if err != nil {
		t.Fatalf("UpdateInstance: %v", err)
	}

	got := store.GetInstanceByID("update-inst")
	if got == nil {
		t.Fatal("GetInstanceByID returned nil")
	}
	if got.State != entities.InstanceStateStopped {
		t.Errorf("State = %q, want %q", got.State, entities.InstanceStateStopped)
	}
	if got.IPAddress != "10.0.0.1" {
		t.Errorf("IPAddress = %q, want %q", got.IPAddress, "10.0.0.1")
	}
}

func TestUpdateInstance_NotFound(t *testing.T) {
	store := setupTestStore(t)

	err := store.UpdateInstance(&entities.ProjectInstance{ID: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent instance")
	}
}

func TestDeleteInstance(t *testing.T) {
	store := setupTestStore(t)

	store.SetInstance(&entities.ProjectInstance{
		ID:        "del-inst",
		ProjectID: "proj",
		State:     entities.InstanceStateRunning,
	})

	err := store.DeleteInstance("del-inst")
	if err != nil {
		t.Fatalf("DeleteInstance: %v", err)
	}

	if got := store.GetInstanceByID("del-inst"); got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestSetAndGetApp(t *testing.T) {
	store := setupTestStore(t)

	project, _ := store.CreateProject("app-test", entities.RuntimeQEMU, entities.Resources{})

	app := &entities.AppSpec{
		Name:  "web",
		Image: "nginx:alpine",
		Ports: []entities.PortMapping{{HostPort: 8080, ContainerPort: 80}},
	}

	err := store.SetApp(project.ID, app)
	if err != nil {
		t.Fatalf("SetApp: %v", err)
	}

	got := store.GetApp(project.ID, "web")
	if got == nil {
		t.Fatal("GetApp returned nil")
	}
	if got.Image != "nginx:alpine" {
		t.Errorf("Image = %q, want %q", got.Image, "nginx:alpine")
	}
}

func TestGetApp_NotFound(t *testing.T) {
	store := setupTestStore(t)

	got := store.GetApp("no-project", "no-app")
	if got != nil {
		t.Fatal("expected nil for missing app")
	}
}

func TestGetApps(t *testing.T) {
	store := setupTestStore(t)

	project, _ := store.CreateProject("multi-app", entities.RuntimeQEMU, entities.Resources{})

	store.SetApp(project.ID, &entities.AppSpec{Name: "web", Image: "nginx"})
	store.SetApp(project.ID, &entities.AppSpec{Name: "api", Image: "myapi"})

	apps := store.GetApps(project.ID)
	if len(apps) != 2 {
		t.Fatalf("GetApps: got %d, want 2", len(apps))
	}
}

func TestGetApps_Empty(t *testing.T) {
	store := setupTestStore(t)

	apps := store.GetApps("no-project")
	if len(apps) != 0 {
		t.Fatalf("GetApps: got %d, want 0", len(apps))
	}
}

func TestDeleteApp(t *testing.T) {
	store := setupTestStore(t)

	project, _ := store.CreateProject("del-app", entities.RuntimeQEMU, entities.Resources{})
	store.SetApp(project.ID, &entities.AppSpec{Name: "doomed"})

	err := store.DeleteApp(project.ID, "doomed")
	if err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}

	if got := store.GetApp(project.ID, "doomed"); got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestSetApp_UpdateExisting(t *testing.T) {
	store := setupTestStore(t)

	project, _ := store.CreateProject("update-app", entities.RuntimeQEMU, entities.Resources{})

	store.SetApp(project.ID, &entities.AppSpec{Name: "web", Image: "nginx:1.0"})
	store.SetApp(project.ID, &entities.AppSpec{Name: "web", Image: "nginx:2.0"})

	got := store.GetApp(project.ID, "web")
	if got.Image != "nginx:2.0" {
		t.Errorf("Image = %q, want %q", got.Image, "nginx:2.0")
	}

	apps := store.GetApps(project.ID)
	if len(apps) != 1 {
		t.Errorf("GetApps: got %d, want 1 (should overwrite, not duplicate)", len(apps))
	}
}

func TestPersistence_AcrossReloads(t *testing.T) {
	dir := t.TempDir()

	// Phase 1: Create data
	store1, _ := NewStore(dir)
	project, _ := store1.CreateProject("persistent", entities.RuntimeQEMU, entities.Resources{CPU: 2})
	store1.SetInstance(&entities.ProjectInstance{
		ID:        "inst-p",
		ProjectID: project.ID,
		State:     entities.InstanceStateRunning,
	})
	store1.SetApp(project.ID, &entities.AppSpec{Name: "web", Image: "nginx"})

	// Phase 2: Reload and verify
	store2, _ := NewStore(dir)

	projects := store2.ListProjects()
	if len(projects) != 1 {
		t.Fatalf("projects after reload: got %d, want 1", len(projects))
	}

	inst := store2.GetInstance(project.ID)
	if inst == nil {
		t.Fatal("instance not persisted")
	}

	app := store2.GetApp(project.ID, "web")
	if app == nil {
		t.Fatal("app not persisted")
	}
	if app.Image != "nginx" {
		t.Errorf("app Image = %q, want %q", app.Image, "nginx")
	}
}
