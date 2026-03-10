//go:build integration

// Package integration contains end-to-end tests that exercise the full GoShip
// stack with real VMs (libvirt/KVM), real cloud-init provisioning, real
// Docker inside the VM, and the HTTP API layer.
//
// Run with: make test-integration
package integration

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/goship/internal/agent/runtime"
	libvirtrt "github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
	apiserver "github.com/guilhermebr/goship/internal/api"
	"github.com/guilhermebr/goship/internal/client"
	"github.com/guilhermebr/goship/internal/proxy"
	"github.com/guilhermebr/goship/internal/shared/state"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// prereqResult holds the outcome of prerequisite checks.
type prereqResult struct {
	vmImagePath    string
	initBinaryPath string
}

// checkPrereqs verifies that all required dependencies are available.
// Returns the resolved paths or an error message for skipping.
func checkPrereqs() (*prereqResult, string) {
	// 1. Base VM image
	vmImage := os.Getenv("GOSHIP_TEST_VM_IMAGE")
	if vmImage == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, "cannot determine home directory"
		}
		vmImage = filepath.Join(home, ".goship", "images", "goship-vm.qcow2")
	}
	if _, err := os.Stat(vmImage); err != nil {
		return nil, fmt.Sprintf("base VM image not found at %s (set GOSHIP_TEST_VM_IMAGE)", vmImage)
	}

	// 2. goship-init binary
	initBinary := os.Getenv("GOSHIP_TEST_INIT_BINARY")
	if initBinary == "" {
		// Try ./bin/goship-init relative to the repo root.
		// The test binary may run from various directories; use the well-known path.
		wd, _ := os.Getwd()
		candidates := []string{
			filepath.Join(wd, "bin", "goship-init"),
			filepath.Join(wd, "..", "..", "bin", "goship-init"),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				initBinary = c
				break
			}
		}
	}
	if initBinary == "" {
		return nil, "goship-init binary not found (run 'make build' or set GOSHIP_TEST_INIT_BINARY)"
	}
	if _, err := os.Stat(initBinary); err != nil {
		return nil, fmt.Sprintf("goship-init binary not found at %s", initBinary)
	}

	// 3. libvirtd running — attempt a connection.
	conn, err := tryLibvirtConnect()
	if err != nil {
		return nil, fmt.Sprintf("libvirt not available: %v", err)
	}
	_ = conn

	// 4. virt-customize installed
	if _, err := exec.LookPath("virt-customize"); err != nil {
		return nil, "virt-customize not found (install libguestfs-tools)"
	}

	return &prereqResult{
		vmImagePath:    vmImage,
		initBinaryPath: initBinary,
	}, ""
}

// tryLibvirtConnect attempts to connect to libvirt and immediately closes.
func tryLibvirtConnect() (bool, error) {
	// We can't import libvirt.NewConnect here without the CGO dependency.
	// Instead, we let the runtime constructor do it — but we do a quick
	// check by trying to create a minimal runtime and closing it.
	// For the prereq check we just verify the libvirtd socket exists.
	socketPaths := []string{
		"/var/run/libvirt/libvirt-sock",
		"/run/libvirt/libvirt-sock",
	}
	for _, p := range socketPaths {
		if _, err := os.Stat(p); err == nil {
			return true, nil
		}
	}
	return false, fmt.Errorf("libvirt socket not found (is libvirtd running?)")
}

// kvmAvailable checks if /dev/kvm exists and is accessible.
func kvmAvailable() bool {
	_, err := os.Stat("/dev/kvm")
	return err == nil
}

// uniqueProjectName generates a unique project name for test isolation.
func uniqueProjectName() string {
	return fmt.Sprintf("e2e-%d", time.Now().UnixNano()%100000)
}

// e2eServer holds all the components of a test server with a real runtime.
type e2eServer struct {
	client  *client.Client
	store   *state.Store
	routes  *proxy.RouteTable
	rt      *libvirtrt.Runtime
	ts      *httptest.Server
	dataDir string
}

// newE2EServer creates a real HTTP server backed by a real libvirt runtime.
func newE2EServer(t *testing.T, prereqs *prereqResult) *e2eServer {
	t.Helper()

	dataDir := t.TempDir()

	enableKVM := kvmAvailable()
	if !enableKVM {
		t.Log("WARNING: /dev/kvm not available, using TCG emulation (very slow)")
	}

	rt, err := libvirtrt.New(
		runtime.WithDataDir(dataDir),
		runtime.WithVMImage(prereqs.vmImagePath),
		runtime.WithInitBinary(prereqs.initBinaryPath),
		runtime.WithKVM(enableKVM),
		runtime.WithProvisionGuest(true),
		runtime.WithInstallDocker(true),
		runtime.WithProgressWriter(os.Stderr),
	)
	if err != nil {
		t.Fatalf("failed to create libvirt runtime: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := rt.Close(); closeErr != nil {
			t.Logf("warning: failed to close runtime: %v", closeErr)
		}
	})

	store, err := state.NewStore(dataDir)
	if err != nil {
		t.Fatalf("failed to create state store: %v", err)
	}

	routes := proxy.NewRouteTable()
	logger := log.New(os.Stderr, "e2e: ", log.LstdFlags)
	srv := apiserver.New(store, rt, logger, routes)

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	cl := client.New(ts.URL)

	return &e2eServer{
		client:  cl,
		store:   store,
		routes:  routes,
		rt:      rt,
		ts:      ts,
		dataDir: dataDir,
	}
}

