# Embedded Container Registry

GoShip includes an embedded OCI-compliant container registry that runs alongside `goship server`. It eliminates the need for external registries (Docker Hub, GHCR, etc.) when deploying locally-built container images to VMs.

## Why?

Without the registry, GoShip transfers images via `docker save | gzip` through virtio-serial or multipart HTTP upload. This sends the **entire image** every time with no layer deduplication and is slow for large images.

The embedded registry enables standard `docker push`/`docker pull` with layer-level caching. Subsequent pushes of the same image only transfer changed layers.

## Architecture

```
Host                                          VM (192.168.122.x)
+-------------------------------------+      +----------------------+
|  goship server                      |      |  Docker daemon       |
|  |-- API server      :8080          |      |  |-- insecure-registries:
|  |-- Reverse proxy   :8081          |      |  |   192.168.122.1:5000
|  +-- Registry        :5000  <-------+------+--+                    |
|                                     |      |  +-- docker pull      |
|  CLI (docker push)                  |      |      192.168.122.1:5000/
|  +-- 192.168.122.1:5000/goship-...  |      |      goship-proj/web:latest
+-------------------------------------+      +----------------------+
```

**Key points:**
- Registry listens on `:5000` (configurable via `GOSHIP_REGISTRY_ADDR`)
- Storage is on disk at `~/.goship/registry/`
- Host pushes via `localhost:5000` (Docker trusts localhost without TLS)
- VMs pull via the host bridge IP (`192.168.122.1:5000`)
- VM Docker is configured with `insecure-registries` via cloud-init automatically
- No host Docker configuration needed — GoShip handles the localhost/bridge-IP translation

## Configuration

The registry address is configured via environment variable or the config system:

```bash
# Default: :5000
export GOSHIP_REGISTRY_ADDR=:5000
```

No additional configuration is needed. When `goship server` starts, the registry starts automatically:

```
goship: registry listening on :5000
```

## Image Naming

When you use `goship compose up --build` or `goship deploy` with a compose file that has `build:` directives, GoShip generates registry-qualified image names:

```
<host-bridge-ip>:<port>/goship-<project>/<service>:latest
```

For example, project `myapp` with service `web`:
```
192.168.122.1:5000/goship-myapp/web:latest
```

This reference works for both pushing from the host and pulling from inside the VM.

## Usage

### Automatic (via compose/deploy)

The simplest way is through `goship compose up` or `goship deploy`. The registry integration is transparent:

```bash
# Start the server (includes registry on :5000)
goship server

# docker-compose.yml with build directives
cat > docker-compose.yml <<EOF
services:
  web:
    build: .
    ports:
      - "8080:80"
  api:
    build: ./backend
    ports:
      - "3000:3000"
EOF

# Build, push to registry, and deploy to VM
goship compose up myproject --build
```

GoShip will:
1. Build the image: `docker build -t 192.168.122.1:5000/goship-myproject/web:latest .`
2. Re-tag to `localhost:5000/...` and push via localhost (no TLS config needed)
3. Deploy to VM — VM pulls from `192.168.122.1:5000/...` (configured via cloud-init)

### Manual push

You can also push images manually. Push via `localhost` (no TLS needed), but reference with the bridge IP for VM compatibility:

```bash
# Tag for localhost push
docker tag myapp:latest localhost:5000/goship-myproject/myapp:latest

# Push via localhost (Docker trusts localhost without TLS)
docker push localhost:5000/goship-myproject/myapp:latest

# Deploy the app referencing the registry via bridge IP (for VM pull)
goship app create myproject myapp -i 192.168.122.1:5000/goship-myproject/myapp:latest -p 8080:80
goship app deploy myproject myapp
```

### Inspecting the registry

```bash
# Check the registry is running
curl http://localhost:5000/v2/
# {}

# List all repositories
curl http://localhost:5000/v2/_catalog
# {"repositories":["goship-myproject/web"]}

# List tags for a repository
curl http://localhost:5000/v2/goship-myproject/web/tags/list
# {"name":"goship-myproject/web","tags":["latest"]}
```

## TLS and Insecure Registries

**Host side:** No configuration needed. GoShip pushes images via `localhost:5000`, which Docker trusts without TLS automatically.

**VM side:** Configured automatically. Cloud-init writes `/etc/docker/daemon.json` with `insecure-registries: ["192.168.122.1:5000"]` when creating VMs.

## Cleanup

When you delete a project, GoShip automatically removes its registry storage:

```bash
goship project delete myproject
# Registry storage for goship-myproject/* is cleaned up
```

## Storage

Registry data is stored at `~/.goship/registry/`. Each project gets a namespace directory:

```
~/.goship/registry/
  goship-myproject/
    web/
      ...  (OCI blobs and manifests)
```

## Future: TLS

Phase 0 uses HTTP only. Future multi-node deployments will use TLS with a self-signed CA.

## Troubleshooting

### "connection refused" when pushing

Make sure `goship server` is running. The registry starts with the server.

### VM can't pull images

Verify the VM can reach the host bridge IP:
```bash
# SSH into the VM
goship project console myproject

# Test connectivity
ping 192.168.122.1
curl http://192.168.122.1:5000/v2/
```

Check that Docker inside the VM has insecure-registries configured:
```bash
docker info | grep -A5 "Insecure Registries"
```

If missing, the VM was likely created before the registry feature was added. Recreate the project to get the updated cloud-init config.

### "server gave HTTP response to HTTPS client"

Docker is trying HTTPS. When pushing manually, use `localhost:5000` (not the bridge IP) — Docker trusts localhost without TLS. For VMs, ensure the VM was created after the registry feature was added so cloud-init configures `insecure-registries`.
