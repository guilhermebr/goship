package commands

// Config holds the CLI configuration. Defaults can be overridden by
// environment variables (prefix GOSHIP_) and CLI flags, in that order.
type Config struct {
	DataDir            string `conf:"default:~/.goship,env:GOSHIP_DATA_DIR"`
	Verbose            bool   `conf:"default:true,env:GOSHIP_VERBOSE"`
	InitBinaryPath     string `conf:"default:~/.goship/bin/goship-init,env:GOSHIP_INIT_BINARY"`
	SkipGuestProvision bool   `conf:"default:false,env:GOSHIP_SKIP_GUEST_PROVISION"`
	InstallDocker      bool   `conf:"default:true,env:GOSHIP_INSTALL_DOCKER"`
	LibvirtURI         string `conf:"default:qemu:///system,env:GOSHIP_LIBVIRT_URI"`
	NetworkType        string `conf:"default:,env:GOSHIP_NETWORK_TYPE"`
	NetworkSource      string `conf:"default:,env:GOSHIP_NETWORK_SOURCE"`
	APIURL             string `conf:"default:,env:GOSHIP_API_URL"`
}
