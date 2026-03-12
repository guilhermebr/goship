package registry

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/registry"
)

// Registry wraps an OCI-compliant container registry backed by disk storage.
type Registry struct {
	handler  http.Handler
	storeDir string
}

// New creates a new Registry with disk-based blob storage under dataDir/registry/.
func New(dataDir string) (*Registry, error) {
	storeDir := filepath.Join(dataDir, "registry")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create registry storage dir: %w", err)
	}

	blobHandler := registry.NewDiskBlobHandler(storeDir)
	handler := registry.New(registry.WithBlobHandler(blobHandler))

	return &Registry{
		handler:  handler,
		storeDir: storeDir,
	}, nil
}

// Handler returns the http.Handler for the registry.
func (r *Registry) Handler() http.Handler {
	return r.handler
}

// StoreDir returns the path to the registry's storage directory.
func (r *Registry) StoreDir() string {
	return r.storeDir
}

// CleanupProject removes all registry storage for a project namespace.
// Project images are stored under the namespace "goship-<project>/".
func (r *Registry) CleanupProject(projectName string) error {
	namespace := fmt.Sprintf("goship-%s", projectName)
	nsDir := filepath.Join(r.storeDir, namespace)

	if _, err := os.Stat(nsDir); os.IsNotExist(err) {
		return nil
	}

	return os.RemoveAll(nsDir)
}
