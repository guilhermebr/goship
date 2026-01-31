package entities

// =============================================================================
// VM Topology Types - Expose virtualization primitives
// =============================================================================

// MemoryBacking defines memory backing options for a VM.
type MemoryBacking struct {
	// Hugepages enables hugepages for memory backing
	Hugepages bool `json:"hugepages,omitempty"`
	// HugepageSize in KB (2048 for 2MB, 1048576 for 1GB)
	HugepageSize int64 `json:"hugepage_size,omitempty"`
	// Source is the memory source type (anonymous, file, memfd)
	Source string `json:"source,omitempty"`
	// Locked prevents memory from being swapped
	Locked bool `json:"locked,omitempty"`
}

// DiskDevice defines a disk device configuration.
type DiskDevice struct {
	// Path to the disk image
	Path string `json:"path"`
	// Format of the disk image (qcow2, raw, etc.)
	Format string `json:"format"`
	// Bus type (virtio, scsi, ide)
	Bus string `json:"bus"`
	// ReadOnly marks the disk as read-only
	ReadOnly bool `json:"read_only,omitempty"`
	// Cache mode (none, writethrough, writeback)
	Cache string `json:"cache,omitempty"`
}

// NetworkDevice defines a network device configuration.
type NetworkDevice struct {
	// Type of network (bridge, user, network)
	Type string `json:"type"`
	// Source is the bridge name or network name
	Source string `json:"source,omitempty"`
	// MACAddress is the MAC address for the interface
	MACAddress string `json:"mac_address,omitempty"`
	// Model is the device model (virtio, e1000, etc.)
	Model string `json:"model,omitempty"`
}

// SerialDevice defines a serial device configuration.
type SerialDevice struct {
	// Type of serial device (virtio-serial, isa-serial)
	Type string `json:"type"`
	// SocketPath is the path to the host socket
	SocketPath string `json:"socket_path"`
	// PortName is the name of the virtio port
	PortName string `json:"port_name,omitempty"`
}

// VMTopology defines the complete VM topology configuration.
type VMTopology struct {
	// CPU topology
	CPU CPUTopology `json:"cpu,omitempty"`
	// Memory backing options
	MemoryBacking *MemoryBacking `json:"memory_backing,omitempty"`
	// Disks attached to the VM
	Disks []DiskDevice `json:"disks,omitempty"`
	// Network interfaces
	Networks []NetworkDevice `json:"networks,omitempty"`
	// Serial devices
	Serials []SerialDevice `json:"serials,omitempty"`
}
