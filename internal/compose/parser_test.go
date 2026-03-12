package compose

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/guilhermebr/goship/pkg/domain/entities"
)

const (
	protocolTCP       = "tcp"
	imageGoshipLatest = "goship-app:latest"
)

func TestParseBytes_MultiService(t *testing.T) {
	yaml := []byte(`
version: "3"
services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    restart: always
  web:
    image: nginx:alpine
    ports:
      - "8080:80"
    environment:
      APP_ENV: production
      DEBUG: "false"
    volumes:
      - ./html:/usr/share/nginx/html:ro
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "0.5"
          memory: 256m
`)

	apps, _, warnings, err := ParseBytes(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}

	if len(apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(apps))
	}

	// Services should be sorted alphabetically.
	if apps[0].Name != "redis" {
		t.Errorf("expected first app to be 'redis', got %q", apps[0].Name)
	}
	if apps[1].Name != "web" {
		t.Errorf("expected second app to be 'web', got %q", apps[1].Name)
	}

	// Check redis.
	redis := apps[0]
	if redis.Image != "redis:7-alpine" {
		t.Errorf("redis image = %q, want %q", redis.Image, "redis:7-alpine")
	}
	if redis.RestartPolicy != entities.RestartPolicyAlways {
		t.Errorf("redis restart = %q, want %q", redis.RestartPolicy, entities.RestartPolicyAlways)
	}
	if len(redis.Ports) != 1 || redis.Ports[0].HostPort != 6379 || redis.Ports[0].ContainerPort != 6379 {
		t.Errorf("redis ports = %+v, want [{6379 6379 tcp}]", redis.Ports)
	}

	// Check web.
	web := apps[1]
	if web.Image != "nginx:alpine" {
		t.Errorf("web image = %q, want %q", web.Image, "nginx:alpine")
	}
	if web.Env["APP_ENV"] != "production" {
		t.Errorf("web env APP_ENV = %q, want %q", web.Env["APP_ENV"], "production")
	}
	if web.Env["DEBUG"] != "false" {
		t.Errorf("web env DEBUG = %q, want %q", web.Env["DEBUG"], "false")
	}
	if len(web.Volumes) != 1 || web.Volumes[0].Source != "./html" || !web.Volumes[0].ReadOnly {
		t.Errorf("web volumes = %+v", web.Volumes)
	}
	if web.RestartPolicy != entities.RestartPolicyAlways {
		t.Errorf(
			"web restart = %q, want %q (unless-stopped maps to always)",
			web.RestartPolicy,
			entities.RestartPolicyAlways,
		)
	}
	if web.Resources.CPU != 0.5 {
		t.Errorf("web cpu = %f, want 0.5", web.Resources.CPU)
	}
	if web.Resources.MemoryMB != 256 {
		t.Errorf("web memory = %d, want 256", web.Resources.MemoryMB)
	}
	if web.ExecutionMode != entities.ExecutionModeContainer {
		t.Errorf("web mode = %q, want %q", web.ExecutionMode, entities.ExecutionModeContainer)
	}
	if web.Replicas != 1 {
		t.Errorf("web replicas = %d, want 1", web.Replicas)
	}
}

