VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X github.com/arcanflows/cli/cmd.Version=$(VERSION)"

.PHONY: build install clean test all completions checksums release

# Build for current platform
build:
	go build $(LDFLAGS) -o bin/arcanflows .

# Install to GOPATH/bin
install:
	go install $(LDFLAGS) .

# Build for all platforms
all: clean
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/arcanflows-darwin-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/arcanflows-darwin-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/arcanflows-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/arcanflows-linux-arm64 .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/arcanflows-windows-amd64.exe .

# Generate shell completions
completions: build
	mkdir -p bin/completions
	./bin/arcanflows completion bash > bin/completions/arcanflows.bash
	./bin/arcanflows completion zsh > bin/completions/_arcanflows
	./bin/arcanflows completion fish > bin/completions/arcanflows.fish

# Generate checksums for release
checksums:
	cd bin && sha256sum arcanflows-* > checksums.txt

# Full release build: all platforms + checksums
release: all checksums
	@echo ""
	@echo "Release binaries ready in bin/"
	@ls -lh bin/arcanflows-*
	@echo ""
	@cat bin/checksums.txt

# Run tests
test:
	go test ./...

# Clean build artifacts
clean:
	rm -rf bin/

# Run locally
run:
	go run . $(ARGS)

# Tidy dependencies
tidy:
	go mod tidy
