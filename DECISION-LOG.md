# GoShip Decision Log

This document records the key architectural and technical decisions made during GoShip's development. It is intended for contributors who want to understand **what** was decided and **why**, without digging through blog posts or git history.

Each entry follows a lightweight ADR (Architecture Decision Record) format: a one-line decision, the context that prompted it, alternatives that were considered, and the rationale for the choice made.

## How to Read This Log

- Entries are grouped by implementation step and ordered chronologically.
- **Status: Accepted** means the decision is current.
- **Status: Superseded** means a later decision replaced it (linked via "Superseded by").
- Cross-references point to related ADRs where decisions interact.

---

## Steps 0-2: Foundation (Jan 31, 2026)

### ADR-001: Use Libvirt for VM management

- **Decision**: Use libvirt (via `libvirt.org/go/libvirt` Go bindings) as the primary VM lifecycle manager instead of shelling out to QEMU directly.
- **Context**: GoShip needs to create, start, stop, and destroy VMs programmatically. QEMU can be invoked directly, but managing the full lifecycle (domain definitions, network setup, device hotplug) manually is error-prone.
- **Alternatives considered**: Direct QEMU invocation via `exec.Command`; Kata Containers; Firecracker.
- **Rationale**: Libvirt provides a stable API for domain XML, network management, storage pools, and device configuration. It handles details like PCI address assignment, MAC generation, and process management. The `ProjectRuntime` interface allows swapping backends later (Kata, Firecracker) without changing the control plane. See `docs/DESIGN.md` section 11 for the runtime evolution roadmap.
- **Status**: Accepted

### ADR-002: Use `qemu:///system` connection URI

- **Decision**: Connect to the system-wide libvirtd daemon via `qemu:///system`, not the per-user `qemu:///session`.
- **Context**: Libvirt has two daemon modes. The session daemon is unprivileged but only supports SLIRP (user-mode) networking with no bridging or virtual network support.
- **Alternatives considered**: `qemu:///session` (no root/group membership needed, but no real networking).
- **Rationale**: GoShip needs `<interface type='network'>` for host-VM communication over libvirt's NAT bridge. SLIRP networking cannot support this. The trade-off is requiring membership in the `libvirt` group, which is acceptable for a development tool.
- **Status**: Accepted

### ADR-003: q35 machine type

- **Decision**: Use the `q35` machine type in domain XML instead of the older `i440fx` (pc).
- **Context**: QEMU supports two main x86 machine types. The `i440fx` is the legacy default; `q35` is the modern chipset with PCIe, AHCI, and better device topology.
- **Alternatives considered**: `i440fx` (pc) — wider compatibility but aging architecture.
- **Rationale**: `q35` supports both BIOS and UEFI boot, has native PCIe (important for future device passthrough and confidential computing), and is the recommended machine type for new VMs. It doesn't lock us out of any future feature.
- **Status**: Accepted

### ADR-004: BIOS (SeaBIOS) over UEFI for Phase 0

- **Decision**: Use BIOS firmware (SeaBIOS, the QEMU default) instead of UEFI (OVMF) for Phase 0.
- **Context**: The domain XML `<os>` section can specify either firmware. UEFI requires the `ovmf` package on the host and additional XML elements (`<loader>`, `<nvram>`).
- **Alternatives considered**: UEFI (OVMF) — needed for Secure Boot and Confidential Computing (SEV-SNP, TDX).
- **Rationale**: BIOS produces a simpler domain XML (three lines in `<os>`, no `<loader>` or `<nvram>` paths), requires no extra host packages, and is sufficient for learning VM lifecycle management. The `q35` machine type supports both, so switching to UEFI later doesn't require a machine type change. See also ADR-009.
- **Status**: Accepted

---

## Step 3: VM Lifecycle (Feb 1, 2026)

### ADR-005: Copy-on-Write (QCOW2) disk images

- **Decision**: Create per-VM disk images as QCOW2 copy-on-write overlays backed by a shared base image, rather than copying the full base image for each VM.
- **Context**: Each VM needs its own writable disk. Copying a 200MB+ base image for every VM wastes disk space and time.
- **Alternatives considered**: Full copy of base image per VM; thin-provisioned raw images.
- **Rationale**: CoW overlays start near-zero size and only grow as the VM writes data. Creation is instant (`qemu-img create -f qcow2 -F qcow2 -b <base>`). The base image stays immutable and reusable. Cleanup is trivial: `os.RemoveAll` on the VM directory removes everything.
- **Status**: Accepted

