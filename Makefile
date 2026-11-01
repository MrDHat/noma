VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/MrDHat/noma/internal/version.Version=$(VERSION)
BINARIES := noma noma-server noma-agent
BIN ?= bin

.PHONY: build test lint fmt dev-up dev-down deploy e2e

build:
	@mkdir -p $(BIN)
	@for b in $(BINARIES); do \
		CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/$$b ./cmd/$$b || exit 1; \
	done

test:
	go test -race ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

dev-up:
	hack/dev-up.sh

deploy:
	hack/deploy.sh

dev-down:
	hack/dev-down.sh

e2e:
	go test -tags e2e -count=1 -v ./test/e2e/...
