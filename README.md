# GoShip

GoShip is a Go-based, self-hosted VM-centric application control plane for project-scoped virtual machines built on the Linux virtualization stack.

---

## Why GoShip Exists

Containers simplify application packaging, but **virtual machines remain the strongest isolation boundary** for multi-tenant systems, regulated workloads, and Confidential Computing.

GoShip is built around a simple model:

> **A project owns one or more virtual machines.**  
> **All applications of that project execute inside those VMs via a project-scoped agent.**

---

## Core Principles

- **VM-first isolation** — Virtual machines are the unit of trust, security, and ownership
- **Explicit virtualization** — CPU topology, memory, devices, and firmware are first-class concepts
- **Agent-mediated control** — Applications are managed declaratively through an agent running inside the VM
- **Upstream-aligned** — Built directly on KVM, QEMU, and Libvirt
- **Runtime-agnostic** — Control plane remains unchanged across QEMU, Kata, Firecracker backends
- **Confidential Computing-ready** | Designed to support SEV-SNP, TDX, and attestation workflows

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                  GoShip Control Plane                   │
│         (Projects, Apps, Nodes, Desired State)          │
└───────────────────────────┬─────────────────────────────┘
                            │
                    Desired State
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                    Node (Linux Host)                    │
│                     GoShip Agent                        │
└───────────────────────────┬─────────────────────────────┘
                            │
                  VM Lifecycle Management
                            │
        ┌───────────────────┼───────────────────┐
        ▼                   ▼                   ▼
┌───────────────┐   ┌───────────────┐   ┌───────────────┐
│  Project VM   │   │  Project VM   │   │  Project VM   │
│   (Alpha)     │   │   (Beta)      │   │   (Gamma)     │
├───────────────┤   ├───────────────┤   ├───────────────┤
│  GoShip Init  │   │  GoShip Init  │   │  GoShip Init  │
│   (PID 1)     │   │   (PID 1)     │   │   (PID 1)     │
├───────────────┤   ├───────────────┤   ├───────────────┤
│  ┌─────────┐  │   │  ┌─────────┐  │   │  ┌─────────┐  │
│  │Container│  │   │  │Container│  │   │  │ Process │  │
│  │  App A  │  │   │  │  App X  │  │   │  │  App P  │  │
│  └─────────┘  │   │  └─────────┘  │   │  └─────────┘  │
│  ┌─────────┐  │   │  ┌─────────┐  │   └───────────────┘
│  │Container│  │   │  │Container│  │
│  │  App B  │  │   │  │  App Y  │  │
│  └─────────┘  │   │  └─────────┘  │
└───────────────┘   └───────────────┘
```

---

## Design Document

The **[Design Document (RFC)](docs/DESIGN.md)** is the authoritative reference for GoShip's architecture and design decisions. It covers:

- Architecture invariants and key components
- Project-to-VM mapping and scaling semantics
- Application lifecycle and VM lifecycle design
- CPU, memory, and device modeling
- Confidential Computing design (experimental)
- Trade-offs and known risks
- Runtime evolution roadmap

---

## Getting Started

### Prerequisites

- Go 1.24+
- `libvirt-dev` / `libvirt-devel`
- `qemu-system-x86_64`, `qemu-img`
- `genisoimage` or `mkisofs` (for cloud-init ISO generation)
- `libguestfs-tools` (for per-VM guest disk provisioning via `virt-customize`)
- KVM-capable host (for `--enable-kvm`)

### Build

```bash
make build
```

### Quick Usage

```bash
# Show version and libvirt info
goshipctl version

# Show host capabilities (CPU, memory, KVM, hugepages, confidential computing)
goshipctl capabilities

# Download the Alpine base VM image
goshipctl image pull

# Build goship-init (in-guest agent)
make build-goship-init

# Create a project (provisions a VM with Docker)
goshipctl project create myproject --memory 1024 --cpu 2 --disk 4096

# Create and deploy a container app
goshipctl app create myproject nginx --image nginx:alpine --port 8080:80
goshipctl app deploy myproject nginx

# Create and deploy a process app (binary auto-uploaded to VM)
goshipctl app create myproject myapi --mode process --binary ./bin/myapi --port 3000:3000 --restart-policy always
goshipctl app deploy myproject myapi

