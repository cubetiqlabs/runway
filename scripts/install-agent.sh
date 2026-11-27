#!/usr/bin/env bash
set -euo pipefail

REPO="${RUNWAY_AGENT_UPDATE_REPO:-cubetiqlabs/runway}"
VERSION="${RUNWAY_AGENT_VERSION:-latest}"
SERVER="${RUNWAY_AGENT_SERVER:-}"
TOKEN="${RUNWAY_AGENT_TOKEN:-}"
RUNTIME="${RUNWAY_AGENT_RUNTIME:-auto}"
SANDBOX="${RUNWAY_AGENT_SANDBOX:-auto}"
DOCKER_SOCKET="${RUNWAY_AGENT_DOCKER_SOCKET:-/var/run/docker.sock}"
ISOLATED_DIND="${RUNWAY_AGENT_ISOLATED_DIND:-true}"
INSTALL_GVISOR="false"
CONCURRENCY="${RUNWAY_AGENT_CONCURRENCY:-2}"
LABELS="${RUNWAY_AGENT_LABELS:-}"
AUTO_UPDATE="${RUNWAY_AGENT_AUTO_UPDATE:-true}"
UNINSTALL="false"

log()  { printf '[runway-agent] %s\n' "$*"; }
err()  { printf '[runway-agent] ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Runway Agent Installer (Linux & macOS)

Usage:
  curl -fsSL https://raw.githubusercontent.com/cubetiqlabs/runway/main/scripts/install-agent.sh | \
    sudo sh -s -- --server https://runway.example.com --token rwa_...

Options:
  --server <url>        Runway control plane URL
  --token <rwa_...>     Agent registration token
  --runtime <mode>      auto | native | docker (default: auto)
  --sandbox <mode>      auto | gvisor | runsc | runc (default: auto)
  --no-isolated-dind    Disable per-job isolated DinD sidecars
  --install-gvisor      Install Google gVisor (runsc) on Linux before starting
  --docker-socket <p>   Docker socket or tcp:// endpoint (default: /var/run/docker.sock)
  --concurrency <n>     Max concurrent jobs (default: 2)
  --labels <a,b>        Comma-separated host labels
  --version <tag>       Release tag or 'latest' (default: latest)
  --repo <owner/repo>   GitHub repo for releases (default: cubetiqlabs/runway)
  --no-auto-update      Disable automatic self-updates
  --uninstall           Stop and remove runway-agent service and binary
  -h, --help            Show help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --server)           SERVER="${2:-}"; shift 2 ;;
    --token)            TOKEN="${2:-}"; shift 2 ;;
    --runtime)          RUNTIME="${2:-}"; shift 2 ;;
    --sandbox)          SANDBOX="${2:-}"; shift 2 ;;
    --no-isolated-dind) ISOLATED_DIND="false"; shift ;;
    --install-gvisor)   INSTALL_GVISOR="true"; shift ;;
    --docker-socket)    DOCKER_SOCKET="${2:-}"; shift 2 ;;
    --concurrency)    CONCURRENCY="${2:-}"; shift 2 ;;
    --labels)         LABELS="${2:-}"; shift 2 ;;
    --version)        VERSION="${2:-}"; shift 2 ;;
    --repo)           REPO="${2:-}"; shift 2 ;;
    --no-auto-update) AUTO_UPDATE="false"; shift ;;
    --uninstall)      UNINSTALL="true"; shift ;;
    -h|--help)        usage; exit 0 ;;
    *)                err "Unknown option: $1" ;;
  esac
done

OS_RAW="$(uname -s)"
ARCH_RAW="$(uname -m)"

case "${OS_RAW}" in
  Linux)  GOOS="linux" ;;
  Darwin) GOOS="darwin" ;;
  *)      err "Unsupported OS: ${OS_RAW}" ;;
esac

case "${ARCH_RAW}" in
  x86_64|amd64)  GOARCH="amd64" ;;
  arm64|aarch64) GOARCH="arm64" ;;
  *)             err "Unsupported CPU architecture: ${ARCH_RAW}" ;;
