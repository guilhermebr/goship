# DESIGN: GoShip Virtualization Control Plane

## Authors

GoShip maintainers

## 1. Abstract

This document describes the design of **GoShip**, a Go-based, **self-hosted VM-centric application control plane** for managing **project-scoped virtual machines** on Linux hosts using the KVM virtualization stack.

GoShip manages **projects and applications** using a **VM-first isolation model**, where every project owns its own virtual machine and applications run exclusively inside that VM via an in-guest agent.

GoShip is intentionally minimal, explicit, and **upstream-aligned**. It does not attempt to abstract or replace existing virtualization technologies. Instead, it provides a thin, auditable layer that exposes VM lifecycle, isolation boundaries, and host capabilities while enabling experimentation with **Confidential Computing** and VM-centric workload models.

> **A project owns one or more virtual machines.**  
> **All applications of that project execute inside those VMs via a project-scoped agent.**

---

## 2. Motivation

Containers have become the dominant unit of deployment, but they are not a universal isolation boundary. In many environments, including:

- Multi-tenant platforms
- Regulated and high-assurance workloads
- Confidential Computing use cases
- Strong security domain separation

**Virtual machines remain the correct primitive**.

GoShip exists to explore a simple but explicit model that mirrors how enterprise virtualization platforms are designed, debugged, and supported—with clear boundaries and predictable behavior.

---

## 3. Goals

The primary goals of GoShip are:

1. **Expose virtualization primitives clearly**
   - VM lifecycle
   - CPU topology
   - Memory configuration
   - Devices and firmware

2. **Stay upstream-aligned**
   - Use KVM, QEMU, and Libvirt directly
   - Avoid re-implementing hypervisor logic
   - Enable upstream bug reproduction and fixes

3. **Enable Confidential Computing experimentation**
   - Detect host support for SEV / TDX
   - Model confidential VM profiles explicitly
   - Plumb attestation-related metadata

4. **Remain auditable and understandable**
   - Small codebase
   - Explicit trade-offs
   - Clear failure modes

5. **Runtime-agnostic control plane**
   - Control plane remains unchanged across QEMU, Kata, Firecracker backends
   - Runtime is pluggable without API changes

---

## 4. Non-Goals

GoShip explicitly does **not** aim to:

- Replace Kubernetes
- Provide a full PaaS
- Abstract virtualization behind opaque APIs
- Compete with cloud providers
- Hide KVM, QEMU, or Libvirt concepts
- Be production-ready (yet)

Complex scheduling, autoscaling, and policy engines are intentionally out of scope.

---

## 5. Architecture Invariants

These invariants **must never be violated**:

| # | Invariant | Description |
|---|-----------|-------------|
| 1 | **Project is the isolation boundary** | Each Project runs in its own VM(s) |
| 2 | **One Project VM per Node** | If a Project runs on N nodes, exactly N VMs exist |
| 3 | **Apps run only inside Project VMs** | Never on the host directly |
| 4 | **App scaling is replicas-per-VM** | Total containers = Nodes × Replicas (symmetric) |
| 5 | **Control Plane never executes workloads** | Strictly declarative |
| 6 | **Agent owns all runtime execution** | VMs, containers, hypervisors |
| 7 | **Desired State is declarative and pull-based** | Agent pulls from Control Plane |
| 8 | **Runtime changes do not affect Control Plane APIs** | Runtime is pluggable |

---

## 6. High-Level Architecture

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

### Virtualization Stack

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

Each GoShip node manages one or more Linux hosts.  
Each project maps to one or more Libvirt domains (VMs).

---

## 7. Key Components

| Component | Responsibility |
|-----------|----------------|
| **Control Plane** | Projects, Apps, Nodes, Desired State computation, Scheduling |
| **Node Agent** | VM lifecycle, Reconciliation, State reporting |
| **GoShip Init** | PID 1 inside VM, Container supervision, Agent communication |
| **Project VM** | Isolated execution environment per project |

**Critical invariant:** The Control Plane **never** executes VMs or containers. All runtime execution is owned by the Agent.

### Control Plane

