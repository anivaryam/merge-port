#!/bin/sh
set -eu

REPO="${REPO:-anivaryam/merge-port}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

TMP="$(mktemp -d)"
BACKUP=""
trap 'rm -rf "$TMP"' EXIT INT TERM

fail() {
  printf 'Install failed: %s\n' "$1" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

as_root() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  else
    command -v sudo >/dev/null 2>&1 || fail "sudo required for ${INSTALL_DIR}; set INSTALL_DIR to a writable directory"
    sudo "$@"
  fi
}

detect_os() {
  case "$(uname -s)" in
    Linux*) printf 'linux' ;;
    Darwin*) printf 'darwin' ;;
    MINGW*|MSYS*|CYGWIN*) fail "install.sh supports Linux/macOS only. On Windows, use brokit, go install, or download a release zip." ;;
    *) fail "unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64' ;;
    aarch64|arm64) printf 'arm64' ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
  esac
}

latest_version() {
  body="$(curl -sSfL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null || true)"
  version="$(printf '%s' "$body" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | sed -n '1p')"
  if [ -z "$version" ]; then
    fail "failed to fetch latest version. If GitHub rate-limited this request, retry later or set VERSION=vX.Y.Z"
  fi
  printf '%s' "$version"
}

checksum_tool() {
  if command -v sha256sum >/dev/null 2>&1; then
    printf 'sha256sum'
  elif command -v shasum >/dev/null 2>&1; then
    printf 'shasum'
  elif command -v cksum >/dev/null 2>&1 && cksum -a sha2 -l 256 /dev/null >/dev/null 2>&1; then
    printf 'cksum'
  else
    fail "missing checksum tool: install sha256sum, shasum, or cksum with sha2 support"
  fi
}

verify_checksum() {
  archive="$1"
  tool="$2"
  line="$(grep "  ${archive}$" "${TMP}/checksums.txt" || true)"
  [ -n "$line" ] || fail "checksum entry not found for ${archive}"
  printf '%s\n' "$line" > "${TMP}/current.sha256"
  if [ "$tool" = "sha256sum" ]; then
    (cd "$TMP" && sha256sum -c current.sha256) >/dev/null
  elif [ "$tool" = "shasum" ]; then
    (cd "$TMP" && shasum -a 256 -c current.sha256) >/dev/null
  else
    expected="$(printf '%s\n' "$line" | sed 's/[[:space:]].*//')"
    actual="$(cd "$TMP" && cksum -a sha2 -l 256 "$archive" | sed -n 's/.*\([[:xdigit:]]\{64\}\).*/\1/p' | sed -n '1p')"
    [ "$actual" = "$expected" ] || fail "checksum mismatch for ${archive}"
  fi
}

ensure_install_dir() {
  if [ -d "$INSTALL_DIR" ]; then
    return
  fi
  parent="$(dirname "$INSTALL_DIR")"
  if [ -w "$parent" ]; then
    mkdir -p "$INSTALL_DIR"
  else
    as_root mkdir -p "$INSTALL_DIR"
  fi
}

install_binary() {
  src="$1"
  dest="${INSTALL_DIR}/merge-port"
  chmod +x "$src"
  ensure_install_dir
  if [ -e "$dest" ]; then
    BACKUP="${dest}.bak.$(date -u +%Y%m%dT%H%M%S)"
    if [ -e "$BACKUP" ]; then
      fail "backup path already exists: ${BACKUP}"
    fi
    if [ -w "$INSTALL_DIR" ]; then
      mv "$dest" "$BACKUP"
    else
      as_root mv "$dest" "$BACKUP"
    fi
    printf 'Existing binary backed up to %s\n' "$BACKUP"
  fi
  if [ -w "$INSTALL_DIR" ]; then
    mv "$src" "$dest" || restore_backup
  else
    as_root mv "$src" "$dest" || restore_backup
  fi
  BACKUP=""
}

restore_backup() {
  dest="${INSTALL_DIR}/merge-port"
  if [ -n "$BACKUP" ] && [ -e "$BACKUP" ]; then
    if [ -w "$INSTALL_DIR" ]; then
      mv "$BACKUP" "$dest"
    else
      as_root mv "$BACKUP" "$dest"
    fi
  fi
  fail "could not install binary; restored previous installation if one existed"
}

main() {
  need curl
  need tar

  OS="$(detect_os)"
  ARCH="$(detect_arch)"
  VERSION="${VERSION:-$(latest_version)}"
  ARCHIVE="merge-port_${OS}_${ARCH}.tar.gz"
  URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"
  CHECKSUMS_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"

  printf 'Downloading merge-port %s for %s/%s...\n' "$VERSION" "$OS" "$ARCH"
  curl -sSfL "$URL" -o "${TMP}/${ARCHIVE}"
  curl -sSfL "$CHECKSUMS_URL" -o "${TMP}/checksums.txt"

  printf 'Verifying checksum...\n'
  verify_checksum "$ARCHIVE" "$(checksum_tool)"

  tar -xzf "${TMP}/${ARCHIVE}" -C "$TMP"
  [ -f "${TMP}/merge-port" ] || fail "archive did not contain merge-port binary"

  printf 'Installing to %s/merge-port...\n' "$INSTALL_DIR"
  install_binary "${TMP}/merge-port"

  "${INSTALL_DIR}/merge-port" --version >/dev/null
  printf 'merge-port %s installed successfully\n' "$VERSION"
  printf 'Run: merge-port --help\n'
}

main "$@"
