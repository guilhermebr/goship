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

## Project Status

**Early-stage — Phase 0**

Current focus:
- VM lifecycle correctness
- GoShip Init implementation
- Project agent communication (virtio-serial)
- End-to-end demo

Future focus:
- Multi-node scheduling
- Health checks and metrics
- Confidential Computing attestation
- Runtime abstraction layer

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