### ADR-006: Disable AppArmor/SELinux (`seclabel type='none'`) for Phase 0

- **Decision**: Add `<seclabel type='none'/>` to domain XML to disable libvirt's security driver confinement.
- **Context**: With `qemu:///system`, QEMU runs as the `libvirt-qemu` user. Disk images in `~/.goship/` are inaccessible due to home directory permissions (`0750`) and AppArmor confinement.
- **Alternatives considered**: Move images to `/var/lib/libvirt/images/` (standard path, already allowed by AppArmor); configure custom AppArmor profiles; use DAC labels to run QEMU as the current user. See ADR-034.
- **Rationale**: `seclabel type='none'` is the simplest path for a single-user development tool. It disables all security driver confinement for GoShip VMs. This is explicitly a Phase 0 shortcut — the DAC label approach (ADR-034) is the planned hardening path.
- **Status**: Accepted (superseded in practice by ADR-034 for file access; both are set in Phase 0)

### ADR-007: Store data in `~/.goship/` (user home)

- **Decision**: Use `~/.goship/` as the default data directory for VM disk images, cloud-init ISOs, sockets, and state.
- **Context**: GoShip needs a persistent location for VM artifacts. System paths like `/var/lib/goship/` require `sudo` for initial setup.
- **Alternatives considered**: `/var/lib/goship/` (follows FHS, avoids permission issues with `qemu:///system`); `/var/lib/libvirt/images/goship/` (standard libvirt path, AppArmor-friendly).
- **Rationale**: `~/.goship/` provides zero-setup experience: clone, build, run. No `sudo`, no system directory creation. This mirrors `~/.docker/` and `~/.kube/` conventions. The trade-off is needing security label workarounds (ADR-006, ADR-034) for QEMU file access. A system path is the better long-term answer.
- **Status**: Accepted

---

## Step 4: Cloud-Init (Feb 2, 2026)

### ADR-008: Cloud-Init NoCloud for VM provisioning

- **Decision**: Use cloud-init with the NoCloud data source (ISO attached as CDROM) for first-boot VM provisioning.
- **Context**: After creating a VM, it boots into a generic Alpine image with no SSH key, no hostname, no identity. Manual console configuration is not automation.
- **Alternatives considered**: Manual configuration via serial console; Packer-based custom image builds; pre-configured images with baked-in credentials.
- **Rationale**: Cloud-init is the industry standard for first-boot provisioning (AWS, GCP, Azure, OpenStack all use it). NoCloud reads configuration from a local CDROM ISO, requiring no metadata server. The base image stays generic; per-VM identity travels as a sidecar ISO. The ISO is read-only, disposable, and cleaned up with the VM directory.
- **Status**: Accepted

### ADR-009: Alpine with BIOS variant base image

- **Decision**: Use `nocloud_alpine-*-x86_64-bios-cloudinit-*.qcow2` as the base VM image.
- **Context**: Alpine publishes multiple variants: `bios` vs `uefi` disk layout, `nocloud` vs other data sources.
- **Alternatives considered**: UEFI variant (GPT-partitioned with EFI System Partition); Ubuntu or Debian cloud images.
- **Rationale**: The `bios` variant matches the SeaBIOS firmware choice (ADR-004) — mismatching firmware and disk layout causes boot failures. Alpine is minimal (~200MB), boots fast, and includes cloud-init. The `nocloud` data source matches our ISO-based provisioning approach (ADR-008).
- **Status**: Accepted

---

## Step 5: Virtio-Serial (Feb 4, 2026)

### ADR-010: Virtio-serial for host-VM communication

- **Decision**: Use virtio-serial (paravirtualized serial port over Unix socket) as the host-to-VM communication channel.
- **Context**: The control plane needs to send commands to the in-VM agent (deploy, stop, status). The channel must work without network dependency, with low latency, and be expressible in libvirt domain XML.
- **Alternatives considered**:
  - **SSH** — Requires key management, network stack, sshd running in VM. Too heavy for a control channel.
  - **QEMU Guest Agent** — Opinionated, limited to predefined commands (file ops, freeze/thaw). Not extensible for custom actions.
  - **Virtio-vsock (AF_VSOCK)** — Modern and promising, but requires `VHOST_VSOCK` kernel module and CID management. Less mature tooling on Alpine.
  - **9P/virtiofs** — Shared filesystem, awkward for request/response (would need polling or inotify).