# Check app status
goshipctl app list myproject
goshipctl app info myproject nginx

# View logs
goshipctl app logs myproject myapi
goshipctl app logs myproject myapi -f          # follow mode
goshipctl project logs myproject               # VM-level logs
goshipctl project logs myproject cloud-init

# Stop and clean up
goshipctl app stop myproject nginx
goshipctl app delete myproject nginx
goshipctl project delete myproject
```

---

## CLI Reference

### `goshipctl version`

Prints version, commit, build time, and libvirt/QEMU version info.

### `goshipctl capabilities`

Discovers and displays host virtualization capabilities: hypervisor type, KVM status, CPU topology, memory, hugepages, and confidential computing support.

### `goshipctl generate-xml [flags]`

Generates a libvirt domain XML document. Does not require a libvirt connection.

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | `goship-vm` | VM name |
| `--uuid` | *(auto)* | VM UUID |
| `--memory` | `512` | Memory in MB |
| `--cpus` | `1` | Number of CPU cores |
| `--enable-kvm` | `true` | Enable KVM acceleration |
| `--disk-path` | `/var/lib/goship/disk.qcow2` | Disk image path |
| `--disk-format` | `qcow2` | Disk format |
| `--network-type` | `network` | Network type (`network`, `bridge`, `user`) |
| `--network-source` | `default` | Network source name |

### `goshipctl image pull [flags]`

Downloads the Alpine Linux NoCloud QCOW2 image for use as the base VM image.

| Flag | Default | Description |
|------|---------|-------------|
| `--output` | `~/.goship/images/goship-vm.qcow2` | Output path for the downloaded image |
| `--force` | `false` | Overwrite existing image |

### `goshipctl image build [flags]`

Builds a plain base image locally (download + resize). It does not install goship-init.

| Flag | Default | Description |
|------|---------|-------------|
| `--output` | `~/.goship/images/goship-vm.qcow2` | Output path for the built image |
| `--force` | `false` | Overwrite existing image |
| `--image-size` | `2G` | Resize image virtual size |

### `goshipctl vm create [flags]`

Creates a CoW disk image from a base image, provisions goship-init in that overlay, generates domain XML, and starts the VM via libvirt.

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | *(required)* | VM name |
| `--base-image` | `~/.goship/images/goship-vm.qcow2` | Base qcow2 image path |
| `--memory` | `512` | Memory in MB |
| `--cpus` | `1` | Number of CPU cores |
| `--enable-kvm` | `true` | Enable KVM acceleration |
| `--network-type` | `network` | Network type |
| `--network-source` | `default` | Network source name |
| `--data-dir` | `~/.goship` | Data directory for VM disk images |
| `--hostname` | *(VM name)* | VM hostname (cloud-init) |
| `--ssh-key` | | Path to SSH public key file (e.g. `~/.ssh/id_ed25519.pub`) |
| `--goship-init` | `./bin/goship-init` | Path to goship-init binary used for per-VM provisioning |
| `--skip-guest-provision` | `false` | Skip goship-init/OpenRC guest provisioning |
| `--install-docker` | `true` | Install/enable Docker during guest provisioning |

#### Cloud-Init Provisioning

When a VM is created with `--ssh-key`, GoShip generates a NoCloud cloud-init ISO that provisions the VM on first boot:

- **User:** `goship` (shell: `/bin/sh`, sudo: passwordless)
- **Password:** `goship` (for console access via `virsh console`)
- **SSH:** Key-based auth using the provided public key
- **Hostname:** Set to `--hostname` flag value (defaults to VM name)

To access the VM:

```bash
# Via SSH (recommended)
ssh goship@<vm-ip>