esac

if [[ "$(id -u)" -eq 0 ]]; then
  BIN_DIR="/usr/local/bin"
  CONF_DIR="/etc/runway-agent"
  WORK_DIR="/var/lib/runway-agent"
  LOG_DIR="${RUNWAY_AGENT_LOG_DIR:-/var/log/runway-agent}"
  LAUNCHD_PLIST="/Library/LaunchDaemons/tech.cubis.runway-agent.plist"
else
  BIN_DIR="${HOME}/.local/bin"
  CONF_DIR="${HOME}/.config/runway-agent"
  WORK_DIR="${HOME}/.local/share/runway-agent"
  LOG_DIR="${RUNWAY_AGENT_LOG_DIR:-${WORK_DIR}/logs}"
  LAUNCHD_PLIST="${HOME}/Library/LaunchAgents/tech.cubis.runway-agent.plist"
fi

BIN_PATH="${BIN_DIR}/runway-agent"
ENV_FILE="${CONF_DIR}/agent.env"

uninstall_agent() {
  if [[ "${GOOS}" == "linux" ]] && command -v systemctl >/dev/null 2>&1; then
    systemctl disable --now runway-agent >/dev/null 2>&1 || true
    rm -f /etc/systemd/system/runway-agent.service
    systemctl daemon-reload >/dev/null 2>&1 || true
  elif [[ "${GOOS}" == "darwin" ]]; then
    launchctl unload -w "${LAUNCHD_PLIST}" >/dev/null 2>&1 || true
    rm -f "${LAUNCHD_PLIST}"
  fi
  rm -f "${BIN_PATH}" "${ENV_FILE}"
  log "Uninstalled runway-agent cleanly."
}

if [[ "${UNINSTALL}" == "true" ]]; then
  uninstall_agent
  exit 0
fi

[[ -n "${SERVER}" ]] || err "--server is required."
[[ -n "${TOKEN}" ]] || err "--token is required."

command -v curl >/dev/null 2>&1 || err "curl is required."
command -v tar >/dev/null 2>&1 || err "tar is required."

if [[ "${VERSION}" == "latest" ]]; then
  RELEASE_API="https://api.github.com/repos/${REPO}/releases/latest"
else
  [[ "${VERSION}" == v* ]] || VERSION="v${VERSION}"
  RELEASE_API="https://api.github.com/repos/${REPO}/releases/tags/${VERSION}"
fi

log "Querying release metadata from ${REPO} (${VERSION})..."
RELEASE_JSON="$(curl -fsSL -H "Accept: application/vnd.github+json" "${RELEASE_API}")"
TAG="$(printf '%s\n' "${RELEASE_JSON}" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
[[ -n "${TAG}" ]] || err "Failed to determine release tag from ${RELEASE_API}."
VER_NOV="${TAG#v}"

ASSET_NAME="runway-agent_${VER_NOV}_${GOOS}_${GOARCH}.tar.gz"
ASSET_URL="https://github.com/${REPO}/releases/download/${TAG}/${ASSET_NAME}"
CHECKSUMS_URL="https://github.com/${REPO}/releases/download/${TAG}/checksums.txt"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

log "Downloading ${ASSET_NAME}..."
curl -fsSL "${ASSET_URL}" -o "${TMP_DIR}/${ASSET_NAME}"
curl -fsSL "${CHECKSUMS_URL}" -o "${TMP_DIR}/checksums.txt"

EXPECTED_HASH="$(awk -v file="${ASSET_NAME}" '{gsub(/^\*/, "", $2); if ($2 == file) {print tolower($1); exit}}' "${TMP_DIR}/checksums.txt")"
[[ -n "${EXPECTED_HASH}" ]] || err "Checksum entry missing for ${ASSET_NAME}."

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL_HASH="$(sha256sum "${TMP_DIR}/${ASSET_NAME}" | awk '{print tolower($1)}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL_HASH="$(shasum -a 256 "${TMP_DIR}/${ASSET_NAME}" | awk '{print tolower($1)}')"
else
  err "sha256sum or shasum is required for checksum verification."
