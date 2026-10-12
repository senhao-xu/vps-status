GO ?= go
PNPM ?= pnpm
VERSION ?= dev
COMMIT ?= unknown
LDFLAGS := -s -w -X github.com/senhao-xu/vps-status/internal/versioninfo.Version=$(VERSION) -X github.com/senhao-xu/vps-status/internal/versioninfo.Commit=$(COMMIT)

.PHONY: build build-go build-web release fmt lint test test-go test-web vet security-smoke load-smoke clean

build: build-web
	mkdir -p bin
	$(GO) build -tags webui -ldflags "$(LDFLAGS)" -o bin/control ./cmd/control
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/agent ./cmd/agent

build-go:
	mkdir -p bin
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/control ./cmd/control
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/agent ./cmd/agent

build-web:
	$(PNPM) --dir web build

release:
	test "$(VERSION)" != "dev"
	sh scripts/release.sh "$(VERSION)" "$(COMMIT)" dist

fmt:
	$(GO) fmt ./...

lint: vet
	$(PNPM) --dir web lint
	$(PNPM) --dir web typecheck

test: test-go test-web

test-go:
	$(GO) test ./...

test-web:
	$(PNPM) --dir web test

vet:
	$(GO) vet ./...

security-smoke:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
	$(PNPM) --dir web audit --prod

load-smoke:
	$(GO) run ./tests/load --agents 5 --tasks-per-agent 4 --duration 15s

clean:
	$(GO) clean
	rm -rf bin dist internal/control/webui/dist web/coverage
