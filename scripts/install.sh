#!/usr/bin/env bash
#
# GoShip Installer
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/guilhermebr/goship/main/scripts/install.sh | bash
#
# Environment variables:
#   GOSHIP_VERSION      Release tag to install (default: latest)
#   GOSHIP_INSTALL_DIR  Where to install goshipctl (default: /usr/local/bin)
#   GOSHIP_SKIP_DEPS    Skip system dependency installation (default: false)

set -euo pipefail

# --- Configuration -----------------------------------------------------------

GOSHIP_VERSION="${GOSHIP_VERSION:-latest}"
GOSHIP_INSTALL_DIR="${GOSHIP_INSTALL_DIR:-/usr/local/bin}"
GOSHIP_SKIP_DEPS="${GOSHIP_SKIP_DEPS:-false}"

GITHUB_REPO="guilhermebr/goship"
GOSHIP_HOME="${HOME}/.goship"

# --- Colors & Output ---------------------------------------------------------

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m' # No Color

info()  { echo -e "${BLUE}[*]${NC} $*"; }
ok()    { echo -e "${GREEN}[+]${NC} $*"; }
warn()  { echo -e "${YELLOW}[!]${NC} $*"; }
err()   { echo -e "${RED}[-]${NC} $*" >&2; }
fatal() { err "$@"; exit 1; }

# --- Cleanup ------------------------------------------------------------------

TMPDIR=""
cleanup() {
    if [ -n "$TMPDIR" ] && [ -d "$TMPDIR" ]; then
        rm -rf "$TMPDIR"
    fi
}
trap cleanup EXIT

# --- Helper Functions ---------------------------------------------------------

need_cmd() {
    if ! command -v "$1" &>/dev/null; then
        fatal "Required command not found: $1"
    fi
}

# Download a URL to a file. Tries curl first, falls back to wget.
download() {
    local url="$1"
    local output="$2"

    if command -v curl &>/dev/null; then
        curl -fsSL -o "$output" "$url"
    elif command -v wget &>/dev/null; then
        wget -q -O "$output" "$url"
    else
        fatal "Neither curl nor wget found. Please install one of them."
    fi
}

# Fetch a URL and print to stdout.
fetch() {
    local url="$1"

    if command -v curl &>/dev/null; then
        curl -fsSL "$url"
    elif command -v wget &>/dev/null; then
        wget -q -O- "$url"
    else
        fatal "Neither curl nor wget found. Please install one of them."
    fi
}

# Run a command with sudo if not root.
maybe_sudo() {
    if [ "$(id -u)" -eq 0 ]; then
        "$@"
    else
        sudo "$@"
    fi
}

# Detect the system package manager.
detect_package_manager() {
    if command -v apt-get &>/dev/null; then
        echo "apt"
    elif command -v dnf &>/dev/null; then
        echo "dnf"
    elif command -v pacman &>/dev/null; then
        echo "pacman"
    else
        echo "unknown"
    fi
}

# --- Pre-flight Checks -------------------------------------------------------

preflight() {
    info "Running pre-flight checks..."

    # Require Linux
    if [ "$(uname -s)" != "Linux" ]; then
        fatal "GoShip only supports Linux. Detected: $(uname -s)"
    fi

    # Require amd64
    local arch
    arch="$(uname -m)"
    if [ "$arch" != "x86_64" ]; then
        fatal "GoShip only supports x86_64 (amd64). Detected: $arch"
    fi

    # Require curl or wget
    if ! command -v curl &>/dev/null && ! command -v wget &>/dev/null; then
        fatal "Either curl or wget is required."
    fi

    # Require sha256sum
    need_cmd sha256sum
    need_cmd tar

    ok "Pre-flight checks passed (Linux x86_64)"
}

# --- Resolve Version ----------------------------------------------------------

resolve_version() {
    if [ "$GOSHIP_VERSION" = "latest" ]; then
        info "Resolving latest release version..."
        local api_url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
        local response
        response="$(fetch "$api_url")" || fatal "Failed to fetch latest release from GitHub API"

        # Extract tag_name without jq: find "tag_name": "vX.Y.Z"
        GOSHIP_VERSION="$(echo "$response" | grep -o '"tag_name":[[:space:]]*"[^"]*"' | head -1 | grep -o '"v[^"]*"' | tr -d '"')"

        if [ -z "$GOSHIP_VERSION" ]; then
            fatal "Could not determine latest version from GitHub API"
        fi
    fi

    # Version string for filenames (strip leading 'v')
    VERSION_NUM="${GOSHIP_VERSION#v}"

    ok "Version: ${GOSHIP_VERSION} (${VERSION_NUM})"
}

# --- Install System Dependencies ---------------------------------------------

