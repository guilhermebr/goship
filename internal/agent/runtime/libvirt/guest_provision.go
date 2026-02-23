package libvirt

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	// defaultResolvConf is injected into the guest during provisioning to avoid
	// DNS issues in environments where host resolv.conf points to local stubs.
	defaultResolvConf = "nameserver 8.8.8.8\nnameserver 1.1.1.1\n"
)

// openrcServiceScript is installed inside the VM and enabled on boot.
const openrcServiceScript = `#!/sbin/openrc-run

name="goship-init"
description="GoShip Init agent"
command="/opt/goship/goship-init"
command_background="yes"
pidfile="/run/${RC_SVCNAME}.pid"
output_log="/var/log/goship-init.log"
error_log="/var/log/goship-init.log"

depend() {
    after localmount
}
`

var (
	lookPath = exec.LookPath
	runCmd   = func(name string, args ...string) ([]byte, error) {
		cmd := exec.Command(name, args...)
		return cmd.CombinedOutput()
	}
)

// GuestProvisionOptions controls per-VM guest disk customization.
type GuestProvisionOptions struct {
	DiskPath       string
	InitBinaryPath string
}

// CheckGuestProvisionDependencies validates host-side tooling used to customize
// a guest disk before VM boot.
func CheckGuestProvisionDependencies() error {
	if _, err := lookPath("virt-customize"); err != nil {
		return errors.New("virt-customize not found (install: sudo apt install libguestfs-tools)")
	}
	return nil
}

// ProvisionGuestDisk injects goship-init and service wiring into a VM overlay disk.
func ProvisionGuestDisk(opts GuestProvisionOptions) error {
	if opts.DiskPath == "" {
		return errors.New("disk path is required")
	}
	if opts.InitBinaryPath == "" {
		return errors.New("goship-init binary path is required")
	}
	if _, err := os.Stat(opts.DiskPath); err != nil {
		return fmt.Errorf("disk not found: %w", err)
	}
	if _, err := os.Stat(opts.InitBinaryPath); err != nil {
		return fmt.Errorf("goship-init binary not found: %w", err)
	}
	if err := CheckGuestProvisionDependencies(); err != nil {
		return err
	}

	serviceFile, err := os.CreateTemp("", "goship-init-service-*")
	if err != nil {
		return fmt.Errorf("creating temp service file: %w", err)
	}
	defer func() { _ = os.Remove(serviceFile.Name()) }()

	if _, writeErr := serviceFile.WriteString(openrcServiceScript); writeErr != nil {
		_ = serviceFile.Close()
		return fmt.Errorf("writing service script: %w", writeErr)
	}
	if chmodErr := serviceFile.Chmod(0o755); chmodErr != nil {
		_ = serviceFile.Close()
		return fmt.Errorf("setting service script permissions: %w", chmodErr)
	}
	if closeErr := serviceFile.Close(); closeErr != nil {
		return fmt.Errorf("closing service script: %w", closeErr)
	}

	resolvFile, err := os.CreateTemp("", "goship-resolv-*")
	if err != nil {
		return fmt.Errorf("creating temp resolv.conf: %w", err)
	}
	defer func() { _ = os.Remove(resolvFile.Name()) }()

	if _, writeErr := resolvFile.WriteString(defaultResolvConf); writeErr != nil {
		_ = resolvFile.Close()
		return fmt.Errorf("writing resolv.conf: %w", writeErr)
	}
	if closeErr := resolvFile.Close(); closeErr != nil {
		return fmt.Errorf("closing resolv.conf: %w", closeErr)
	}

	args := buildVirtCustomizeArgs(opts, serviceFile.Name(), resolvFile.Name())
	out, err := runCmd("virt-customize", args...)
	if err != nil {
		return fmt.Errorf("virt-customize failed: %w\n%s", err, string(out))
	}

	return nil
}

func buildVirtCustomizeArgs(opts GuestProvisionOptions, serviceScriptPath, resolvPath string) []string {
	args := []string{
		"-a", opts.DiskPath,
		"--mkdir", "/opt/goship",
		"--copy-in", opts.InitBinaryPath + ":/opt/goship/",
		"--chmod", "0755:/opt/goship/goship-init",
		"--copy-in", serviceScriptPath + ":/etc/init.d/",
		"--move", "/etc/init.d/" + filepath.Base(serviceScriptPath) + ":/etc/init.d/goship-init",
		"--chmod", "0755:/etc/init.d/goship-init",
		"--run-command", "rc-update add goship-init default",
		"--upload", resolvPath + ":/etc/resolv.conf",
	}

	return args
}
