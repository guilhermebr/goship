package libvirt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// CloudInitConfig holds the parameters for generating a cloud-init NoCloud ISO.
type CloudInitConfig struct {
	InstanceID string // unique ID (UUID); cloud-init uses this to detect re-runs
	Hostname   string
	SSHKey     string // public key content (not path)
}

// GenerateCloudInitISO creates a NoCloud ISO at outputPath containing the
// cloud-init meta-data and user-data derived from config.
//
// It requires either genisoimage or mkisofs to be available on the host.
func GenerateCloudInitISO(config *CloudInitConfig, outputPath string) error {
	tmpDir, err := os.MkdirTemp("", "goship-cloudinit-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write meta-data.
	metaData := fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", config.InstanceID, config.Hostname)
	metaDataPath := filepath.Join(tmpDir, "meta-data")
	if err := os.WriteFile(metaDataPath, []byte(metaData), 0644); err != nil {
		return fmt.Errorf("failed to write meta-data: %w", err)
	}

	// Write user-data.
	// - "default" preserves the image's default user (alpine) so SSH access works
	//   even if goship user creation fails.
	// - Top-level ssh_authorized_keys applies to the default user (proven to work
	//   with Alpine's cloud-init).
	// - The goship user gets its own SSH key nested under the users entry.
	userData := "#cloud-config\n"
	userData += fmt.Sprintf("hostname: %s\n", config.Hostname)
	userData += "users:\n"
	userData += "  - default\n"
	userData += "  - name: goship\n"
	userData += "    shell: /bin/sh\n"
	userData += "    groups: wheel\n"
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
	if err := os.WriteFile(userDataPath, []byte(userData), 0644); err != nil {
		return fmt.Errorf("failed to write user-data: %w", err)
	}

	// Find ISO generation tool.
	isoTool, err := findISOTool()
	if err != nil {
		return err
	}

	// Generate ISO.
	cmd := exec.Command(isoTool, "-output", outputPath, "-volid", "cidata", "-joliet", "-rock", userDataPath, metaDataPath)
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
	return "", fmt.Errorf("neither genisoimage nor mkisofs found in PATH; install one to use cloud-init")
}
