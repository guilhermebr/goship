package libvirt

import (
	"fmt"

	"libvirt.org/go/libvirt"
)

// VersionInfo holds formatted libvirt and QEMU version strings.
type VersionInfo struct {
	LibvirtVersion string
	QEMUVersion    string
}

// GetVersionInfo connects to libvirt at the given URI and returns version info.
func GetVersionInfo() (*VersionInfo, error) {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return nil, fmt.Errorf("connecting to libvirt: %w", err)
	}
	defer func() { _, _ = conn.Close() }()

	// Get libvirt version
	libVer, err := conn.GetLibVersion()
	if err != nil {
		return nil, fmt.Errorf("getting libvirt version: %w", err)
	}

	// Get QEMU version from hypervisor
	qemuVer, err := conn.GetVersion()
	if err != nil {
		return nil, fmt.Errorf("getting QEMU version: %w", err)
	}

	return &VersionInfo{
		LibvirtVersion: formatVersion(libVer),
		QEMUVersion:    formatVersion(qemuVer),
	}, nil
}

// formatVersion formats a libvirt encoded version (major*1000000 + minor*1000 + patch)
// into a "major.minor.patch" string.
func formatVersion(ver uint32) string {
	const (
		majorDivisor = 1000000
		minorDivisor = 1000
	)
	major := ver / majorDivisor
	minor := (ver / minorDivisor) % minorDivisor
	patch := ver % minorDivisor
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}
