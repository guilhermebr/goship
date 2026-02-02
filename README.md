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

# Generate domain XML (offline, no libvirt needed)
goshipctl generate-xml --name my-vm --memory 1024 --cpus 2

# Download the Alpine base VM image
goshipctl image pull

# Create and start a VM (with cloud-init provisioning)
goshipctl vm create --name my-vm --ssh-key ~/.ssh/id_ed25519.pub

# List VMs
goshipctl vm list

# Destroy a VM
goshipctl vm destroy --name my-vm
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

Downloads the Alpine Linux nocloud QCOW2 image for use as the base VM image.

| Flag | Default | Description |
|------|---------|-------------|
| `--output` | `~/.goship/images/goship-vm.qcow2` | Output path for the downloaded image |
| `--force` | `false` | Overwrite existing image |

### `goshipctl vm create [flags]`

Creates a CoW disk image from a base image, generates domain XML, and starts the VM via libvirt.

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

### `goshipctl vm destroy [flags]`

Stops the VM, undefines it from libvirt, and removes its disk image.

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | *(required)* | VM name |
| `--data-dir` | `~/.goship` | Data directory |
| `--keep-disk` | `false` | Keep disk image after destroying |

### `goshipctl vm list`

Lists all GoShip-managed VMs and their current state.

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