func TestParseBytes_CommandString(t *testing.T) {
	yaml := []byte(`
services:
  app:
    image: myapp
    command: python app.py --port 8080
`)

	apps, _, _, err := ParseBytes(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := apps[0].Command
	expected := []string{"python", "app.py", "--port", "8080"}
	if len(cmd) != len(expected) {
		t.Fatalf("command length = %d, want %d", len(cmd), len(expected))
	}
	for i, v := range expected {
		if cmd[i] != v {
			t.Errorf("command[%d] = %q, want %q", i, cmd[i], v)
		}
	}
}

func TestParseBytes_CommandList(t *testing.T) {
	yaml := []byte(`
services:
  app:
    image: myapp
    command:
      - python
      - app.py
      - --port
      - "8080"
`)

	apps, _, _, err := ParseBytes(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := apps[0].Command
	expected := []string{"python", "app.py", "--port", "8080"}
	if len(cmd) != len(expected) {
		t.Fatalf("command length = %d, want %d", len(cmd), len(expected))
	}
	for i, v := range expected {
		if cmd[i] != v {
			t.Errorf("command[%d] = %q, want %q", i, cmd[i], v)
		}
	}
}

func TestParseBytes_EnvironmentMap(t *testing.T) {
	yaml := []byte(`
services:
  app:
    image: myapp
    environment:
      FOO: bar
      NUM: 42
`)

	apps, _, _, err := ParseBytes(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	env := apps[0].Env
	if env["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", env["FOO"], "bar")
	}
	if env["NUM"] != "42" {
		t.Errorf("NUM = %q, want %q", env["NUM"], "42")
	}
}

func TestParseBytes_EnvironmentList(t *testing.T) {
	yaml := []byte(`
services:
  app:
    image: myapp
    environment:
      - FOO=bar
      - EMPTY=
      - NOVALUE
`)

	apps, _, _, err := ParseBytes(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	env := apps[0].Env
	if env["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", env["FOO"], "bar")
	}
	if env["EMPTY"] != "" {
		t.Errorf("EMPTY = %q, want empty string", env["EMPTY"])
	}
	if env["NOVALUE"] != "" {
		t.Errorf("NOVALUE = %q, want empty string", env["NOVALUE"])
	}
}

func TestParseBytes_PortWithIPBind(t *testing.T) {
	yaml := []byte(`
services:
  db:
    image: postgres:16
    ports:
      - "127.0.0.1:5432:5432"
`)

	apps, _, _, err := ParseBytes(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ports := apps[0].Ports
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
	if ports[0].HostPort != 5432 || ports[0].ContainerPort != 5432 {
		t.Errorf("port = %d:%d, want 5432:5432", ports[0].HostPort, ports[0].ContainerPort)
	}
	if ports[0].Protocol != protocolTCP {
		t.Errorf("protocol = %q, want %q", ports[0].Protocol, protocolTCP)
	}
}

func TestParseBytes_VolumeReadOnly(t *testing.T) {
	yaml := []byte(`
services:
  app:
    image: myapp
    volumes:
      - ./data:/app/data
      - ./config:/app/config:ro
`)

	apps, _, _, err := ParseBytes(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vols := apps[0].Volumes
	if len(vols) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(vols))
	}

	if vols[0].ReadOnly {
		t.Error("first volume should not be read-only")
	}
	if !vols[1].ReadOnly {
		t.Error("second volume should be read-only")
	}
	if vols[1].Destination != "/app/config" {
		t.Errorf("second volume destination = %q, want %q", vols[1].Destination, "/app/config")
	}
}

func TestParseBytes_PortWithProtocol(t *testing.T) {
	yaml := []byte(`
services:
  app:
    image: myapp
    ports:
      - "8080:80"
      - "5353:53/udp"
`)

	apps, _, _, err := ParseBytes(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ports := apps[0].Ports
	if len(ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(ports))
	}

	if ports[0].Protocol != "tcp" {
		t.Errorf("port 0 protocol = %q, want %q", ports[0].Protocol, "tcp")
	}
	if ports[1].Protocol != "udp" {
		t.Errorf("port 1 protocol = %q, want %q", ports[1].Protocol, "udp")
	}
	if ports[1].HostPort != 5353 || ports[1].ContainerPort != 53 {
		t.Errorf("port 1 = %d:%d, want 5353:53", ports[1].HostPort, ports[1].ContainerPort)
	}
}

func TestParseBytes_ResourceLimits(t *testing.T) {
	tests := []struct {
		name    string
		memory  string
		wantMB  int64
		cpus    string
		wantCPU float64
	}{
		{"megabytes", "512m", 512, "1.5", 1.5},
		{"gigabytes", "2g", 2048, "0.25", 0.25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := []byte(`
services:
  app:
    image: myapp
    deploy:
      resources:
        limits:
          cpus: "` + tt.cpus + `"
          memory: ` + tt.memory + `
`)

			apps, _, _, err := ParseBytes(yaml)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if apps[0].Resources.MemoryMB != tt.wantMB {
				t.Errorf("memory = %d, want %d", apps[0].Resources.MemoryMB, tt.wantMB)
			}
			if apps[0].Resources.CPU != tt.wantCPU {
				t.Errorf("cpu = %f, want %f", apps[0].Resources.CPU, tt.wantCPU)
			}
		})
	}
}

func TestParseBytes_RestartPolicies(t *testing.T) {
	tests := []struct {
		compose string
		want    entities.RestartPolicy
	}{
		{"always", entities.RestartPolicyAlways},
		{"unless-stopped", entities.RestartPolicyAlways},
		{"on-failure", entities.RestartPolicyOnFailure},
		{"no", entities.RestartPolicyNever},
	}

	for _, tt := range tests {
		t.Run(tt.compose, func(t *testing.T) {
			yaml := []byte(`
services:
  app:
    image: myapp
    restart: ` + tt.compose + `
`)

			apps, _, _, err := ParseBytes(yaml)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if apps[0].RestartPolicy != tt.want {
				t.Errorf("restart = %q, want %q", apps[0].RestartPolicy, tt.want)
			}
		})
	}
}

func TestParseBytes_EmptyServices(t *testing.T) {
	yaml := []byte(`
version: "3"
services:
`)

	_, _, _, err := ParseBytes(yaml)
	if err == nil {
		t.Fatal("expected error for empty services")
	}
}

func TestParseBytes_InvalidYAML(t *testing.T) {
	yaml := []byte(`
services:
  app:
    image: [invalid yaml
`)

	_, _, _, err := ParseBytes(yaml)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParseBytes_BuildStringContext(t *testing.T) {
	yaml := []byte(`
services:
  app:
    build: .
    ports:
      - "8080:80"
  redis:
    image: redis:7-alpine
`)

	apps, builds, _, err := ParseBytes(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(apps))
	}

	// app should have generated image name.
	if apps[0].Name != "app" {
		t.Errorf("expected first app to be 'app', got %q", apps[0].Name)
	}
	if apps[0].Image != imageGoshipLatest {
		t.Errorf("app image = %q, want %q", apps[0].Image, imageGoshipLatest)
	}

	// Should have BuildContext for app.
	bc, ok := builds["app"]
	if !ok {
		t.Fatal("expected BuildContext for 'app'")
	}
	if bc.Context != "." {
		t.Errorf("build context = %q, want %q", bc.Context, ".")
	}
	if bc.Dockerfile != "" {
		t.Errorf("build dockerfile = %q, want empty", bc.Dockerfile)
	}
	if bc.ImageName != imageGoshipLatest {
		t.Errorf("build image name = %q, want %q", bc.ImageName, imageGoshipLatest)
	}

	// redis should have no build context.
	if _, ok := builds["redis"]; ok {
		t.Error("redis should not have a BuildContext")
	}
}

func TestParseBytes_BuildObjectContext(t *testing.T) {
	yaml := []byte(`
services:
  app:
    build:
      context: ./src
      dockerfile: Dockerfile.prod
    ports:
      - "8080:80"
`)

	apps, builds, _, err := ParseBytes(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}
	if apps[0].Image != imageGoshipLatest {
		t.Errorf("app image = %q, want %q", apps[0].Image, imageGoshipLatest)
	}

	bc, ok := builds["app"]
	if !ok {
		t.Fatal("expected BuildContext for 'app'")
	}
	if bc.Context != "./src" {
		t.Errorf("build context = %q, want %q", bc.Context, "./src")
	}
	if bc.Dockerfile != "Dockerfile.prod" {
		t.Errorf("build dockerfile = %q, want %q", bc.Dockerfile, "Dockerfile.prod")
	}
}

func TestParseBytes_BuildWithImage_UsesImage(t *testing.T) {
	yaml := []byte(`
services:
  app:
    image: myapp:v1
    build:
      context: .
    ports:
      - "8080:80"
`)

	apps, builds, warnings, err := ParseBytes(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}
	// When both image and build are set, image takes precedence, no BuildContext.
	if apps[0].Image != "myapp:v1" {
		t.Errorf("app image = %q, want %q", apps[0].Image, "myapp:v1")
	}
	if _, ok := builds["app"]; ok {
		t.Error("should not have BuildContext when image is set")
	}

	// Should not have any build-related warnings.
	for _, w := range warnings {
		if w == `service "app": build is not supported (ignored)` {
			t.Error("should not warn about build when image is set")
		}
	}
}

func TestParseBytes_UnsupportedFieldWarnings(t *testing.T) {
	yaml := []byte(`
services:
  app:
    image: myapp
    depends_on:
      - db
    networks:
      - frontend
`)

	apps, _, warnings, err := ParseBytes(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}

	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings (depends_on, networks), got %d: %v", len(warnings), warnings)
	}
}

