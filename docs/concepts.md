# GoShip Concepts

This document explains the core ideas behind GoShip and how its components fit together.

## Overview

GoShip is a VM-centric application platform. Instead of deploying apps directly to a host, GoShip gives each project its own virtual machine and runs applications inside that VM through an in-guest agent.

```
You (CLI) --> goship --> libvirt/QEMU --> VM --> GoShip Init --> Your App
```

GoShip also supports an API mode where the CLI talks to a REST API server (`goship server`) instead of calling libvirt directly:

```
You (CLI) --> goship --> goship server (HTTP) --> libvirt/QEMU --> VM --> GoShip Init --> Your App
```

## Projects

A **project** is the top-level isolation boundary. Each project owns exactly one VM (in Phase 0) and all applications within that project run inside that VM.

Projects have:
- **Name** — unique identifier (e.g., `myapp`)
- **Resources** — CPU cores, memory (MB), and disk (MB) allocated to the VM
- **State** — lifecycle state of the project
- **Domains** — one or more domain names for reverse proxy routing (e.g., `["myapp.local"]`)
- **Default domain** — primary domain used when none is specified

Project states:

| State | Meaning |
|-------|---------|
| `pending` | Project created, VM not yet provisioned |
| `creating` | VM is being provisioned |
| `running` | VM is up and GoShip Init is responsive |
| `stopped` | VM has been shut down |

```bash
goship project create myapp --cpu 2 --memory 1024 --disk 4096
goship project list
goship project info myapp
goship project delete myapp
```

## Applications

Applications run inside project VMs. GoShip supports two execution modes:

### Container Mode

Deploys Docker images inside the VM. The in-guest agent pulls the image and manages the container lifecycle via the Docker SDK.

```bash
goship app create myproject web --mode container --image nginx:alpine --port 8080:80
goship app deploy myproject web
```

### Process Mode

Runs a binary directly inside the VM. When you deploy, GoShip uploads the binary from your host to the VM over virtio-serial, then starts it as a supervised process with auto-restart support.

```bash
goship app create myproject api --mode process --binary ./bin/myapi --port 3000:3000
goship app deploy myproject api
```

### App Lifecycle

```
create --> deploy --> running --> stop --> delete
  (definition)  (push to VM)          (cleanup)
```

- **create** — Saves the app definition (image, ports, env vars) to the state store. Nothing runs yet.
- **deploy** — Sends the app spec to GoShip Init inside the VM. For containers, it pulls the image and starts the container. For processes, it uploads the binary and starts it.
- **stop** — Stops the running app inside the VM without removing it.
- **delete** — Removes the app from the VM and deletes the definition from state.

### Restart Policies (Process Mode)

Process mode apps support restart policies:

| Policy | Behavior |
|--------|----------|
| `never` | Process is not restarted if it exits |
| `always` | Always restart, with exponential backoff (1s, 2s, 4s, ... up to 60s) |
| `on-failure` | Restart only on non-zero exit codes, with exponential backoff |

## Reverse Proxy

GoShip includes a built-in HTTP reverse proxy that routes requests to apps inside VMs based on the `Host` header. This eliminates the need to know VM IP addresses — apps are accessible at human-friendly domain names.

### Routing Model

Routes follow the pattern `{hostname}.{domain}` → `{VM_IP}:{port}`:

- **Domains** are set on the project (e.g., `myapp.local`)
- **Hostname** defaults to the app name, but can be customized per app
- **Port** is the first host port from the app's port mappings

For example, a project with domain `myapp.local` and an app named `web` with port `8080:80` produces the route `web.myapp.local` → `192.168.122.10:8080`.

### Available Flag

Apps are externally routable by default. To prevent an app from being registered in the proxy (e.g., internal databases or background workers), set `available` to `false`. The `Available` field is a `*bool` — `nil` means available (default true).

### Route Lifecycle

- **Registered** when an app is deployed (`app deploy`)
- **Removed** when an app is stopped or deleted (`app stop`, `app delete`)
- **Reconciled** when project domains or app hostname/available change
- **Rebuilt** from state on `goship server` startup

### Proxy Server

The proxy listens on a separate port (default `:8081`, configurable via `GOSHIP_PROXY_ADDR`). This keeps API traffic (`:8080`) cleanly separated from proxied application traffic.

```bash
# Access an app through the proxy
curl http://web.myapp.local:8081/
```

## Nodes

A **node** is a compute host in the GoShip cluster. In Phase 0, there is a single implicit node (the local machine). The node entity lays the groundwork for multi-node scheduling in future phases.

