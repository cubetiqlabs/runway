#!/bin/sh
set -eu

# Installs Google gVisor (runsc + containerd-shim-runsc-v1 + gvisor-bin helpers)
# on Linux hosts and registers the "runsc" OCI runtime with Docker Engine
# using an atomic SIGHUP/reload so running containers are never disrupted.

INSTALL_DOCKER="false"

log()  { printf '[gvisor] %s\n' "$*"; }
warn() { printf '[gvisor] WARN: %s\n' "$*" >&2; }
err()  { printf '[gvisor] ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Runway gVisor Installer (Linux)

Usage:
  curl -fsSL https://raw.githubusercontent.com/cubetiqlabs/runway/main/scripts/install-gvisor.sh | sudo sh
  curl -fsSL https://raw.githubusercontent.com/cubetiqlabs/runway/main/scripts/install-gvisor.sh | sudo sh -s -- --install-docker

Options:
  --install-docker   Install Docker Engine if not already present (never restarts an already-running Docker daemon)
  -h, --help         Show help
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --install-docker) INSTALL_DOCKER="true"; shift ;;
    -h|--help)        usage; exit 0 ;;
    *)                err "Unknown option: $1" ;;
  esac
done

[ "$(uname -s)" = "Linux" ] || err "gVisor (runsc) requires a Linux host."
[ "$(id -u)" -eq 0 ] || err "Run as root (sudo)."

pkg_install() {
  if command -v apt-get >/dev/null 2>&1; then
    DEBIAN_FRONTEND=noninteractive apt-get update -qq </dev/null
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends "$@" </dev/null
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y -q "$@" </dev/null
  elif command -v yum >/dev/null 2>&1; then
    yum install -y -q "$@" </dev/null
  elif command -v apk >/dev/null 2>&1; then
    apk add --no-cache "$@" </dev/null
  else
    err "Unsupported package manager; install manually: $*"
  fi
}

if ! command -v curl >/dev/null 2>&1 || ! command -v tar >/dev/null 2>&1 || ! command -v zstd >/dev/null 2>&1; then
  log "Installing required tools (ca-certificates, curl, tar, zstd)..."
  pkg_install ca-certificates curl tar zstd
fi

TMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT INT TERM

if ! command -v docker >/dev/null 2>&1; then
  if [ "${INSTALL_DOCKER}" = "true" ]; then
    log "Docker Engine not found; installing via https://get.docker.com..."
    curl -fsSL https://get.docker.com -o "${TMP_DIR}/get-docker.sh"
    sh "${TMP_DIR}/get-docker.sh" </dev/null
    if command -v systemctl >/dev/null 2>&1; then
      systemctl enable --now docker </dev/null >/dev/null 2>&1 || true
    fi
  else
    err "Docker Engine must be installed first (or pass --install-docker)."
  fi
fi

command -v docker >/dev/null 2>&1 || err "Docker Engine installation incomplete."

ARCH_RAW="$(uname -m)"
case "${ARCH_RAW}" in
  x86_64|amd64)  ARCH="x86_64" ;;
  arm64|aarch64) ARCH="aarch64" ;;
  *)             err "Unsupported architecture for gVisor: ${ARCH_RAW}" ;;
esac

if command -v sha512sum >/dev/null 2>&1; then
  SHA512_CMD="sha512sum -c"
elif command -v shasum >/dev/null 2>&1; then
  SHA512_CMD="shasum -a 512 -c"
else
  err "sha512sum or shasum is required to verify gVisor downloads."
fi

URL="https://storage.googleapis.com/gvisor/releases/release/latest/${ARCH}"

log "Downloading gVisor archive (${ARCH})..."
(
  cd "${TMP_DIR}"
  curl -fsSL --retry 3 --retry-delay 2 "${URL}/gvisor.tar.zstd" -o gvisor.tar.zstd
  curl -fsSL --retry 3 --retry-delay 2 "${URL}/gvisor.tar.zstd.sha512" -o gvisor.tar.zstd.sha512
  ${SHA512_CMD} gvisor.tar.zstd.sha512
)

