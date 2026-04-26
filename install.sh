#!/bin/sh
set -e

REPO="anivaryam/merge-port"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Detect OS
OS="$(uname -s)"
case "$OS" in
  Linux*)  OS="linux" ;;
  Darwin*) OS="darwin" ;;
  MINGW*|MSYS*|CYGWIN*) OS="windows" ;;
  *) echo "Unsupported OS: $OS" && exit 1 ;;
esac

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" && exit 1 ;;
esac

# Get latest version
VERSION="$(curl -sSf https://api.github.com/repos/${REPO}/releases/latest | grep '"tag_name"' | cut -d'"' -f4)"
if [ -z "$VERSION" ]; then
  echo "Failed to fetch latest version"
  exit 1
fi

# Download
EXT="tar.gz"
if [ "$OS" = "windows" ]; then
  EXT="zip"
fi

URL="https://github.com/${REPO}/releases/download/${VERSION}/merge-port_${OS}_${ARCH}.${EXT}"
echo "Downloading merge-port ${VERSION} for ${OS}/${ARCH}..."

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

curl -sSfL "$URL" -o "${TMP}/merge-port.${EXT}"

# Extract
if [ "$EXT" = "zip" ]; then
  unzip -q "${TMP}/merge-port.${EXT}" -d "$TMP"
else
  tar -xzf "${TMP}/merge-port.${EXT}" -C "$TMP"
fi

# Install
if [ -w "$INSTALL_DIR" ]; then
  mv "${TMP}/merge-port" "${INSTALL_DIR}/merge-port"
else
  sudo mv "${TMP}/merge-port" "${INSTALL_DIR}/merge-port"
fi

	chmod +x "${INSTALL_DIR}/merge-port"
	echo "merge-port ${VERSION} installed to ${INSTALL_DIR}/merge-port"
