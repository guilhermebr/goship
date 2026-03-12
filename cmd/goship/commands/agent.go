package commands

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ardanlabs/conf/v3"
	"github.com/spf13/cobra"

	"github.com/guilhermebr/goship/internal/agent"
	"github.com/guilhermebr/goship/internal/agent/runtime"
	"github.com/guilhermebr/goship/internal/agent/runtime/libvirt"
)

// AgentConfig holds agent-specific configuration.
type AgentConfig struct {
	NodeID    string        `conf:""`
	ServerUrl string        `conf:"default:http://localhost:8080"` //nolint:revive // SERVER_URL
	Interval  time.Duration `conf:"default:10s"`
}

var agentCfg AgentConfig

// agentCmd starts the GoShip node agent.
var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Start the GoShip node agent",
	Long: `Start the GoShip node agent that sends heartbeats to the control plane
and reconciles desired state by pulling project/app configurations.

The agent requires a node ID (either previously registered or auto-registered).`,
	RunE: runAgent,
	// Override parent's PersistentPreRunE/PersistentPostRunE — the agent
	// manages its own lifecycle (runtime, shutdown).
	PersistentPreRunE:  func(cmd *cobra.Command, args []string) error { return nil },
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error { return nil },
}

func init() {
	// Parse agent-specific env vars (prefix GOSHIP_).
	_, _ = conf.Parse("GOSHIP", &agentCfg)

	agentCmd.Flags().StringVar(&agentCfg.NodeID, "node-id", agentCfg.NodeID,
		"Node ID for this agent (required, env: GOSHIP_NODE_ID)")
	agentCmd.Flags().StringVar(&agentCfg.ServerUrl, "server-url", agentCfg.ServerUrl,
		"GoShip control plane URL (env: GOSHIP_SERVER_URL)")
	agentCmd.Flags().DurationVar(&agentCfg.Interval, "interval", agentCfg.Interval,
		"Heartbeat and reconciliation interval (env: GOSHIP_INTERVAL)")
}

func runAgent(_ *cobra.Command, _ []string) error {
	if agentCfg.NodeID == "" {
		return errors.New("--node-id is required")
	}

	logger := log.New(os.Stdout, "goship-agent: ", log.LstdFlags)
	logger.Printf("goship agent %s (%s) built %s", version, commit, buildTime)

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
		if netType == "network" && netSource == "" { //nolint:goconst // matches existing pattern in root.go/server.go
			netSource = "default" //nolint:goconst // matches existing pattern
		}
		opts = append(opts, runtime.WithNetwork(netType, netSource))
	}

	agentRT, err := libvirt.New(opts...)
	if err != nil {
		return fmt.Errorf("failed to initialize libvirt runtime: %w", err)
	}
	defer func() {
		if closeErr := agentRT.Close(); closeErr != nil {
			logger.Printf("failed to close runtime: %v", closeErr)
		}
	}()

	a := agent.New(agentCfg.NodeID, agentCfg.ServerUrl, agentRT, agentCfg.Interval, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return a.Run(ctx)
}
