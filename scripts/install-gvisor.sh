#!/usr/bin/env bash
set -euo pipefail

# Installs Google gVisor (runsc + containerd-shim-runsc-v1) on Linux hosts and
# registers the "runsc" OCI runtime with Docker Engine.

log()  { printf '[gvisor] %s\n' "$*"; }
err()  { printf '[gvisor] ERROR: %s\n' "$*" >&2; exit 1; }

[[ "$(uname -s)" == "Linux" ]] || err "gVisor (runsc) requires a Linux host."
[[ "$(id -u)" -eq 0 ]] || err "Run as root (sudo)."
command -v curl >/dev/null 2>&1 || err "curl is required."
command -v docker >/dev/null 2>&1 || err "Docker Engine must be installed first."

case "$(uname -m)" in
  x86_64|amd64)  ARCH="x86_64" ;;
  arm64|aarch64) ARCH="aarch64" ;;
  *)             err "Unsupported architecture for gVisor: $(uname -m)" ;;
esac

if command -v sha512sum >/dev/null 2>&1; then
  SHA512_CMD="sha512sum -c"
elif command -v shasum >/dev/null 2>&1; then
  SHA512_CMD="shasum -a 512 -c"
else
  err "sha512sum or shasum is required to verify gVisor downloads."
fi

URL="https://storage.googleapis.com/gvisor/releases/release/latest/${ARCH}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

log "Downloading gVisor runsc binaries for ${ARCH}..."
(
  cd "${TMP_DIR}"
  curl -fsSL "${URL}/runsc" -o runsc
  curl -fsSL "${URL}/runsc.sha512" -o runsc.sha512
  curl -fsSL "${URL}/containerd-shim-runsc-v1" -o containerd-shim-runsc-v1
  curl -fsSL "${URL}/containerd-shim-runsc-v1.sha512" -o containerd-shim-runsc-v1.sha512
  ${SHA512_CMD} runsc.sha512 containerd-shim-runsc-v1.sha512
)

install -m 0755 "${TMP_DIR}/runsc" /usr/local/bin/runsc
install -m 0755 "${TMP_DIR}/containerd-shim-runsc-v1" /usr/local/bin/containerd-shim-runsc-v1

log "Registering runsc runtime with Docker Engine..."
/usr/local/bin/runsc install

if command -v systemctl >/dev/null 2>&1; then
  systemctl reload docker >/dev/null 2>&1 || systemctl restart docker
fi

if docker info --format '{{json .Runtimes}}' 2>/dev/null | grep -q '"runsc"'; then
  log "gVisor (runsc) installed and verified in Docker Engine."
else
  log "gVisor installed; verify 'docker info' shows the runsc runtime."
fi
