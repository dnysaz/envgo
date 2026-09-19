APP     := envgo
VERSION ?= $(shell git describe --tags --always 2>/dev/null || date +%Y%m%d)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOBUILD := CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)"

# Prefer the locally built/downloaded UPX, fall back to one on PATH.
UPX := $(shell if [ -x tools/upx ]; then echo tools/upx; else command -v upx 2>/dev/null; fi)

.PHONY: build test vet clean release tools/upx install-upx install

build:
	$(GOBUILD) -o $(APP) .

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(APP)
	rm -rf dist

# Interactive installer (builds + installs to ~/.local/bin/envgo, shows banner + help).
install:
	./scripts/install.sh

# Build (if needed) a host UPX in ./tools/upx.
tools/upx install-upx:
	./scripts/install-upx.sh

# Build every release target. UPX is applied to Linux/Windows only:
# UPX-compressed Mach-O binaries break macOS code signing on Apple Silicon.
release:
	@mkdir -p dist
	@if [ -z "$(UPX)" ]; then \
		echo "WARN: upx not found; run 'make install-upx' for smaller binaries (skipping compression)"; \
	fi
	@set -e; for spec in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do \
		os=$${spec%/*}; arch=$${spec#*/}; ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		bin=dist/$(APP)-$$os-$$arch$$ext; \
		GOOS=$$os GOARCH=$$arch $(GOBUILD) -o $$bin .; \
		if [ "$$os" != "darwin" ] && [ -n "$(UPX)" ]; then \
			$(UPX) --best --lzma --quiet $$bin || echo "WARN: upx failed for $$spec"; \
		fi; \
	done
	@$(MAKE) --no-print-directory -s sizes

sizes:
	@echo ""
	@printf "%-28s %10s\n" "ARTIFACT" "SIZE"
	@printf "%-28s %10s\n" "----------------------------" "----------"
	@for f in dist/$(APP)-*; do \
		bytes=$$(wc -c < "$$f"); \
		printf "%-28s %9.2f MB\n" "$$(basename $$f)" \
			"$$(awk -v b="$$bytes" 'BEGIN { printf "%.2f", b/1048576 }')"; \
	done