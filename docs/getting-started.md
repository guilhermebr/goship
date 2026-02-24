# Getting Started

This guide takes you from zero to a running application in a GoShip VM.

## Prerequisites

GoShip requires a Linux host with hardware virtualization support. Install the following:

```bash
# Ubuntu/Debian
sudo apt install -y \
  libvirt-dev libvirt-daemon-system \
  qemu-system-x86 qemu-utils \
  genisoimage \
  libguestfs-tools

# Verify KVM is available (recommended but not required)
ls /dev/kvm

# Add your user to the libvirt group
sudo usermod -aG libvirt $USER
# Log out and back in for the group change to take effect

# Start libvirtd
sudo systemctl enable --now libvirtd
```

**Note:** GoShip works without KVM using QEMU TCG (software emulation), but performance will be significantly degraded. See [Troubleshooting — QEMU TCG Fallback](troubleshooting.md#qemu-tcg-fallback) for details.

You also need:
- **Go 1.26+** — to build GoShip from source
- **Docker** (optional) — only needed inside VMs for container mode; GoShip installs it automatically during VM provisioning

## Install GoShip

Clone and build:

```bash
git clone https://github.com/guilhermebr/goship.git
cd goship
make build
```

This produces two binaries in `./bin/`:
- `goshipctl` — CLI tool (runs on your host)
- `goship-init` — VM agent (injected into VMs automatically)

Verify the build:

```bash
./bin/goshipctl version
```

You should see version info along with your libvirt and QEMU versions.

## Prepare a VM Image

Download the Alpine Linux base image that GoShip uses for VMs:

```bash
./bin/goshipctl image pull
```

This downloads an Alpine NoCloud QCOW2 image to `~/.goship/images/goship-vm.qcow2`. The image is shared read-only — each VM gets its own copy-on-write overlay.

## Create Your First Project

A project is an isolated VM where your apps will run:

```bash
./bin/goshipctl project create myapp --cpu 1 --memory 512
```

This command:
1. Creates a CoW disk overlay from the base image
2. Provisions `goship-init` into the overlay using `virt-customize`
3. Installs Docker inside the VM image
4. Generates a cloud-init ISO with the VM's identity
5. Builds libvirt domain XML and starts the VM
6. Waits for GoShip Init to become ready

You'll see boot progress as the VM starts up. Once complete, the project is running.

Check it:

```bash
./bin/goshipctl project list
./bin/goshipctl project info myapp
```

## Deploy a Container App

Create an app definition and deploy it:

```bash
# Define the app
./bin/goshipctl app create myapp web --mode container --image nginx:alpine --port 8080:80

# Deploy it to the VM
./bin/goshipctl app deploy myapp web
```

The deploy command sends the app spec to GoShip Init inside the VM, which pulls the image and starts the container.

## Check Status

```bash
# List all apps in the project
./bin/goshipctl app list myapp

# Get detailed info for a specific app
./bin/goshipctl app info myapp web
```

## View Logs

```bash
# App logs
./bin/goshipctl app logs myapp web

# Follow mode (polls every 2s)
./bin/goshipctl app logs myapp web -f

# VM-level logs (GoShip Init)
./bin/goshipctl project logs myapp

# Cloud-init logs (provisioning)
./bin/goshipctl project logs myapp cloud-init
```

## Deploy a Process App

GoShip can also run plain binaries directly inside VMs, without Docker. Build a static binary and deploy it:

```bash
# Create the app definition pointing to your local binary
./bin/goshipctl app create myapp api \
  --mode process \
  --binary ./my-server \
  --port 3000:3000 \
  --restart-policy always

# Deploy — automatically uploads the binary to the VM
./bin/goshipctl app deploy myapp api
```

The binary is uploaded over virtio-serial with SHA256 verification, then started as a supervised process inside the VM.

## Clean Up

```bash
# Stop and remove the app
./bin/goshipctl app stop myapp web
./bin/goshipctl app delete myapp web

# Delete the project and its VM
./bin/goshipctl project delete myapp
```

## Next Steps

- [Concepts](concepts.md) — Understand how GoShip works under the hood
- [CLI Reference](cli-reference.md) — Every command, flag, and example
- [Troubleshooting](troubleshooting.md) — Common issues and fixes
