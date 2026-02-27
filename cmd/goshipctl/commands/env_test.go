package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/guilhermebr/goship/internal/vault"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

func TestEnvSet_Plain(t *testing.T) {
	setupTestState(t)

	_, _ = store.CreateProject("myproj", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})

	buf := new(bytes.Buffer)
	envSetCmd.SetOut(buf)
	envSecretFlag = false

	if err := runEnvSet(envSetCmd, []string{"myproj", "FOO=bar", "BAZ=qux"}); err != nil {
		t.Fatalf("runEnvSet: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "FOO=bar") {
		t.Errorf("expected FOO=bar in output, got:\n%s", output)
	}
	if !strings.Contains(output, "BAZ=qux") {
		t.Errorf("expected BAZ=qux in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Environment updated") {
		t.Errorf("expected 'Environment updated' in output, got:\n%s", output)
	}

	// Verify persistence.
	project, _ := store.GetProject("myproj")
	if project.Env["FOO"] != "bar" {
		t.Errorf("expected FOO=bar in project.Env, got: %v", project.Env)
	}
}

func TestEnvSet_Secret(t *testing.T) {
	setupTestState(t)

	dir := t.TempDir()
	cfg.DataDir = dir

	_, _ = store.CreateProject("secproj", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})

	buf := new(bytes.Buffer)
	envSetCmd.SetOut(buf)
	envSecretFlag = true
	defer func() { envSecretFlag = false }()

	if err := runEnvSet(envSetCmd, []string{"secproj", "DB_PASS=s3cret"}); err != nil {
		t.Fatalf("runEnvSet: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, maskedValue) {
		t.Errorf("expected masked value in output, got:\n%s", output)
	}
	if !strings.Contains(output, "(encrypted)") {
		t.Errorf("expected '(encrypted)' in output, got:\n%s", output)
	}

	// Verify the stored value is actually encrypted.
	project, _ := store.GetProject("secproj")
	if !vault.IsEncrypted(project.Env["DB_PASS"]) {
		t.Errorf("expected encrypted value for DB_PASS, got: %s", project.Env["DB_PASS"])
	}
}

func TestEnvSet_InvalidFormat(t *testing.T) {
	setupTestState(t)

	_, _ = store.CreateProject("badproj", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})

	envSecretFlag = false

	err := runEnvSet(envSetCmd, []string{"badproj", "NOEQUALS"})
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "invalid format") {
		t.Errorf("expected 'invalid format' error, got: %v", err)
	}
}

func TestEnvSet_ProjectNotFound(t *testing.T) {
	setupTestState(t)

	err := runEnvSet(envSetCmd, []string{"ghost", "FOO=bar"})
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
	if !strings.Contains(err.Error(), "project not found") {
		t.Errorf("expected 'project not found', got: %v", err)
	}
}

func TestEnvSet_SortedOutput(t *testing.T) {
	setupTestState(t)

	_, _ = store.CreateProject("sortproj", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})

	buf := new(bytes.Buffer)
	envSetCmd.SetOut(buf)
	envSecretFlag = false

	if err := runEnvSet(envSetCmd, []string{"sortproj", "ZZZ=last", "AAA=first"}); err != nil {
		t.Fatalf("runEnvSet: %v", err)
	}

	output := buf.String()
	aPos := strings.Index(output, "AAA=")
	zPos := strings.Index(output, "ZZZ=")
	if aPos < 0 || zPos < 0 {
		t.Fatalf("expected both keys in output, got:\n%s", output)
	}
	if aPos > zPos {
		t.Errorf("expected AAA before ZZZ in sorted output, got:\n%s", output)
	}
}

func TestEnvList_Empty(t *testing.T) {
	setupTestState(t)

	_, _ = store.CreateProject("emptyproj", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})

	buf := new(bytes.Buffer)
	envListCmd.SetOut(buf)
	envShowValues = false

	if err := runEnvList(envListCmd, []string{"emptyproj"}); err != nil {
		t.Fatalf("runEnvList: %v", err)
	}

	if !strings.Contains(buf.String(), "No environment variables") {
		t.Errorf("expected 'No environment variables', got:\n%s", buf.String())
	}
}

