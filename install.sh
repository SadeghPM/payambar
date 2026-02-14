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

generate_jwt_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
  else
    dd if=/dev/urandom bs=32 count=1 2>/dev/null | xxd -p -c 64
  fi
}

fetch_latest_asset_url() {
  local url
  url=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | python3 - <<'PY'
import json, re, sys
data = json.load(sys.stdin)
assets = data.get("assets") or []
patterns = [r"linux.*amd64", r"linux.*x86_64", r"linux.*64", r"linux", r"amd64", r"x86_64"]
for pat in patterns:
    for a in assets:
        name = a.get("name", "")
        if re.search(pat, name, re.IGNORECASE):
            print(a.get("browser_download_url", ""))
            sys.exit(0)
if assets:
    print(assets[0].get("browser_download_url", ""))
    sys.exit(0)
sys.exit(1)
PY
  ) || true

  if [ -z "${url}" ]; then
    echo "[error] Could not find a release asset to download." >&2
    exit 1
  fi

  echo "${url}"
}

download_and_extract() {
  local asset_url="$1"
  local workdir
  workdir=$(mktemp -d)
  local archive="${workdir}/release.bin"

  echo "[info] Downloading ${asset_url}"
  curl -fL "${asset_url}" -o "${archive}"

  echo "[info] Extracting archive"
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

  local bin_path
  bin_path=$(find "${workdir}" -maxdepth 3 -type f -perm -111 -name "payambar*" | head -n 1)
  if [ -z "${bin_path}" ]; then
    echo "[error] Unable to locate payambar binary in downloaded asset." >&2
    exit 1
  fi

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
  systemctl enable --now "${SERVICE_NAME}"
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
  ensure_root "$@"
  require_cmd curl
  require_cmd systemctl
  require_cmd python3
  require_cmd tar

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
