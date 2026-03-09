# CLI Reference

Complete reference for `goshipctl`, the GoShip command-line tool.

## Global Flags

These flags apply to all commands:

| Flag | Default | Description |
|------|---------|-------------|
| `--data-dir` | `~/.goship` | Data directory for GoShip state, images, and VM files |
| `-v, --verbose` | `true` | Enable verbose output |
| `--goship-init` | `./bin/goship-init` | Path to the goship-init binary used during VM provisioning |
| `--skip-guest-provision` | `false` | Skip guest disk provisioning (goship-init injection) |
| `--install-docker` | `true` | Install Docker during guest provisioning |
| `--api-url` | *(empty)* | GoShip API server URL (env: `GOSHIP_API_URL`). When set, CLI uses HTTP instead of direct libvirt |

### `goshipd` Configuration

`goshipd` is configured via environment variables (using `ardanlabs/conf`):

| Variable | Default | Description |
|----------|---------|-------------|
| `GOSHIP_ADDR` | `:8080` | API server listen address |
| `GOSHIP_PROXY_ADDR` | `:8081` | Reverse proxy listen address |
| `GOSHIP_DATA_DIR` | `~/.goship` | Data directory |
| `GOSHIP_INIT_BINARY_PATH` | `./bin/goship-init` | Path to goship-init binary |
| `GOSHIP_LIBVIRT_URI` | `qemu:///system` | Libvirt connection URI |

### API Mode

When `--api-url` (or `GOSHIP_API_URL`) is set, `goshipctl` talks to a running `goshipd` server over HTTP instead of calling libvirt directly. This enables remote management without requiring libvirt on the client machine.

```bash
# Start the API server
goshipd &

# Use CLI in API mode
export GOSHIP_API_URL=http://localhost:8080
goshipctl project list
goshipctl project create myapp --cpu 1 --memory 512
```

Some commands are not available in API mode and return an error:
- `project console`, `project logs`, `project edit`, `project update-init`
- `app edit`, `app push-image`
- `env set`, `env list`, `env delete`

---

## `goshipctl version`

Prints version, commit hash, build time, and libvirt/QEMU version info.

```bash
goshipctl version
```

**Example output:**

```
goshipctl dev
  commit: 827853a
  built:  2025-01-15T10:30:00Z

Libvirt version: 10.0.0
QEMU version:    8.2.2
```

---

## `goshipctl capabilities`

Discovers and displays host virtualization capabilities: hypervisor type, KVM status, CPU topology, memory, hugepages, and confidential computing support.

```bash
goshipctl capabilities
```

**Example output:**

```
Host Capabilities
=================

Hypervisor:    QEMU
Architecture:  x86_64
KVM:           yes

CPU Model:     Skylake-Client
CPU Vendor:    Intel
Topology:      1 socket(s), 8 core(s), 2 thread(s) [16 vCPUs]

Memory:        32768 MB (32.0 GB)
Hugepages:     yes (2048kB)
Confidential:  none detected

Libvirt:       10.0.0
QEMU:          8.2.2
```

---

## `goshipctl generate-xml`

Generates a libvirt domain XML document from flags. Does not require a libvirt connection. Useful for experimenting with VM configurations.

