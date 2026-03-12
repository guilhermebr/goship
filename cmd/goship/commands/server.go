package commands

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/guilhermebr/goship/internal/agent/runtime"
	"github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
	apiserver "github.com/guilhermebr/goship/internal/api"
	"github.com/guilhermebr/goship/internal/proxy"
	"github.com/guilhermebr/goship/internal/registry"
	"github.com/guilhermebr/goship/internal/shared/state"
	"github.com/guilhermebr/goship/pkg/domain/entities"
)

// serverCmd starts the GoShip API server and reverse proxy.
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the GoShip API server and reverse proxy",
	Long: `Start the GoShip API server (default :8080) and reverse proxy (default :8081).

The server provides a REST API for managing projects and apps, and a reverse
proxy that routes HTTP traffic to apps running inside VMs based on domain names.`,
	RunE: runServer,
	// Override parent's PersistentPreRunE/PersistentPostRunE — the server
	// manages its own lifecycle (runtime, store, shutdown).
	PersistentPreRunE:  func(cmd *cobra.Command, args []string) error { return nil },
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error { return nil },
}

//nolint:funlen // server startup wiring
func runServer(cmd *cobra.Command, args []string) error {
	logger := log.New(os.Stdout, "goship: ", log.LstdFlags)

	logger.Printf("goship server %s (%s) built %s", version, commit, buildTime)

	// Initialize state store.
	serverStore, err := state.NewStore(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("failed to initialize state store: %w", err)
	}

	// Initialize libvirt runtime.
	// Compute VM-visible registry address using the host bridge IP.
	// VMs can't use localhost, so we use the default libvirt bridge IP (192.168.122.1).
	vmRegistryAddr := fmt.Sprintf("192.168.122.1:%s",
		strings.TrimPrefix(cfg.RegistryAddr, ":"))

	opts := []runtime.RuntimeOption{
		runtime.WithDataDir(cfg.DataDir),
		runtime.WithVMImage(cfg.DataDir + "/images/goship-vm.qcow2"),
		runtime.WithInitBinary(cfg.InitBinaryPath),
		runtime.WithProvisionGuest(!cfg.SkipGuestProvision),
		runtime.WithInstallDocker(cfg.InstallDocker),
		runtime.WithRegistryAddr(vmRegistryAddr),
	}

	if cfg.LibvirtURI != "" {
		opts = append(opts, runtime.WithLibvirtURI(cfg.LibvirtURI))
	}

	netType := cfg.NetworkType
	netSource := cfg.NetworkSource
	if netType != "" {
		if netType == netTypeNetwork && netSource == "" {
			netSource = netSourceDefault
		}
		opts = append(opts, runtime.WithNetwork(netType, netSource))
	}

	serverRT, err := libvirt.New(opts...)
	if err != nil {
		return fmt.Errorf("failed to initialize libvirt runtime: %w", err)
	}
	defer func() {
		if closeErr := serverRT.Close(); closeErr != nil {
			logger.Printf("failed to close runtime: %v", closeErr)
		}
	}()

	// Reconcile state with libvirt on startup.
	serverReconcileState(serverStore, serverRT, logger)

	// Initialize embedded container registry.
	reg, err := registry.New(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("failed to initialize registry: %w", err)
	}

	// Create route table and API server.
	routes := proxy.NewRouteTable()
	srv := apiserver.New(serverStore, serverRT, logger, routes, reg)
	srv.RebuildRoutes()

	httpServer := &http.Server{
		Addr:              cfg.ServerAddr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start reverse proxy.
	proxyServer := proxy.New(cfg.ProxyAddr, routes)
	go func() {
		logger.Printf("proxy listening on %s", cfg.ProxyAddr)
		if proxyErr := proxyServer.ListenAndServe(); proxyErr != nil && !errors.Is(proxyErr, http.ErrServerClosed) {
			logger.Printf("proxy error: %v", proxyErr)
		}
	}()

	// Start embedded container registry.
	registryServer := &http.Server{
		Addr:              cfg.RegistryAddr,
		Handler:           reg.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		logger.Printf("registry listening on %s", cfg.RegistryAddr)
		if regErr := registryServer.ListenAndServe(); regErr != nil && !errors.Is(regErr, http.ErrServerClosed) {
			logger.Printf("registry error: %v", regErr)
		}
	}()

	// Start server in a goroutine.
	errCh := make(chan error, 1)
	go func() {
		logger.Printf("listening on %s", cfg.ServerAddr)
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

	if err := proxyServer.Close(); err != nil {
		logger.Printf("failed to close proxy: %v", err)
	}

	if err := registryServer.Shutdown(shutdownCtx); err != nil {
		logger.Printf("failed to shutdown registry: %v", err)
	}

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}

	logger.Print("server stopped")
	return nil
}

// serverReconcileState synchronizes the state store with actual libvirt domain states.
func serverReconcileState(s *state.Store, r *libvirt.Runtime, logger *log.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	projects := s.ListProjects()
	for _, project := range projects {
		instance := s.GetInstance(project.ID)
		if instance == nil {
			continue
		}

		// Load all instances into the runtime so it knows about them.
		r.LoadInstance(instance)

		switch instance.State {
		case entities.InstanceStateRunning, entities.InstanceStateStarting, entities.InstanceStateStopping:
		default:
			continue
		}

		oldState := instance.State
		status, err := r.GetInstanceStatus(ctx, instance.ID)
		if err != nil {
			continue
		}

		if status.State == oldState {
			continue
		}

		instance.State = status.State
		_ = s.UpdateInstance(instance)

		newProjectState := mapInstanceToProjectState(status.State)
		if newProjectState != project.State {
			project.State = newProjectState
			_ = s.UpdateProject(project)
		}

		logger.Printf("reconciled project %s: %s -> %s", project.Name, oldState, status.State)
	}
}
