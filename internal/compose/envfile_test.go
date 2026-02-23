package compose

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	content := `# Database credentials
POSTGRES_USER=awa_user
POSTGRES_PASSWORD=awa_password
POSTGRES_DB=awa

# With export prefix
export API_KEY=my-api-key

# With double quotes
QUOTED_VAL="hello world"

# With single quotes
SINGLE_QUOTED='single value'

# Inline comment
INLINE=value # this is a comment

# Empty value
EMPTY_VAL=

# Value with equals sign
CONNECTION_STRING=host=localhost port=5432
`

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	env, err := LoadEnvFile(path)
	if err != nil {
		t.Fatalf("LoadEnvFile() error: %v", err)
	}

	tests := []struct {
		key  string
		want string
	}{
		{"POSTGRES_USER", "awa_user"},
		{"POSTGRES_PASSWORD", "awa_password"},
		{"POSTGRES_DB", "awa"},
		{"API_KEY", "my-api-key"},
		{"QUOTED_VAL", "hello world"},
		{"SINGLE_QUOTED", "single value"},
		{"INLINE", "value"},
		{"EMPTY_VAL", ""},
		{"CONNECTION_STRING", "host=localhost port=5432"},
	}

	for _, tt := range tests {
		got, ok := env[tt.key]
		if !ok {
			t.Errorf("key %q not found", tt.key)
			continue
		}
		if got != tt.want {
			t.Errorf("key %q: got %q, want %q", tt.key, got, tt.want)
		}
	}

	// Verify comments and blank lines are not parsed.
	if _, ok := env["#"]; ok {
		t.Error("comment line parsed as key")
	}
}

func TestLoadEnvFile_NotFound(t *testing.T) {
	_, err := LoadEnvFile("/nonexistent/.env")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestParseEnvFileField_String(t *testing.T) {
	paths, err := parseEnvFileField(".env.compose")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 1 || paths[0] != ".env.compose" {
		t.Errorf("got %v, want [.env.compose]", paths)
	}
}

func TestParseEnvFileField_List(t *testing.T) {
	input := []any{".env", ".env.local"}
	paths, err := parseEnvFileField(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 2 || paths[0] != ".env" || paths[1] != ".env.local" {
		t.Errorf("got %v, want [.env .env.local]", paths)
	}
}

func TestParseEnvFileField_InvalidType(t *testing.T) {
	_, err := parseEnvFileField(42)
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
}

func TestStripQuotes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`"hello"`, "hello"},
		{`'hello'`, "hello"},
		{"hello", "hello"},
		{`"hello`, `"hello`},
		{"", ""},
		{`""`, ""},
		{`"value with spaces"`, "value with spaces"},
		{"value # comment", "value"},
	}

	for _, tt := range tests {
		got := stripQuotes(tt.input)
		if got != tt.want {
			t.Errorf("stripQuotes(%q): got %q, want %q", tt.input, got, tt.want)
		}
	}
}
