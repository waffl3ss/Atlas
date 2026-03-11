APP_NAME    := atlas
CMD_PATH    := ./cmd/atlas
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS     := -s -w -X main.Version=$(VERSION)

# All target platforms
PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64 \
	windows/arm64

# ---------- default ----------
.PHONY: all
all: build-all

# ---------- single (native) build ----------
.PHONY: build
build:
	@echo "Building $(APP_NAME) (native)..."
	go build -ldflags "$(LDFLAGS)" -o $(APP_NAME) $(CMD_PATH)
	@echo "  -> $(APP_NAME)"

# ---------- individual OS targets ----------
.PHONY: linux
linux: linux-amd64 linux-arm64

.PHONY: linux-amd64
linux-amd64:
	@$(MAKE) --no-print-directory _build GOOS=linux GOARCH=amd64

.PHONY: linux-arm64
linux-arm64:
	@$(MAKE) --no-print-directory _build GOOS=linux GOARCH=arm64

.PHONY: darwin
darwin: darwin-amd64 darwin-arm64

.PHONY: darwin-amd64
darwin-amd64:
	@$(MAKE) --no-print-directory _build GOOS=darwin GOARCH=amd64

.PHONY: darwin-arm64
darwin-arm64:
	@$(MAKE) --no-print-directory _build GOOS=darwin GOARCH=arm64

.PHONY: windows
windows: windows-amd64 windows-arm64

.PHONY: windows-amd64
windows-amd64:
	@$(MAKE) --no-print-directory _build GOOS=windows GOARCH=amd64

.PHONY: windows-arm64
windows-arm64:
	@$(MAKE) --no-print-directory _build GOOS=windows GOARCH=arm64

# ---------- all platforms ----------
.PHONY: build-all
build-all:
	@$(foreach p,$(PLATFORMS),$(MAKE) --no-print-directory _build GOOS=$(word 1,$(subst /, ,$(p))) GOARCH=$(word 2,$(subst /, ,$(p)));)

# ---------- internal cross-compile target ----------
_EXT = $(if $(filter windows,$(GOOS)),.exe,)
_OUT = $(APP_NAME)-$(GOOS)-$(GOARCH)$(_EXT)

.PHONY: _build
_build:
	@echo "Building $(GOOS)/$(GOARCH)..."
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(_OUT) $(CMD_PATH)
	@echo "  -> $(_OUT)"

# ---------- run ----------
.PHONY: run
run:
	go run $(CMD_PATH)

# ---------- clean ----------
.PHONY: clean
clean:
	@echo "Cleaning binaries..."
	@rm -f $(APP_NAME) $(foreach p,$(PLATFORMS),$(APP_NAME)-$(word 1,$(subst /, ,$(p)))-$(word 2,$(subst /, ,$(p)))$(if $(filter windows,$(word 1,$(subst /, ,$(p)))),.exe,))

# ---------- help ----------
.PHONY: help
help:
	@echo "Usage:"
	@echo "  make              Build all platforms"
	@echo "  make build        Build for current OS/arch"
	@echo "  make run          Run locally (go run)"
	@echo ""
	@echo "  make linux        Build linux/amd64 + linux/arm64"
	@echo "  make linux-amd64  Build linux/amd64"
	@echo "  make linux-arm64  Build linux/arm64"
	@echo ""
	@echo "  make darwin        Build darwin/amd64 + darwin/arm64"
	@echo "  make darwin-amd64  Build darwin/amd64 (Intel Mac)"
	@echo "  make darwin-arm64  Build darwin/arm64 (Apple Silicon)"
	@echo ""
	@echo "  make windows        Build windows/amd64 + windows/arm64"
	@echo "  make windows-amd64  Build windows/amd64"
	@echo "  make windows-arm64  Build windows/arm64"
	@echo ""
	@echo "  make build-all    Build all platforms"
	@echo "  make clean        Remove built binaries"
	@echo ""
	@echo "  make help         Show this help"
