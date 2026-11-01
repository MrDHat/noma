VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/MrDHat/noma/internal/version.Version=$(VERSION)
BINARIES := noma noma-server noma-agent

.PHONY: build test lint fmt dev-up dev-down e2e

build:
	@mkdir -p bin
	@for b in $(BINARIES); do \
		CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$$b ./cmd/$$b || exit 1; \
	done

test:
	go test -race ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

dev-up:
	hack/dev-up.sh

dev-down:
	hack/dev-down.sh

e2e:
	go test -tags e2e -count=1 -v ./test/e2e/...
