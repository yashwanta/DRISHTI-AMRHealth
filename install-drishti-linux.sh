#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALLER="${SCRIPT_DIR}/scripts/install-linux.sh"

if [ ! -f "$INSTALLER" ]; then
  echo "Missing installer script: $INSTALLER" >&2
  exit 1
fi

exec bash "$INSTALLER" "$@"