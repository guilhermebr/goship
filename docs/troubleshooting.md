# Troubleshooting

Common problems and how to fix them.

## Permission Denied When Creating VMs

**Symptom:**

```
error: internal error: process exited while connecting to monitor:
qemu-system-x86_64: Could not open '/home/user/.goship/vms/.../disk.qcow2': Permission denied
```

**Cause:** When using `qemu:///system`, QEMU runs as the `libvirt-qemu` user, which cannot access files in your home directory. Two layers cause this:

1. **AppArmor** — Confines QEMU to an allowed path list that doesn't include home directories.
2. **DAC permissions** — The `libvirt-qemu` user cannot traverse `/home/user/` (typically `0750`).

**Fix:** GoShip handles this by adding `<seclabel type='none'/>` to the domain XML, which disables confinement for GoShip VMs. If you're still hitting this:

```bash
# Check for AppArmor denials
dmesg | grep DENIED

# Inspect path permissions
namei -l ~/.goship/vms/myproject/disk.qcow2

# Verify which user QEMU runs as
ps aux | grep qemu
```

For production deployments, consider moving images to `/var/lib/libvirt/images/` which is already in AppArmor's allowlist.

---

## KVM Not Available

**Symptom:**

```
Could not access KVM kernel module: No such file or directory
```

Or VMs run extremely slowly.

**Fix:**

```bash
# Check if /dev/kvm exists
ls -la /dev/kvm

# Load the KVM module
sudo modprobe kvm_intel   # Intel CPUs
sudo modprobe kvm_amd     # AMD CPUs

# Verify KVM is loaded
lsmod | grep kvm
```

If `modprobe` fails, check your BIOS/UEFI settings — hardware virtualization (VT-x / AMD-V) may be disabled.

```bash
# Check CPU virtualization support
grep -E 'vmx|svm' /proc/cpuinfo
```

### QEMU TCG Fallback

When KVM is not available, GoShip automatically falls back to QEMU TCG (Tiny Code Generator) software emulation. Two things change:

1. **Domain type** switches from `kvm` to `qemu`
2. **CPU mode** switches from `host-passthrough` to `host-model`

`host-passthrough` cannot work without KVM because it passes the host's physical CPU directly to the guest, which requires hardware virtualization extensions. `host-model` approximates the host CPU using only features that TCG can emulate in software.

**Performance impact:** QEMU TCG is significantly slower than KVM — expect 10-100x slower CPU performance. I/O performance (disk, network) is less affected since virtio devices work the same way.

Verify the current mode with:

```bash
goshipctl capabilities
# Look for the "CPU Mode" and "KVM" lines
```

---

## Libvirt Default Network Issues

**Symptom:**

```
virError(Code=55, Domain=19, Message='network 'default' is not active')
```

Or VM creation fails with a network-related error.

**What GoShip does automatically:** When creating a VM with `NetworkType: "network"`, GoShip checks whether the libvirt network is active before starting the VM. If the `default` network exists but is inactive (common after a reboot), GoShip starts it automatically and enables autostart. If the `default` network doesn't exist at all (fresh libvirt install), GoShip creates the standard NAT network (192.168.122.0/24) automatically.

**If auto-start fails** (e.g., permission issues):

```bash
# Check network status
virsh -c qemu:///system net-list --all

# Start it manually
sudo virsh net-start default
sudo virsh net-autostart default

# If the default network doesn't exist at all
sudo virsh net-define /usr/share/libvirt/networks/default.xml
sudo virsh net-start default
sudo virsh net-autostart default
```

**Custom networks:** GoShip only auto-creates the `default` network. If you use a custom network name (e.g., `--network-source mynet`), you must create and start it yourself:

```bash
virsh -c qemu:///system net-define mynet.xml
virsh -c qemu:///system net-start mynet
```

**Bridge networking:** If using `NetworkType: "bridge"` instead of `"network"`, libvirt network management doesn't apply — the bridge must be configured at the OS level.

---

## VM Fails to Boot

**Symptom:** `project create` hangs or times out during boot.

**Fix:**

```bash
# Check libvirt logs for the VM
sudo journalctl -u libvirtd --no-pager -n 50

# Check QEMU logs
sudo cat /var/log/libvirt/qemu/goship-*.log

# Verify the base image exists and is valid
qemu-img info ~/.goship/images/goship-vm.qcow2

# Verify the VM domain is defined
virsh -c qemu:///system list --all
```

Common causes:
- Base image missing — run `goshipctl image pull`
- Corrupted CoW overlay — delete the project and recreate it
- Insufficient memory — try `--memory 512` or higher

---

## GoShip Init Not Responding (Ping Timeout)

**Symptom:** `vm ping` or commands to the VM time out with no response.

**Fix:**

```bash
# Check if the socket file exists
ls -la ~/.goship/vms/<project>/goship.sock

# Try a manual ping via socat
echo '{"action":"ping"}' | socat - UNIX-CONNECT:$HOME/.goship/vms/<project>/goship.sock

# Verify the virtio-serial channel in domain XML
virsh -c qemu:///system dumpxml goship-<project> | grep -A3 'channel type'

# Check GoShip Init logs via the VM console
goshipctl project console <project>
# Login: goship / goship
# Then: cat /var/log/goship-init.log
```

