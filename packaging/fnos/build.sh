#!/bin/sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
VERSION="${VELIN_FNOS_VERSION:-0.3.17}"
VERSION="${VERSION#v}"
IMAGE="${VELIN_FNOS_IMAGE:-ghcr.io/ttyob/velinwebssh:${VERSION}}"
OUTPUT_DIR="${VELIN_FNOS_OUTPUT_DIR:-$SCRIPT_DIR/../../dist/fnos}"
FNPACK_BIN="${FNPACK_BIN:-}"
FN_PACK_TMP=""
STAGE_DIR=""

if [ -z "$FNPACK_BIN" ] && command -v fnpack >/dev/null 2>&1; then
  FNPACK_BIN="$(command -v fnpack)"
fi

if [ -z "$FNPACK_BIN" ]; then
  case "$(uname -m)" in
    x86_64|amd64) FN_PACK_ARCH=amd64 ;;
    aarch64|arm64) FN_PACK_ARCH=arm64 ;;
    *) echo "Unsupported fnpack architecture: $(uname -m)" >&2; exit 1 ;;
  esac
  FN_PACK_TMP="$(mktemp -d)"
  trap 'rm -rf "$FN_PACK_TMP" "$STAGE_DIR"' EXIT INT TERM
  FNPACK_BIN="$FN_PACK_TMP/fnpack"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "https://static2.fnnas.com/fnpack/fnpack-1.2.3-linux-${FN_PACK_ARCH}" -o "$FNPACK_BIN"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$FNPACK_BIN" "https://static2.fnnas.com/fnpack/fnpack-1.2.3-linux-${FN_PACK_ARCH}"
  else
    echo "fnpack is missing and curl/wget is unavailable" >&2
    exit 1
  fi
  chmod 755 "$FNPACK_BIN"
else
  trap 'rm -rf "$STAGE_DIR"' EXIT INT TERM
fi

STAGE_DIR="$(mktemp -d)"
cp -R "$SCRIPT_DIR"/. "$STAGE_DIR/"
rm -f "$STAGE_DIR/build.sh"
rm -rf "$STAGE_DIR/dist"

sed -i "s/^version=.*/version=${VERSION}/" "$STAGE_DIR/manifest"
sed -i "s|image: ghcr.io/ttyob/velinwebssh:latest|image: ${IMAGE}|" \
  "$STAGE_DIR/app/docker/docker-compose.yaml"

mkdir -p "$OUTPUT_DIR"
(
  cd "$STAGE_DIR"
  "$FNPACK_BIN" build --directory "$STAGE_DIR"
)
PACKAGE_FILE="$(find "$STAGE_DIR" -maxdepth 1 -type f -name '*.fpk' -print -quit)"
if [ -z "$PACKAGE_FILE" ]; then
  echo "fnpack did not produce an .fpk file" >&2
  exit 1
fi

OUTPUT_FILE="$OUTPUT_DIR/velin-fnos-${VERSION}.fpk"
cp "$PACKAGE_FILE" "$OUTPUT_FILE"
printf '%s\n' "$OUTPUT_FILE"
