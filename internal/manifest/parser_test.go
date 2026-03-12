package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidManifest(t *testing.T) {
	content := `
version: "1"
project: webstore

resources:
  cpu: 4
  memory: "8G"
  disk: "40G"

domains:
  - "store.example.com"
  - "api.store.example.com"
default_domain: "store.example.com"

compose:
  file: docker-compose.yml
  build: true

env:
  DB_NAME: "webstore"
  LOG_LEVEL: "info"

secrets:
  DB_PASS: "super-secret"
`

	path := writeTemp(t, content)

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if m.Version != "1" {
		t.Errorf("Version = %q, want %q", m.Version, "1")
	}
	if m.Project != "webstore" {
		t.Errorf("Project = %q, want %q", m.Project, "webstore")
	}
	if m.Resources.CPU != 4 {
		t.Errorf("Resources.CPU = %v, want 4", m.Resources.CPU)
	}
	if m.Resources.Memory != "8G" {
		t.Errorf("Resources.Memory = %q, want %q", m.Resources.Memory, "8G")
	}
	if m.Resources.Disk != "40G" {
		t.Errorf("Resources.Disk = %q, want %q", m.Resources.Disk, "40G")
	}
	if len(m.Domains) != 2 {
		t.Errorf("len(Domains) = %d, want 2", len(m.Domains))
	}
	if m.DefaultDomain != "store.example.com" {
		t.Errorf("DefaultDomain = %q, want %q", m.DefaultDomain, "store.example.com")
	}
	if m.Compose.File != "docker-compose.yml" {
		t.Errorf("Compose.File = %q, want %q", m.Compose.File, "docker-compose.yml")
	}
	if m.Compose.Build == nil || !*m.Compose.Build {
		t.Error("Compose.Build should be true")
	}
	if m.Env["DB_NAME"] != "webstore" {
		t.Errorf("Env[DB_NAME] = %q, want %q", m.Env["DB_NAME"], "webstore")
	}
	if m.Secrets["DB_PASS"] != "super-secret" {
		t.Errorf("Secrets[DB_PASS] = %q, want %q", m.Secrets["DB_PASS"], "super-secret")
	}
}

func TestLoad_MissingVersion(t *testing.T) {
	content := `project: test`
	path := writeTemp(t, content)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() should fail for missing version")
	}
}

func TestLoad_MissingProject(t *testing.T) {
	content := `version: "1"`
	path := writeTemp(t, content)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() should fail for missing project")
	}
}

func TestResources_ToEntityResources(t *testing.T) {
	r := &Resources{CPU: 4, Memory: "8G", Disk: "40G"}

	er, err := r.ToEntityResources()
	if err != nil {
		t.Fatalf("ToEntityResources() error: %v", err)
	}

	if er.CPU != 4 {
		t.Errorf("CPU = %v, want 4", er.CPU)
	}
	if er.MemoryMB != 8192 {
		t.Errorf("MemoryMB = %d, want 8192", er.MemoryMB)
	}
	if er.DiskMB != 40960 {
		t.Errorf("DiskMB = %d, want 40960", er.DiskMB)
	}
}

func TestResources_ToEntityResources_Nil(t *testing.T) {
	var r *Resources

	er, err := r.ToEntityResources()
	if err != nil {
		t.Fatalf("ToEntityResources() error: %v", err)
	}
	if er.CPU != 0 || er.MemoryMB != 0 || er.DiskMB != 0 {
		t.Error("nil Resources should return zero values")
	}
}

