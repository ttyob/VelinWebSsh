#!/bin/sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
INSTALL_DIR="${VELIN_INSTALL_DIR:-$SCRIPT_DIR}"

if [ ! -f "$INSTALL_DIR/compose.yaml" ] || [ ! -f "$INSTALL_DIR/.env" ]; then
  echo "未在 ${INSTALL_DIR} 找到 Velin，请先运行 install.sh。" >&2
  exit 1
fi

DOCKER="docker"
if ! docker info >/dev/null 2>&1; then
  if command -v sudo >/dev/null 2>&1 && sudo -n docker info >/dev/null 2>&1; then
    DOCKER="sudo docker"
  else
    echo "当前用户无权访问 Docker。" >&2
    exit 1
  fi
fi

cd "$INSTALL_DIR"
$DOCKER compose pull
$DOCKER compose up -d --remove-orphans
echo "Velin 已更新：$($DOCKER compose ps --format json | tr '\n' ' ')"