- **Rationale**: Virtio-serial provides a Unix domain socket on the host and a character device (`/dev/virtio-ports/goship.0`) in the VM. It's full-duplex, low overhead, debuggable with `socat`, and has first-class libvirt support. The protocol is entirely under our control.
- **Status**: Accepted

### ADR-011: Newline-delimited JSON (NDJSON) protocol

- **Decision**: Use newline-delimited JSON as the wire protocol over the virtio-serial channel.
- **Context**: Need a protocol for request/response messages between host and VM agent.
- **Alternatives considered**: Binary protocol (protobuf, msgpack); custom text protocol.
- **Rationale**: JSON is human-readable (debuggable with `socat` or `cat`), supported by Go's stdlib (`encoding/json`), and appropriate for control plane traffic (small command messages, not data streams). Parsing overhead is irrelevant for 50-byte messages. A binary protocol would add complexity for zero benefit at this scale.
- **Status**: Accepted

### ADR-012: Bind mode for virtio-serial socket

- **Decision**: Use `mode='bind'` (not `mode='connect'`) for the virtio-serial Unix socket.
- **Context**: Libvirt supports two socket modes: `bind` (QEMU creates and listens on the socket) and `connect` (QEMU dials an existing socket you created).
- **Alternatives considered**: Connect mode — host code creates the socket, QEMU dials it.
- **Rationale**: In bind mode, the socket lifecycle matches the VM lifecycle automatically — QEMU creates it on VM start and removes it on VM stop. No orphan socket cleanup needed. Connect mode would require the host to manage socket creation and lifecycle independently.
- **Status**: Accepted

### ADR-013: Socket co-located with VM disk directory

- **Decision**: Place the virtio-serial Unix socket (`goship.sock`) in the same directory as the VM's disk image (`~/.goship/vms/{name}/`).
- **Context**: The socket file needs a predictable location. Options include a central socket directory or per-VM placement.
- **Alternatives considered**: Central socket directory (e.g., `~/.goship/sockets/`); `/var/run/goship/`.
- **Rationale**: Co-location means cleanup is trivial — `os.RemoveAll` on the VM directory removes disk, cloud-init ISO, and socket together. No separate socket registry, no orphan cleanup on crash. The trade-off (needing the VM name to find the socket) is acceptable since the state store already tracks this.
- **Status**: Accepted

---

## Step 6: GoShip Init + Per-VM Provisioning (Feb 10, 2026)

### ADR-014: GoShip Init as PID 1 in-guest agent

- **Decision**: Build a custom in-guest agent (`goship-init`) that runs as PID 1 inside VMs, handling filesystem mounts, networking, and command dispatch.
- **Context**: The VM needs an agent to receive commands over the virtio-serial channel and execute them (deploy apps, stop processes, report status).
- **Alternatives considered**: Use Alpine's init system (OpenRC) as PID 1 and run goship-init as a regular service; use systemd inside the VM.
- **Rationale**: Running as PID 1 gives the agent full control over the VM's lifecycle, including mounting `/proc`, `/sys`, `/dev`, and bringing up networking. When running under OpenRC (which is also supported), the PID 1 duties are skipped via `os.Getpid() == 1` check. This dual-mode design supports both bare init and managed service scenarios.
- **Status**: Accepted

### ADR-015: Static binary for goship-init (`CGO_ENABLED=0`)

- **Decision**: Build `goship-init` as a fully static binary with `CGO_ENABLED=0`.
- **Context**: The binary runs inside minimal VM images where specific shared libraries may not exist.
- **Alternatives considered**: Dynamic binary with bundled shared libraries; Alpine-compatible musl-linked binary.
- **Rationale**: `CGO_ENABLED=0` forces pure Go compilation, producing a self-contained binary with no libc dependency. This guarantees it runs on any Linux VM regardless of the installed C library. Verified with `file bin/goship-init` showing "statically linked".
- **Status**: Accepted

### ADR-016: Per-VM provisioning instead of baking into base image