# Via console (useful for debugging)
virsh console <vm-name>
# login: goship / password: goship
```

### `goshipctl vm destroy [flags]`

Stops the VM, undefines it from libvirt, and removes its disk image.

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | *(required)* | VM name |
| `--data-dir` | `~/.goship` | Data directory |
| `--keep-disk` | `false` | Keep disk image after destroying |

### `goshipctl vm list`

Lists all GoShip-managed VMs and their current state.

### `goshipctl project create <name> [flags]`

Creates a new project with its own isolated VM. Provisions the VM with cloud-init, installs Docker (if enabled), and injects goship-init.

| Flag | Default | Description |
|------|---------|-------------|
| `--cpu` | `1` | Number of CPU cores |
| `--memory` | `512` | Memory in MB |
| `--disk` | `4096` | Disk size in MB |
| `--network-type` | | Network type (`network`, `bridge`, `user`) |
| `--network-source` | | Network source name |

### `goshipctl project list`

Lists all projects with their VM state and IP address.

### `goshipctl project info <name>`

Shows detailed project information including VM status, resources, and IP address.

### `goshipctl project delete <name>`

Destroys the project's VM and removes the project from state.

### `goshipctl project console <name>`

Opens an interactive serial console to the project VM via `virsh console`.

### `goshipctl project logs <name> [source] [flags]`

Shows logs from the project VM.

| Argument/Flag | Default | Description |
|---------------|---------|-------------|
| `source` | `goship-init` | Log source: `goship-init` or `cloud-init` |
| `--file` | | Arbitrary log file path inside VM (must be under `/var/log/`) |
| `-n, --lines` | `100` | Number of log lines to show |
| `-f, --follow` | `false` | Follow log output (poll every 2s) |

### `goshipctl project stop <name>`

Gracefully stops the project VM (ACPI shutdown).

### `goshipctl project start <name>`

Starts a stopped project VM.

### `goshipctl project restart <name>`

Restarts a project VM (stop then start).

### `goshipctl project update-init <name> [flags]`

Pushes a new goship-init binary into a running VM over virtio-serial using a chunked transfer protocol.

| Flag | Default | Description |
|------|---------|-------------|
| `--binary` | *(--goship-init value)* | Path to goship-init binary |
| `--restart` | `false` | Restart the VM after successful update |

### `goshipctl app create <project> <appname> [flags]`

Creates an application definition in the project state store. Does not deploy — use `app deploy` to start it.

| Flag | Default | Description |
|------|---------|-------------|
| `-m, --mode` | `container` | Execution mode: `container` or `process` |
| `-i, --image` | | Container image (required for container mode) |
| `-b, --binary` | | Binary path inside VM (required for process mode) |
| `-p, --port` | | Port mapping `host:container` (repeatable) |
| `-e, --env` | | Environment variable `KEY=VALUE` (repeatable) |
| `--cpu` | `0` | CPU limit (cores) |
| `--memory` | `0` | Memory limit in MB |
| `-d, --description` | | App description |
| `-g, --tag` | | Tags (repeatable) |
| `--restart-policy` | `never` | Restart policy: `never`, `always`, or `on-failure` |

### `goshipctl app deploy <project> <appname>`

Deploys an application to the project VM. Sends the app spec over virtio-serial to the in-VM goship-init agent, which pulls the image (container mode) or starts the binary (process mode).

For process mode apps with a local binary path, `deploy` automatically uploads the binary to the VM (chunked transfer with SHA256 verification) before starting it.

### `goshipctl app list <project>`

Lists all applications in a project with live status from the VM.

### `goshipctl app info <project> <appname>`

Shows detailed application info including configuration and live status from the VM.

### `goshipctl app stop <project> <appname>`

Stops a running application inside the project VM.

### `goshipctl app delete <project> <appname>`

Removes the application from the VM (best-effort) and deletes it from the state store.

### `goshipctl app logs <project> <appname> [flags]`

Shows application logs from the project VM. For process mode apps, reads from per-process log files (`/var/log/goship-<appname>.log`). For container mode apps, reads Docker container logs.

| Flag | Default | Description |
|------|---------|-------------|
| `-n, --lines` | `100` | Number of log lines to show |
| `-f, --follow` | `false` | Follow log output (poll every 2s) |

---

## Contributing

GoShip values:

- Small, reviewable changes
- Clear documentation
- Explicit trade-offs
- Upstream-first thinking

---

## License

Apache 2.0

---

## Acknowledgements

This project exists thanks to the Linux virtualization community and the decades of work behind KVM, QEMU, Libvirt, and the Linux kernel.