Common causes:
- Guest provisioning was skipped (`--skip-guest-provision`) — goship-init is not installed
- goship-init binary is incompatible — rebuild with `make build-goship-init` (must be `CGO_ENABLED=0`)
- VM is still booting — cloud-init and Docker installation can take 30-60 seconds

---

## Docker Not Available Inside VM

**Symptom:** Container app deploy fails with Docker-related errors.

**Fix:**

Docker is installed via cloud-init during the first boot, which can take a minute. Check progress:

```bash
# View cloud-init logs
goshipctl project logs <project> cloud-init

# SSH into the VM (if you have the IP) and check Docker
ssh goship@<vm-ip> 'rc-service docker status'

# Or via console
goshipctl project console <project>
# Login: goship / goship
# Then: rc-service docker status
```

If Docker installation failed:
- Check if the VM has internet access (needed to pull packages)
- Check `/var/log/cloud-init-output.log` inside the VM for errors

---

## Container Image Pull Fails

**Symptom:** App deploy succeeds but the container fails to start because the image couldn't be pulled.

**Cause:** The VM may not have DNS resolution configured properly.

**Fix:**

```bash
# Check DNS inside the VM
goshipctl project console <project>
# Login: goship / goship
# Then:
cat /etc/resolv.conf
ping -c 1 docker.io

# If DNS is empty, add a nameserver
echo "nameserver 8.8.8.8" | sudo tee /etc/resolv.conf
```

If using NAT networking (the default), the libvirt `default` network should provide DNS. Verify:

```bash
virsh -c qemu:///system net-info default
virsh -c qemu:///system net-dhcp-leases default
```

---

## Binary Upload Fails

**Symptom:** `app deploy` for a process mode app fails during binary upload.

**Fix:**

```bash
# Verify the binary exists and is executable
ls -la ./bin/myapp
file ./bin/myapp  # should show "statically linked" or "ELF 64-bit"

# Ensure the binary is built for Linux/amd64 (the VM architecture)
GOOS=linux GOARCH=amd64 go build -o ./bin/myapp ./cmd/myapp
```

For Go binaries deployed to Alpine-based VMs, build statically:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ./bin/myapp ./cmd/myapp
```

---

## Libvirt Connection Refused

**Symptom:**

```
Error: cannot connect to libvirt: failed to connect to the hypervisor
```

**Fix:**

```bash
# Check if libvirtd is running
sudo systemctl status libvirtd

# Start it if needed
sudo systemctl enable --now libvirtd

# Verify your user is in the libvirt group
groups | grep libvirt

# If not, add yourself and re-login
sudo usermod -aG libvirt $USER
```

GoShip connects to `qemu:///system` which requires either root or membership in the `libvirt` group.

Note: `virsh` without `-c` defaults to `qemu:///session` for non-root users, which is a separate namespace. Always use:

```bash
virsh -c qemu:///system list --all
# Or set the default permanently:
echo 'export LIBVIRT_DEFAULT_URI="qemu:///system"' >> ~/.bashrc
```

---

## BIOS vs UEFI Image Mismatch

**Symptom:** VM boots to a blank screen or "No bootable device" error.

**Cause:** GoShip uses BIOS-based Alpine images by default. The domain XML assumes BIOS firmware (SeaBIOS). If you're using a UEFI image, the firmware types won't match.

**Fix:** Use the correct Alpine image variant. GoShip downloads the BIOS version:

```
nocloud_alpine-3.23.3-x86_64-bios-cloudinit-r0.qcow2
```

If you need UEFI (for Secure Boot or Confidential Computing), you'd need to modify the domain XML to include OVMF firmware paths. This is not supported in Phase 0.

---

## Useful Debugging Commands

### Libvirt and VM Status

```bash
# List all GoShip VMs
virsh -c qemu:///system list --all

# Get detailed VM info
virsh -c qemu:///system dominfo goship-<project>

# Dump the full domain XML
virsh -c qemu:///system dumpxml goship-<project>

# Check DHCP leases (find VM IP address)
virsh -c qemu:///system net-dhcp-leases default
```

### GoShip State

```bash
# View the state file directly
cat ~/.goship/state.json | python3 -m json.tool

# List VM disk files
ls -la ~/.goship/vms/*/

# Check base image
qemu-img info ~/.goship/images/goship-vm.qcow2
```

### Inside the VM

```bash
# Open a console
goshipctl project console <project>
# Login: goship / goship

# Once inside:
cat /var/log/goship-init.log     # GoShip Init logs
cat /var/log/cloud-init-output.log  # Provisioning logs
rc-status                        # Service status (OpenRC)
docker ps                        # Running containers
ls /opt/goship/                  # GoShip Init binary location
ls /dev/virtio-ports/            # Virtio-serial device
```

### Virtio-Serial Debugging

```bash
# Test communication manually
echo '{"action":"ping"}' | socat - UNIX-CONNECT:$HOME/.goship/vms/<project>/goship.sock

# Check the socket file
ls -la ~/.goship/vms/<project>/goship.sock

# Inside the VM: verify the serial device
ls /dev/virtio-ports/goship.0
lsmod | grep virtio
```