- **Decision**: Provision `goship-init` into each VM's CoW overlay disk during `goshipctl project create`, rather than baking it into the shared base image.
- **Context**: The original approach (baking `goship-init` into the base image via `virt-customize` during `image build`) was fragile: GitHub release 404s, `apk add` failures during offline image customization, and one bad base image contaminating every VM.
- **Alternatives considered**: Keep baking into base image (original approach, ADR-014's initial implementation); download the binary inside the VM via cloud-init `runcmd`.
- **Rationale**: Per-VM provisioning keeps the base image immutable and plain. Failures are contained to a single VM creation. Iterating on `goship-init` no longer requires rebuilding the base image. The `virt-customize` tool operates on the CoW overlay, so the base image is never mutated. This was a rework of Step 6 after repeated failures with the original approach.
- **Status**: Accepted (supersedes the original "bake into base image" approach)

### ADR-017: Docker installation via cloud-init, not virt-customize

- **Decision**: Install Docker packages via cloud-init `packages` directive (at first boot with network access) instead of via `virt-customize` (offline image modification).
- **Context**: `virt-customize` runs offline — no network access inside the image, so `apk add docker` only works if packages are cached. Cloud-init runs at first boot when the VM has network access via DHCP.
- **Alternatives considered**: Pre-install Docker in the base image; download Docker binary manually via cloud-init `runcmd`.
- **Rationale**: Cloud-init's `packages` directive is the standard way to install packages on first boot. The VM has network access (DHCP from libvirt's default network), so package installation works reliably. This also removed Docker-related code from `guest_provision.go`, simplifying the provisioning pipeline.
- **Status**: Accepted

### ADR-018: Single mutex for virtio-serial communication

- **Decision**: Use a single `sync.Mutex` to serialize access to the virtio-serial socket in `VMCommunicator`, rather than a connection pool.
- **Context**: The communicator sends commands to the VM agent over a single Unix socket connection. Concurrent access could corrupt the NDJSON framing.
- **Alternatives considered**: Connection pooling (multiple concurrent connections to the socket); per-command connection (dial, send, read, close).
- **Rationale**: In Phase 0, commands are sequential — deploy, then status, then stop. There's no concurrent command stream. A connection pool adds complexity for zero benefit. Per-command connections would work but add latency for the connect/disconnect overhead on every operation.
- **Status**: Accepted

### ADR-019: 30-second default timeout for commands

- **Decision**: Default to a 30-second timeout for virtio-serial commands, overridable via context deadline.
- **Context**: The timeout must accommodate the worst case: VM just booted, cloud-init still running, GoShip Init not yet listening.
- **Alternatives considered**: Shorter default (5 seconds) with caller-side retry; no default timeout (rely on context).
- **Rationale**: 30 seconds covers the first-boot window where cloud-init is still running and services haven't started yet. Callers can set shorter deadlines via context for operations like ping where fast feedback matters. No retry logic in the communicator — retry policy belongs to the caller who knows the operational context.
- **Status**: Accepted

---

## Steps 7-9: State Store & Project CLI (Feb 13, 2026)

### ADR-020: JSON file state store for Phase 0

- **Decision**: Use a JSON file (`~/.goship/state.json`) with `sync.RWMutex` for state persistence instead of a database.
- **Context**: GoShip needs to persist project and app definitions across CLI invocations.
- **Alternatives considered**: SQLite; BoltDB/bbolt; etcd.
- **Rationale**: A JSON file is the simplest persistence mechanism that works. It's human-readable (debuggable with `cat`), requires no dependencies, and every write persists to disk immediately. The `sync.RWMutex` provides thread safety for concurrent reads. This is explicitly a Phase 0 choice — the state store is concrete (no interface) and will be replaced with SQLite or similar in later phases.
- **Status**: Accepted

### ADR-021: Separation of `app create` vs `app deploy`

- **Decision**: Split app lifecycle into `app create` (define in state store) and `app deploy` (send to VM), rather than a single command that does both.
- **Context**: Users need to define app specifications (image, ports, env vars, resources) and then deploy them to VMs.
- **Alternatives considered**: Single `app deploy` that creates and deploys in one step.
- **Rationale**: Separation lets users define apps, review them with `app info`, edit with `app edit`, and deploy when ready. It mirrors the declarative model where desired state is defined first, then applied. The state store holds the app definition; the VM holds the running instance. `app delete` does best-effort cleanup on the VM, then always removes from state (ADR-024).
- **Status**: Accepted

