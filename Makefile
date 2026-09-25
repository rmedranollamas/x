.PHONY: all build build-all build-linux-amd64 build-linux-arm64 install test test-e2e vet fmt clean

GO ?= $(shell command -v go 2>/dev/null || echo /usr/local/go/bin/go)
GOFMT ?= $(shell command -v gofmt 2>/dev/null || echo /usr/local/go/bin/gofmt)
DIST_DIR := dist

all: fmt vet test-e2e build-all

$(DIST_DIR):
	mkdir -p $(DIST_DIR)

build: $(DIST_DIR)
	CGO_ENABLED=0 $(GO) build -o $(DIST_DIR)/x-agent ./cmd/x-agent
	cp $(DIST_DIR)/x-agent ./x-agent

build-linux-amd64: $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -o $(DIST_DIR)/x-agent-linux-amd64 ./cmd/x-agent

build-linux-arm64: $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -o $(DIST_DIR)/x-agent-linux-arm64 ./cmd/x-agent

build-all: build build-linux-amd64 build-linux-arm64

install:
	CGO_ENABLED=0 $(GO) install ./cmd/x-agent

test:
	$(GO) test -v ./...

test-e2e:
	$(GO) test -v ./tests/e2e/...

vet:
	$(GO) vet ./...

fmt:
	$(GOFMT) -s -w .

clean:
	rm -rf $(DIST_DIR) ./x-agent
