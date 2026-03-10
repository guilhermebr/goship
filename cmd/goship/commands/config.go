package commands

// Config holds the CLI configuration. Defaults can be overridden by
// environment variables (prefix GOSHIP_) and CLI flags, in that order.
// Environment variable names are derived automatically from the prefix
// and field name (e.g. DataDir -> GOSHIP_DATA_DIR).
type Config struct {
	DataDir            string `conf:"default:~/.goship"`
	Verbose            bool   `conf:"default:true"`
	InitBinaryPath     string `conf:"default:~/.goship/bin/goship-init"`
	SkipGuestProvision bool   `conf:"default:false"`
	InstallDocker      bool   `conf:"default:true"`
	LibvirtURI         string `conf:"default:qemu:///system"`
	NetworkType        string
	NetworkSource      string
	ApiUrl             string //nolint:revive // APIURL would derive env var GOSHIP_APIURL instead of GOSHIP_API_URL
	Direct             bool   `conf:"default:false"`
	ServerAddr         string `conf:"default::8080"`
	ProxyAddr          string `conf:"default::8081"`
}
