package libvirt

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"libvirt.org/go/libvirt"

	"github.com/guilhermebr/goship/pkg/domain/entities"
)

const (
	// DomainPrefix is the naming prefix for all GoShip-managed libvirt domains.
	DomainPrefix = "goship-"
)

// connectToLibvirt opens a connection to the local libvirt daemon.
// It tries qemu:///system first (works for root and users with libvirt
// group membership or polkit rules). For non-root users, if the system
// connection fails it falls back to qemu:///session.
func connectToLibvirt() (*libvirt.Connect, error) {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err == nil {
		return conn, nil
	}

	if os.Getuid() != 0 {
		conn, err = libvirt.NewConnect("qemu:///session")
		if err == nil {
			fmt.Fprintf(os.Stderr, "WARNING: Could not connect to qemu:///system (permission denied).\n")
			fmt.Fprintf(os.Stderr, "  Using qemu:///session instead (per-user, limited features).\n")
			fmt.Fprintf(os.Stderr, "  To fix: add your user to the 'libvirt' group:\n")
			fmt.Fprintf(os.Stderr, "    sudo usermod -aG libvirt $USER && newgrp libvirt\n\n")
			return conn, nil
		}
	}

	return nil, fmt.Errorf("cannot connect to libvirt: %w", err)
}

// VMManager owns a libvirt connection and a data directory,
// providing high-level VM Create / Destroy / List operations.
type VMManager struct {
	conn    *libvirt.Connect
	dataDir string
}

// NewVMManager returns a VMManager backed by the given libvirt connection and a cleanup function.
// dataDir is the root directory for VM disk images (e.g. ~/.goship).
func NewVMManager(dataDir string) (*VMManager, func(), error) {
	conn, err := connectToLibvirt()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to libvirt: %w", err)
	}
	return &VMManager{conn: conn, dataDir: dataDir}, func() { conn.Close() }, nil
}

// CreateVMOptions holds the parameters for creating a new VM.
type CreateVMOptions struct {
	Name           string
	BaseImage      string
	MemoryMB       int64
	DiskMB         int64 // resize CoW overlay to this size (0 = inherit base image size)
	CPUs           int
	EnableKVM      bool
	SecurityNone   bool
	NetworkType    string
	NetworkSource  string
	Hostname       string // VM hostname (cloud-init)
	SSHKey         string // SSH public key content (cloud-init)
	InitBinaryPath string
	ProvisionGuest bool
	InstallDocker  bool
}

// DestroyResult holds the outcome of a Destroy operation.
type DestroyResult struct {
	DiskRemoved bool
	DiskDir     string
}

// VMInfo describes a GoShip-managed VM.
type VMInfo struct {
	Name   string // user-facing name (without prefix)
	Domain string // libvirt domain name (with prefix)
	UUID   string
	State  string
	Memory int64
	CPUs   int
	Disk   string
}

// domainName returns the libvirt domain name for a user-facing VM name.
func domainName(name string) string {
	return DomainPrefix + name
}

// userFacingName strips the DomainPrefix from a libvirt domain name.
func userFacingName(domain string) string {
	return strings.TrimPrefix(domain, DomainPrefix)
}

// vmDir returns the directory for a VM's disk images.
func vmDir(dataDir, name string) string {
	return filepath.Join(dataDir, "vms", name)
}

// diskPath returns the path for a VM's primary disk image.
func diskPath(dataDir, name string) string {
	return filepath.Join(vmDir(dataDir, name), "disk.qcow2")
}