```bash
goshipctl generate-xml [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | `goship-vm` | VM name |
| `--uuid` | *(auto-generated)* | VM UUID |
| `--memory` | `512` | Memory in MB |
| `--cpus` | `1` | Number of CPU cores |
| `--enable-kvm` | `true` | Enable KVM acceleration |
| `--disk-path` | `/var/lib/goship/disk.qcow2` | Disk image path |
| `--disk-format` | `qcow2` | Disk format |
| `--network-type` | `network` | Network type (`network`, `bridge`, `user`) |
| `--network-source` | `default` | Network source name |

**Example:**

```bash
goshipctl generate-xml --name test-vm --memory 1024 --cpus 2
```

---

## `goshipctl image`

Manage VM base images.

### `goshipctl image pull`

Downloads the Alpine Linux NoCloud QCOW2 image used as the GoShip base image.

```bash
goshipctl image pull [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--output` | `~/.goship/images/goship-vm.qcow2` | Output path for the downloaded image |
| `--force` | `false` | Overwrite existing image |

**Example:**

```bash
goshipctl image pull
goshipctl image pull --force  # re-download
```

### `goshipctl image build`

Builds a plain base image locally by downloading the Alpine source and resizing it. Does not install goship-init (that happens per-VM during `project create`).

```bash
goshipctl image build [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--output` | `~/.goship/images/goship-vm.qcow2` | Output path for the built image |
| `--force` | `false` | Overwrite existing image |
| `--image-size` | `2G` | Resize image to this virtual size |

**Example:**

```bash
goshipctl image build --image-size 4G
```

---

## `goshipctl project`

Manage projects. Each project runs in its own isolated VM.

### `goshipctl project create <name>`

Creates a new project with its own VM. Provisions the VM with cloud-init, injects goship-init, and optionally installs Docker.

```bash
goshipctl project create <name> [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--cpu` | `1` | Number of CPU cores |
| `--memory` | `512` | Memory in MB |
| `--disk` | `4096` | Disk size in MB |
| `--network-type` | | Network type (`network`, `bridge`, `user`) |
| `--network-source` | | Network source name |

**Example:**

```bash
goshipctl project create webapp --cpu 2 --memory 1024 --disk 8192
goshipctl project create minimal  # uses defaults: 1 CPU, 512MB RAM, 4GB disk
```

### `goshipctl project list`

Lists all projects with state, runtime, resources, and creation time.

```bash
goshipctl project list
```

Alias: `goshipctl project ls`

### `goshipctl project info <name>`

Shows detailed project information including VM instance details and apps.

```bash
goshipctl project info myapp
```

**Example output:**

```
Project: myapp
  ID:       a1b2c3d4
  State:    running
  Runtime:  qemu
  CPU:      2 cores
  Memory:   1024 MB
  Disk:     4096 MB
  Created:  2025-01-15T10:30:00Z

VM Instance:
  ID:       e5f6g7h8
  State:    running
  IP:       192.168.122.45
  Domain:   goship-myapp

Apps:
  - web (image: nginx:alpine)
  - api (binary: /opt/goship/binaries/api/myapi)
```

### `goshipctl project delete <name>`

Destroys the project's VM and removes the project from the state store.

```bash
goshipctl project delete myapp
```

Alias: `goshipctl project rm myapp`

### `goshipctl project console <name>`

Opens an interactive serial console to the project VM via `virsh console`. Use `Ctrl+]` to exit.

```bash
goshipctl project console myapp
```

### `goshipctl project logs <name> [source]`

Shows logs from the project VM.

```bash
goshipctl project logs <name> [source] [flags]
```

**Log sources** (positional argument):

| Source | Path in VM |
|--------|-----------|
| `goship-init` *(default)* | `/var/log/goship-init.log` |
| `cloud-init` | `/var/log/cloud-init-output.log` |

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `-n, --lines` | `100` | Number of log lines to show |
| `-f, --follow` | `false` | Follow log output (poll every 2s) |
| `--file` | | Arbitrary log file path inside the VM (must be under `/var/log/`) |

**Examples:**

```bash
goshipctl project logs myapp                     # GoShip Init logs (default)
goshipctl project logs myapp cloud-init           # Cloud-init provisioning logs
goshipctl project logs myapp -f                   # Follow GoShip Init logs
goshipctl project logs myapp --file /var/log/messages  # Arbitrary log file
```

### `goshipctl project stop <name>`

Gracefully stops the project VM by sending an ACPI shutdown signal.

```bash
goshipctl project stop myapp
```

### `goshipctl project start <name>`

Starts a previously stopped project VM.

```bash
goshipctl project start myapp
```

### `goshipctl project restart <name>`

Restarts a project VM (stop, wait for shutdown, then start).

```bash
goshipctl project restart myapp
```

### `goshipctl project update-init <name>`

Pushes a new goship-init binary into a running VM over virtio-serial using a chunked transfer protocol (512KB chunks with SHA256 verification).

```bash
goshipctl project update-init <name> [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--binary` | *(uses global `--goship-init` value)* | Path to the goship-init binary |
| `--restart` | `false` | Restart the VM after successful update |

**Example:**

```bash
make build-goship-init
goshipctl project update-init myapp --restart
```

---

## `goshipctl app`

Manage applications inside project VMs.

### `goshipctl app create <project> <appname>`

Creates an application definition in the project. Does not start anything — use `app deploy` to push it to the VM.

```bash
goshipctl app create <project> <appname> [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `-m, --mode` | `container` | Execution mode: `container` or `process` |
| `-i, --image` | | Container image (required for container mode) |
| `-b, --binary` | | Path to binary (required for process mode) |
| `-p, --port` | | Port mapping `host:container` (repeatable) |
| `-e, --env` | | Environment variable `KEY=VALUE` (repeatable) |
| `--cpu` | `0` | CPU limit in cores |
| `--memory` | `0` | Memory limit in MB |
| `-r, --replicas` | `1` | Number of replicas |
| `-d, --description` | | App description |
| `-g, --tag` | | Tags (repeatable) |
| `--restart-policy` | `never` | Restart policy: `never`, `always`, or `on-failure` |
| `--hostname` | *(app name)* | Hostname for reverse proxy routing (default: app name) |
| `--available` | `true` | Whether the app is routable via the reverse proxy |

**Examples:**

```bash
# Container app
goshipctl app create myapp web \
  --image nginx:alpine \
  --port 8080:80 \
  --env "ENV=production"

# Process app with auto-restart
goshipctl app create myapp api \
  --mode process \
  --binary ./bin/myapi \
  --port 3000:3000 \
  --restart-policy always \
  --env "DATABASE_URL=postgres://..."
```

### `goshipctl app deploy <project> <appname>`

Deploys an application to the project VM. For container mode, GoShip Init pulls the image and starts the container. For process mode with a local binary, the binary is automatically uploaded to the VM (with SHA256 verification) before starting.

```bash
goshipctl app deploy myapp web
```

### `goshipctl app list <project>`

Lists all applications in a project with live status from the VM.

```bash
goshipctl app list myapp
```

Alias: `goshipctl app ls myapp`

**Example output:**

```
NAME  MODE       IMAGE/BINARY                        STATUS   PORTS
web   container  nginx:alpine                        running  8080:80
api   process    /opt/goship/binaries/api/myapi      running  3000:3000
```

### `goshipctl app info <project> <appname>`

Shows detailed application information including configuration and live status.

```bash
goshipctl app info myapp web
```

### `goshipctl app stop <project> <appname>`

Stops a running application inside the project VM.

```bash
goshipctl app stop myapp web
```

### `goshipctl app delete <project> <appname>`

Removes the application from the VM (best-effort) and deletes it from the state store.

```bash
goshipctl app delete myapp web
```

Alias: `goshipctl app rm myapp web`

### `goshipctl app logs <project> <appname>`

Shows application logs. For container apps, reads Docker logs. For process apps, reads from `/var/log/goship-<appname>.log` inside the VM.

```bash
goshipctl app logs <project> <appname> [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `-n, --lines` | `100` | Number of log lines to show |
| `-f, --follow` | `false` | Follow log output (poll every 2s) |

**Examples:**

```bash
goshipctl app logs myapp web
goshipctl app logs myapp api -n 50
goshipctl app logs myapp web -f
```

---

### `goshipctl app push-image <project> <image>`

Exports a local Docker image, compresses it with gzip, and transfers it into the project VM over virtio-serial. No registry needed.

```bash
goshipctl app push-image myapp myimage:latest
```

### `goshipctl app edit <project> <appname>`

Modifies an application's configuration without deploying. Changes take effect on next `app deploy`.

```bash
goshipctl app edit <project> <appname> [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `-i, --image` | | Container image |
| `-b, --binary` | | Binary path (process mode) |
| `-p, --port` | | Port mapping (repeatable, replaces all) |
| `-e, --env` | | Set env var `KEY=VALUE` (repeatable) |
| `--env-file` | | Load env vars from a file |
| `-d, --description` | | App description |
| `-g, --tag` | | Tags (repeatable, replaces all) |
| `--restart-policy` | | Restart policy: `never`, `always`, `on-failure` |
| `--cpu` | | CPU limit in cores |
| `--memory` | | Memory limit (e.g., `512M`, `2G`) |

**Example:**

```bash
goshipctl app edit myapp web --port 9090:80 --env "LOG_LEVEL=debug"
goshipctl app deploy myapp web  # apply changes
```

---

## `goshipctl vm`

Low-level VM lifecycle commands. These are experimental commands for learning and debugging — use `project` commands for normal workflows.

### `goshipctl vm create <name>`

Creates a CoW disk overlay, provisions goship-init, generates domain XML, and starts a VM.

```bash
goshipctl vm create <name> [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--base-image` | `~/.goship/images/goship-vm.qcow2` | Base qcow2 image path |
| `--memory` | `512` | Memory in MB |
| `--cpus` | `1` | Number of CPU cores |
| `--enable-kvm` | `true` | Enable KVM acceleration |
| `--network-type` | `network` | Network type (`network`, `bridge`, `user`) |
| `--network-source` | `default` | Network source name |
| `--data-dir` | `~/.goship` | Data directory |
| `--hostname` | *(VM name)* | VM hostname |
| `--ssh-key` | | Path to SSH public key file |
| `--goship-init` | `./bin/goship-init` | Path to goship-init binary |
| `--skip-guest-provision` | `false` | Skip guest provisioning |
| `--install-docker` | `true` | Install Docker during provisioning |

### `goshipctl vm destroy <name>`

Stops and undefines a VM, optionally removing its disk.

```bash
goshipctl vm destroy <name> [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--data-dir` | `~/.goship` | Data directory |
| `--keep-disk` | `false` | Keep disk image after destroying |

### `goshipctl vm list`

Lists all GoShip-managed VMs (domains with the `goship-` prefix) and their state.

```bash
goshipctl vm list
```

### `goshipctl vm ping <name>`

Sends a ping command to the GoShip Init agent inside a VM via virtio-serial.

```bash
goshipctl vm ping myvm
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--data-dir` | `~/.goship` | Data directory |

---

## API Endpoints (goshipd)

When running `goshipd`, the following additional endpoints are available for reverse proxy management:

### Project Domains

**`PUT /api/v1/projects/{id}/domains`** — Update domains assigned to a project.

```bash
curl -X PUT http://localhost:8080/api/v1/projects/myapp/domains \
  -d '{"domains":["myapp.local","myapp.dev"],"default_domain":"myapp.local"}'
```

### Proxy Routes

**`GET /api/v1/proxy/routes`** — List all active proxy routes.

```bash
curl http://localhost:8080/api/v1/proxy/routes
```

**Example response:**

```json
[
  {"domain": "web.myapp.local", "backend": "192.168.122.10:8080"},
  {"domain": "api.myapp.local", "backend": "192.168.122.10:3000"}
]
```

### App Update (Proxy Fields)

**`PATCH /api/v1/projects/{id}/apps/{name}`** — Update app fields including hostname and available.

```bash
curl -X PATCH http://localhost:8080/api/v1/projects/myapp/apps/web \
  -d '{"hostname":"www","available":true}'
```