func TestParseBytes_EnvFileWithSubstitution(t *testing.T) {
	// Create a temp dir with an .env file.
	dir := t.TempDir()
	envContent := `POSTGRES_USER=awa_user
POSTGRES_PASSWORD=awa_password
POSTGRES_DB=awa
`
	if err := os.WriteFile(filepath.Join(dir, ".env.compose"), []byte(envContent), 0o644); err != nil {
		t.Fatal(err)
	}

	yaml := []byte(`
services:
  db:
    image: postgres:16
    env_file:
      - .env.compose
    environment:
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: ${POSTGRES_DB}
    ports:
      - "5432:5432"
`)

	apps, _, _, err := ParseBytes(yaml, WithBaseDir(dir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}

	env := apps[0].Env
	if env["POSTGRES_USER"] != "awa_user" {
		t.Errorf("POSTGRES_USER = %q, want %q", env["POSTGRES_USER"], "awa_user")
	}
	if env["POSTGRES_PASSWORD"] != "awa_password" {
		t.Errorf("POSTGRES_PASSWORD = %q, want %q", env["POSTGRES_PASSWORD"], "awa_password")
	}
	if env["POSTGRES_DB"] != "awa" {
		t.Errorf("POSTGRES_DB = %q, want %q", env["POSTGRES_DB"], "awa")
	}
}

func TestParseBytes_EnvFileOnlyNoEnvironment(t *testing.T) {
	dir := t.TempDir()
	envContent := `DB_HOST=localhost
DB_PORT=5432
`
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0o644); err != nil {
		t.Fatal(err)
	}

	yaml := []byte(`
services:
  app:
    image: myapp
    env_file: .env
`)

	apps, _, _, err := ParseBytes(yaml, WithBaseDir(dir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	env := apps[0].Env
	if env["DB_HOST"] != "localhost" {
		t.Errorf("DB_HOST = %q, want %q", env["DB_HOST"], "localhost")
	}
	if env["DB_PORT"] != "5432" {
		t.Errorf("DB_PORT = %q, want %q", env["DB_PORT"], "5432")
	}
}

func TestParseBytes_EnvironmentOverridesEnvFile(t *testing.T) {
	dir := t.TempDir()
	envContent := `MODE=production
PORT=3000
`
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0o644); err != nil {
		t.Fatal(err)
	}

	yaml := []byte(`
services:
  app:
    image: myapp
    env_file: .env
    environment:
      MODE: development
`)

	apps, _, _, err := ParseBytes(yaml, WithBaseDir(dir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	env := apps[0].Env
	// environment: section should override env_file for same key.
	if env["MODE"] != "development" {
		t.Errorf("MODE = %q, want %q (environment should override env_file)", env["MODE"], "development")
	}
	// env_file keys not in environment: should be preserved.
	if env["PORT"] != "3000" {
		t.Errorf("PORT = %q, want %q", env["PORT"], "3000")
	}
}

func TestParseBytes_SubstitutionWithDefault(t *testing.T) {
	yaml := []byte(`
services:
  db:
    image: postgres:16
    environment:
      POSTGRES_USER: ${POSTGRES_USER:-postgres}
      POSTGRES_DB: ${POSTGRES_DB:-mydb}
`)

	apps, _, _, err := ParseBytes(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	env := apps[0].Env
	if env["POSTGRES_USER"] != "postgres" {
		t.Errorf("POSTGRES_USER = %q, want %q (should use default)", env["POSTGRES_USER"], "postgres")
	}
	if env["POSTGRES_DB"] != "mydb" {
		t.Errorf("POSTGRES_DB = %q, want %q (should use default)", env["POSTGRES_DB"], "mydb")
	}
}

func TestParse_EnvFileIntegration(t *testing.T) {
	// Test the full Parse() flow with a real compose file and env file.
	dir := t.TempDir()

	envContent := `TEMPORAL_POSTGRES_USER=temporal
TEMPORAL_POSTGRES_PASSWORD=temporal_pass
TEMPORAL_POSTGRES_DB=temporal
`
	if err := os.WriteFile(filepath.Join(dir, ".env.compose"), []byte(envContent), 0o644); err != nil {
		t.Fatal(err)
	}

	composeContent := `
services:
  temporal-db:
    image: postgres:16-alpine
    env_file:
      - .env.compose
    environment:
      POSTGRES_USER: ${TEMPORAL_POSTGRES_USER}
      POSTGRES_PASSWORD: ${TEMPORAL_POSTGRES_PASSWORD}
      POSTGRES_DB: ${TEMPORAL_POSTGRES_DB}
    ports:
      - "5432:5432"
`
	composePath := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0o644); err != nil {
		t.Fatal(err)
	}

	apps, _, _, err := Parse(composePath)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}

	env := apps[0].Env
	if env["POSTGRES_USER"] != "temporal" {
		t.Errorf("POSTGRES_USER = %q, want %q", env["POSTGRES_USER"], "temporal")
	}
	if env["POSTGRES_PASSWORD"] != "temporal_pass" {
		t.Errorf("POSTGRES_PASSWORD = %q, want %q", env["POSTGRES_PASSWORD"], "temporal_pass")
	}
	if env["POSTGRES_DB"] != "temporal" {
		t.Errorf("POSTGRES_DB = %q, want %q", env["POSTGRES_DB"], "temporal")
	}

	// env_file vars should also be present (merged as base).
	if env["TEMPORAL_POSTGRES_USER"] != "temporal" {
		t.Errorf("TEMPORAL_POSTGRES_USER = %q, want %q", env["TEMPORAL_POSTGRES_USER"], "temporal")
	}
}