// Create orchestrates full VM creation: verifies the base image, creates the
// CoW disk, generates domain XML, and defines+starts the VM via libvirt.
func (m *VMManager) Create(opts CreateVMOptions) (*VMInfo, error) {
	// Verify base image exists.
	if _, err := os.Stat(opts.BaseImage); os.IsNotExist(err) {
		return nil, fmt.Errorf("base image not found: %s", opts.BaseImage)
	}

	// Create VM directory.
	dir := vmDir(m.dataDir, opts.Name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create VM directory: %w", err)
	}
	if err := sanitizeVMDirACL(dir); err != nil {
		return nil, fmt.Errorf("failed to sanitize VM directory ACLs: %w", err)
	}

	success := false
	defer func() {
		if !success {
			os.RemoveAll(dir)
		}
	}()

	// Create CoW disk image.
	disk := diskPath(m.dataDir, opts.Name)
	if err := CreateDiskImage(opts.BaseImage, disk); err != nil {
		return nil, fmt.Errorf("failed to create disk image: %w", err)
	}

	// Resize CoW overlay so the guest has enough space for packages (e.g. Docker).
	if opts.DiskMB > 0 {
		size := fmt.Sprintf("%dM", opts.DiskMB)
		cmd := exec.Command("qemu-img", "resize", disk, size)
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to resize disk to %s: %w\n%s", size, err, string(out))
		}
	}

	if opts.ProvisionGuest {
		if err := ProvisionGuestDisk(GuestProvisionOptions{
			DiskPath:       disk,
			InitBinaryPath: opts.InitBinaryPath,
		}); err != nil {
			return nil, fmt.Errorf("failed to provision guest disk: %w", err)
		}
	}

	// Generate UUID.
	uuid, err := GenerateUUID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate UUID: %w", err)
	}

	// Generate cloud-init ISO if hostname or SSH key provided.
	var cdroms []CDROMDevice
	if opts.Hostname != "" || opts.SSHKey != "" {
		isoPath := filepath.Join(dir, "cloud-init.iso")
		hostname := opts.Hostname
		if hostname == "" {
			hostname = opts.Name
		}
		err := GenerateCloudInitISO(&CloudInitConfig{
			InstanceID:    uuid,
			Hostname:      hostname,
			SSHKey:        opts.SSHKey,
			InstallDocker: opts.InstallDocker,
		}, isoPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create cloud-init ISO: %w", err)
		}
		cdroms = append(cdroms, CDROMDevice{Path: isoPath, Format: "raw"})
	}

	// Build domain config.
	dName := domainName(opts.Name)
	config := &DomainConfig{
		Name:         dName,
		UUID:         uuid,
		MemoryMB:     opts.MemoryMB,
		EnableKVM:    opts.EnableKVM,
		SecurityNone: opts.SecurityNone,
		DACLabel:     fmt.Sprintf("+%d:+%d", os.Getuid(), os.Getgid()),
		CPU: entities.CPUTopology{
			Sockets: 1,
			Cores:   opts.CPUs,
			Threads: 1,
		},
		Disks: []entities.DiskDevice{
			{
				Path:   disk,
				Format: "qcow2",
				Bus:    "virtio",
			},
		},
		CDROMs: cdroms,
		Networks: []entities.NetworkDevice{
			{
				Type:   opts.NetworkType,
				Source: opts.NetworkSource,
				Model:  "virtio",
			},
		},
		Serials: []entities.SerialDevice{
			{
				Type:       "virtio-serial",
				SocketPath: filepath.Join(dir, "goship.sock"),
				PortName:   "goship.0",
			},
		},
	}

	// Generate domain XML.
	domainXML, err := GenerateDomainXML(config)
	if err != nil {
		return nil, fmt.Errorf("failed to generate domain XML: %w", err)
	}

	// Define and start VM.
	domain, err := CreateVM(m.conn, domainXML)
	if err != nil {
		return nil, fmt.Errorf("failed to create VM: %w", err)
	}
	defer func() { _ = domain.Free() }()

	success = true
	return &VMInfo{
		Name:   opts.Name,
		Domain: dName,
		UUID:   uuid,
		State:  "running",
		Memory: opts.MemoryMB,
		CPUs:   opts.CPUs,
		Disk:   disk,
	}, nil
}

// Destroy stops a VM, undefines it from libvirt, and optionally removes its disk directory.
func (m *VMManager) Destroy(name string, keepDisk bool) (*DestroyResult, error) {
	dName := domainName(name)

	domain, err := LookupVM(m.conn, dName)
	if err != nil {
		return nil, fmt.Errorf("VM %q not found: %w", name, err)
	}
	defer func() { _ = domain.Free() }()

	if err := DestroyVM(domain); err != nil {
		return nil, fmt.Errorf("failed to destroy VM: %w", err)
	}

	result := &DestroyResult{}
	if !keepDisk && m.dataDir != "" {
		dir := vmDir(m.dataDir, name)
		result.DiskDir = dir
		if err := os.RemoveAll(dir); err == nil {
			result.DiskRemoved = true
		}
	}

	return result, nil
}

// List returns all GoShip-managed VMs (domains with the DomainPrefix).
func (m *VMManager) List() ([]VMInfo, error) {
	domains, err := m.conn.ListAllDomains(0)
	if err != nil {
		return nil, fmt.Errorf("failed to list domains: %w", err)
	}

	// TODO: collect and surface per-domain errors instead of silently skipping.
	var vms []VMInfo
	for _, domain := range domains {
		name, err := domain.GetName()
		if err != nil {
			_ = domain.Free()
			continue
		}

		if !strings.HasPrefix(name, DomainPrefix) {
			_ = domain.Free()
			continue
		}

		state, _, err := GetVMState(&domain)
		if err != nil {
			_ = domain.Free()
			continue
		}

		vms = append(vms, VMInfo{
			Name:   userFacingName(name),
			Domain: name,
			State:  FormatVMState(state),
		})
		_ = domain.Free()
	}

	return vms, nil
}

// GenerateUUID generates a v4 UUID using crypto/rand.
func GenerateUUID() (string, error) {
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return "", err
	}
	// Set version 4 (bits 12-15 of time_hi_and_version)
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	// Set variant (bits 6-7 of clock_seq_hi_and_reserved)
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]), nil
}

// sanitizeVMDirACL removes inherited ACL entries that may strip write access
// from libvirt-created sockets. This is best-effort and only runs when setfacl
// is available on the host.
func sanitizeVMDirACL(dir string) error {
	if _, err := exec.LookPath("setfacl"); err != nil {
		return nil
	}

	commands := [][]string{
		{"-b", dir}, // remove access ACL entries
		{"-k", dir}, // remove default ACL entries
	}
	for _, args := range commands {
		cmd := exec.Command("setfacl", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("setfacl %s failed: %w\n%s", strings.Join(args, " "), err, string(out))
		}
	}

	if err := os.Chmod(dir, 0o755); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	return nil
}