---

## Steps 10-11: Docker in VM & App CLI (Feb 15, 2026)

### ADR-022: Two execution modes (container + process)

- **Decision**: Support two app execution modes inside VMs: `container` (Docker/Podman) and `process` (direct binary with supervisor).
- **Context**: Some workloads are naturally containerized; others are single Go binaries or custom daemons that don't need container overhead.
- **Alternatives considered**: Container-only (simpler, but forces containerization of everything); process-only (misses the container ecosystem).
- **Rationale**: The `ExecutorManager` routes by mode and aggregates status from both engines. Docker is tried first for stop/remove (cheaper name lookup), with process manager as fallback. Both implement the same `AppExecutor` interface. This matches the design doc's supported execution modes.
- **Status**: Accepted

### ADR-023: Boot progress streaming during VM creation

- **Decision**: Stream cloud-init logs to the terminal during `project create` so users can see what the VM is doing.
- **Context**: VM creation takes 30-60 seconds (cloud-init installs packages, starts services). Without feedback, users see a blank terminal and don't know if it's working.
- **Alternatives considered**: Simple spinner with no detail; silent creation with post-check.
- **Rationale**: The `ProgressWriter` polls the VM's cloud-init log (`/var/log/cloud-init-output.log`) via the virtio-serial channel and prints new lines as they appear. This gives real-time visibility into Docker installation, service startup, and any errors. The offset tracker ensures only new lines are printed on each poll.
- **Status**: Accepted

### ADR-024: Best-effort VM operations on app delete

- **Decision**: When deleting an app, attempt to stop and remove it from the VM (best-effort), then always remove it from the state store regardless of VM operation success.
- **Context**: The VM might be stopped, unreachable, or in an error state when the user wants to delete an app definition.
- **Alternatives considered**: Require VM to be running for delete; leave state store entry if VM operation fails.
- **Rationale**: The state store is the source of truth for desired state. If the VM is unreachable, the user still needs to clean up app definitions. The app will be re-removed from the VM on next reconciliation or won't exist if the VM is destroyed. Blocking on VM reachability would create a poor user experience.
- **Status**: Accepted

---

## Step 12: Process Mode (Feb 17, 2026)

### ADR-025: Chunked base64 binary upload (512KB chunks + SHA256)

- **Decision**: Transfer binaries from host to VM using a three-phase protocol (begin/data/finish) over the NDJSON virtio-serial channel, with 512KB base64-encoded chunks and SHA256 integrity verification.
- **Context**: Process-mode apps reference host-side binaries that don't exist inside the VM. The virtio-serial channel speaks NDJSON, so large binaries can't fit in a single message.
- **Alternatives considered**: SCP/SSH-based transfer (requires network + sshd); shared filesystem (9p/virtiofs); single large JSON message.
- **Rationale**: The chunked protocol reuses the existing virtio-serial channel with no new infrastructure. 512KB chunks balance transfer speed against JSON message size (~700KB after base64 expansion). SHA256 verification catches corruption. Atomic rename (`.new` suffix during transfer) prevents partial uploads from corrupting existing binaries. Path sanitization (`filepath.Base`) prevents directory traversal attacks.
- **Status**: Accepted

### ADR-026: Auto-restart with exponential backoff

- **Decision**: Implement auto-restart for managed processes with exponential backoff (1s, 2s, 4s, 8s, 16s, capped at 30s, max 5 attempts) and a `stopping` flag to prevent restart during explicit stop/remove.
- **Context**: Process-mode apps that crash should be restarted automatically (if the restart policy says so), but restart loops waste CPU and fill logs.
- **Alternatives considered**: Immediate restart with no backoff; fixed-interval restart; delegate to systemd.
- **Rationale**: Three restart policies (`always`, `on-failure`, `never`) cover common use cases. Exponential backoff prevents tight restart loops. The `stopping` flag prevents a race where `Stop()` sends SIGTERM, the process exits, and the monitor tries to restart it. The mutex is released during the backoff sleep to avoid blocking other operations.
- **Status**: Accepted

### ADR-027: Process groups (`Setpgid`) for clean shutdown

