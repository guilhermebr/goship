# Roadmap

GoShip is built incrementally. This roadmap lists what has shipped and what is planned. Items are grouped by theme, not by release version — each can be scheduled and delivered independently.

---

## Shipped

- [x] Project lifecycle — create, start, stop, restart, delete VMs
- [x] Container apps — Docker inside VMs with port mapping, env vars, resource limits
- [x] Process apps — binary upload, auto-restart, exponential backoff, log files
- [x] Compose support — docker-compose.yml parsing, local image build and push
- [x] Image management — pull base images, build overlays, push into VMs
- [x] VM resource editing — resize CPU, memory, disk without recreating
- [x] Boot progress streaming — real-time cloud-init output during provisioning
- [x] KVM/TCG fallback — automatic hardware detection, graceful software emulation
- [x] Virtio-serial communication — JSON protocol between host and VM agent

---

## API & CLI

- [ ] REST API server (goshipd) and CLI HTTP client
- [ ] Security — API key authentication, TLS, vault-encrypted secrets

## Environment & Networking

- [ ] Project-level environment variables with encrypted secrets
- [ ] Reverse proxy with domain-based routing to VM ports

## Multi-Node

- [ ] Node agent, SQLite store, workload scheduler

## Observability

- [ ] Host, VM, and app metrics with in-memory ring buffer
- [ ] eBPF observability
- [ ] TUI dashboard
- [ ] Grafana integration

## AI Integration

- [ ] MCP server for AI tool integration
- [ ] AI assistant with Ollama, OpenAI, and Anthropic backends
- [ ] Log monitoring and anomaly detection
- [ ] Auto-healing — increase memory on OOM, adjust CPU on throttling
- [ ] Root cause analysis and fix suggestions from logs
- [ ] Repository integration — connect a git repo, auto-fix, commit, and deploy
- [ ] Learning from past incidents to prevent recurrence

## Custom Images

- [ ] Image builder tooling — build custom GoShip base images
- [ ] Alpine (current default)
- [ ] Debian
- [ ] Red Hat (RHEL/Fedora)
- [ ] openSUSE MicroOS
- [ ] Gentoo

## Container Runtimes

- [ ] Podman
- [ ] containerd (nerdctl)
- [ ] LXC/LXD

## MicroVMs & Alternative Hypervisors

- [ ] Firecracker — MicroVM backend, sub-second boot (<125ms)
- [ ] Kata Containers — OCI-native isolation, higher density
- [ ] Cloud Hypervisor — Rust-based VMM for modern workloads
- [ ] QEMU microvm — minimal QEMU machine type

## FreeBSD & Jails

- [ ] FreeBSD VM image support
- [ ] FreeBSD Jails as an isolation primitive (alongside containers)
- [ ] BHyve hypervisor integration

## Confidential Computing & Hardware

- [ ] SEV-SNP, TDX attestation
- [ ] GPU and device passthrough
- [ ] UEFI firmware and Secure Boot

---

*This roadmap reflects current plans and will evolve. Contributions and feedback are welcome.*
