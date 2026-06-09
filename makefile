GOCMD=go
GOBUILD=${GOCMD} build
GOCLEAN=${GOCMD} clean

# -trimpath strips local filesystem paths (e.g. /home/<user>/...) from the
# binary — privacy + reproducibility. Used for every build.
BUILDFLAGS=-trimpath -ldflags="-s -w"

all: build clean

build:
	go build $(BUILDFLAGS) -o 99dps .

# winres embeds Windows version metadata (ProductName/Version/Description, shown
# in Explorer → Properties and Task Manager) from versioninfo.json. The
# _windows_amd64 suffix scopes the .syso to that target, so non-Windows builds
# ignore it. No-op (with a note) if goversioninfo isn't installed.
winres:
	@if command -v goversioninfo >/dev/null 2>&1; then \
		goversioninfo -64 -o resource_windows_amd64.syso versioninfo.json && echo "embedded version metadata"; \
	else \
		echo "goversioninfo not found — building without version metadata (run 'make tools')"; \
	fi

# cross-compile a Windows binary (pure Go, no cgo — runs anywhere).
windows: winres
	GOOS=windows GOARCH=amd64 go build $(BUILDFLAGS) -o 99dps.exe .

# a distributable Windows build: binary + SHA-256 for the recipient to verify.
release-windows: windows
	@sha256sum 99dps.exe | tee 99dps.exe.sha256
	@echo "Built 99dps.exe. Before sending: scan at https://www.virustotal.com and share the .sha256."

# a friend-ready zip: just the exe + plain-language instructions (no checksum —
# a non-technical recipient doesn't need it). Send 99dps-windows.zip.
dist-windows: windows
	@rm -rf dist 99dps-windows.zip && mkdir dist
	@cp 99dps.exe dist/
	@cp packaging/windows-readme.txt "dist/READ-ME-FIRST.txt"
	@cd dist && zip -q -r ../99dps-windows.zip .
	@echo "Created 99dps-windows.zip — scan 99dps.exe on virustotal.com, then send the zip."

clean:
	${GOCLEAN}
	rm -rf dist 99dps-windows.zip
	rm -f 99dps 99dps.exe 99dps.exe.sha256 resource_windows_amd64.syso

test:
	go test ./...

# static analysis + known-vuln scan. Requires the tools (run `make tools`).
# golangci-lint must be built with Go >= the go.mod version; `make tools`
# installs it with the local toolchain. Run `make lint-windows` to analyze the
# build-tagged Windows code, which the host lint skips.
lint:
	gofmt -l .
	go vet ./...
	golangci-lint run ./...
	govulncheck ./...

lint-windows:
	GOOS=windows go vet ./...
	GOOS=windows golangci-lint run ./...
	GOOS=windows govulncheck ./...

tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest

.PHONY: all build winres windows release-windows dist-windows clean test lint lint-windows tools
