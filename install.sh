#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="payambar"
REPO="4xmen/payambar"
INSTALL_DIR="/opt/${SERVICE_NAME}"
DATA_DIR="/var/lib/${SERVICE_NAME}"
UPLOAD_DIR="${DATA_DIR}/uploads"
ENV_DIR="/etc/${SERVICE_NAME}"
ENV_FILE="${ENV_DIR}/${SERVICE_NAME}.env"
SYSTEMD_UNIT="/etc/systemd/system/${SERVICE_NAME}.service"
TARGET_OS=""
TARGET_ARCH=""
ACTION="install"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "[error] Required command '$1' not found. Install it and retry." >&2
    exit 1
  fi
}

ensure_root() {
  if [ "${EUID}" -ne 0 ]; then
    exec sudo -E bash "$0" "$@"
  fi
}

usage() {
  cat <<EOF
Usage: $0 [--install|--update]

Options:
  --install   Install or reinstall ${SERVICE_NAME} (default)
  --update    Update existing ${SERVICE_NAME} binary to latest release
  -h, --help  Show this help
EOF
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --install)
        ACTION="install"
        ;;
      --update)
        ACTION="update"
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        echo "[error] Unknown argument: $1" >&2
        usage
        exit 1
        ;;
    esac
    shift
  done
}

detect_platform() {
  local raw_os raw_arch
  raw_os=$(uname -s | tr '[:upper:]' '[:lower:]')
  raw_arch=$(uname -m)

  case "${raw_os}" in
    linux)
      TARGET_OS="linux"
      ;;
    *)
      echo "[error] Unsupported OS '${raw_os}'. This installer currently supports Linux systemd hosts." >&2
      exit 1
      ;;
  esac

  case "${raw_arch}" in
    x86_64|amd64)
      TARGET_ARCH="amd64"
      ;;
    aarch64|arm64)
      TARGET_ARCH="arm64"
      ;;
    *)
      echo "[error] Unsupported CPU architecture '${raw_arch}'." >&2
      exit 1
      ;;
  esac
}

ensure_update_target_exists() {
  if [ "${ACTION}" = "update" ] && [ ! -x "${INSTALL_DIR}/payambar" ]; then
    echo "[error] Update requested but ${INSTALL_DIR}/payambar is not installed." >&2
    echo "[error] Run with --install first." >&2
    exit 1
  fi
}

generate_jwt_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
  else
    dd if=/dev/urandom bs=32 count=1 2>/dev/null | xxd -p -c 64
  fi
}

is_elf_binary() {
  local file_path="$1"
  local magic
  magic=$(od -An -t x1 -N4 "${file_path}" 2>/dev/null | tr -d '[:space:]')
  [ "${magic}" = "7f454c46" ]
}

fetch_latest_asset_url() {
  local url
  url=$(TARGET_OS="${TARGET_OS}" TARGET_ARCH="${TARGET_ARCH}" curl -fsSL \
    -H "Accept: application/vnd.github+json" \
    -H "User-Agent: ${SERVICE_NAME}-installer" \
    "https://api.github.com/repos/${REPO}/releases/latest" | python3 -c '
import json
import re
import sys
import os

data = json.load(sys.stdin)
assets = data.get("assets") or []
target_os = os.environ.get("TARGET_OS", "")
target_arch = os.environ.get("TARGET_ARCH", "")
patterns = [
    rf"{target_os}[-_.]?{target_arch}",
    rf"{target_os}.*{target_arch}",
    rf"{target_os}[-_.]?(x86_64|amd64|64)" if target_arch == "amd64" else rf"{target_os}[-_.]?(aarch64|arm64)",
]
for pat in patterns:
    if not pat:
        continue
    for asset in assets:
        name = asset.get("name", "")
        if re.search(pat, name, re.IGNORECASE):
            print(asset.get("browser_download_url", ""))
            sys.exit(0)
sys.exit(1)
'
  ) || true

  if [ -z "${url}" ]; then
    echo "[error] Could not find a release asset for ${TARGET_OS}/${TARGET_ARCH}." >&2
    exit 1
  fi

  echo "${url}"
}

