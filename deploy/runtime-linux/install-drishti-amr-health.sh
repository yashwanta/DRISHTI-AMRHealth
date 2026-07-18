#!/usr/bin/env bash
set -euo pipefail

HOST_PORT=8099
INSTALL_ROOT=/opt/drishti-amr-health
PUBLIC_URL=''
LLM_URL=https://llm.eidonix.com
LLM_MODEL=deepseek-v4-pro
OPEN_FIREWALL=0
SKIP_LLM_KEY=0

usage() {
  cat <<'EOF'
Usage: sudo ./install-drishti-amr-health.sh [options]
  --port 8099                 Plant LAN listening port
  --public-url URL            Browser URL, e.g. http://10.222.42.10:8099
  --install-root PATH         Default: /opt/drishti-amr-health
  --llm-url URL               Default: https://llm.eidonix.com
  --llm-model MODEL           Default: deepseek-v4-pro
  --open-firewall             Allow the selected TCP port through firewalld/ufw
  --skip-llm-key              Install without an Agent API key
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --port) HOST_PORT="$2"; shift 2 ;;
    --public-url) PUBLIC_URL="$2"; shift 2 ;;
    --install-root) INSTALL_ROOT="$2"; shift 2 ;;
    --llm-url) LLM_URL="$2"; shift 2 ;;
    --llm-model) LLM_MODEL="$2"; shift 2 ;;
    --open-firewall) OPEN_FIREWALL=1; shift ;;
    --skip-llm-key) SKIP_LLM_KEY=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage; exit 2 ;;
  esac
done

[[ $EUID -eq 0 ]] || { echo 'Run this installer with sudo.' >&2; exit 1; }
[[ "$HOST_PORT" =~ ^[0-9]+$ ]] && (( HOST_PORT >= 1 && HOST_PORT <= 65535 )) || {
  echo 'Port must be a number from 1 through 65535.' >&2
  exit 1
}
BUNDLE_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
IMAGE_ARCHIVE="$BUNDLE_ROOT/payload/drishti-runtime-images.tar"
[[ -f "$IMAGE_ARCHIVE" ]] || { echo "Missing $IMAGE_ARCHIVE" >&2; exit 1; }

install_podman() {
  command -v podman >/dev/null 2>&1 && return
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y podman curl
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y podman curl
  elif command -v yum >/dev/null 2>&1; then
    yum install -y podman curl
  elif command -v zypper >/dev/null 2>&1; then
    zypper --non-interactive install podman curl
  else
    echo 'Unsupported package manager. Install Podman and curl, then rerun.' >&2
    exit 1
  fi
}

install_podman
command -v curl >/dev/null 2>&1 || { echo 'curl is required.' >&2; exit 1; }
podman info >/dev/null

if [[ -z "$PUBLIC_URL" ]]; then
  plant_ip=$(hostname -I 2>/dev/null | awk '{print $1}')
  [[ -n "$plant_ip" ]] || plant_ip=$(ip route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") {print $(i+1); exit}}')
  [[ -n "$plant_ip" ]] || { echo 'Could not detect the server LAN IP; pass --public-url.' >&2; exit 1; }
  PUBLIC_URL="http://${plant_ip}:${HOST_PORT}"
fi
ALLOWED_ORIGINS="http://localhost:${HOST_PORT},${PUBLIC_URL}"

mkdir -p "$INSTALL_ROOT/data/config" "$INSTALL_ROOT/data/ssh" \
  "$INSTALL_ROOT/data/rds-snapshots" "$INSTALL_ROOT/data/keys"
touch "$INSTALL_ROOT/data/ssh/known_hosts"
install -m 0755 "$BUNDLE_ROOT/start-drishti-amr-health.sh" "$INSTALL_ROOT/start-drishti-amr-health.sh"

echo 'Loading bundled application and PostgreSQL images...'
podman load -i "$IMAGE_ARCHIVE"

random_hex() { od -An -N"${1:-32}" -tx1 /dev/urandom | tr -d ' \n'; }
umask 077
{
  printf 'HOST_PORT=%q\n' "$HOST_PORT"
  printf 'PUBLIC_URL=%q\n' "$PUBLIC_URL"
  printf 'ALLOWED_ORIGINS=%q\n' "$ALLOWED_ORIGINS"
  printf 'LLM_URL=%q\n' "$LLM_URL"
  printf 'LLM_MODEL=%q\n' "$LLM_MODEL"
  printf 'DATABASE_PASSWORD=%q\n' "$(random_hex 32)"
  printf 'ENCRYPTION_KEY=%q\n' "$(random_hex 32)"
  printf 'SESSION_SECRET=%q\n' "$(random_hex 48)"
} >"$INSTALL_ROOT/runtime.env"
chmod 0600 "$INSTALL_ROOT/runtime.env"

if [[ $SKIP_LLM_KEY -eq 0 ]]; then
  read -r -s -p 'Enter the Agent API key (not stored in the installer): ' llm_key
  echo
  if [[ -n "$llm_key" ]]; then
    podman secret inspect drishti_llm_api_key >/dev/null 2>&1 && podman secret rm drishti_llm_api_key >/dev/null
    printf '%s' "$llm_key" | podman secret create drishti_llm_api_key - >/dev/null
  fi
  unset llm_key
fi

cat >/etc/systemd/system/drishti-amr-health.service <<EOF
[Unit]
Description=DRISHTI AMR Health
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
Environment=DRISHTI_INSTALL_ROOT=$INSTALL_ROOT
ExecStart=$INSTALL_ROOT/start-drishti-amr-health.sh
ExecStop=/usr/bin/podman stop AMR-Health
TimeoutStartSec=180

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable drishti-amr-health.service >/dev/null

if [[ $OPEN_FIREWALL -eq 1 ]]; then
  if command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld; then
    firewall-cmd --permanent --add-port="${HOST_PORT}/tcp"
    firewall-cmd --reload
  elif command -v ufw >/dev/null 2>&1 && ufw status | grep -q '^Status: active'; then
    ufw allow "${HOST_PORT}/tcp"
  else
    echo "No active firewalld/ufw detected; verify TCP ${HOST_PORT} in the site firewall."
  fi
fi

systemctl restart drishti-amr-health.service
echo "Installation complete: $PUBLIC_URL"
echo "Other plant computers can open this URL if routing and TCP ${HOST_PORT} are allowed."
