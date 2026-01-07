# GoShip

GoShip is a Go-based, **self-hosted VM-centric application control plane** for **project-scoped virtual machines** built on top of the Linux virtualization stack.

It manages **projects and applications** using a **VM-first isolation model**, where every project owns its own virtual machine and applications run exclusively inside that VM via an in-guest agent.

GoShip is intentionally minimal, explicit, and upstream-aligned. It exposes virtualization primitives rather than hiding them behind opaque abstractions.

---

## Why GoShip Exists

Containers simplify application packaging, but **virtual machines remain the strongest isolation boundary** for:

- Multi-tenant systems
- Regulated and high-assurance workloads
- Confidential Computing
- Strong security domain separation

GoShip is built around a simple but explicit model:

> **A project owns one or more virtual machines.**  
> **All applications of that project execute inside those VMs via a project-scoped agent.**

This mirrors how enterprise virtualization platforms are designed, debugged, and supported—with clear boundaries and predictable behavior.

---

## Core Principles

| Principle | Description |
|-----------|-------------|
| **VM-first isolation** | Virtual machines are the unit of trust, security, and ownership |
| **Explicit virtualization** | CPU topology, memory, devices, and firmware are first-class concepts |
| **Agent-mediated control** | Applications are managed declaratively through an agent running inside the VM |
| **Upstream-aligned** | Built directly on KVM, QEMU, and cloud-init—no hidden abstraction layers |
| **Runtime-agnostic** | Control plane remains unchanged across QEMU, Kata, Firecracker backends |
| **Confidential Computing-ready** | Designed to support SEV-SNP, TDX, and attestation workflows |

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
│  │Container│  │   │  │Container│  │   │  │Container│  │
│  │  App A  │  │   │  │  App X  │  │   │  │  App P  │  │
│  └─────────┘  │   │  └─────────┘  │   │  └─────────┘  │
│  ┌─────────┐  │   │  ┌─────────┐  │   └───────────────┘
│  │Container│  │   │  │Container│  │
│  │  App B  │  │   │  │  App Y  │  │
│  └─────────┘  │   │  └─────────┘  │
└───────────────┘   └───────────────┘
```

### Key Components

| Component | Responsibility |
|-----------|----------------|
| **Control Plane** | Projects, Apps, Nodes, Desired State computation, Scheduling |
| **Node Agent** | VM lifecycle, Reconciliation, State reporting |
| **GoShip Init** | PID 1 inside VM, Container supervision, Agent communication |
| **Project VM** | Isolated execution environment per project |

**Critical invariant:** The Control Plane **never** executes VMs or containers. All runtime execution is owned by the Agent.

---

## Project and Application Model

### Projects

- A **Project** is an isolation and ownership boundary
- Each project maps to **one VM per node**
- If a project runs on `N` nodes, there are exactly `N` Project VMs
- All Project VMs are **symmetric replicas** of the project environment

### Applications

- Applications belong to a project
- Applications are declared to GoShip and executed inside Project VMs
- Scaling an app means increasing replicas **per VM**

### Scaling Semantics

```
Total containers = N (nodes) × R (replicas per VM)
```

Example:
- Project runs on **3 nodes**
- App scaled to **2 replicas**

Result:
- VM on Node 1: 2 containers
- VM on Node 2: 2 containers
- VM on Node 3: 2 containers
- **Total: 6 containers**

This model ensures predictable behavior with no per-node special cases.

---

## Application Lifecycle

1. User declares desired application state via CLI or API
2. Control Plane computes Desired State and distributes to Agents
3. Agent reconciles: creates/updates Project VM if needed
4. **GoShip Init** (inside VM) receives configuration and:
   - Starts application containers
   - Restarts on failure
   - Reports health and status
5. Agent reports Actual State back to Control Plane

GoShip does **not**:
- SSH into VMs for control (SSH is emergency debug only)
- Execute processes directly on the host
- Inspect application memory or secrets

---

## Virtualization Stack

GoShip uses standard Linux virtualization:

```
GoShip Control Plane (Go)
        │
        │ Desired State
        ▼
GoShip Agent (Go)
        │
        │ VM Management
        ▼
      Libvirt
        │
        ▼
   QEMU / KVM
        │
        │ /dev/kvm
        ▼
Linux Host Kernel (KVM, IOMMU)
```

### Exposed Primitives

GoShip exposes (not hides):

- CPU topology and pinning
- Memory backing (hugepages planned)
- Disk and network devices
- Host capability discovery
- Confidential VM flags (SEV/TDX)

---

## Runtime Evolution

GoShip separates architecture from runtime implementation. The control plane is runtime-agnostic.

| Phase | Runtime | Goal |
|-------|---------|------|
| **0** | QEMU via Libvirt | Prove the Project VM model |
| **1** | QEMU + Docker-in-VM | Production-ready multi-node |
| **2** | Runtime abstraction | Podman, runtime selection |
| **3** | Kata Containers | Higher density, OCI-native |
| **4** | Firecracker (via Flintlock or similar managers) | MicroVM, fast boot (<125ms) |

The control plane API and agent architecture remain unchanged across all phases.

---

## Confidential Computing (Experimental)

When Confidential Computing is enabled:

- VM memory is encrypted (AMD SEV / Intel TDX)
- Application execution happens inside encrypted memory
- The host cannot inspect application state
- Secrets are provisioned post-attestation

GoShip Init runs **inside the confidential VM**, preserving the trust boundary.

**Status:** Experimental. Host capability detection and VM profile modeling are implemented. Full attestation flows are in progress.

---

## What GoShip Is (and Is Not)

### GoShip Is

- A virtualization-centric application platform
- A project and application manager via VM-resident agents
- A testbed for Confidential Computing
- A VM-first alternative to container-only platforms
- A learning and contribution vehicle for the Linux virtualization ecosystem

### GoShip Is Not

- A Kubernetes replacement
- A PaaS abstraction layer
- A cloud provider
- A wrapper hiding virtualization internals
- Production-ready (yet)

---

## Trade-offs

### Accepted

| Trade-off | Benefit |
|-----------|---------|
| Higher memory per project (VM cost) | Strong isolation |
| Slower cold starts than containers | Security guarantees |
| Lower density than pure containers | Operational clarity |

### Known Risks

- Nested virtualization availability on some VPS providers
- Networking complexity (VM ↔ host ↔ ingress)
- VM image management overhead

---

## Project Status

“GoShip is intended as a vehicle for upstream contributions to KVM, QEMU, Libvirt, and related projects.”

**Early-stage - Phase 0**

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