func TestEnvList_WithVars(t *testing.T) {
	setupTestState(t)

	project, _ := store.CreateProject("listproj", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	project.Env = map[string]string{"APP_PORT": "8080", "APP_ENV": "prod"}
	_ = store.UpdateProject(project)

	buf := new(bytes.Buffer)
	envListCmd.SetOut(buf)
	envShowValues = false

	if err := runEnvList(envListCmd, []string{"listproj"}); err != nil {
		t.Fatalf("runEnvList: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "APP_PORT") {
		t.Errorf("expected APP_PORT in output, got:\n%s", output)
	}
	if !strings.Contains(output, "8080") {
		t.Errorf("expected 8080 in output, got:\n%s", output)
	}
	if !strings.Contains(output, "KEY") {
		t.Errorf("expected header row in output, got:\n%s", output)
	}
}

func TestEnvList_MasksSecrets(t *testing.T) {
	setupTestState(t)

	dir := t.TempDir()
	cfg.DataDir = dir

	v, _ := initVault()
	encrypted, _ := v.Encrypt("s3cret")

	project, _ := store.CreateProject("maskproj", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	project.Env = map[string]string{"DB_PASS": encrypted, "PORT": "5432"}
	_ = store.UpdateProject(project)

	buf := new(bytes.Buffer)
	envListCmd.SetOut(buf)
	envShowValues = false

	if err := runEnvList(envListCmd, []string{"maskproj"}); err != nil {
		t.Fatalf("runEnvList: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, maskedValue) {
		t.Errorf("expected masked value for DB_PASS, got:\n%s", output)
	}
	if !strings.Contains(output, "5432") {
		t.Errorf("expected plain PORT value, got:\n%s", output)
	}
	if strings.Contains(output, "s3cret") {
		t.Error("secret plaintext should not appear in output")
	}
}

func TestEnvList_ShowValues(t *testing.T) {
	setupTestState(t)

	dir := t.TempDir()
	cfg.DataDir = dir

	v, _ := initVault()
	encrypted, _ := v.Encrypt("s3cret")

	project, _ := store.CreateProject("showproj", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	project.Env = map[string]string{"DB_PASS": encrypted}
	_ = store.UpdateProject(project)

	buf := new(bytes.Buffer)
	envListCmd.SetOut(buf)
	envShowValues = true
	defer func() { envShowValues = false }()

	if err := runEnvList(envListCmd, []string{"showproj"}); err != nil {
		t.Fatalf("runEnvList: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "s3cret") {
		t.Errorf("expected decrypted value with --show-values, got:\n%s", output)
	}
}

func TestEnvDelete(t *testing.T) {
	setupTestState(t)

	project, _ := store.CreateProject("delproj", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	project.Env = map[string]string{"FOO": "bar", "BAZ": "qux"}
	_ = store.UpdateProject(project)

	buf := new(bytes.Buffer)
	envDeleteCmd.SetOut(buf)

	if err := runEnvDelete(envDeleteCmd, []string{"delproj", "FOO"}); err != nil {
		t.Fatalf("runEnvDelete: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "FOO: deleted") {
		t.Errorf("expected 'FOO: deleted' in output, got:\n%s", output)
	}

	// Verify persistence.
	project, _ = store.GetProject("delproj")
	if _, ok := project.Env["FOO"]; ok {
		t.Error("expected FOO to be deleted from project.Env")
	}
	if project.Env["BAZ"] != "qux" {
		t.Error("expected BAZ to remain unchanged")
	}
}

func TestEnvDelete_NotFound(t *testing.T) {
	setupTestState(t)

	project, _ := store.CreateProject("delproj2", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})
	project.Env = map[string]string{"FOO": "bar"}
	_ = store.UpdateProject(project)

	buf := new(bytes.Buffer)
	envDeleteCmd.SetOut(buf)

	if err := runEnvDelete(envDeleteCmd, []string{"delproj2", "MISSING"}); err != nil {
		t.Fatalf("runEnvDelete: %v", err)
	}

	if !strings.Contains(buf.String(), "MISSING: not found (skipped)") {
		t.Errorf("expected skip message, got:\n%s", buf.String())
	}
}

func TestEnvDelete_NoEnvSet(t *testing.T) {
	setupTestState(t)

	_, _ = store.CreateProject("noenproj", entities.RuntimeQEMU, entities.Resources{CPU: 1, MemoryMB: 512})

	err := runEnvDelete(envDeleteCmd, []string{"noenproj", "FOO"})
	if err == nil {
		t.Fatal("expected error when no env vars are set")
	}
	if !strings.Contains(err.Error(), "no environment variables") {
		t.Errorf("expected 'no environment variables' error, got: %v", err)
	}
}

func TestDecryptProjectEnv_MixedPlainAndEncrypted(t *testing.T) {
	dir := t.TempDir()
	cfg.DataDir = dir

	v, err := initVault()
	if err != nil {
		t.Fatalf("initVault: %v", err)
	}

	encrypted, _ := v.Encrypt("secret-value")

	env := map[string]string{
		"PLAIN":  "hello",
		"SECRET": encrypted,
	}

	result, err := decryptProjectEnv(env)
	if err != nil {
		t.Fatalf("decryptProjectEnv: %v", err)
	}

	if result["PLAIN"] != "hello" {
		t.Errorf("expected PLAIN=hello, got: %s", result["PLAIN"])
	}
	if result["SECRET"] != "secret-value" {
		t.Errorf("expected SECRET=secret-value, got: %s", result["SECRET"])
	}
}

func TestDecryptProjectEnv_AllPlain(t *testing.T) {
	env := map[string]string{"A": "1", "B": "2"}

	result, err := decryptProjectEnv(env)
	if err != nil {
		t.Fatalf("decryptProjectEnv: %v", err)
	}

	if result["A"] != "1" || result["B"] != "2" {
		t.Errorf("expected plain values unchanged, got: %v", result)
	}
}

func TestDecryptProjectEnv_Empty(t *testing.T) {
	result, err := decryptProjectEnv(map[string]string{})
	if err != nil {
		t.Fatalf("decryptProjectEnv: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty result, got: %v", result)
	}
}

func TestMergeProjectEnv(t *testing.T) {
	dir := t.TempDir()
	cfg.DataDir = dir

	v, _ := initVault()
	encrypted, _ := v.Encrypt("db-secret")

	projectEnv := map[string]string{
		"DB_HOST": "localhost",
		"DB_PASS": encrypted,
	}

	app := &entities.AppSpec{
		Env: map[string]string{
			"DB_HOST":  "prod-host", // App overrides project.
			"APP_PORT": "8080",
		},
	}

	if err := mergeProjectEnv(projectEnv, app); err != nil {
		t.Fatalf("mergeProjectEnv: %v", err)
	}

	if app.Env["DB_HOST"] != "prod-host" {
		t.Errorf("expected app override DB_HOST=prod-host, got: %s", app.Env["DB_HOST"])
	}
	if app.Env["DB_PASS"] != "db-secret" {
		t.Errorf("expected decrypted DB_PASS=db-secret, got: %s", app.Env["DB_PASS"])
	}
	if app.Env["APP_PORT"] != "8080" {
		t.Errorf("expected APP_PORT=8080, got: %s", app.Env["APP_PORT"])
	}
}

func TestMergeProjectEnv_EmptyProjectEnv(t *testing.T) {
	app := &entities.AppSpec{
		Env: map[string]string{"FOO": "bar"},
	}

	if err := mergeProjectEnv(nil, app); err != nil {
		t.Fatalf("mergeProjectEnv: %v", err)
	}

	if app.Env["FOO"] != "bar" {
		t.Errorf("expected app env unchanged, got: %v", app.Env)
	}
}
