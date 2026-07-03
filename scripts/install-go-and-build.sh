#!/bin/bash
# install-go-and-build.sh
#
# Build and install the DHCP server.
#
# Usage:
#   sudo ./scripts/install-go-and-build.sh
#   sudo ./scripts/install-go-and-build.sh go1.26.4.linux-amd64.tar.gz
#
# If go-tarball is provided, Go will be installed from it.
# If go-tarball is omitted, Go will be searched under /usr/local/go and $PATH.
#
# Optional environment variables:
#   GO_INSTALL_DIR  - where to extract Go (default: /usr/local)
#   INSTALL_PREFIX  - where to install the built binary (default: /opt/dhcp-server)
#   SKIP_GO_INSTALL - if set, never install Go even if tarball is provided

set -euo pipefail

GO_TARBALL="${1:-}"
GO_INSTALL_DIR="${GO_INSTALL_DIR:-/usr/local}"
INSTALL_PREFIX="${INSTALL_PREFIX:-/opt/dhcp-server}"
PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

usage() {
    echo "Usage: $0 [go-tarball]"
    echo ""
    echo "If go-tarball is provided, Go will be installed from it."
    echo "If go-tarball is omitted, Go will be searched under /usr/local/go and PATH."
    echo "Example: $0 go1.26.4.linux-amd64.tar.gz"
    exit 1
}

if [ -n "$GO_TARBALL" ] && [ ! -f "$GO_TARBALL" ]; then
    echo "ERROR: tarball not found: $GO_TARBALL"
    exit 1
fi

install_go() {
    echo "==> Installing Go from $GO_TARBALL to $GO_INSTALL_DIR ..."
    rm -rf "$GO_INSTALL_DIR/go"
    tar -C "$GO_INSTALL_DIR" -xzf "$GO_TARBALL"

    # Make Go available system-wide for future shells.
    cat > /etc/profile.d/go.sh <<EOF
export GOROOT=$GO_INSTALL_DIR/go
export PATH=\$GOROOT/bin:\$PATH
EOF
    chmod 644 /etc/profile.d/go.sh
}

if [ -n "$GO_TARBALL" ] && [ -z "${SKIP_GO_INSTALL:-}" ]; then
    install_go
fi

# Try to locate Go even when sudo resets PATH.
GO_BIN=""
for candidate in "$GO_INSTALL_DIR/go/bin/go" "/usr/local/go/bin/go" "$(command -v go 2>/dev/null || true)"; do
    if [ -n "$candidate" ] && [ -x "$candidate" ]; then
        GO_BIN="$candidate"
        break
    fi
done

if [ -z "$GO_BIN" ]; then
    echo "ERROR: Go not found."
    echo "       Provide a go-tarball, or install Go under /usr/local/go, or add go to PATH."
    exit 1
fi

export GOROOT="$(cd "$(dirname "$GO_BIN")/.." && pwd)"
export PATH="$GOROOT/bin:$PATH"

# Honor existing GOPATH if set, otherwise use a local cache.
export GOPATH="${GOPATH:-$GO_INSTALL_DIR/gopath}"
export PATH="$GOPATH/bin:$PATH"
# Use the local toolchain so Go does not try to download a newer version.
export GOTOOLCHAIN=local
# Use a Go module proxy accessible from mainland networks.
# Override with: GOPROXY=https://your-proxy,direct
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
# Override with: GOSUMDB=off if checksum database is unreachable.
export GOSUMDB="${GOSUMDB:-sum.golang.org}"

mkdir -p "$GOPATH"

echo "==> Go version:"
go version

echo "==> Building dhcp-server ..."
cd "$PROJECT_DIR"

# Load system-wide environment (nvm, fnm, /usr/local/bin, etc.) when running under sudo.
if [ -f /etc/profile ]; then
    # shellcheck source=/dev/null
    . /etc/profile >/dev/null 2>&1 || true