func TestE2E_FullLifecycle(t *testing.T) {
	prereqs, skipMsg := checkPrereqs()
	if skipMsg != "" {
		t.Skip(skipMsg)
	}

	e2e := newE2EServer(t, prereqs)
	cl := e2e.client
	routes := e2e.routes
	projectName := uniqueProjectName()

	// ---------------------------------------------------------------
	// 1. Health check
	// ---------------------------------------------------------------
	t.Log("Step 1: Health check")
	resp, err := e2e.ts.Client().Get(e2e.ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 from /healthz, got %d", resp.StatusCode)
	}

	// ---------------------------------------------------------------
	// 2. Project create (this creates a real VM — can take 5-15 min)
	// ---------------------------------------------------------------
	t.Log("Step 2: Creating project (real VM)...")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	_ = ctx // The client doesn't accept context; the timeout is on the server side.
	// The client has a 5-minute HTTP timeout which we need to extend for VM creation.
	e2e.client = client.New(e2e.ts.URL)

	projectResp, err := cl.CreateProject(
		projectName,
		entities.Resources{CPU: 1, MemoryMB: 512, DiskMB: 4096},
	)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if projectResp.Name != projectName {
		t.Fatalf("expected project name %q, got %q", projectName, projectResp.Name)
	}
	if projectResp.Instance == nil {
		t.Fatal("expected instance after project creation")
	}
	projectID := projectResp.ID
	t.Logf("Project created: id=%s, instance_ip=%s", projectID, projectResp.Instance.IPAddress)

	// Register cleanup to destroy VM even if test fails.
	t.Cleanup(func() {
		t.Log("Cleanup: destroying project VM...")
		if delErr := cl.DeleteProject(projectID); delErr != nil {
			t.Logf("warning: cleanup DeleteProject failed: %v", delErr)
		}
	})

	// ---------------------------------------------------------------
	// 3. Project list & get
	// ---------------------------------------------------------------
	t.Log("Step 3: Project list & get")
	projects, err := cl.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}

	gotProject, err := cl.GetProject(projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if gotProject.Name != projectName {
		t.Fatalf("expected name %q, got %q", projectName, gotProject.Name)
	}
	if gotProject.Instance == nil || gotProject.Instance.IPAddress == "" {
		t.Fatal("expected instance with IP address")
	}

	// ---------------------------------------------------------------
	// 4. App create (container mode)
	// ---------------------------------------------------------------
	t.Log("Step 4: Creating app")
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
		t.Fatalf("expected app name 'web', got %q", app.Name)
	}

	// ---------------------------------------------------------------
	// 5. App deploy (pulls image inside VM — can take several minutes)
	// ---------------------------------------------------------------
	t.Log("Step 5: Deploying app (docker pull inside VM)...")
	deployed, err := cl.DeployApp(projectID, "web", nil)
	if err != nil {
		t.Fatalf("DeployApp: %v", err)
	}
	if deployed.Name != "web" {
		t.Fatalf("expected deployed app 'web', got %q", deployed.Name)
	}
	t.Log("App deployed successfully")

	// ---------------------------------------------------------------
	// 6. App list & get
	// ---------------------------------------------------------------
	t.Log("Step 6: App list & get")
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
		t.Fatalf("expected image 'nginx:alpine', got %q", gotApp.Image)
	}

	// ---------------------------------------------------------------
	// 7. App logs (nginx should output startup logs)
	// ---------------------------------------------------------------
	t.Log("Step 7: App logs")
	// Give nginx a moment to start and produce logs.
	time.Sleep(5 * time.Second)
	logs, err := cl.GetAppLogs(projectID, "web", 50)
	if err != nil {
		t.Fatalf("GetAppLogs: %v", err)
	}
	t.Logf("App logs (first 200 chars): %.200s", logs)
	// We don't assert specific content because nginx log output varies,
	// but we verify we got something back without error.

	// ---------------------------------------------------------------
	// 8. Environment variables
	// ---------------------------------------------------------------
	t.Log("Step 8: Environment variables")
	envResp, err := cl.SetEnv(projectID, map[string]string{"FOO": "bar"}, false)
	if err != nil {
		t.Fatalf("SetEnv: %v", err)
	}
	if envResp.Env["FOO"] != "bar" {
		t.Fatalf("expected FOO=bar, got %q", envResp.Env["FOO"])
	}

	envList, err := cl.ListEnv(projectID, false)
	if err != nil {
		t.Fatalf("ListEnv: %v", err)
	}
	if envList.Env["FOO"] != "bar" {
		t.Fatalf("expected FOO=bar in list, got %q", envList.Env["FOO"])
	}

	_, err = cl.DeleteEnv(projectID, []string{"FOO"})
	if err != nil {
		t.Fatalf("DeleteEnv: %v", err)
	}

	envList, err = cl.ListEnv(projectID, false)
	if err != nil {
		t.Fatalf("ListEnv after delete: %v", err)
	}
	if _, ok := envList.Env["FOO"]; ok {
		t.Fatal("expected FOO to be deleted")
	}

	// ---------------------------------------------------------------
	// 9. Domains & proxy routes
	// ---------------------------------------------------------------
	t.Log("Step 9: Domains & proxy routes")
	_, err = cl.UpdateDomains(projectID, apiserver.UpdateDomainsRequest{
		Domains: []string{"e2e.local"},
	})
	if err != nil {
		t.Fatalf("UpdateDomains: %v", err)
	}

	// After setting domains, deployed app should have a route.
	backend, ok := routes.Lookup("web.e2e.local")
	if !ok {
		t.Fatal("expected route 'web.e2e.local' to be registered")
	}
	expectedBackend := fmt.Sprintf("%s:8080", gotProject.Instance.IPAddress)
	if backend != expectedBackend {
		t.Fatalf("expected backend %q, got %q", expectedBackend, backend)
	}

	// Send a real HTTP request through the reverse proxy to verify nginx is reachable.
	proxyHandler := proxy.New(":0", routes)
	proxySrv := httptest.NewServer(proxyHandler)
	defer proxySrv.Close()

	proxyReq, err := http.NewRequest("GET", proxySrv.URL, nil)
	if err != nil {
		t.Fatalf("creating proxy request: %v", err)
	}
	proxyReq.Host = "web.e2e.local"

	proxyResp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		t.Fatalf("proxy HTTP request failed: %v", err)
	}
	defer proxyResp.Body.Close()

	if proxyResp.StatusCode != http.StatusOK {
		t.Fatalf("expected proxy response 200, got %d", proxyResp.StatusCode)
	}
	body, err := io.ReadAll(proxyResp.Body)
	if err != nil {
		t.Fatalf("reading proxy response body: %v", err)
	}
	if !strings.Contains(string(body), "nginx") {
		t.Fatalf("expected proxy response to contain 'nginx', got: %s", string(body))
	}
	t.Log("Reverse proxy successfully forwarded request to nginx in VM")

	// ---------------------------------------------------------------
	// 10. App stop (removes route)
	// ---------------------------------------------------------------
	t.Log("Step 10: App stop")
	if err := cl.StopApp(projectID, "web"); err != nil {
		t.Fatalf("StopApp: %v", err)
	}

	if _, ok := routes.Lookup("web.e2e.local"); ok {
		t.Fatal("expected route removed after app stop")
	}

	// ---------------------------------------------------------------
	// 11. App delete
	// ---------------------------------------------------------------
	t.Log("Step 11: App delete")
	if err := cl.DeleteApp(projectID, "web"); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}

	apps, err = cl.ListApps(projectID)
	if err != nil {
		t.Fatalf("ListApps after delete: %v", err)
	}
	if len(apps) != 0 {
		t.Fatalf("expected 0 apps after delete, got %d", len(apps))
	}

	// ---------------------------------------------------------------
	// 12. Project stop
	// ---------------------------------------------------------------
	t.Log("Step 12: Project stop")
	if err := cl.StopProject(projectID); err != nil {
		t.Fatalf("StopProject: %v", err)
	}

	stoppedProject, err := cl.GetProject(projectID)
	if err != nil {
		t.Fatalf("GetProject after stop: %v", err)
	}
	if stoppedProject.State != entities.ProjectStateStopped {
		t.Fatalf("expected project state 'stopped', got %q", stoppedProject.State)
	}

	// ---------------------------------------------------------------
	// 13. Project start
	// ---------------------------------------------------------------
	t.Log("Step 13: Project start")
	startResp, err := cl.StartProject(projectID)
	if err != nil {
		t.Fatalf("StartProject: %v", err)
	}
	if startResp.State != entities.ProjectStateRunning {
		t.Fatalf("expected project state 'running' after start, got %q", startResp.State)
	}

	// ---------------------------------------------------------------
	// 14. Project delete (final cleanup — also done by t.Cleanup)
	// ---------------------------------------------------------------
	t.Log("Step 14: Project delete")
	if err := cl.DeleteProject(projectID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	projects, err = cl.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects after delete: %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("expected 0 projects after delete, got %d", len(projects))
	}

	t.Log("E2E lifecycle test completed successfully!")
}