func TestResolve_Environment(t *testing.T) {
	content := `
version: "1"
project: myapp

resources:
  cpu: 4
  memory: "8G"
  disk: "40G"

domains:
  - "app.example.com"

compose:
  file: docker-compose.yml

env:
  LOG_LEVEL: "info"
  DB_NAME: "myapp"

secrets:
  DB_PASS: "base-pass"

environments:
  dev:
    resources:
      cpu: 2
      memory: "4G"
    domains:
      - "app.dev.local"
    compose:
      file: docker-compose.dev.yml
    env:
      LOG_LEVEL: "debug"
    secrets:
      DB_PASS: "dev-pass"
`

	path := writeTemp(t, content)

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	resolved, err := m.Resolve("dev")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	// Resources: field-level override.
	if resolved.Resources.CPU != 2 {
		t.Errorf("Resources.CPU = %v, want 2", resolved.Resources.CPU)
	}
	if resolved.Resources.Memory != "4G" {
		t.Errorf("Resources.Memory = %q, want %q", resolved.Resources.Memory, "4G")
	}
	// Disk not overridden — should keep base.
	if resolved.Resources.Disk != "40G" {
		t.Errorf("Resources.Disk = %q, want %q", resolved.Resources.Disk, "40G")
	}

	// Domains: full replace.
	if len(resolved.Domains) != 1 || resolved.Domains[0] != "app.dev.local" {
		t.Errorf("Domains = %v, want [app.dev.local]", resolved.Domains)
	}
	if resolved.DefaultDomain != "app.dev.local" {
		t.Errorf("DefaultDomain = %q, want %q", resolved.DefaultDomain, "app.dev.local")
	}

	// Compose: field-level override.
	if resolved.Compose.File != "docker-compose.dev.yml" {
		t.Errorf("Compose.File = %q, want %q", resolved.Compose.File, "docker-compose.dev.yml")
	}

	// Env: map merge.
	if resolved.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("Env[LOG_LEVEL] = %q, want %q", resolved.Env["LOG_LEVEL"], "debug")
	}
	if resolved.Env["DB_NAME"] != "myapp" {
		t.Errorf("Env[DB_NAME] = %q, want %q (base env should be preserved)", resolved.Env["DB_NAME"], "myapp")
	}

	// Secrets: map merge.
	if resolved.Secrets["DB_PASS"] != "dev-pass" {
		t.Errorf("Secrets[DB_PASS] = %q, want %q", resolved.Secrets["DB_PASS"], "dev-pass")
	}

	// Environments should be cleared after resolve.
	if resolved.Environments != nil {
		t.Error("Environments should be nil after Resolve()")
	}
}

func TestResolve_NoEnv(t *testing.T) {
	content := `
version: "1"
project: myapp
`
	path := writeTemp(t, content)

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Empty envName returns the manifest as-is.
	resolved, err := m.Resolve("")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if resolved.Project != "myapp" {
		t.Errorf("Project = %q, want %q", resolved.Project, "myapp")
	}
}

func TestResolve_UnknownEnv(t *testing.T) {
	content := `
version: "1"
project: myapp
`
	path := writeTemp(t, content)

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	_, err = m.Resolve("nonexistent")
	if err == nil {
		t.Fatal("Resolve() should fail for unknown environment")
	}
}