- **Decision**: Run managed processes in their own process groups via `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`.
- **Context**: When stopping a process, SIGTERM should go to the managed process and its children, not to the GoShip Init agent.
- **Alternatives considered**: Send signals directly to the PID (misses child processes); use cgroups for process tracking.
- **Rationale**: `Setpgid: true` isolates the managed process's process group from the agent. The stop sequence sends SIGTERM, waits up to 10 seconds on the `done` channel, then falls back to SIGKILL. The `done` channel (not a second `cmd.Wait()` call) coordinates between the monitor goroutine and the stop logic to avoid double-wait panics.
- **Status**: Accepted

### ADR-028: Accurate process status via `/proc`

- **Decision**: Check `/proc/<pid>/status` for kernel-level process state instead of relying solely on in-memory state tracking.
- **Context**: The in-memory state might say "running" when the process was killed by the OOM killer or became a zombie.
- **Alternatives considered**: Rely only on in-memory state from the monitor goroutine; use `kill(pid, 0)` for existence check only.
- **Rationale**: `/proc/<pid>/status` provides the actual kernel process state: `S (sleeping)`, `R (running)`, `Z (zombie)`, `T (stopped)`. If the file doesn't exist, the process has exited. This enriches the CLI output from just "running" to `running (pid 1234, S (sleeping))`, which is much more useful for debugging.
- **Status**: Accepted

### ADR-029: Per-process log files

- **Decision**: Redirect each managed process's stdout/stderr to its own log file at `/var/log/goship-<appname>.log` with `O_APPEND` mode.
- **Context**: Previously, all managed processes shared the Init agent's stdout, making logs from different apps indistinguishable.
- **Alternatives considered**: Structured logging to a central file with app name prefixes; in-memory ring buffer.
- **Rationale**: Per-process files integrate with the existing `ActionLogs` handler (which reads any file under `/var/log/`) with no new protocol needed. `O_APPEND` ensures logs survive process restarts. The log file handle is stored in `managedProcess` and cleaned up on Remove (but the file persists on disk for post-mortem analysis).
- **Status**: Accepted

---

## Release & Hardening (Feb 18-20, 2026)

### ADR-030: `libvirt_dlopen` build tag for portability

- **Decision**: Build `goshipctl` with `-tags libvirt_dlopen` so the binary loads libvirt at runtime via `dlopen()` instead of linking at compile time.
- **Context**: The default Go libvirt bindings use `pkg-config: libvirt`, which creates a hard version dependency. A binary built on libvirt 9.7.0 won't start on a machine with libvirt 9.0.0 due to ELF symbol version mismatches.
- **Alternatives considered**: Static linking against libvirt (complex, not well-supported); build on oldest supported version; ship per-distro binaries.
- **Rationale**: With `libvirt_dlopen`, the binary has no compile-time libvirt dependency. At runtime, it calls `dlopen("libvirt.so.0", ...)` and resolves symbols lazily. Functions that exist work normally; missing functions fail gracefully at the call site. CGO is still required (the dlopen calls themselves are C code), but `libvirt-dev` is not needed at build time.
- **Status**: Accepted

### ADR-031: KVM with QEMU TCG fallback

- **Decision**: Detect KVM availability at runtime and fall back to QEMU TCG (software emulation) when `/dev/kvm` is not present.
- **Context**: GoShip was originally KVM-only. On cloud VMs without nested virtualization, containers without `/dev/kvm`, or laptops with VT-x disabled, VM creation would fail.
- **Alternatives considered**: Require KVM (simpler, but limits where GoShip runs); always use TCG (works everywhere, but wastes hardware acceleration).
- **Rationale**: The domain XML `type` attribute switches between `kvm` and `qemu` based on a single `EnableKVM` boolean from capabilities discovery. TCG is 10-100x slower for CPU-bound work but I/O (virtio) performance is identical. For development and testing, TCG is perfectly usable. A warning is printed when falling back to TCG.
- **Status**: Accepted

### ADR-032: `host-passthrough` (KVM) vs `host-model` (TCG) CPU modes

