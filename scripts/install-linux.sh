#!/usr/bin/env bash
set -euo pipefail

HOST_PORT="${HOST_PORT:-8088}"
IMAGE_NAME="${IMAGE_NAME:-drishti-amr-health}"
CONTAINER_NAME="${CONTAINER_NAME:-AMR-Health}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

log() { printf '\n==> %s\n' "$1"; }
has_cmd() { command -v "$1" >/dev/null 2>&1; }
run_sudo() {
  if [ "$(id -u)" -eq 0 ]; then "$@"; else sudo "$@"; fi
}

install_podman() {
  if has_cmd podman; then
    podman --version
    return
  fi

  log "Installing Podman and runtime dependencies"
  if has_cmd apt-get; then
    run_sudo apt-get update
    run_sudo apt-get install -y podman curl ca-certificates fuse-overlayfs slirp4netns uidmap
  elif has_cmd dnf; then
    run_sudo dnf install -y podman curl ca-certificates fuse-overlayfs slirp4netns shadow-utils
  elif has_cmd yum; then
    run_sudo yum install -y podman curl ca-certificates fuse-overlayfs slirp4netns shadow-utils
  elif has_cmd zypper; then
    run_sudo zypper --non-interactive install podman curl ca-certificates fuse-overlayfs slirp4netns shadow
  elif has_cmd apk; then
    run_sudo apk add --no-cache podman curl ca-certificates fuse-overlayfs slirp4netns shadow
  else
    echo "Podman is not installed and this script does not know this package manager." >&2
    echo "Install Podman manually, then rerun this script." >&2
    exit 1
  fi
  podman --version
}

volume_suffix() {
  if has_cmd getenforce; then
    local mode
    mode="$(getenforce 2>/dev/null || true)"
    if [ "$mode" = "Enforcing" ] || [ "$mode" = "Permissive" ]; then
      printf ':Z'
    fi
  fi
}

log "Checking Podman"
install_podman

log "Checking container runtime"
podman info >/dev/null

log "Preparing local data config"
mkdir -p data/config data/rds-snapshots data/keys
if [ ! -f data/config/api-connections.json ]; then
  cp data/config/api-connections.example.json data/config/api-connections.json
  echo "Created data/config/api-connections.json from example. Add real plant URLs in the app Admin page."
else
  echo "Keeping existing local data/config/api-connections.json."
fi

log "Building container image"
podman build -t "$IMAGE_NAME" .

log "Replacing running container"
podman rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
VOLUME_SPEC="${ROOT_DIR}/data:/app/data$(volume_suffix)"
podman run -d \
  --name "$CONTAINER_NAME" \
  -p "${HOST_PORT}:8090" \
  -v "$VOLUME_SPEC" \
  --restart unless-stopped \
  "$IMAGE_NAME"

log "Verifying app"
sleep 3
if has_cmd curl; then
  curl -fsS "http://localhost:${HOST_PORT}/api/health" >/dev/null
elif has_cmd python3; then
  python3 - <<PY
import urllib.request
urllib.request.urlopen('http://localhost:${HOST_PORT}/api/health', timeout=15).read()
PY
else
  echo "Install succeeded, but curl/python3 is unavailable for health verification."
fi

echo "DRISHTI - AMR Health is running: http://localhost:${HOST_PORT}"
echo "Use Admin > RDS API Connections to add real plant RDS URLs."