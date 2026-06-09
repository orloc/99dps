GOCMD=go
GOBUILD=${GOCMD} build
GOCLEAN=${GOCMD} clean

all: build clean

build:
	go build -o 99dps -ldflags="-s -w" .
clean:
	${GOCLEAN}
	rm -f 99dps

test:
	go test ./...

# static analysis + known-vuln scan. Requires the tools (run `make tools`).
# golangci-lint must be built with Go >= the go.mod version; `make tools`
# installs it with the local toolchain.
lint:
	gofmt -l .
	go vet ./...
	golangci-lint run ./...
	govulncheck ./...

tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest

.PHONY: all build clean test lint tools