- **Decision**: Use `host-passthrough` CPU mode when KVM is available and `host-model` when falling back to TCG.
- **Context**: The CPU mode was originally hardcoded to `host-passthrough`, which requires KVM (it passes the real host CPU through to the guest). Without KVM, libvirt rejects the domain or QEMU crashes.
- **Alternatives considered**: Always use `host-model` (works everywhere but masks CPU features under KVM); use a specific CPU model like `Skylake-Server`.
- **Rationale**: `host-passthrough` gives the best performance and full CPU feature exposure under KVM. `host-model` is libvirt's recommended mode for maximum compatibility — it approximates the host CPU using QEMU's built-in definitions and works with both KVM and TCG. Both conditionals use the same `EnableKVM` field, keeping the logic simple. See ADR-031.
- **Status**: Accepted

### ADR-033: Auto-manage libvirt default network

- **Decision**: Automatically ensure the libvirt `default` network is active before VM creation, including defining it from scratch if missing.
- **Context**: After a fresh install or reboot, the `default` network is often inactive. VM creation fails with `network 'default' is not active`. Users shouldn't need to remember `virsh net-start default`.
- **Alternatives considered**: Require manual network setup (documented in README); use bridge networking instead of libvirt networks.
- **Rationale**: The `EnsureNetwork` function handles three cases: network active (no-op), network inactive (start + enable autostart), network missing and name is "default" (define from standard XML + start + enable autostart). For non-default networks that don't exist, an error with instructions is returned. Setting autostart makes this a one-time fix.
- **Status**: Accepted

### ADR-034: DAC labels (numeric UIDs) for QEMU file access

- **Decision**: Set a DAC (Discretionary Access Control) security label in domain XML to run QEMU as the current user's UID:GID, using numeric IDs with the `+` prefix.
- **Context**: With `qemu:///system`, QEMU runs as `libvirt-qemu` by default and cannot access files in `~/.goship/`. ADR-006 disables all security drivers as a workaround, but that's the nuclear option.
- **Alternatives considered**: Move images to `/var/lib/libvirt/images/` (eliminates the problem entirely — this is the best long-term answer); keep `seclabel type='none'` only; configure AppArmor profiles.
- **Rationale**: The DAC label `+<uid>:+<gid>` (via `os.Getuid()`/`os.Getgid()`) tells libvirt to run QEMU as the current user. Numeric IDs (with `+` prefix) are more portable than usernames — they work in containers and minimal environments. `relabel='no'` prevents libvirt from `chown`ing disk images. Currently, `SecurityNone=true` takes priority in the XML template, but the DAC label is populated and ready for when security is tightened in later phases.
- **Status**: Accepted

---

## Compose Build Support (Feb 22, 2026)

### ADR-035: Build-and-push for compose services with `build:` context

- **Decision**: When a compose service has `build:` but no `image:`, run `docker build` on the host and push the resulting image into the VM using the existing `pushLocalImage` flow, rather than skipping the service.
- **Context**: Docker Compose files commonly use `build: .` for application services that are built from local source. GoShip's compose parser previously skipped these services with a warning ("build is not supported"), which meant users had to manually build and tag images before deploying.
- **Alternatives considered**:
  - **Keep skipping** — Simple, but breaks the most common compose workflow where the app service uses `build:`.
  - **Build inside the VM** — Send the build context into the VM and run `docker build` there. Avoids host Docker dependency but requires transferring potentially large source trees over virtio-serial and having Docker build tooling ready inside the VM.
  - **Require explicit `image:` alongside `build:`** — Force users to add an image name. Less ergonomic and non-standard compared to Docker Compose behavior.
- **Rationale**: Building on the host matches the Docker Compose workflow: `docker compose build` runs on the host, then images are used by services. GoShip already has the `pushLocalImage` mechanism (docker save, gzip, virtio-serial transfer, docker load) used by `app deploy --local-image` and `app push-image`. Reusing this for compose builds requires no new protocol or infrastructure. A deterministic image name (`goship-<service>:latest`) is generated for services without an explicit `image:` field. The `--build` flag (default: `true`) lets users skip builds when images are already pushed.
- **Status**: Accepted

---

## Summary of Superseded Decisions

| Original | Superseded by | What changed |
|----------|--------------|--------------|
| Bake goship-init into base image (original Step 6) | ADR-016 | Moved to per-VM provisioning via CoW overlay |
| Install Docker via virt-customize | ADR-017 | Moved to cloud-init packages (needs network) |
| `seclabel type='none'` as sole access fix (ADR-006) | ADR-034 | DAC labels provide targeted access without disabling all security |
