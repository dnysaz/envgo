APP     := envgo
GO      ?= go
# Every consumer (banner, install.sh, Install_envGo.command, dist/VERSION) prints
# "envGo v<VERSION>", so the variable itself must not carry the leading "v" or the
# released binary shows "envGo vv1.0.4". Strip it here, at the single source.
VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//' || date +%Y%m%d)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOBUILD := CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags="$(LDFLAGS)"
ARTIFACT_DIR ?= dist

.PHONY: build test vet clean release release-linux-windows dist-macos dist-macos-binaries manifest tools/upx install-upx install sizes version

build:
	$(GOBUILD) -o $(APP) .

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

clean:
	rm -f $(APP) $(APP).exe

# Single source of truth for the version. The compiled-in default in main.go is
# "dev"; this is what every artifact and every installer reads.
version:
	@printf '%s\n' "$(VERSION)"

install:
	./scripts/install.sh

tools/upx install-upx:
	./scripts/install-upx.sh

release:
	@$(MAKE) --no-print-directory release-linux-windows

# Rebuild the two macOS zips from the already-verified dist/ binaries. Each zip
# ships SHA256SUMS alongside the binary so Install_envGo.command can verify the
# download instead of trusting it.
# Build both macOS binaries. release-linux-windows deliberately never touches
# them, which means a carried-forward macOS binary can silently go stale against
# the linux/windows ones — so the macOS half of a release is done here, on a Mac.
# CGO is disabled, so this cross-compiles arm64 from an Intel Mac and vice versa.
dist-macos-binaries:
	@set -eu; \
	host_os="$$($(GO) env GOHOSTOS)"; \
	test "$$host_os" = darwin || { echo "dist-macos-binaries must run on macOS (GOHOSTOS=$$host_os)" >&2; exit 1; }; \
	for arch in amd64 arm64; do \
		bin="$(ARTIFACT_DIR)/$(APP)-darwin-$$arch"; \
		tmp="$$bin.tmp"; \
		GOOS=darwin GOARCH=$$arch $(GOBUILD) -o "$$tmp" .; \
		metadata="$$($(GO) version -m "$$tmp")"; \
		case "$$metadata" in *"GOOS=darwin"*) ;; *) echo "Wrong target OS for $$bin" >&2; rm -f "$$tmp"; exit 1;; esac; \
		case "$$metadata" in *"GOARCH=$$arch"*) ;; *) echo "Wrong target architecture for $$bin" >&2; rm -f "$$tmp"; exit 1;; esac; \
		mv "$$tmp" "$$bin"; \
		echo "built $(ARTIFACT_DIR)/$(APP)-darwin-$$arch"; \
	done

# Repack the two macOS zips from the binaries in dist/, then refresh the manifest.
# Each zip ships SHA256SUMS next to the binary so Install_envGo.command can
# verify the download instead of trusting it.
dist-macos: dist-macos-binaries
	@set -eu; \
	hash_file() { \
		if command -v sha256sum >/dev/null 2>&1; then sha256sum "$$1"; else shasum -a 256 "$$1"; fi; \
	}; \
	verify_manifest() { \
		if command -v sha256sum >/dev/null 2>&1; then sha256sum -c "$$1" >/dev/null; else shasum -a 256 -c "$$1" >/dev/null; fi; \
	}; \
	out_abs="$$(cd "$(ARTIFACT_DIR)" && pwd -P)"; \
	stage=""; \
	cleanup() { if [ -n "$$stage" ]; then rm -rf "$$stage"; fi; }; \
	trap cleanup 0; \
	for arch in arm64 amd64; do \
		bin="$$out_abs/$(APP)-darwin-$$arch"; \
		test -f "$$bin" || { echo "Missing $(ARTIFACT_DIR)/$(APP)-darwin-$$arch" >&2; exit 1; }; \
		if [ "$$arch" = "arm64" ]; then label="AppleSilicon"; else label="Intel"; fi; \
		zipfile="envGo-macOS-$$label.zip"; \
		stage="$$(mktemp -d "$$out_abs/.macos.XXXXXX")"; \
		cp -p "$$bin" "$$stage/$(APP)-darwin-$$arch"; \
		cp -p Install_envGo.command README.md "$$stage/"; \
		(cd "$$stage" && : > SHA256SUMS && hash_file "$(APP)-darwin-$$arch" >> SHA256SUMS); \
		(cd "$$stage" && verify_manifest SHA256SUMS); \
		rm -f "$$out_abs/$$zipfile"; \
		(cd "$$stage" && zip -q -r -X -D "$$out_abs/$$zipfile" . \
			-x '.DS_Store' -x '*/.DS_Store' -x '__MACOSX' -x '__MACOSX/*'); \
		rm -rf "$$stage"; \
		stage=""; \
		echo "packaged $(ARTIFACT_DIR)/$$zipfile"; \
	done; \
	$(MAKE) --no-print-directory -s manifest ARTIFACT_DIR="$(ARTIFACT_DIR)"

