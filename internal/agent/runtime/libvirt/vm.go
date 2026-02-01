package libvirt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"libvirt.org/go/libvirt"
)

// CreateDiskImage creates a CoW (copy-on-write) qcow2 disk image backed by basePath.
func CreateDiskImage(basePath, diskPath string) error {
	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		return fmt.Errorf("base image not found: %s", basePath)
	}

	// Convert to absolute path so qemu-img resolves the backing file correctly
	// regardless of where the disk image is created.
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return fmt.Errorf("failed to resolve base image path: %w", err)
	}

	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", absBase, diskPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("qemu-img create failed: %w\n%s", err, output)
	}

	return nil
}

// CreateVM defines and starts a VM from domain XML.
func CreateVM(conn *libvirt.Connect, domainXML string) (*libvirt.Domain, error) {
	domain, err := conn.DomainDefineXML(domainXML)
	if err != nil {
		return nil, fmt.Errorf("failed to define domain: %w", err)
	}

	if err := domain.Create(); err != nil {
		// Clean up the defined domain if start fails.
		_ = domain.Undefine()
		return nil, fmt.Errorf("failed to start domain: %w", err)
	}

	return domain, nil
}

// DestroyVM stops a running VM and undefines it. Idempotent: safe to call on
// already-stopped or undefined domains.
func DestroyVM(domain *libvirt.Domain) error {
	state, _, err := GetVMState(domain)
	if err != nil {
		return fmt.Errorf("failed to get domain state: %w", err)
	}

	if state == libvirt.DOMAIN_RUNNING || state == libvirt.DOMAIN_PAUSED {
		if err := domain.Destroy(); err != nil {
			return fmt.Errorf("failed to destroy domain: %w", err)
		}
	}

	if err := domain.Undefine(); err != nil {
		return fmt.Errorf("failed to undefine domain: %w", err)
	}

	return nil
}

// GetVMState returns the current state of a domain.
func GetVMState(domain *libvirt.Domain) (libvirt.DomainState, int, error) {
	state, reason, err := domain.GetState()
	if err != nil {
		return libvirt.DOMAIN_NOSTATE, 0, fmt.Errorf("failed to get state: %w", err)
	}
	return state, reason, nil
}

// LookupVM finds a domain by name.
func LookupVM(conn *libvirt.Connect, name string) (*libvirt.Domain, error) {
	domain, err := conn.LookupDomainByName(name)
	if err != nil {
		return nil, fmt.Errorf("domain %q not found: %w", name, err)
	}
	return domain, nil
}

// FormatVMState returns a human-readable string for a domain state.
func FormatVMState(state libvirt.DomainState) string {
	switch state {
	case libvirt.DOMAIN_NOSTATE:
		return "no state"
	case libvirt.DOMAIN_RUNNING:
		return "running"
	case libvirt.DOMAIN_BLOCKED:
		return "blocked"
	case libvirt.DOMAIN_PAUSED:
		return "paused"
	case libvirt.DOMAIN_SHUTDOWN:
		return "shutting down"
	case libvirt.DOMAIN_SHUTOFF:
		return "shut off"
	case libvirt.DOMAIN_CRASHED:
		return "crashed"
	case libvirt.DOMAIN_PMSUSPENDED:
		return "pm suspended"
	default:
		return "unknown"
	}
}
