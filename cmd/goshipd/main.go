// GoShip API server (goshipd).
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ardanlabs/conf/v3"

	"github.com/guilhermebr/goship/internal/agent/runtime"
	"github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
	apiserver "github.com/guilhermebr/goship/internal/api"
	"github.com/guilhermebr/goship/internal/shared/state"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

type Config struct {
	Addr               string `conf:"default::8080"`
	DataDir            string `conf:"default:~/.goship"`
	InitBinaryPath     string `conf:"default:./bin/goship-init"`
	SkipGuestProvision bool   `conf:"default:false"`
	InstallDocker      bool   `conf:"default:true"`
	LibvirtURI         string `conf:"default:qemu:///system"`
	NetworkType        string
	NetworkSource      string
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

//nolint:funlen // server startup wiring
func run() error {
	var cfg Config

	help, err := conf.Parse("GOSHIP", &cfg)
	if err != nil {
		if errors.Is(err, conf.ErrHelpWanted) {
			fmt.Println(help)
			return nil
		}
		return fmt.Errorf("parsing config: %w", err)
	}

	logger := log.New(os.Stdout, "goshipd: ", log.LstdFlags)

	logger.Printf("goshipd %s (%s) built %s", Version, Commit, BuildTime)

	// Initialize state store.
	store, err := state.NewStore(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("failed to initialize state store: %w", err)
	}

	// Initialize libvirt runtime.
	opts := []runtime.RuntimeOption{
		runtime.WithDataDir(cfg.DataDir),
		runtime.WithVMImage(cfg.DataDir + "/images/goship-vm.qcow2"),
		runtime.WithInitBinary(cfg.InitBinaryPath),
		runtime.WithProvisionGuest(!cfg.SkipGuestProvision),
		runtime.WithInstallDocker(cfg.InstallDocker),
	}

	if cfg.LibvirtURI != "" {
		opts = append(opts, runtime.WithLibvirtURI(cfg.LibvirtURI))
	}

	netType := cfg.NetworkType
	netSource := cfg.NetworkSource
	if netType != "" {
		if netType == "network" && netSource == "" {
			netSource = "default"
		}
		opts = append(opts, runtime.WithNetwork(netType, netSource))
	}

	rt, err := libvirt.New(opts...)
	if err != nil {
		return fmt.Errorf("failed to initialize libvirt runtime: %w", err)
	}
	defer func() {
		if closeErr := rt.Close(); closeErr != nil {
			logger.Printf("failed to close runtime: %v", closeErr)
		}
	}()

	// Reconcile state with libvirt on startup.
	reconcileState(store, rt, logger)

	// Create API server.
	srv := apiserver.New(store, rt, logger)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start server in a goroutine.
	errCh := make(chan error, 1)
	go func() {
		logger.Printf("listening on %s", cfg.Addr)
		if listenErr := httpServer.ListenAndServe(); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			errCh <- listenErr
		}
	}()

	// Wait for interrupt signal.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		logger.Print("shutting down...")
	case listenErr := <-errCh:
		return fmt.Errorf("server error: %w", listenErr)
	}

	// Graceful shutdown with 10s timeout.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}

	logger.Print("server stopped")
	return nil
}

// reconcileState synchronizes the state store with actual libvirt domain states.
func reconcileState(store *state.Store, rt *libvirt.Runtime, logger *log.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	projects := store.ListProjects()
	for _, project := range projects {
		instance := store.GetInstance(project.ID)
		if instance == nil {
			continue
		}

		// Load all instances into the runtime so it knows about them.
		rt.LoadInstance(instance)

		switch instance.State {
		case entities.InstanceStateRunning, entities.InstanceStateStarting, entities.InstanceStateStopping:
		default:
			continue
		}

		oldState := instance.State
		status, err := rt.GetInstanceStatus(ctx, instance.ID)
		if err != nil {
			continue
		}

		if status.State == oldState {
			continue
		}

		instance.State = status.State
		_ = store.UpdateInstance(instance)

		newProjectState := mapInstanceToProjectState(status.State)
		if newProjectState != project.State {
			project.State = newProjectState
			_ = store.UpdateProject(project)
		}

		logger.Printf("reconciled project %s: %s -> %s", project.Name, oldState, status.State)
	}
}

// mapInstanceToProjectState derives the project state from an instance state.
func mapInstanceToProjectState(is entities.InstanceState) entities.ProjectState {
	switch is {
	case entities.InstanceStateRunning:
		return entities.ProjectStateRunning
	case entities.InstanceStateStopped:
		return entities.ProjectStateStopped
	case entities.InstanceStateStopping:
		return entities.ProjectStateStopping
	case entities.InstanceStateFailed:
		return entities.ProjectStateFailed
	default:
		return entities.ProjectStatePending
	}
}