func TestLoad_SecretsFile(t *testing.T) {
	dir := t.TempDir()

	testContent := `
# Database credentials
DB_PASS=from-file
API_KEY=file-key
`
	secretsPath := filepath.Join(dir, ".env.secrets")
	if err := os.WriteFile(secretsPath, []byte(testContent), 0o600); err != nil {
		t.Fatal(err)
	}

	manifestContent := `
version: "1"
project: myapp
secrets_file: ".env.secrets"
secrets:
  DB_PASS: "inline-override"
`
	manifestPath := filepath.Join(dir, "goship.yml")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := Load(manifestPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Inline secrets override file secrets.
	if m.Secrets["DB_PASS"] != "inline-override" {
		t.Errorf(
			"Secrets[DB_PASS] = %q, want %q (inline should override file)",
			m.Secrets["DB_PASS"],
			"inline-override",
		)
	}
	// File-only secret should be present.
	if m.Secrets["API_KEY"] != "file-key" {
		t.Errorf("Secrets[API_KEY] = %q, want %q", m.Secrets["API_KEY"], "file-key")
	}
}

// fullManifest is a complete manifest with multiple environments used across tests.
const fullManifest = `
version: "1"
project: blogplatform

resources:
  cpu: 4
  memory: "8G"
  disk: "40G"

domains:
  - "blog.example.com"
  - "api.blog.example.com"
default_domain: "blog.example.com"

compose:
  file: docker-compose.yml
  build: false

env:
  DB_NAME: "blogdb"
  DB_USER: "bloguser"
  CACHE_URL: "redis://cache:6379"
  LOG_LEVEL: "info"

secrets:
  DB_PASS: "change-me-in-production"
  SESSION_KEY: "change-me-session-key"

environments:
  dev:
    resources:
      cpu: 2
      memory: "4G"
      disk: "20G"
    domains:
      - "blog.dev.local"
      - "api.blog.dev.local"
    env:
      LOG_LEVEL: "debug"

  prod:
    resources:
      cpu: 8
      memory: "16G"
      disk: "100G"
    domains:
      - "blog.example.com"
      - "api.blog.example.com"
      - "cdn.blog.example.com"
    secrets:
      DB_PASS: "prod-db-password"
      SESSION_KEY: "prod-session-key"
`

func TestLoad_FullManifest(t *testing.T) {
	path := writeTemp(t, fullManifest)

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if m.Version != "1" {
		t.Errorf("Version = %q, want %q", m.Version, "1")
	}
	if m.Project != "blogplatform" {
		t.Errorf("Project = %q, want %q", m.Project, "blogplatform")
	}

	// Resources.
	if m.Resources == nil {
		t.Fatal("Resources should not be nil")
	}
	if m.Resources.CPU != 4 {
		t.Errorf("Resources.CPU = %v, want 4", m.Resources.CPU)
	}
	if m.Resources.Memory != "8G" {
		t.Errorf("Resources.Memory = %q, want %q", m.Resources.Memory, "8G")
	}
	if m.Resources.Disk != "40G" {
		t.Errorf("Resources.Disk = %q, want %q", m.Resources.Disk, "40G")
	}

	er, err := m.Resources.ToEntityResources()
	if err != nil {
		t.Fatalf("ToEntityResources() error: %v", err)
	}
	if er.MemoryMB != 8192 {
		t.Errorf("MemoryMB = %d, want 8192", er.MemoryMB)
	}
	if er.DiskMB != 40960 {
		t.Errorf("DiskMB = %d, want 40960", er.DiskMB)
	}

	// Domains.
	if len(m.Domains) != 2 {
		t.Fatalf("len(Domains) = %d, want 2", len(m.Domains))
	}
	if m.Domains[0] != "blog.example.com" {
		t.Errorf("Domains[0] = %q, want %q", m.Domains[0], "blog.example.com")
	}
	if m.Domains[1] != "api.blog.example.com" {
		t.Errorf("Domains[1] = %q, want %q", m.Domains[1], "api.blog.example.com")
	}
	if m.DefaultDomain != "blog.example.com" {
		t.Errorf("DefaultDomain = %q, want %q", m.DefaultDomain, "blog.example.com")
	}

	// Compose.
	if m.Compose == nil {
		t.Fatal("Compose should not be nil")
	}
	if m.Compose.File != "docker-compose.yml" {
		t.Errorf("Compose.File = %q, want %q", m.Compose.File, "docker-compose.yml")
	}
	if m.Compose.Build == nil || *m.Compose.Build {
		t.Error("Compose.Build should be false")
	}

	// Env vars.
	if len(m.Env) != 4 {
		t.Errorf("len(Env) = %d, want 4", len(m.Env))
	}
	if m.Env["DB_NAME"] != "blogdb" {
		t.Errorf("Env[DB_NAME] = %q, want %q", m.Env["DB_NAME"], "blogdb")
	}
	if m.Env["CACHE_URL"] != "redis://cache:6379" {
		t.Errorf("Env[CACHE_URL] = %q, want %q", m.Env["CACHE_URL"], "redis://cache:6379")
	}

	// Secrets.
	if len(m.Secrets) != 2 {
		t.Errorf("len(Secrets) = %d, want 2", len(m.Secrets))
	}
	if _, ok := m.Secrets["DB_PASS"]; !ok {
		t.Error("Secrets should contain DB_PASS")
	}
	if _, ok := m.Secrets["SESSION_KEY"]; !ok {
		t.Error("Secrets should contain SESSION_KEY")
	}

	// Environments present before resolve.
	if len(m.Environments) != 2 {
		t.Fatalf("len(Environments) = %d, want 2", len(m.Environments))
	}
}

func TestResolve_DevOverlay(t *testing.T) {
	path := writeTemp(t, fullManifest)

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	resolved, err := m.Resolve("dev")
	if err != nil {
		t.Fatalf("Resolve(dev) error: %v", err)
	}

	// Resources: all three overridden.
	if resolved.Resources.CPU != 2 {
		t.Errorf("Resources.CPU = %v, want 2", resolved.Resources.CPU)
	}
	if resolved.Resources.Memory != "4G" {
		t.Errorf("Resources.Memory = %q, want %q", resolved.Resources.Memory, "4G")
	}
	if resolved.Resources.Disk != "20G" {
		t.Errorf("Resources.Disk = %q, want %q", resolved.Resources.Disk, "20G")
	}

	er, err := resolved.Resources.ToEntityResources()
	if err != nil {
		t.Fatalf("ToEntityResources() error: %v", err)
	}
	if er.MemoryMB != 4096 {
		t.Errorf("MemoryMB = %d, want 4096", er.MemoryMB)
	}
	if er.DiskMB != 20480 {
		t.Errorf("DiskMB = %d, want 20480", er.DiskMB)
	}

	// Domains: full replace.
	if len(resolved.Domains) != 2 {
		t.Fatalf("len(Domains) = %d, want 2", len(resolved.Domains))
	}
	if resolved.Domains[0] != "blog.dev.local" {
		t.Errorf("Domains[0] = %q, want %q", resolved.Domains[0], "blog.dev.local")
	}
	if resolved.Domains[1] != "api.blog.dev.local" {
		t.Errorf("Domains[1] = %q, want %q", resolved.Domains[1], "api.blog.dev.local")
	}
	if resolved.DefaultDomain != "blog.dev.local" {
		t.Errorf("DefaultDomain = %q, want %q", resolved.DefaultDomain, "blog.dev.local")
	}

	// Env: map merge — LOG_LEVEL overridden, others preserved.
	if resolved.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("Env[LOG_LEVEL] = %q, want %q", resolved.Env["LOG_LEVEL"], "debug")
	}
	if resolved.Env["DB_NAME"] != "blogdb" {
		t.Errorf("Env[DB_NAME] = %q, want %q", resolved.Env["DB_NAME"], "blogdb")
	}
	if resolved.Env["CACHE_URL"] != "redis://cache:6379" {
		t.Errorf("Env[CACHE_URL] = %q, want base value", resolved.Env["CACHE_URL"])
	}

	// Secrets: not overridden in dev.
	if len(resolved.Secrets) != 2 {
		t.Errorf("len(Secrets) = %d, want 2", len(resolved.Secrets))
	}
	if resolved.Secrets["DB_PASS"] != "change-me-in-production" {
		t.Error("Secrets[DB_PASS] should keep base value")
	}

	// Compose: not overridden.
	if resolved.Compose.File != "docker-compose.yml" {
		t.Errorf("Compose.File = %q, want %q", resolved.Compose.File, "docker-compose.yml")
	}

	// Environments cleared.
	if resolved.Environments != nil {
		t.Error("Environments should be nil after Resolve()")
	}
}

