package commands

import (
	"testing"

	"github.com/ardanlabs/conf/v3"
)

func TestConfigEnvVars_String(t *testing.T) {
	tests := []struct {
		envVar string
		getter func(Config) string
	}{
		{"GOSHIP_DATA_DIR", func(c Config) string { return c.DataDir }},
		{"GOSHIP_INIT_BINARY_PATH", func(c Config) string { return c.InitBinaryPath }},
		{"GOSHIP_LIBVIRT_URI", func(c Config) string { return c.LibvirtURI }},
		{"GOSHIP_NETWORK_TYPE", func(c Config) string { return c.NetworkType }},
		{"GOSHIP_NETWORK_SOURCE", func(c Config) string { return c.NetworkSource }},
		{"GOSHIP_API_URL", func(c Config) string { return c.ApiUrl }},
	}

	for _, tt := range tests {
		t.Run(tt.envVar, func(t *testing.T) {
			t.Setenv(tt.envVar, "test-value")
			got := parseConfig(t)
			if val := tt.getter(got); val != "test-value" {
				t.Fatalf("expected %q from env %s, got %q", "test-value", tt.envVar, val)
			}
		})
	}
}

func TestConfigEnvVars_Bool(t *testing.T) {
	tests := []struct {
		envVar string
		getter func(Config) bool
	}{
		{"GOSHIP_VERBOSE", func(c Config) bool { return c.Verbose }},
		{"GOSHIP_SKIP_GUEST_PROVISION", func(c Config) bool { return c.SkipGuestProvision }},
		{"GOSHIP_INSTALL_DOCKER", func(c Config) bool { return c.InstallDocker }},
	}

	for _, tt := range tests {
		t.Run(tt.envVar, func(t *testing.T) {
			t.Setenv(tt.envVar, "true")
			got := parseConfig(t)
			if !tt.getter(got) {
				t.Fatalf("expected true from env %s", tt.envVar)
			}
		})
	}
}

func parseConfig(t *testing.T) Config {
	t.Helper()
	var c Config
	if _, err := conf.Parse("GOSHIP", &c); err != nil {
		t.Fatalf("conf.Parse failed: %v", err)
	}
	return c
}
