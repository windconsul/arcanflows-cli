#!/bin/sh
# ArcanFlows CLI Installer
#
# Quick install (anyone, no auth required):
#   curl -fsSL https://raw.githubusercontent.com/windconsul/arcanflows-cli/main/install.sh | sh
#   wget -qO- https://raw.githubusercontent.com/windconsul/arcanflows-cli/main/install.sh | sh
#
# Override the source repo (for forks / mirrors):
#   ARCANFLOWS_CLI_REPO=youruser/yourfork curl ... | sh
#
# Pin a specific version (default is latest release):
#   ARCANFLOWS_CLI_VERSION=v0.7.2 curl ... | sh
#
# Custom install dir (default is /usr/local/bin, with sudo if needed):
#   ARCANFLOWS_CLI_INSTALL_DIR=$HOME/.local/bin curl ... | sh

set -e

REPO="${ARCANFLOWS_CLI_REPO:-windconsul/arcanflows-cli}"
BINARY_NAME="arcanflows"
INSTALL_DIR="${ARCANFLOWS_CLI_INSTALL_DIR:-/usr/local/bin}"
PINNED_VERSION="${ARCANFLOWS_CLI_VERSION:-}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
DIM='\033[2m'
BOLD='\033[1m'
RESET='\033[0m'

info() { printf "${CYAN}${BOLD}==>${RESET} %s\n" "$1"; }
success() { printf "${GREEN}✓${RESET} %s\n" "$1"; }
error() { printf "${RED}✗${RESET} %s\n" "$1" >&2; exit 1; }

# Detect OS and architecture
detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$OS" in
        linux)  OS="linux" ;;
        darwin) OS="darwin" ;;
        *)      error "Unsupported OS: $OS" ;;
    esac

    case "$ARCH" in
        x86_64|amd64) ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *)             error "Unsupported architecture: $ARCH" ;;
    esac

    PLATFORM="${OS}-${ARCH}"
}

# Get release version (pinned via env, or latest)
get_latest_version() {
    if [ -n "$PINNED_VERSION" ]; then
        VERSION="$PINNED_VERSION"
        return
    fi

    if command -v curl >/dev/null 2>&1; then
        VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')
    elif command -v wget >/dev/null 2>&1; then
        VERSION=$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')
    fi

    if [ -z "$VERSION" ]; then
        error "No releases found in ${REPO}. Either the repo has no published releases or you need to set ARCANFLOWS_CLI_REPO."
    fi
}

# Download and install
install() {
    detect_platform
    get_latest_version

    BINARY="${BINARY_NAME}-${PLATFORM}"
    if [ "$OS" = "windows" ]; then
        BINARY="${BINARY}.exe"
    fi

    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY}"

    info "Installing ArcanFlows CLI ${VERSION} (${PLATFORM})"
    printf "${DIM}  Downloading from ${DOWNLOAD_URL}${RESET}\n"

    # Create temp directory
    TMP_DIR=$(mktemp -d)
    trap "rm -rf $TMP_DIR" EXIT

    # Download
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$DOWNLOAD_URL" -o "${TMP_DIR}/${BINARY_NAME}" || error "Download failed. Check if release exists for ${PLATFORM}."
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "${TMP_DIR}/${BINARY_NAME}" "$DOWNLOAD_URL" || error "Download failed. Check if release exists for ${PLATFORM}."
    else
        error "Neither curl nor wget found. Please install one of them."
    fi

    # Make executable
    chmod +x "${TMP_DIR}/${BINARY_NAME}"

    # Install
    if [ -w "$INSTALL_DIR" ]; then
        mv "${TMP_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    else
        info "Installing to ${INSTALL_DIR} (requires sudo)"
        sudo mv "${TMP_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    fi

    success "ArcanFlows CLI installed to ${INSTALL_DIR}/${BINARY_NAME}"

    # Verify
    if command -v arcanflows >/dev/null 2>&1; then
        printf "\n"
        arcanflows version
        printf "\n"
        success "Installation complete!"
        printf "\n  Get started:\n"
        printf "    ${BOLD}arcanflows login${RESET}          Log in to your account\n"
        printf "    ${BOLD}arcanflows chat${RESET}           Chat with an AI agent\n"
        printf "    ${BOLD}arcanflows init${RESET}           Initialize project instructions\n"
        printf "    ${BOLD}arcanflows --help${RESET}         Show all commands\n\n"
    else
        printf "\n"
        success "Binary installed. Add ${INSTALL_DIR} to your PATH if not already."
    fi
}

install