# Recompute dist/SHA256SUMS from whatever is currently in dist/. The release
# target writes the manifest as part of building; this is the standalone path
# for when an artifact changed afterwards (repackaged zips, a new VERSION file).
# Verify from the repository root: shasum -a 256 -c dist/SHA256SUMS
ARTIFACTS = envgo-darwin-amd64 envgo-darwin-arm64 envGo-macOS-AppleSilicon.zip envGo-macOS-Intel.zip envgo-linux-amd64 envgo-linux-arm64 envgo-windows-amd64.exe envgo-windows-arm64.exe Install_envGo.ps1 VERSION

manifest:
	@set -eu; \
	if command -v sha256sum >/dev/null 2>&1; then checksum="sha256sum"; \
	elif command -v shasum >/dev/null 2>&1; then checksum="shasum"; \
	else echo "sha256sum or shasum is required" >&2; exit 1; fi; \
	hash_file() { \
		local output; \
		if [ "$$checksum" = "sha256sum" ]; then output="$$(sha256sum "$$1")"; else output="$$(shasum -a 256 "$$1")"; fi; \
		printf '%s\n' "$${output%% *}"; \
	}; \
	tmp="$(ARTIFACT_DIR)/.SHA256SUMS.new"; \
	: > "$$tmp"; \
	for name in $(ARTIFACTS); do \
		test -f "$(ARTIFACT_DIR)/$$name" || { echo "Missing release artifact: $$name" >&2; rm -f "$$tmp"; exit 1; }; \
		hash="$$(hash_file "$(ARTIFACT_DIR)/$$name")"; \
		printf '%s  %s\n' "$$hash" "$(ARTIFACT_DIR)/$$name" >> "$$tmp"; \
	done; \
	mv "$$tmp" "$(ARTIFACT_DIR)/SHA256SUMS"; \
	echo "wrote $(ARTIFACT_DIR)/SHA256SUMS ($(words $(ARTIFACTS)) entries)"; \
	if [ "$$checksum" = "sha256sum" ]; then sha256sum -c "$(ARTIFACT_DIR)/SHA256SUMS" >/dev/null; else shasum -a 256 -c "$(ARTIFACT_DIR)/SHA256SUMS" >/dev/null; fi; \
	echo "verified"