fi

# Try to locate yarn/npm even when sudo resets PATH.
# Override with: YARN_BIN=/path/to/yarn NPM_BIN=/path/to/npm
YARN_BIN="${YARN_BIN:-$(command -v yarn 2>/dev/null || true)}"
NPM_BIN="${NPM_BIN:-$(command -v npm 2>/dev/null || true)}"

# Also check common global paths.
[ -z "$YARN_BIN" ] && [ -x "/usr/local/bin/yarn" ] && YARN_BIN="/usr/local/bin/yarn"
[ -z "$YARN_BIN" ] && [ -x "/usr/bin/yarn" ] && YARN_BIN="/usr/bin/yarn"
[ -z "$NPM_BIN" ] && [ -x "/usr/local/bin/npm" ] && NPM_BIN="/usr/local/bin/npm"
[ -z "$NPM_BIN" ] && [ -x "/usr/bin/npm" ] && NPM_BIN="/usr/bin/npm"

# Build web UI if a package manager is available on the target server.
if [ -n "$YARN_BIN" ] && [ -x "$YARN_BIN" ]; then
    echo "==> Building web UI with yarn ($YARN_BIN) ..."
    cd "$PROJECT_DIR/internal/web/ui"
    "$YARN_BIN" install
    "$YARN_BIN" build
    cd "$PROJECT_DIR"
elif [ -n "$NPM_BIN" ] && [ -x "$NPM_BIN" ]; then
    echo "==> Building web UI with npm ($NPM_BIN) ..."
    cd "$PROJECT_DIR/internal/web/ui"
    "$NPM_BIN" install
    "$NPM_BIN" run build
    cd "$PROJECT_DIR"
else
    echo "WARN: yarn/npm not found, skipping web UI build."
    echo "      Install Node.js or run 'yarn build' / 'npm run build' in internal/web/ui manually."
fi

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
BUILDTIME="${BUILDTIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
LDFLAGS="-X github.com/dhcp-server/dhcp-server/internal/buildinfo.Version=$VERSION -X github.com/dhcp-server/dhcp-server/internal/buildinfo.BuildTime=$BUILDTIME"

echo "==> Building version: $VERSION ($BUILDTIME)"
go build -ldflags "$LDFLAGS" -o build/dhcp-server ./cmd/dhcp-server

echo "==> Installing to $INSTALL_PREFIX ..."
mkdir -p "$INSTALL_PREFIX/build"

# Avoid "cp: same file" error when PROJECT_DIR equals INSTALL_PREFIX.
if [ "$PROJECT_DIR/build/dhcp-server" != "$INSTALL_PREFIX/build/dhcp-server" ]; then
    cp -f build/dhcp-server "$INSTALL_PREFIX/build/"
fi

# Copy supporting files only if not already present, to avoid overwriting configs.
[ -d "$INSTALL_PREFIX/configs" ] || cp -r configs "$INSTALL_PREFIX/"
[ -d "$INSTALL_PREFIX/scripts" ] || cp -r scripts "$INSTALL_PREFIX/"
[ -d "$INSTALL_PREFIX/systemd" ] || cp -r systemd "$INSTALL_PREFIX/"

echo "==> Build complete."
echo "    Binary: $INSTALL_PREFIX/build/dhcp-server"
echo "    Config: $INSTALL_PREFIX/configs/config.yaml"
echo ""
echo "Next steps:"
echo "  1. Edit $INSTALL_PREFIX/configs/config.yaml"
echo "  2. Run: sudo $INSTALL_PREFIX/build/dhcp-server -config=$INSTALL_PREFIX/configs/config.yaml"
echo ""
echo "Optional systemd service:"
echo "  sudo cp $INSTALL_PREFIX/systemd/dhcp-server.service /etc/systemd/system/"
echo "  sudo systemctl daemon-reload"
echo "  sudo systemctl enable --now dhcp-server"