install_deps() {
    if [ "$GOSHIP_SKIP_DEPS" = "true" ]; then
        info "Skipping system dependency installation (GOSHIP_SKIP_DEPS=true)"
        return
    fi

    local pkg_mgr
    pkg_mgr="$(detect_package_manager)"

    info "Installing system dependencies via ${pkg_mgr}..."

    case "$pkg_mgr" in
        apt)
            maybe_sudo apt-get update -qq
            maybe_sudo apt-get install -y -qq \
                qemu-kvm \
                libvirt-daemon-system \
                libvirt-clients \
                virtinst \
                libvirt-dev \
                qemu-system-x86 \
                qemu-utils \
                libguestfs-tools \
                genisoimage \
                bridge-utils \
                cloud-image-utils
            ;;
        dnf)
            maybe_sudo dnf install -y \
                qemu-kvm \
                libvirt \
                libvirt-devel \
                virt-install \
                libguestfs-tools \
                genisoimage
            ;;
        pacman)
            maybe_sudo pacman -Sy --noconfirm \
                qemu-full \
                libvirt \
                virt-install \
                libguestfs
            ;;
        *)
            warn "Unknown package manager. Please install these manually:"
            warn "  - qemu-kvm / qemu-system-x86"
            warn "  - libvirt + libvirt-dev"
            warn "  - libguestfs-tools"
            warn "  - genisoimage"
            warn "  - virt-install"
            return
            ;;
    esac

    # Enable and start libvirtd
    info "Enabling libvirtd service..."
    maybe_sudo systemctl enable --now libvirtd 2>/dev/null || warn "Could not enable libvirtd (systemctl not available?)"

    # Add user to libvirt group
    if [ "$(id -u)" -ne 0 ]; then
        local current_user
        current_user="$(whoami)"
        if ! id -nG "$current_user" | grep -qw libvirt; then
            info "Adding ${current_user} to libvirt group..."
            maybe_sudo usermod -aG libvirt "$current_user"
            warn "You may need to log out and back in for group changes to take effect."
        fi
    fi

    ok "System dependencies installed"
}

# --- Download & Install Binaries ----------------------------------------------

install_binaries() {
    info "Downloading GoShip ${GOSHIP_VERSION} binaries..."

    TMPDIR="$(mktemp -d)"
    local base_url="https://github.com/${GITHUB_REPO}/releases/download/${GOSHIP_VERSION}"

    local goshipctl_archive="goshipctl_${VERSION_NUM}_linux_amd64.tar.gz"
    local goshipinit_archive="goship-init_${VERSION_NUM}_linux_amd64.tar.gz"
    local checksums_file="checksums.txt"

    # Download archives and checksums
    download "${base_url}/${goshipctl_archive}" "${TMPDIR}/${goshipctl_archive}"
    download "${base_url}/${goshipinit_archive}" "${TMPDIR}/${goshipinit_archive}"
    download "${base_url}/${checksums_file}" "${TMPDIR}/${checksums_file}"

    ok "Downloaded archives"

    # Verify checksums
    info "Verifying SHA256 checksums..."
    (
        cd "$TMPDIR"
        # Filter checksums.txt to only the files we downloaded
        grep -E "(${goshipctl_archive}|${goshipinit_archive})" "${checksums_file}" > verify.txt
        sha256sum -c verify.txt
    ) || fatal "Checksum verification failed!"

    ok "Checksums verified"

    # Extract and install goshipctl
    info "Installing goshipctl to ${GOSHIP_INSTALL_DIR}/..."
    tar xzf "${TMPDIR}/${goshipctl_archive}" -C "$TMPDIR"
    maybe_sudo install -d "${GOSHIP_INSTALL_DIR}"
    maybe_sudo install -m 755 "${TMPDIR}/goshipctl" "${GOSHIP_INSTALL_DIR}/goshipctl"

    ok "goshipctl installed to ${GOSHIP_INSTALL_DIR}/goshipctl"

    # Extract and install goship-init
    local init_dir="${GOSHIP_HOME}/bin"
    info "Installing goship-init to ${init_dir}/..."
    tar xzf "${TMPDIR}/${goshipinit_archive}" -C "$TMPDIR"
    install -d "${init_dir}"
    install -m 755 "${TMPDIR}/goship-init" "${init_dir}/goship-init"

    ok "goship-init installed to ${init_dir}/goship-init"
}

# --- Post-install Setup -------------------------------------------------------

post_install() {
    info "Setting up GoShip directories..."

    mkdir -p "${GOSHIP_HOME}/images"
    mkdir -p "${GOSHIP_HOME}/instances"

    ok "Created ${GOSHIP_HOME}/{images,instances}"

    # Check KVM support
    if [ -e /dev/kvm ]; then
        ok "/dev/kvm exists - KVM acceleration available"
    else
        warn "/dev/kvm not found - KVM acceleration may not be available"
        warn "  Check that your CPU supports virtualization and it is enabled in BIOS"
    fi
}

# --- Summary ------------------------------------------------------------------

print_summary() {
    echo ""
    echo -e "${GREEN}${BOLD}GoShip ${GOSHIP_VERSION} installed successfully!${NC}"
    echo ""
    echo "  Binaries:"
    echo "    goshipctl   → ${GOSHIP_INSTALL_DIR}/goshipctl"
    echo "    goship-init → ${GOSHIP_HOME}/bin/goship-init"
    echo ""
    echo "  Data directories:"
    echo "    ${GOSHIP_HOME}/images/"
    echo "    ${GOSHIP_HOME}/instances/"
    echo ""
    echo -e "${BOLD}Next steps:${NC}"
    echo "  1. Download the base VM image:"
    echo "       goshipctl image pull"
    echo ""
    echo "  2. Create your first project:"
    echo "       goshipctl project create myapp --cpu 1 --memory 512"
    echo ""
    echo "  3. Deploy an app:"
    echo "       goshipctl app create myapp web --image nginx:alpine --port 8080:80"
    echo "       goshipctl app deploy myapp web"
    echo ""

    # Remind about group change if needed
    if [ "$(id -u)" -ne 0 ] && ! id -nG "$(whoami)" | grep -qw libvirt 2>/dev/null; then
        echo -e "${YELLOW}Note: Log out and back in for libvirt group membership to take effect.${NC}"
        echo ""
    fi
}

# --- Main ---------------------------------------------------------------------

main() {
    echo ""
    echo -e "${BOLD}GoShip Installer${NC}"
    echo ""

    preflight
    resolve_version
    install_deps
    install_binaries
    post_install
    print_summary
}

main
