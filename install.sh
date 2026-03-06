#!/bin/sh
set -eu

REPO_PATH="github.com/pmclSF/gauntlet/cmd/gauntlet"
INSTALL_METHOD="${GAUNTLET_INSTALL_METHOD:-auto}" # auto|go|binary
INSTALL_HOST="${GAUNTLET_INSTALL_HOST:-https://gauntlet.dev}"
BINARY_NAME="${GAUNTLET_BINARY_NAME:-gauntlet}"
INSTALL_DIR="${GAUNTLET_INSTALL_DIR:-$HOME/.local/bin}"
CHECKSUMS_URL="${GAUNTLET_CHECKSUMS_URL:-$INSTALL_HOST/checksums.txt}"

echo "Installing Gauntlet..."

sha256_file() {
    file_path="$1"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$file_path" | awk '{print $1}'
        return
    fi
    if command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$file_path" | awk '{print $1}'
        return
    fi
    echo "ERROR: sha256sum or shasum is required to verify the downloaded binary checksum." >&2
    exit 1
}

install_with_go() {
    echo "Using go install..."
    go install "${REPO_PATH}@latest"
    echo ""
    echo "Gauntlet installed to $(go env GOPATH)/bin/gauntlet"
    echo "Make sure $(go env GOPATH)/bin is in your PATH."
}

install_prebuilt_binary() {
    if ! command -v curl >/dev/null 2>&1; then
        echo "ERROR: curl is required for binary install mode." >&2
        exit 1
    fi

    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64) arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
    esac

    archive_name="gauntlet-${os}-${arch}.tar.gz"
    binary_url="${GAUNTLET_BINARY_URL:-$INSTALL_HOST/releases/$archive_name}"
    tmp_archive="$(mktemp -t gauntlet-install-archive.XXXXXX)"
    checksums_tmp="$(mktemp -t gauntlet-checksums.XXXXXX)"
    tmp_dir="$(mktemp -d -t gauntlet-install.XXXXXX)"
    trap 'rm -f "$tmp_archive" "$checksums_tmp"; rm -rf "$tmp_dir"' EXIT INT TERM

    echo "Downloading ${archive_name}..."
    curl -fsSL "$binary_url" -o "$tmp_archive"
    curl -fsSL "$CHECKSUMS_URL" -o "$checksums_tmp"

    EXPECTED_SHA="$(awk -v n="$archive_name" '$2==n {print $1; exit}' "$checksums_tmp")"
    if [ -z "$EXPECTED_SHA" ]; then
        echo "ERROR: checksum not found for $archive_name in $CHECKSUMS_URL" >&2
        exit 1
    fi

    ACTUAL_SHA="$(sha256_file "$tmp_archive")"
    if [ "$EXPECTED_SHA" != "$ACTUAL_SHA" ]; then
        echo "ERROR: checksum mismatch — binary may be corrupt or tampered" >&2
        echo "  expected: $EXPECTED_SHA" >&2
        echo "  actual:   $ACTUAL_SHA" >&2
        exit 1
    fi

    tar -xzf "$tmp_archive" -C "$tmp_dir"
    extracted="$tmp_dir/$BINARY_NAME"
    if [ ! -f "$extracted" ]; then
        echo "ERROR: downloaded archive did not contain $BINARY_NAME" >&2
        exit 1
    fi

    mkdir -p "$INSTALL_DIR"
    install -m 0755 "$extracted" "$INSTALL_DIR/$BINARY_NAME"
    echo ""
    echo "Gauntlet installed to $INSTALL_DIR/$BINARY_NAME"
    echo "Make sure $INSTALL_DIR is in your PATH."
}

case "$INSTALL_METHOD" in
    go)
        if ! command -v go >/dev/null 2>&1; then
            echo "ERROR: INSTALL_METHOD=go but Go is not installed." >&2
            exit 1
        fi
        install_with_go
        ;;
    binary)
        install_prebuilt_binary
        ;;
    auto)
        if command -v go >/dev/null 2>&1; then
            install_with_go
        else
            install_prebuilt_binary
        fi
        ;;
    *)
        echo "ERROR: unknown GAUNTLET_INSTALL_METHOD=$INSTALL_METHOD (expected auto|go|binary)" >&2
        exit 1
        ;;
esac

echo ""
echo "Verify: gauntlet --version"
echo "Get started: gauntlet init"
