// GoShip API server (goshipd).
package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/ardanlabs/conf/v3"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

type Config struct {
	Addr               string `conf:"default::8080,env:GOSHIP_ADDR"`
	DataDir            string `conf:"default:~/.goship,env:GOSHIP_DATA_DIR"`
	InitBinaryPath     string `conf:"default:./bin/goship-init,env:GOSHIP_INIT_BINARY"`
	SkipGuestProvision bool   `conf:"default:false,env:GOSHIP_SKIP_GUEST_PROVISION"`
	InstallDocker      bool   `conf:"default:true,env:GOSHIP_INSTALL_DOCKER"`
	LibvirtURI         string `conf:"default:qemu:///system,env:GOSHIP_LIBVIRT_URI"`
	NetworkType        string `conf:"default:,env:GOSHIP_NETWORK_TYPE"`
	NetworkSource      string `conf:"default:,env:GOSHIP_NETWORK_SOURCE"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

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

	fmt.Fprintf(os.Stdout, "goshipd %s (%s) built %s\n", Version, Commit, BuildTime)
	fmt.Fprintf(os.Stdout, "listening on %s (data: %s)\n", cfg.Addr, cfg.DataDir)

	// TODO: wire up HTTP server, runtime, and state store.

	return nil
}
