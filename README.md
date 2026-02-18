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
- **Confidential Computing-ready** — Designed to support SEV-SNP, TDX, and attestation workflows

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

## Quick Start

```bash
# Build
make build

# Download base VM image
goshipctl image pull

# Create a project (provisions a VM with Docker)
goshipctl project create myapp --cpu 1 --memory 512

# Deploy a container app
goshipctl app create myapp web --image nginx:alpine --port 8080:80
goshipctl app deploy myapp web

# Check status and logs
goshipctl app list myapp
goshipctl app logs myapp web

# Clean up
goshipctl app delete myapp web
goshipctl project delete myapp
```

See the [Getting Started guide](docs/getting-started.md) for the full walkthrough.

---

## Documentation

| Document | Description |
|----------|-------------|
| [Getting Started](docs/getting-started.md) | Install GoShip and deploy your first app |
| [Concepts](docs/concepts.md) | How GoShip works — projects, apps, VMs, architecture |
| [CLI Reference](docs/cli-reference.md) | Every command, flag, and example |
| [Troubleshooting](docs/troubleshooting.md) | Common problems and fixes |
| [Design Document](docs/DESIGN.md) | Full architecture RFC |

---

## Prerequisites

- Linux with KVM support (`/dev/kvm`)
- Go 1.22+
- System packages: `libvirt-dev`, `qemu-system-x86`, `qemu-utils`, `genisoimage`, `libguestfs-tools`
- User in the `libvirt` group

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