Each node has:
- **Hostname** — unique human-readable identifier
- **Endpoint** — agent address (`ip:port`) for future node-to-node communication
- **Labels** — key-value pairs for scheduling and organization (e.g., `region=us-east`)
- **Resources** — CPU and memory available on the node

Node statuses:

| Status | Meaning |
|--------|---------|
| `online` | Node is healthy and accepting workloads |
| `offline` | Node is unreachable or shut down |
| `draining` | Node is being evacuated — no new workloads scheduled |

```bash
goship node register worker-1 --endpoint 10.0.0.5:9090 --label region=us-east
goship node list
goship node info worker-1
goship node drain worker-1
goship node remove worker-1
```

## Architecture

GoShip uses a three-tier execution model:

```
┌──────────────────────────────────┐
│  goship (CLI on your host)    │  You run commands here
└──────────────┬───────────────────┘
               │  Calls libvirt API
               ▼
┌──────────────────────────────────┐
│  Libvirt Runtime                 │  Manages VM lifecycle
│  (QEMU/KVM via libvirt-go)      │  (create, destroy, start, stop)
└──────────────┬───────────────────┘
               │  Virtio-serial (JSON protocol)
               ▼
┌──────────────────────────────────┐
│  GoShip Init (PID 1 inside VM)  │  Manages apps inside the VM
│  - Docker Manager (containers)  │  (deploy, stop, remove, status, logs)
│  - Process Manager (binaries)   │
└──────────────────────────────────┘
```

- **goship** — The CLI tool you interact with. By default it talks directly to libvirt. When `GOSHIP_API_URL` is set, it talks to `goship server` over HTTP instead.
- **goship server** — The REST API server. It wraps the same libvirt runtime and state store behind a JSON API, enabling remote management.
- **Libvirt Runtime** — The backend that translates project/app operations into VM lifecycle calls (domain XML, CoW disks, cloud-init, virtio-serial communication).
- **GoShip Init** — A static Go binary injected into each VM during provisioning. It runs as PID 1, listens on a virtio-serial device, and executes commands from the host (deploy, stop, status, logs).

## VM Lifecycle

When you run `goship project create`, several things happen in sequence:

1. **CoW disk creation** — A copy-on-write QCOW2 overlay is created from the base image. The base image stays clean; each VM writes changes to its own overlay.
2. **Guest provisioning** — `virt-customize` runs against the overlay to inject `goship-init`, set up its OpenRC service, and optionally install Docker.
3. **Cloud-init ISO** — A small ISO is generated with the VM's hostname and identity, attached as a CDROM.
4. **Domain XML** — A libvirt domain definition is generated specifying CPU, memory, disks, network, and a virtio-serial channel.
5. **VM start** — Libvirt defines and starts the domain. QEMU boots the VM with KVM acceleration.
6. **Agent ready** — GoShip Init starts inside the VM, opens the virtio-serial device, and begins accepting commands.

## Communication

Host-to-VM communication uses **virtio-serial**, a paravirtualized serial port bridged by QEMU:

- **Host side:** Unix domain socket at `~/.goship/vms/<project>/goship.sock`
- **Guest side:** Character device at `/dev/virtio-ports/goship.0`

The protocol is newline-delimited JSON (NDJSON). Example exchange:

```
Host  --> {"action":"ping"}
Guest <-- {"status":"ok"}

Host  --> {"action":"deploy","app":{"name":"web","image":"nginx:alpine"}}
Guest <-- {"status":"ok"}

Host  --> {"action":"status"}
Guest <-- {"status":"ok","apps":[{"name":"web","state":"running"}]}
```

This channel is network-independent — it works even if the VM has no IP address yet.

## State Management

GoShip persists all state to a JSON file at `~/.goship/state.json`. This includes:

- Projects (name, ID, resources, state)
- Instances (VM ID, domain name, IP address, state)
- Apps (name, mode, image/binary, ports, env vars)

The state file is read/written with a mutex for thread safety. Every write is persisted to disk immediately.

## Directory Layout

GoShip stores all its data under `~/.goship/` by default (configurable with `--data-dir`):

```
~/.goship/
├── state.json                  # All project/app state
├── images/
│   └── goship-vm.qcow2        # Base VM image (shared, read-only)
└── vms/
    └── <project-name>/
        ├── disk.qcow2          # CoW overlay (per-VM writes)
        ├── cloud-init.iso      # VM identity and provisioning
        └── goship.sock         # Virtio-serial Unix socket
```

## What's Next

- [Getting Started](getting-started.md) — Install GoShip and deploy your first app
- [CLI Reference](cli-reference.md) — Complete command reference
- [Troubleshooting](troubleshooting.md) — Common issues and fixes
- [Design Document](DESIGN.md) — Full architecture RFC