mkdir -p "${TMP_DIR}/extract"
log "Extracting gVisor binaries..."
zstd -d -q -f "${TMP_DIR}/gvisor.tar.zstd" -o "${TMP_DIR}/gvisor.tar"
tar -xf "${TMP_DIR}/gvisor.tar" -C "${TMP_DIR}/extract"

[ -f "${TMP_DIR}/extract/runsc" ] || err "Extracted archive missing runsc binary."
[ -f "${TMP_DIR}/extract/containerd-shim-runsc-v1" ] || err "Extracted archive missing containerd-shim-runsc-v1."

install -m 0755 "${TMP_DIR}/extract/runsc" /usr/local/bin/runsc
install -m 0755 "${TMP_DIR}/extract/containerd-shim-runsc-v1" /usr/local/bin/containerd-shim-runsc-v1
if [ -d "${TMP_DIR}/extract/gvisor-bin" ]; then
  install -d -m 0755 /usr/local/bin/gvisor-bin
  for bin in "${TMP_DIR}/extract"/gvisor-bin/*; do
    [ -f "${bin}" ] || continue
    install -m 0755 "${bin}" "/usr/local/bin/gvisor-bin/$(basename "${bin}")"
  done
fi

/usr/local/bin/runsc --version

DAEMON_JSON="/etc/docker/daemon.json"
DAEMON_BAK=""
if [ -f "${DAEMON_JSON}" ]; then
  DAEMON_BAK="${DAEMON_JSON}.bak.$(date -u '+%Y%m%dT%H%M%SZ')"
  cp -a "${DAEMON_JSON}" "${DAEMON_BAK}"
  log "Backed up existing ${DAEMON_JSON} to ${DAEMON_BAK}"
fi

validate_daemon_json() {
  _file="$1"
  [ -f "${_file}" ] || return 1
  if command -v jq >/dev/null 2>&1; then
    jq empty "${_file}" >/dev/null 2>&1
  elif command -v python3 >/dev/null 2>&1; then
    python3 -m json.tool "${_file}" >/dev/null 2>&1
  else
    return 0
  fi
}

log "Registering runsc runtime with Docker Engine (default runtime stays unchanged)..."
if ! /usr/local/bin/runsc install; then
  if [ -n "${DAEMON_BAK}" ] && [ -f "${DAEMON_BAK}" ]; then
    cp -a "${DAEMON_BAK}" "${DAEMON_JSON}"
    warn "Restored ${DAEMON_JSON} from backup after runsc install failure."
  fi
  err "Failed to register runsc runtime with Docker."
fi

if ! validate_daemon_json "${DAEMON_JSON}"; then
  if [ -n "${DAEMON_BAK}" ] && [ -f "${DAEMON_BAK}" ]; then
    cp -a "${DAEMON_BAK}" "${DAEMON_JSON}"
    warn "Restored ${DAEMON_JSON} from backup because generated JSON was invalid."
  fi
  err "Invalid JSON in ${DAEMON_JSON}; aborted without reloading Docker."
fi

log "Reloading Docker daemon configuration without restarting running containers..."
reloaded="false"
if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet docker; then
  if systemctl reload docker >/dev/null 2>&1; then
    reloaded="true"
  fi
fi

if [ "${reloaded}" != "true" ]; then
  dockerd_pid=""
  if [ -f /var/run/docker.pid ]; then
    dockerd_pid="$(tr -d '[:space:]' </var/run/docker.pid || true)"
  elif command -v pidof >/dev/null 2>&1; then
    dockerd_pid="$(pidof dockerd | awk '{print $1}' || true)"
  fi
  if [ -n "${dockerd_pid}" ]; then
    kill -SIGHUP "${dockerd_pid}"
    reloaded="true"
  fi
fi

verified="false"
for _ in 1 2 3 4 5; do
  if docker info --format '{{json .Runtimes}}' 2>/dev/null | grep -q '"runsc"'; then
    verified="true"
    break
  fi
  sleep 1
done

if [ "${verified}" = "true" ]; then
  log "gVisor (runsc) installed and active in Docker Engine with zero container downtime."
else
  warn "runsc is configured in ${DAEMON_JSON}, but docker info does not show it yet."
  warn "Running containers were left untouched. To activate manually during a maintenance window, run: sudo systemctl restart docker"
fi
