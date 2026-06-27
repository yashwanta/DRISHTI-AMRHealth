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

  log "Installing Podman"
  if has_cmd apt-get; then
    run_sudo apt-get update
    run_sudo apt-get install -y podman
  elif has_cmd dnf; then
    run_sudo dnf install -y podman
  elif has_cmd yum; then
    run_sudo yum install -y podman
  elif has_cmd zypper; then
    run_sudo zypper --non-interactive install podman
  elif has_cmd apk; then
    run_sudo apk add --no-cache podman
  else
    echo "Podman is not installed and this script does not know this package manager." >&2
    echo "Install Podman manually, then rerun this script." >&2
    exit 1
  fi
  podman --version
}

log "Checking Podman"
install_podman

log "Preparing local data config"
mkdir -p data/config data/rds-snapshots
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
podman run -d \
  --name "$CONTAINER_NAME" \
  -p "${HOST_PORT}:8090" \
  -v "${ROOT_DIR}/data:/app/data:Z" \
  --restart unless-stopped \
  "$IMAGE_NAME"

log "Verifying app"
sleep 3
if has_cmd curl; then
  curl -fsS "http://localhost:${HOST_PORT}/api/health" >/dev/null
else
  python3 - <<PY
import urllib.request
urllib.request.urlopen('http://localhost:${HOST_PORT}/api/health', timeout=15).read()
PY
fi

echo "DRISHTI - AMR Health is running: http://localhost:${HOST_PORT}"
echo "Use Admin > RDS API Connections to add real plant RDS URLs."