GOCLEAN=go clean

# -trimpath strips local filesystem paths (e.g. /home/<user>/...) from the
# binary — privacy + reproducibility. Used for every build.
BUILDFLAGS=-trimpath -ldflags="-s -w"

# the analysis suite, reused by `lint` (host) and `lint-windows` (GOOS=windows).
ANALYZE=go vet ./... && golangci-lint run ./... && govulncheck ./...

.DEFAULT_GOAL := help

help: ## Show this help (the default target)
	@echo "99dps — make targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[1;33m%-16s\033[0m %s\n", $$1, $$2}'

all: build clean ## Build then clean (a quick compile check)

build: ## Build the Linux binary (./99dps)
	go build $(BUILDFLAGS) -o 99dps .

# winres embeds Windows version metadata (ProductName/Version/Description, shown
# in Explorer → Properties and Task Manager) from versioninfo.json. The
# _windows_amd64 suffix scopes the .syso to that target, so non-Windows builds
# ignore it. No-op (with a note) if goversioninfo isn't installed.
winres: ## (internal) Generate the Windows version+icon resource
	@if command -v goversioninfo >/dev/null 2>&1; then \
		goversioninfo -64 -o resource_windows_amd64.syso versioninfo.json && echo "embedded version metadata"; \
	else \
		echo "goversioninfo not found — building without version metadata (run 'make tools')"; \
	fi

windows: winres ## Cross-compile the Windows binary (99dps.exe)
	GOOS=windows GOARCH=amd64 go build $(BUILDFLAGS) -o 99dps.exe .

release-windows: windows ## Windows binary + SHA-256 (for your own VirusTotal/records)
	@sha256sum 99dps.exe | tee 99dps.exe.sha256
	@echo "Built 99dps.exe. Before sending: scan at https://www.virustotal.com and share the .sha256."

dist-windows: windows ## Friend-ready zip: exe + plain-language readme (send this)
	@rm -rf dist 99dps-windows.zip && mkdir dist
	@cp 99dps.exe dist/
	@cp packaging/windows-readme.txt "dist/READ-ME-FIRST.txt"
	@cd dist && zip -q -r ../99dps-windows.zip .
	@echo "Created 99dps-windows.zip — scan 99dps.exe on virustotal.com, then send the zip."

clean: ## Remove all build artifacts
	${GOCLEAN}
	rm -rf dist 99dps-windows.zip
	rm -f 99dps 99dps.exe 99dps.exe.sha256 resource_windows_amd64.syso

test: ## Run all tests
	go test ./...

# static analysis + known-vuln scan. Requires the tools (run `make tools`).
# golangci-lint must be built with Go >= the go.mod version; `make tools`
# installs it with the local toolchain.
lint: ## Lint + vuln scan (host/Linux build)
	gofmt -l .
	$(ANALYZE)

lint-windows: export GOOS := windows
lint-windows: ## Lint + vuln scan the Windows build (host lint skips build-tagged files)
	$(ANALYZE)

tools: ## Install the dev tools (golangci-lint, govulncheck, goversioninfo)
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest

.PHONY: help all build winres windows release-windows dist-windows clean test lint lint-windows tools