fi

[[ "${ACTUAL_HASH}" == "${EXPECTED_HASH}" ]] || err "SHA-256 verification failed (${ACTUAL_HASH} != ${EXPECTED_HASH})."

mkdir -p "${BIN_DIR}" "${CONF_DIR}" "${WORK_DIR}" "${LOG_DIR}"
tar -xzf "${TMP_DIR}/${ASSET_NAME}" -C "${TMP_DIR}"
install -m 0755 "${TMP_DIR}/runway-agent" "${BIN_PATH}"

if [[ "${INSTALL_GVISOR}" == "true" ]]; then
  if [[ "${GOOS}" != "linux" ]] || [[ "$(id -u)" -ne 0 ]]; then
    err "--install-gvisor requires running as root on Linux."
  fi
  log "Installing gVisor (runsc) sandbox runtime..."
  curl -fsSL "https://raw.githubusercontent.com/${REPO}/main/scripts/install-gvisor.sh" | bash
fi

umask 077
cat >"${ENV_FILE}" <<EOF
RUNWAY_AGENT_SERVER=${SERVER}
RUNWAY_AGENT_TOKEN=${TOKEN}
RUNWAY_AGENT_RUNTIME=${RUNTIME}
RUNWAY_AGENT_SANDBOX=${SANDBOX}
RUNWAY_AGENT_ISOLATED_DIND=${ISOLATED_DIND}
RUNWAY_AGENT_DOCKER_SOCKET=${DOCKER_SOCKET}
RUNWAY_AGENT_WORK_DIR=${WORK_DIR}
RUNWAY_AGENT_LOG_DIR=${LOG_DIR}
RUNWAY_AGENT_CONCURRENCY=${CONCURRENCY}
RUNWAY_AGENT_LABELS=${LABELS}
RUNWAY_AGENT_AUTO_UPDATE=${AUTO_UPDATE}
RUNWAY_AGENT_UPDATE_REPO=${REPO}
EOF
chmod 600 "${ENV_FILE}"

if [[ "${GOOS}" == "linux" ]] && [[ "$(id -u)" -eq 0 ]] && command -v systemctl >/dev/null 2>&1; then
  cat >/etc/systemd/system/runway-agent.service <<EOF
[Unit]
Description=Runway Distributed Runner Agent
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=${ENV_FILE}
ExecStart=${BIN_PATH} run
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now runway-agent
  systemctl restart runway-agent
  log "runway-agent (${TAG}) installed and running via systemd (logs: ${LOG_DIR}/agent.log)."
elif [[ "${GOOS}" == "darwin" ]] && command -v launchctl >/dev/null 2>&1; then
  mkdir -p "$(dirname "${LAUNCHD_PLIST}")" "${LOG_DIR}"
  cat >"${LAUNCHD_PLIST}" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>tech.cubis.runway-agent</string>
  <key>ProgramArguments</key>
  <array>
    <string>${BIN_PATH}</string>
    <string>run</string>
    <string>--config</string>
    <string>${ENV_FILE}</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ThrottleInterval</key>
  <integer>5</integer>
  <key>StandardOutPath</key>
  <string>${LOG_DIR}/service.log</string>
  <key>StandardErrorPath</key>
  <string>${LOG_DIR}/service.log</string>
</dict>
</plist>
EOF
  launchctl unload -w "${LAUNCHD_PLIST}" >/dev/null 2>&1 || true
  launchctl load -w "${LAUNCHD_PLIST}"
  log "runway-agent (${TAG}) installed and running via launchd (logs: ${LOG_DIR}/agent.log)."
else
  log "runway-agent (${TAG}) installed to ${BIN_PATH}."
  log "Run manually with: ${BIN_PATH} run --config ${ENV_FILE}"
fi
