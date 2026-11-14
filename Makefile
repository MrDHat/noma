VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/MrDHat/noma/internal/version.Version=$(VERSION)
BINARIES := noma noma-server noma-agent
BIN ?= bin

.PHONY: build test lint fmt dev-up daily-up dev-down deploy e2e demo-real aws-up verify verify-lifecycle verify-credentials verify-teardown check-claude-login

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

# The dogfood cluster, from a published release: make daily-up VERSION=v0.3.0
daily-up:
	hack/daily-up.sh $(VERSION)

dev-down:
	hack/dev-down.sh

demo-real:
	hack/demo-real.sh

e2e:
	go test -tags e2e -count=1 -v ./test/e2e/...

# The EKS cluster and its prerequisites (infra/aws). It costs money while
# it runs.
aws-up:
	hack/aws-up.sh

verify: verify-lifecycle verify-credentials verify-teardown

verify-lifecycle:
	hack/verify-model.sh lifecycle

verify-credentials:
	hack/verify-model.sh credentials

verify-teardown:
	hack/verify-model.sh teardown

# Linux only. CLAUDE=pinned|latest|VERSION
check-claude-login:
	hack/check-claude-login-dir.sh $(or $(CLAUDE),latest)