- Manages Projects, Apps, Nodes lifecycle
- Computes Desired State for each Node
- Makes scheduling decisions
- Exposes REST API
- **NEVER executes VMs or containers**

### Node Agent

- Runs on each Node
- Pulls Desired State from Control Plane (every 10s)
- Creates/destroys Project VMs via Libvirt
- Manages containers inside VMs
- Reports Actual State back to Control Plane

### GoShip Init

- Runs inside Project VMs as PID 1
- Receives configuration from Agent (via virtio-serial)
- Starts and supervises workloads
- Reports health back to Agent

**Supported execution modes:**
- Containers (Podman, Docker, containerd)
- Direct processes (systemd-managed, non-containerized)

---

## 8. Project-to-VM Mapping

### Model

- A **Project** is an isolation and ownership boundary
- Each project maps to **one VM per node**
- If a project runs on `N` nodes, there are exactly `N` Project VMs
- All Project VMs are **symmetric replicas** of the project environment
- Applications execute *inside* the project VM as either:
  - **Containers** (Podman, Docker, containerd)
  - **Direct processes** (systemd-managed, non-containerized)

### Scaling Semantics

```
Total containers = N (nodes) × R (replicas per VM)
```

**Example:**
- Project runs on **3 nodes**
- App scaled to **2 replicas**

**Result:**
- VM on Node 1: 2 containers
- VM on Node 2: 2 containers
- VM on Node 3: 2 containers
- **Total: 6 containers**

This model ensures predictable behavior with no per-node special cases.

### Constraints

- Scaling a project means adding nodes (and thus VMs)
- Scaling an application means scaling replicas inside each VM
- Cross-project sharing is explicitly disallowed
- This model prioritizes isolation and debuggability over density

---

## 9. Application Lifecycle

1. **User declares desired application state** via CLI or API
2. **Control Plane computes Desired State** and distributes to Agents
3. **Agent reconciles**: creates/updates Project VM if needed
4. **GoShip Init** (inside VM) receives configuration and:
   - Starts application containers
   - Restarts on failure
   - Reports health and status
5. **Agent reports Actual State** back to Control Plane

GoShip does **not**:
- SSH into VMs for control (SSH is emergency debug only)
- Execute processes directly on the host
- Inspect application memory or secrets

---

## 10. VM Lifecycle Design

VM lifecycle operations are explicit and synchronous:

- Create
- Start
- Stop
- Destroy

VM definitions are expressed in Libvirt domain XML, generated programmatically.

Design constraints:
- No hidden defaults
- No automatic mutation of user-specified topology
- Errors surface directly from Libvirt/QEMU where possible

---

## 11. Runtime Evolution

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

## 12. CPU, Memory, and Device Modeling

GoShip exposes (not hides):

- CPU topology (sockets, cores, threads)
- CPU feature flags (host-model / host-passthrough)
- Memory size and backing (hugepages planned)
- Disk and network devices
- Host capability discovery
- Confidential VM flags (SEV/TDX)

Rationale:
- CPU and memory misconfiguration are a common source of production issues
- Hiding these details makes troubleshooting harder, not easier

---

## 13. Confidential Computing Design

### Scope

Confidential Computing support in GoShip is **experimental**.

**Status:** Host capability detection and VM profile modeling are implemented. Full attestation flows are in progress.

### When Enabled