download_and_extract() {
  local asset_url="$1"
  local workdir
  workdir=$(mktemp -d)
  local archive="${workdir}/release.bin"

  echo "[info] Downloading ${asset_url}" >&2
  curl -fL "${asset_url}" -o "${archive}"

  echo "[info] Extracting archive" >&2
  case "${asset_url}" in
    *.tar.gz|*.tgz)
      tar -xzf "${archive}" -C "${workdir}"
      ;;
    *.zip)
      require_cmd unzip
      unzip -q "${archive}" -d "${workdir}"
      ;;
    *)
      # Assume it's a raw binary
      cp "${archive}" "${workdir}/payambar"
      ;;
  esac

  local bin_path=""
  local candidates

  candidates=$(find "${workdir}" -maxdepth 5 -type f \( \
    -name "payambar-${TARGET_OS}-${TARGET_ARCH}" -o \
    -name "payambar" -o \
    -name "payambar*" \
  \) | sort)

  while IFS= read -r candidate; do
    [ -n "${candidate}" ] || continue
    case "${candidate}" in
      *.txt|*.md|*.sha256|*.sha512|*.sum|*.asc|*.sig)
        continue
        ;;
    esac
    if is_elf_binary "${candidate}"; then
      bin_path="${candidate}"
      break
    fi
  done <<EOF
${candidates}
EOF

  if [ -z "${bin_path}" ]; then
    echo "[error] Unable to locate payambar binary in downloaded asset." >&2
    echo "[error] Found files were not valid Linux ELF binaries for ${TARGET_OS}/${TARGET_ARCH}." >&2
    exit 1
  fi
  chmod +x "${bin_path}"

  echo "${bin_path}"
}

setup_user_and_dirs() {
  if ! id -u "${SERVICE_NAME}" >/dev/null 2>&1; then
    useradd --system --home "${DATA_DIR}" --shell /usr/sbin/nologin "${SERVICE_NAME}"
  fi

  install -d -m 755 "${INSTALL_DIR}" "${DATA_DIR}" "${UPLOAD_DIR}" "${ENV_DIR}"
  chown -R "${SERVICE_NAME}:${SERVICE_NAME}" "${DATA_DIR}" "${UPLOAD_DIR}"
}

install_binary() {
  local src_bin="$1"
  install -m 755 "${src_bin}" "${INSTALL_DIR}/payambar"
  chown "root:root" "${INSTALL_DIR}/payambar"
}

write_env_file() {
  if [ ! -f "${ENV_FILE}" ]; then
    cat >"${ENV_FILE}" <<EOF
PORT=8080
ENVIRONMENT=production
DATABASE_PATH=${DATA_DIR}/payambar.db
FILE_STORAGE_PATH=${UPLOAD_DIR}
JWT_SECRET=$(generate_jwt_secret)
CORS_ORIGINS=*
MAX_UPLOAD_SIZE=10485760
STUN_SERVERS=stun:stun.l.google.com:19302
TURN_SERVER=
TURN_USERNAME=
TURN_PASSWORD=
EOF
    chmod 640 "${ENV_FILE}"
    chown root:root "${ENV_FILE}"
  fi
}

write_systemd_unit() {
  cat >"${SYSTEMD_UNIT}" <<EOF
[Unit]
Description=Payambar messenger server
After=network.target

[Service]
User=${SERVICE_NAME}
Group=${SERVICE_NAME}
WorkingDirectory=${INSTALL_DIR}
EnvironmentFile=${ENV_FILE}
ExecStart=${INSTALL_DIR}/payambar
Restart=on-failure
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
}

start_service() {
  touch "${DATA_DIR}/payambar.db"
  chown "${SERVICE_NAME}:${SERVICE_NAME}" "${DATA_DIR}/payambar.db"
  systemctl enable "${SERVICE_NAME}"
  if systemctl is-active --quiet "${SERVICE_NAME}"; then
    systemctl restart "${SERVICE_NAME}"
  else
    systemctl start "${SERVICE_NAME}"
  fi
}

print_port_hint() {
  local port="8080"
  if [ -f "${ENV_FILE}" ]; then
    port=$(grep -E '^PORT=' "${ENV_FILE}" | tail -n1 | cut -d'=' -f2- || echo "8080")
  fi
  echo "[info] Payambar is starting. Expected listening port: ${port}"
  echo "[info] Open: http://<server-ip>:${port}"
}

main() {
  parse_args "$@"
  ensure_root "$@"
  require_cmd curl
  require_cmd systemctl
  require_cmd python3
  require_cmd tar
  detect_platform
  ensure_update_target_exists

  echo "[info] Action: ${ACTION}"
  echo "[info] Detected platform: ${TARGET_OS}/${TARGET_ARCH}"

  local asset_url
  asset_url=$(fetch_latest_asset_url)
  local bin_path
  bin_path=$(download_and_extract "${asset_url}")

  setup_user_and_dirs
  install_binary "${bin_path}"
  write_env_file
  write_systemd_unit
  start_service
  print_port_hint
}

main "$@"
