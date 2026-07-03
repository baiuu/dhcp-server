.PHONY: build ui run install uninstall setup install-go install-node install-yarn test clean fmt lint

BINARY=dhcp-server
BUILD_DIR=build
INSTALL_PREFIX?=/opt/dhcp-server
SYSTEMD_SERVICE=dhcp-server.service

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILDTIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/dhcp-server/dhcp-server/internal/buildinfo.Version=$(VERSION) \
           -X github.com/dhcp-server/dhcp-server/internal/buildinfo.BuildTime=$(BUILDTIME)

# Build environment versions (binary tarball installs, no apt/dpkg)
GO_VERSION?=1.26.4
GO_TARBALL?=go$(GO_VERSION).linux-amd64.tar.gz
GO_URL?=https://go.dev/dl/$(GO_TARBALL)
GO_INSTALL_DIR?=/usr/local
GO_BIN?=$(GO_INSTALL_DIR)/go/bin/go

NODE_VERSION?=22.16.0
NODE_TARBALL?=node-v$(NODE_VERSION)-linux-x64.tar.xz
NODE_URL?=https://nodejs.org/dist/v$(NODE_VERSION)/$(NODE_TARBALL)
NODE_INSTALL_DIR?=/usr/local
NODE_DIR?=$(NODE_INSTALL_DIR)/node-v$(NODE_VERSION)-linux-x64
NODE_BIN?=$(NODE_DIR)/bin/node
NPM_BIN?=$(NODE_DIR)/bin/npm
YARN_BIN?=$(NODE_DIR)/bin/yarn

# Prefer locally installed toolchain even when PATH is not yet sourced.
export PATH := $(NODE_DIR)/bin:$(GO_INSTALL_DIR)/go/bin:$(PATH)

build: ui
	mkdir -p $(BUILD_DIR)
	$(GO_BIN) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/dhcp-server

ui:
	@if [ -x "$(YARN_BIN)" ]; then \
		echo "==> Building web UI with yarn ..."; \
		cd internal/web/ui && $(YARN_BIN) install && $(YARN_BIN) build; \
	elif [ -x "$(NPM_BIN)" ]; then \
		echo "==> Building web UI with npm ..."; \
		cd internal/web/ui && $(NPM_BIN) install && $(NPM_BIN) run build; \
	else \
		echo "ERROR: yarn or npm not found. Run 'sudo make setup' first."; \
		exit 1; \
	fi

run: build
	sudo $(BUILD_DIR)/$(BINARY) -config=configs/config.yaml

install: build
	@echo "==> Installing to $(INSTALL_PREFIX) ..."
	mkdir -p $(INSTALL_PREFIX)/build
	cp -f $(BUILD_DIR)/$(BINARY) $(INSTALL_PREFIX)/build/
	cp -rf scripts $(INSTALL_PREFIX)/
	cp -rf systemd $(INSTALL_PREFIX)/
	[ -d $(INSTALL_PREFIX)/configs ] || cp -rf configs $(INSTALL_PREFIX)/
	@echo "==> Installing systemd service ..."
	cp -f systemd/$(SYSTEMD_SERVICE) /etc/systemd/system/
	systemctl daemon-reload
	@echo ""
	@echo "Installation complete. Next steps:"
	@echo "  1. Generate JWT keys:   sudo $(INSTALL_PREFIX)/scripts/generate-jwt-keys.sh"
	@echo "  2. Edit config:         sudo nano $(INSTALL_PREFIX)/configs/config.yaml"
	@echo "  3. Start service:       sudo systemctl enable --now dhcp-server"

uninstall:
	@echo "==> Stopping and disabling service ..."
	-systemctl stop $(SYSTEMD_SERVICE) 2>/dev/null || true
	-systemctl disable $(SYSTEMD_SERVICE) 2>/dev/null || true
	@echo "==> Removing systemd service ..."
	rm -f /etc/systemd/system/$(SYSTEMD_SERVICE)
	systemctl daemon-reload
	@echo "==> Removing installed files ..."
	rm -rf $(INSTALL_PREFIX)
	@echo "Uninstall complete."

# Install the full build environment from upstream binary tarballs.
# Run with sudo on a fresh Linux server (no apt/dpkg packages required).
setup: install-go install-node install-yarn
	@echo "==> Build environment ready."
	@echo "    Go:      $(GO_BIN)"
	@echo "    Node:    $(NODE_BIN)"
	@echo "    Yarn:    $(YARN_BIN)"

install-go:
	@if [ -x "$(GO_BIN)" ]; then \
		echo "Go already installed: $$($(GO_BIN) version)"; \
	else \
		echo "==> Downloading $(GO_TARBALL) ..."; \
		curl -fsSL -o /tmp/$(GO_TARBALL) $(GO_URL); \
		echo "==> Installing Go to $(GO_INSTALL_DIR)/go ..."; \
		rm -rf $(GO_INSTALL_DIR)/go; \
		tar -C $(GO_INSTALL_DIR) -xzf /tmp/$(GO_TARBALL); \
		echo 'export PATH=$(GO_INSTALL_DIR)/go/bin:$$PATH' > /etc/profile.d/dhcp-server-go.sh; \
		echo "Go installed: $$($(GO_BIN) version)"; \
	fi

install-node:
	@if [ -x "$(NODE_BIN)" ]; then \
		echo "Node.js already installed: $$($(NODE_BIN) --version)"; \
	else \
		echo "==> Downloading $(NODE_TARBALL) ..."; \
		curl -fsSL -o /tmp/$(NODE_TARBALL) $(NODE_URL); \
		echo "==> Installing Node.js to $(NODE_INSTALL_DIR) ..."; \
		rm -rf $(NODE_DIR); \
		tar -C $(NODE_INSTALL_DIR) -xJf /tmp/$(NODE_TARBALL); \
		ln -sf $(NODE_BIN) /usr/local/bin/node; \
		ln -sf $(NPM_BIN) /usr/local/bin/npm; \
		echo 'export PATH=$(NODE_DIR)/bin:$$PATH' > /etc/profile.d/dhcp-server-node.sh; \
		echo "Node.js installed: $$($(NODE_BIN) --version)"; \
	fi

install-yarn: install-node
	@if [ -x "$(YARN_BIN)" ]; then \
		echo "yarn already installed: $$($(YARN_BIN) --version)"; \
	else \
		echo "==> Installing yarn ..."; \
		$(NPM_BIN) install -g yarn; \
		ln -sf $(YARN_BIN) /usr/local/bin/yarn; \
		echo "yarn installed: $$($(YARN_BIN) --version)"; \
	fi

test:
	$(GO_BIN) test ./...

clean:
	rm -rf $(BUILD_DIR)
	rm -rf internal/web/ui/node_modules internal/web/ui/dist
	rm -rf internal/web/dist/*
	-touch internal/web/dist/.gitkeep

fmt:
	$(GO_BIN) fmt ./...

lint:
	golangci-lint run ./... || true