func TestResolve_ProdOverlay(t *testing.T) {
	path := writeTemp(t, fullManifest)

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	resolved, err := m.Resolve("prod")
	if err != nil {
		t.Fatalf("Resolve(prod) error: %v", err)
	}

	// Resources: all three overridden.
	if resolved.Resources.CPU != 8 {
		t.Errorf("Resources.CPU = %v, want 8", resolved.Resources.CPU)
	}
	if resolved.Resources.Memory != "16G" {
		t.Errorf("Resources.Memory = %q, want %q", resolved.Resources.Memory, "16G")
	}
	if resolved.Resources.Disk != "100G" {
		t.Errorf("Resources.Disk = %q, want %q", resolved.Resources.Disk, "100G")
	}

	er, err := resolved.Resources.ToEntityResources()
	if err != nil {
		t.Fatalf("ToEntityResources() error: %v", err)
	}
	if er.CPU != 8 {
		t.Errorf("CPU = %v, want 8", er.CPU)
	}
	if er.MemoryMB != 16384 {
		t.Errorf("MemoryMB = %d, want 16384", er.MemoryMB)
	}
	if er.DiskMB != 102400 {
		t.Errorf("DiskMB = %d, want 102400", er.DiskMB)
	}

	// Domains: full replace — prod has 3 domains.
	if len(resolved.Domains) != 3 {
		t.Fatalf("len(Domains) = %d, want 3", len(resolved.Domains))
	}
	if resolved.Domains[0] != "blog.example.com" {
		t.Errorf("Domains[0] = %q, want %q", resolved.Domains[0], "blog.example.com")
	}
	if resolved.Domains[2] != "cdn.blog.example.com" {
		t.Errorf("Domains[2] = %q, want %q", resolved.Domains[2], "cdn.blog.example.com")
	}
	if resolved.DefaultDomain != "blog.example.com" {
		t.Errorf("DefaultDomain = %q, want %q", resolved.DefaultDomain, "blog.example.com")
	}

	// Env: not overridden in prod.
	if resolved.Env["LOG_LEVEL"] != "info" {
		t.Errorf("Env[LOG_LEVEL] = %q, want %q", resolved.Env["LOG_LEVEL"], "info")
	}
	if len(resolved.Env) != 4 {
		t.Errorf("len(Env) = %d, want 4", len(resolved.Env))
	}

	// Secrets: overridden in prod.
	if resolved.Secrets["DB_PASS"] != "prod-db-password" {
		t.Errorf(
			"Secrets[DB_PASS] = %q, want %q",
			resolved.Secrets["DB_PASS"],
			"prod-db-password",
		)
	}
	if resolved.Secrets["SESSION_KEY"] != "prod-session-key" {
		t.Errorf(
			"Secrets[SESSION_KEY] = %q, want %q",
			resolved.Secrets["SESSION_KEY"],
			"prod-session-key",
		)
	}

	// Compose: not overridden.
	if resolved.Compose.File != "docker-compose.yml" {
		t.Errorf("Compose.File = %q, want %q", resolved.Compose.File, "docker-compose.yml")
	}
}

func TestResolve_BaseUnmodified(t *testing.T) {
	path := writeTemp(t, fullManifest)

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Resolve dev — should not modify original.
	_, err = m.Resolve("dev")
	if err != nil {
		t.Fatalf("Resolve(dev) error: %v", err)
	}

	if m.Resources.CPU != 4 {
		t.Errorf("Resources.CPU = %v, want 4 (original modified)", m.Resources.CPU)
	}
	if m.Env["LOG_LEVEL"] != "info" {
		t.Errorf("Env[LOG_LEVEL] = %q, want %q (original modified)", m.Env["LOG_LEVEL"], "info")
	}
	if len(m.Domains) != 2 {
		t.Errorf("len(Domains) = %d, want 2 (original modified)", len(m.Domains))
	}
	if m.Environments == nil {
		t.Error("Environments should not be nil (original modified)")
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "goship.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
