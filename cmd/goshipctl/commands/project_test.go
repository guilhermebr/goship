package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/guilhermebr/goship/internal/shared/state"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

func setupTestState(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	var err error
	store, err = state.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
}

func TestProjectList_Empty(t *testing.T) {
	setupTestState(t)

	buf := new(bytes.Buffer)
	projectListCmd.SetOut(buf)

	if err := runProjectList(projectListCmd, nil); err != nil {
		t.Fatalf("runProjectList: %v", err)
	}

	if !strings.Contains(buf.String(), "No projects found") {
		t.Errorf("expected 'No projects found', got: %s", buf.String())
	}
}

func TestProjectList_WithProjects(t *testing.T) {
	setupTestState(t)

	_, _ = store.CreateProject("web-app", entities.RuntimeQEMU, entities.Resources{CPU: 2, MemoryMB: 1024})
	_, _ = store.CreateProject("api-svc", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})

	buf := new(bytes.Buffer)
	projectListCmd.SetOut(buf)

	if err := runProjectList(projectListCmd, nil); err != nil {
		t.Fatalf("runProjectList: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "web-app") {
		t.Errorf("expected 'web-app' in output, got: %s", output)
	}
	if !strings.Contains(output, "api-svc") {
		t.Errorf("expected 'api-svc' in output, got: %s", output)
	}
	if !strings.Contains(output, "NAME") {
		t.Errorf("expected header row in output, got: %s", output)
	}
}

func TestProjectInfo(t *testing.T) {
	setupTestState(t)

	project, _ := store.CreateProject("my-project", entities.RuntimeQEMU, entities.Resources{
		CPU:      2,
		MemoryMB: 1024,
		DiskMB:   8192,
	})

	buf := new(bytes.Buffer)
	projectInfoCmd.SetOut(buf)

	if err := runProjectInfo(projectInfoCmd, []string{project.Name}); err != nil {
		t.Fatalf("runProjectInfo: %v", err)
	}

	output := buf.String()
	checks := []string{
		"Project: my-project",
		project.ID,
		"pending",
		"qemu",
		"2 cores",
		"1024 MB",
		"8192 MB",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("expected %q in output, got:\n%s", check, output)
		}
	}
}

func TestProjectInfo_NotFound(t *testing.T) {
	setupTestState(t)

	err := runProjectInfo(projectInfoCmd, []string{"ghost"})
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
}

func TestProjectInfo_WithInstance(t *testing.T) {
	setupTestState(t)

	project, _ := store.CreateProject("inst-proj", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	_ = store.SetInstance(&entities.ProjectInstance{
		ID:         "inst-123",
		ProjectID:  project.ID,
		State:      entities.InstanceStateRunning,
		DomainName: "goship-inst-proj",
	})

	buf := new(bytes.Buffer)
	projectInfoCmd.SetOut(buf)

	if err := runProjectInfo(projectInfoCmd, []string{"inst-proj"}); err != nil {
		t.Fatalf("runProjectInfo: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "VM Instance:") {
		t.Errorf("expected VM Instance section, got:\n%s", output)
	}
	if !strings.Contains(output, "inst-123") {
		t.Errorf("expected instance ID in output, got:\n%s", output)
	}
	if !strings.Contains(output, "goship-inst-proj") {
		t.Errorf("expected domain name in output, got:\n%s", output)
	}
}

func TestProjectInfo_WithApps(t *testing.T) {
	setupTestState(t)

	project, _ := store.CreateProject("app-proj", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	_ = store.SetApp(project.ID, &entities.AppSpec{Name: "web", Image: "nginx:alpine"})
	_ = store.SetApp(project.ID, &entities.AppSpec{Name: "worker", ExecutionMode: entities.ExecutionModeProcess, Binary: "/usr/bin/worker"})

	buf := new(bytes.Buffer)
	projectInfoCmd.SetOut(buf)

	if err := runProjectInfo(projectInfoCmd, []string{"app-proj"}); err != nil {
		t.Fatalf("runProjectInfo: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Apps:") {
		t.Errorf("expected Apps section, got:\n%s", output)
	}
	if !strings.Contains(output, "web (image: nginx:alpine)") {
		t.Errorf("expected container app in output, got:\n%s", output)
	}
	if !strings.Contains(output, "worker (binary: /usr/bin/worker)") {
		t.Errorf("expected process app in output, got:\n%s", output)
	}
}
