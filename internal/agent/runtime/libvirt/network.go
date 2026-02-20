package libvirt

import (
	"fmt"
	"os"

	"libvirt.org/go/libvirt"
)

// defaultNetworkXML is the standard libvirt default NAT network configuration.
// It provides DHCP on 192.168.122.0/24 with NAT forwarding to the host.
const defaultNetworkXML = `<network>
  <name>default</name>
  <bridge name='virbr0' stp='on' delay='0'/>
  <forward mode='nat'/>
  <ip address='192.168.122.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.122.2' end='192.168.122.254'/>
    </dhcp>
  </ip>
</network>`

// EnsureNetwork checks that the named libvirt network exists and is active.
// If the network exists but is inactive, it starts it and enables autostart.
// If the network doesn't exist and the name is "default", it creates the
// standard NAT network. For any other missing network name, it returns an
// error with instructions for the user.
//
// This only applies to type="network" configurations — bridge and user
// networking are managed outside libvirt.
func EnsureNetwork(conn *libvirt.Connect, name string) error {
	net, err := conn.LookupNetworkByName(name)
	if err != nil {
		// Network not found — create it only if it's the default network.
		if name == "default" {
			return createDefaultNetwork(conn)
		}
		return fmt.Errorf("network %q not found in libvirt; create it with: virsh net-define <xml> && virsh net-start %s", name, name)
	}
	defer func() { _ = net.Free() }()

	active, err := net.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check network %q state: %w", name, err)
	}

	if active {
		return nil
	}

	// Network exists but is not active — start it.
	fmt.Fprintf(os.Stderr, "Network %q is not active, starting it...\n", name)
	if err := net.Create(); err != nil {
		return fmt.Errorf("failed to start network %q: %w", name, err)
	}

	if err := net.SetAutostart(true); err != nil {
		// Non-fatal: network is running, autostart is just convenience.
		fmt.Fprintf(os.Stderr, "WARNING: could not enable autostart for network %q: %v\n", name, err)
	}

	return nil
}

// createDefaultNetwork defines, starts, and enables autostart for the standard
// libvirt default NAT network.
func createDefaultNetwork(conn *libvirt.Connect) error {
	fmt.Fprintf(os.Stderr, "Network \"default\" not found, creating standard NAT network...\n")

	net, err := conn.NetworkDefineXML(defaultNetworkXML)
	if err != nil {
		return fmt.Errorf("failed to define default network: %w", err)
	}
	defer func() { _ = net.Free() }()

	if err := net.Create(); err != nil {
		return fmt.Errorf("failed to start default network: %w", err)
	}

	if err := net.SetAutostart(true); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: could not enable autostart for default network: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "Default network created and started (192.168.122.0/24 NAT)\n")
	return nil
}
