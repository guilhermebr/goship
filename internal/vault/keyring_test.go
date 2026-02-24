package vault

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateMasterKey_CreateOnFirst(t *testing.T) {
	dir := t.TempDir()

	key, err := LoadOrCreateMasterKey(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateMasterKey() error: %v", err)
	}

	if len(key) != masterKeyLen {
		t.Errorf("key length = %d, want %d", len(key), masterKeyLen)
	}

	// Verify file permissions.
	info, err := os.Stat(filepath.Join(dir, masterKeyFile))
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions = %o, want 600", perm)
	}
}

func TestLoadOrCreateMasterKey_LoadOnSecond(t *testing.T) {
	dir := t.TempDir()

	key1, err := LoadOrCreateMasterKey(dir)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}

	key2, err := LoadOrCreateMasterKey(dir)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}

	if string(key1) != string(key2) {
		t.Error("second call should return the same key")
	}
}

func TestLoadOrCreateMasterKey_TolerantOfTrailingNewline(t *testing.T) {
	dir := t.TempDir()

	// Create a key, then append a trailing newline to the file (simulating manual editing).
	key1, err := LoadOrCreateMasterKey(dir)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}

	keyPath := filepath.Join(dir, masterKeyFile)
	encoded := hex.EncodeToString(key1) + "\n"
	writeErr := os.WriteFile(keyPath, []byte(encoded), 0o600)
	if writeErr != nil {
		t.Fatalf("WriteFile error: %v", writeErr)
	}

	key2, err := LoadOrCreateMasterKey(dir)
	if err != nil {
		t.Fatalf("second call with trailing newline error: %v", err)
	}

	if string(key1) != string(key2) {
		t.Error("should load same key despite trailing newline")
	}
}

func TestLoadOrCreateMasterKey_DirectoryPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secure")

	_, err := LoadOrCreateMasterKey(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateMasterKey() error: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory permissions = %o, want 700", perm)
	}
}

func TestLoadOrCreateMasterKey_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")

	key, err := LoadOrCreateMasterKey(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateMasterKey() error: %v", err)
	}

	if len(key) != masterKeyLen {
		t.Errorf("key length = %d, want %d", len(key), masterKeyLen)
	}
}