release-linux-windows:
	@set -eu; \
	host_os="$$($(GO) env GOHOSTOS)"; \
	host_arch="$$($(GO) env GOHOSTARCH)"; \
	GOOS="$$host_os" GOARCH="$$host_arch" $(GO) test ./...; \
	GOOS="$$host_os" GOARCH="$$host_arch" $(GO) vet ./...; \
	mkdir -p "$(ARTIFACT_DIR)"; \
	test -f "$(ARTIFACT_DIR)/SHA256SUMS" || { echo "Missing $(ARTIFACT_DIR)/SHA256SUMS" >&2; exit 1; }; \
	lock="$(ARTIFACT_DIR).lock"; \
	stage=""; \
	if ! mkdir "$$lock"; then echo "Cannot acquire release lock: $$lock" >&2; exit 1; fi; \
	cleanup() { \
		status="$$?"; \
		trap - 0; \
		if [ -n "$$stage" ]; then rm -rf "$$stage"; fi; \
		rmdir "$$lock" 2>/dev/null || true; \
		exit "$$status"; \
	}; \
	trap cleanup 0; \
	trap 'exit 129' 1; \
	trap 'exit 130' 2; \
	trap 'exit 143' 15; \
	stage="$$(mktemp -d "$(ARTIFACT_DIR).release.XXXXXX")"; \
	stage_abs="$$(cd "$$stage" && pwd -P)"; \
	cp -pR "$(ARTIFACT_DIR)/." "$$stage/"; \
	rm -f "$$stage"/envgo-linux-* "$$stage"/envgo-windows-* "$$stage"/Install_envGo.ps1; \
	find "$$stage" -type f -name '.DS_Store' -delete; \
	find "$$stage" -type d -name '__MACOSX' -prune -exec rm -rf {} + 2>/dev/null || true; \
	test -z "$$(find "$$stage" -name '.DS_Store' -o -name '__MACOSX')" || { echo "Staging directory still holds macOS metadata" >&2; exit 1; }; \
	printf '%s\n' "$(VERSION)" > "$$stage/VERSION"; \
	for spec in linux/amd64 linux/arm64 windows/amd64 windows/arm64; do \
		os=$${spec%/*}; arch=$${spec#*/}; ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		bin="$$stage/$(APP)-$$os-$$arch$$ext"; \
		tmp="$$bin.tmp"; \
		GOOS=$$os GOARCH=$$arch $(GOBUILD) -o "$$tmp" .; \
		metadata="$$($(GO) version -m "$$tmp")"; \
		case "$$metadata" in *"GOOS=$$os"*) ;; *) echo "Wrong target OS for $$bin" >&2; exit 1;; esac; \
		case "$$metadata" in *"GOARCH=$$arch"*) ;; *) echo "Wrong target architecture for $$bin" >&2; exit 1;; esac; \
		mv "$$tmp" "$$bin"; \
	done; \
	cp scripts/install.ps1 "$$stage/Install_envGo.ps1"; \
	checksum=""; \
	if command -v sha256sum >/dev/null 2>&1; then \
		checksum="sha256sum"; \
	elif command -v shasum >/dev/null 2>&1; then \
		checksum="shasum"; \
	else \
		echo "sha256sum or shasum is required" >&2; \
		exit 1; \
	fi; \
	hash_file() { \
		local output; \
		if [ "$$checksum" = "sha256sum" ]; then \
			output="$$(sha256sum "$$1")"; \
		else \
			output="$$(shasum -a 256 "$$1")"; \
		fi; \
		printf '%s\n' "$${output%% *}"; \
	}; \
	mac_expected="$$stage/.mac-expected.tmp"; \
	mac_entries="$$stage/.mac-entries.tmp"; \
	mac_entries_path="$$stage_abs/.mac-entries.tmp"; \
	printf '%s\n' envgo-darwin-amd64 envgo-darwin-arm64 envGo-macOS-AppleSilicon.zip envGo-macOS-Intel.zip > "$$mac_expected"; \
	awk 'NR == FNR { expected[$$0] = 1; next } { hash = substr($$0, 1, 64); name = substr($$0, 67); sub(/^.*[\\\/]/, "", name); if (name in expected) print hash, name }' "$$mac_expected" "$(ARTIFACT_DIR)/SHA256SUMS" > "$$mac_entries"; \
	awk '{ print $$1 "  " $$2 }' "$$mac_entries" > "$$mac_entries.canonical"; \
	mv "$$mac_entries.canonical" "$$mac_entries"; \
	LC_ALL=C sort "$$mac_expected" > "$$stage/.mac-expected.sorted"; \
	awk '{ print $$2 }' "$$mac_entries" | LC_ALL=C sort > "$$stage/.mac-actual.sorted"; \
	cmp "$$stage/.mac-expected.sorted" "$$stage/.mac-actual.sorted" || { echo "Existing macOS checksum entries are incomplete" >&2; exit 1; }; \
	if [ "$$checksum" = "sha256sum" ]; then \
		(cd "$(ARTIFACT_DIR)" && sha256sum -c "$$mac_entries_path") >/dev/null; \
	else \
		(cd "$(ARTIFACT_DIR)" && shasum -a 256 -c "$$mac_entries_path") >/dev/null; \
	fi; \
	: > "$$stage/SHA256SUMS"; \
	while read -r hash name; do printf '%s  %s\n' "$$hash" "$(ARTIFACT_DIR)/$$name" >> "$$stage/SHA256SUMS"; done < "$$mac_entries"; \
	for name in envgo-linux-amd64 envgo-linux-arm64 envgo-windows-amd64.exe envgo-windows-arm64.exe Install_envGo.ps1 VERSION; do \
		test -f "$$stage/$$name" || { echo "Missing release artifact: $$name" >&2; exit 1; }; \
		hash="$$(hash_file "$$stage/$$name")"; \
		printf '%s  %s\n' "$$hash" "$(ARTIFACT_DIR)/$$name" >> "$$stage/SHA256SUMS"; \
	done; \
	: > "$$stage/.SHA256SUMS.stage"; \
	for name in envgo-darwin-amd64 envgo-darwin-arm64 envGo-macOS-AppleSilicon.zip envGo-macOS-Intel.zip envgo-linux-amd64 envgo-linux-arm64 envgo-windows-amd64.exe envgo-windows-arm64.exe Install_envGo.ps1 VERSION; do \
		hash="$$(hash_file "$$stage/$$name")"; \
		printf '%s  %s\n' "$$hash" "$$name" >> "$$stage/.SHA256SUMS.stage"; \
	done; \
	if [ "$$checksum" = "sha256sum" ]; then \
		(cd "$$stage" && sha256sum -c .SHA256SUMS.stage >/dev/null); \
	else \
		(cd "$$stage" && shasum -a 256 -c .SHA256SUMS.stage >/dev/null); \
	fi; \
	rm -f "$$stage/.mac-expected.tmp" "$$stage/.mac-entries.tmp" "$$stage/.mac-expected.sorted" "$$stage/.mac-actual.sorted" "$$stage/.SHA256SUMS.stage"; \
	backup="$(ARTIFACT_DIR).previous.$$$$"; \
	rm -rf "$$backup"; \
	mv "$(ARTIFACT_DIR)" "$$backup"; \
	if mv "$$stage" "$(ARTIFACT_DIR)"; then \
		stage=""; \
		rm -rf "$$backup"; \
	else \
		mv "$$backup" "$(ARTIFACT_DIR)"; \
		stage=""; \
		exit 1; \
	fi; \
	trap - 0; \
	rmdir "$$lock"; \
	$(MAKE) --no-print-directory -s sizes ARTIFACT_DIR="$(ARTIFACT_DIR)"

sizes:
	@echo ""
	@printf "%-28s %10s\n" "ARTIFACT" "SIZE"
	@printf "%-28s %10s\n" "----------------------------" "----------"
	@set -eu; found=0; \
	for f in "$(ARTIFACT_DIR)"/$(APP)-*; do \
		[ -f "$$f" ] || continue; \
		found=1; \
		bytes=$$(wc -c < "$$f"); \
		printf "%-28s %9.2f MB\n" "$$(basename "$$f")" \
			"$$(awk -v b="$$bytes" 'BEGIN { printf "%.2f", b/1048576 }')"; \
	done; \
	[ "$$found" -eq 1 ] || { echo "No envgo artifacts found in $(ARTIFACT_DIR)" >&2; exit 1; }
