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

## Current Features

- **Project lifecycle** — Create, start, stop, restart, delete project VMs
- **Container apps** — Deploy Docker containers inside VMs with port mapping, env vars, and resource limits
- **Process apps** — Deploy binaries directly with auto-restart, exponential backoff, and per-process log files
- **Binary upload** — Transparent chunked transfer of local binaries into VMs over virtio-serial
- **Local image deploy** — `--local-image` flag exports a host Docker image, compresses it, and pushes it into the VM before deploying
- **Image management** — Pull base Alpine cloud images, build local images, push images into VMs (`app push-image`)
- **App editing** — Modify app specs (ports, env, image, resources) without redeploying
- **Boot progress streaming** — Real-time cloud-init log output during project creation
- **Portable builds** — `libvirt_dlopen` build tag removes compile-time libvirt version dependency
- **KVM/TCG fallback** — Automatic detection of KVM; graceful fallback to QEMU software emulation
- **Auto network setup** — Ensures the libvirt `default` network is active before VM creation
- **DAC security labels** — Numeric UID/GID labels for precise QEMU file access control

---

## Install

```bash
# Install latest release (includes system dependencies)
curl -fsSL https://raw.githubusercontent.com/guilhermebr/goship/main/scripts/install.sh | bash

# Install specific version
curl -fsSL https://raw.githubusercontent.com/guilhermebr/goship/main/scripts/install.sh | GOSHIP_VERSION=v0.1.0 bash

# Skip system deps if already installed
curl -fsSL https://raw.githubusercontent.com/guilhermebr/goship/main/scripts/install.sh | GOSHIP_SKIP_DEPS=true bash
```

Or build from source:

```bash
make build
sudo make install
```

---

## Quick Start

```bash
# Download base VM image
goshipctl image pull

# Create a project (provisions a VM with Docker)
goshipctl project create myapp --cpu 1 --memory 512

# Deploy a container app
goshipctl app create myapp web --image nginx:alpine --port 8080:80
goshipctl app deploy myapp web

# Deploy using a locally built Docker image (pushed into VM automatically)
goshipctl app create myapp api --image myapi:latest --port 3000:3000
goshipctl app deploy myapp api --local-image

# Deploy a process-mode app (binary uploaded into VM automatically)
goshipctl app create myapp worker --mode process --binary ./bin/myworker --restart-policy on-failure
goshipctl app deploy myapp worker

# Check status and logs
goshipctl app list myapp
goshipctl app logs myapp web
goshipctl app logs myapp worker --follow

# Edit an app and redeploy
goshipctl app edit myapp web --port 9090:80
goshipctl app deploy myapp web

# Clean up
goshipctl app delete myapp web
goshipctl app delete myapp worker
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
| [Decision Log](DECISION-LOG.md) | Architectural decisions and their rationale (ADR-001 through ADR-034) |

---

## Prerequisites

- Linux (KVM recommended; QEMU TCG software emulation used as fallback when `/dev/kvm` is unavailable)
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
