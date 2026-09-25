.PHONY: all build build-all build-linux-amd64 build-linux-arm64 test test-e2e vet fmt clean

GO ?= /usr/local/go/bin/go
GOFMT ?= gofmt
DIST_DIR := dist

all: fmt vet test-e2e build-all

$(DIST_DIR):
	mkdir -p $(DIST_DIR)

build: $(DIST_DIR)
	CGO_ENABLED=0 $(GO) build -o $(DIST_DIR)/x-agent ./cmd/x-agent

build-linux-amd64: $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -o $(DIST_DIR)/x-agent-linux-amd64 ./cmd/x-agent

build-linux-arm64: $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -o $(DIST_DIR)/x-agent-linux-arm64 ./cmd/x-agent

build-all: build build-linux-amd64 build-linux-arm64

test:
	$(GO) test -v ./...

test-e2e:
	$(GO) test -v ./tests/e2e/...

vet:
	$(GO) vet ./...

fmt:
	$(GOFMT) -s -w .

clean:
	rm -rf $(DIST_DIR)