- VM memory is encrypted (AMD SEV / Intel TDX)
- Application execution happens inside encrypted memory
- The host cannot inspect application state
- Secrets are provisioned [post-attestation](#post-attestation-secret-provisioning)

GoShip Init runs **inside the confidential VM**, preserving the trust boundary.

### Supported Technologies

- AMD SEV / SEV-ES / SEV-SNP
- Intel TDX (detection only, depending on host support)

### Design Principles

- Explicit opt-in
- Conservative defaults
- No claims of production readiness
- No attempt to hide hardware or firmware limitations

Attestation workflows are intentionally decoupled and may be mocked initially.

---

## 14. Host Capability Discovery

At startup, GoShip inspects the host for:

- CPU virtualization extensions
- KVM availability
- Confidential Computing support flags
- Libvirt driver capabilities

This information is surfaced directly to users and operators.

---

## 15. Trade-offs

### Accepted Trade-offs

| Trade-off | Benefit |
|-----------|---------|
| Higher memory per project (VM cost) | Strong isolation |
| Slower cold starts than containers | Security guarantees |
| Lower density than pure containers | Operational clarity |

### Known Risks

- Nested virtualization availability on some VPS providers
- Networking complexity (VM ↔ host ↔ ingress)
- VM image management overhead
- Early Confidential Computing support
- Dependency on host firmware and kernel quality
- Limited portability across hypervisors

These are explicit and documented by design.

---

## 16. Error Handling and Debuggability

Errors are treated as first-class signals.

Design rules:
- Prefer upstream error messages over reworded abstractions
- Preserve QEMU and Libvirt error context
- Avoid retry loops that mask failures

The goal is to make upstream bug reports actionable.

---

## 17. Testing and Validation Strategy

Initial validation focuses on correctness, not coverage.

Planned validation layers:
- Unit tests for domain generation
- Integration tests using Libvirt
- Host capability detection tests
- Future Avocado-based VM validation

Testing is designed to mirror how enterprise virtualization stacks are validated.

---

## 18. Security Considerations

Security boundaries are explicit:

- Projects are isolated by VMs
- No shared namespaces across projects
- Device access is conservative by default
- Confidential VM features are opt-in

GoShip does not attempt to harden the kernel or hypervisor itself.

---

## 19. Upstream Collaboration Model

GoShip follows an upstream-first philosophy:

- Bugs found in GoShip should be minimized reproductions
- Fixes should land upstream where appropriate
- GoShip consumes upstream releases without patch carry where possible

This mirrors enterprise distribution maintenance workflows.

> "GoShip is intended as a vehicle for upstream contributions to KVM, QEMU, Libvirt, and related projects."

---

## 20. Conclusion

GoShip is a deliberately small and explicit control plane designed to explore VM-centric platforms and Confidential Computing on Linux.

Its primary value lies in:
- Clarity
- Upstream alignment
- Debuggability
- Correctness
- Runtime-agnostic design

GoShip is:
- A virtualization-centric application platform
- A project and application manager via VM-resident agents
- A testbed for Confidential Computing
- A VM-first alternative to container-only platforms
- A learning and contribution vehicle for the Linux virtualization ecosystem

GoShip is **not**:
- A Kubernetes replacement
- A PaaS abstraction layer
- A cloud provider
- A wrapper hiding virtualization internals
- Production-ready (yet)

Complexity is introduced only when it is unavoidable.

---

## Appendix A: Glossary

### Attestation

**Attestation** is a cryptographic verification process that proves a confidential VM is:
- Running on genuine trusted hardware (AMD SEV / Intel TDX)
- Booted with expected firmware and software (measured boot)
- Not tampered with since boot

The hardware generates a signed attestation report containing measurements (cryptographic hashes) of the VM's initial state. A remote verifier can check this report against expected values to establish trust.

### Post-Attestation Secret Provisioning

**Post-attestation** refers to actions that occur only after successful attestation verification. In the context of secrets:

1. Confidential VM boots with encrypted memory (no secrets yet)
2. VM generates hardware-signed attestation report
3. VM sends report to a Secret Provisioning Service
4. Service verifies attestation (checks signatures, measurements)
5. **Only after successful verification** → secrets are released to the VM
6. GoShip Init receives secrets and configures applications

This pattern ensures secrets are never exposed to unverified or potentially compromised environments.

### External References

- [Confidential Computing Consortium](https://confidentialcomputing.io/) — Industry consortium defining standards
- [AMD SEV Documentation](https://www.amd.com/en/developer/sev.html) — AMD Secure Encrypted Virtualization
- [Intel TDX Documentation](https://www.intel.com/content/www/us/en/developer/tools/trust-domain-extensions/overview.html) — Intel Trust Domain Extensions
- [Linux Kernel CoCo Documentation](https://docs.kernel.org/virt/hyperv/coco.html) — Kernel confidential computing support
- [Linux Kernel CoCo Security](https://docs.kernel.org/security/secrets/coco.html) --- Kernel confidential computing security

---

End of document.
