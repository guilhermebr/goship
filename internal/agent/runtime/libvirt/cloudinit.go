package libvirt

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// CloudInitConfig holds the parameters for generating a cloud-init NoCloud ISO.
type CloudInitConfig struct {
	InstanceID    string // unique ID (UUID); cloud-init uses this to detect re-runs
	Hostname      string
	SSHKey        string // public key content (not path)
	InstallDocker bool   // install Docker via cloud-init packages
	RegistryAddr  string // host registry address for insecure-registries (e.g., "192.168.122.1:5000")
}

// GenerateCloudInitISO creates a NoCloud ISO at outputPath containing the
// cloud-init meta-data and user-data derived from config.
//
// It requires either genisoimage or mkisofs to be available on the host.
//
//nolint:funlen // Cloud-init ISO generation requires sequential steps
func GenerateCloudInitISO(config *CloudInitConfig, outputPath string) error {
	tmpDir, err := os.MkdirTemp("", "goship-cloudinit-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Write meta-data.
	metaData := fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", config.InstanceID, config.Hostname)
	metaDataPath := filepath.Join(tmpDir, "meta-data")
	if writeErr := os.WriteFile(metaDataPath, []byte(metaData), 0o644); writeErr != nil {
		return fmt.Errorf("failed to write meta-data: %w", writeErr)
	}

	// Write user-data.
	// - "default" preserves the image's default user (alpine) so SSH access works
	//   even if goship user creation fails.
	// - Top-level ssh_authorized_keys applies to the default user (proven to work
	//   with Alpine's cloud-init).
	// - The goship user gets its own SSH key nested under the users entry.
	userData := "#cloud-config\n"
	userData += fmt.Sprintf("hostname: %s\n", config.Hostname)

	// Docker: install packages via cloud-init (VM has network access, unlike virt-customize).
	if config.InstallDocker {
		userData += "packages:\n"
		userData += "  - docker\n"
		userData += "  - docker-cli\n"
		userData += "runcmd:\n"
		// cgroups must be running before Docker can start on Alpine.
		userData += "  - [rc-update, add, cgroups, boot]\n"
		userData += "  - [service, cgroups, start]\n"
		// Docker must be in the 'default' runlevel (not 'boot') because its init
		// script has 'need net'. If Docker is in 'boot', cloud-init's network
		// reconfiguration during the boot→default transition stops Docker and it
		// fails to restart ("cannot start docker as networking would not start").
		userData += "  - [rc-update, add, docker, default]\n"
		userData += "  - [service, docker, start]\n"

		// Configure insecure registry after Docker is installed and started.
		// We use runcmd (not write_files) because /etc/docker/ doesn't exist
		// until the docker package is installed above.
		if config.RegistryAddr != "" {
			userData += "  - [mkdir, -p, /etc/docker]\n"
			// Use the shell string form (not list form) so we can use redirection.
			userData += fmt.Sprintf(
				"  - echo '{\"insecure-registries\":[\"%s\"]}' > /etc/docker/daemon.json\n",
				config.RegistryAddr)
			userData += "  - [service, docker, restart]\n"
		}
	}

	goshipGroups := "wheel"
	if config.InstallDocker {
		goshipGroups = "wheel, docker"
	}

	userData += "users:\n"
	userData += "  - default\n"
	userData += "  - name: goship\n"
	userData += "    shell: /bin/sh\n"
	userData += fmt.Sprintf("    groups: %s\n", goshipGroups)
	userData += "    sudo: ALL=(ALL) NOPASSWD:ALL\n"
	userData += "    lock_passwd: false\n"
	userData += "    plain_text_passwd: goship\n"
	if config.SSHKey != "" {
		userData += "    ssh_authorized_keys:\n"
		userData += fmt.Sprintf("      - %s\n", config.SSHKey)
		userData += "ssh_authorized_keys:\n"
		userData += fmt.Sprintf("  - %s\n", config.SSHKey)
	}
	userDataPath := filepath.Join(tmpDir, "user-data")
	if writeErr := os.WriteFile(userDataPath, []byte(userData), 0o644); writeErr != nil {
		return fmt.Errorf("failed to write user-data: %w", writeErr)
	}

	// Find ISO generation tool.
	isoTool, err := findISOTool()
	if err != nil {
		return err
	}

	// Generate ISO.
	cmd := exec.Command(
		isoTool,
		"-output",
		outputPath,
		"-volid",
		"cidata",
		"-joliet",
		"-rock",
		userDataPath,
		metaDataPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w\n%s", isoTool, err, output)
	}

	return nil
}

// findISOTool returns the path to genisoimage or mkisofs, whichever is available.
func findISOTool() (string, error) {
	if path, err := exec.LookPath("genisoimage"); err == nil {
		return path, nil
	}
	if path, err := exec.LookPath("mkisofs"); err == nil {
		return path, nil
	}
	return "", errors.New("neither genisoimage nor mkisofs found in PATH; install one to use cloud-init")
}
