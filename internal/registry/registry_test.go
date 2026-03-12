package registry_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guilhermebr/goship/internal/registry"
)

func TestNew(t *testing.T) {
	reg, err := registry.New(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reg.Handler() == nil {
		t.Fatal("expected non-nil handler")
	}

	if reg.StoreDir() == "" {
		t.Fatal("expected non-empty store dir")
	}
}

func TestRegistryV2Endpoint(t *testing.T) {
	reg, err := registry.New(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ts := httptest.NewServer(reg.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v2/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestCleanupProject(t *testing.T) {
	reg, err := registry.New(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cleanup non-existent project should not error.
	if err := reg.CleanupProject("nonexistent"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
