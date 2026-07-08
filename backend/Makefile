.PHONY: build generate test test-unit test-integration test-e2e test-invariants

VERSION ?= $(shell ./scripts/resolve-version.sh)
LDFLAGS ?= -s -w -X main.Version=$(VERSION)

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -trimpath -o bin/server ./cmd/server

generate:
	go generate ./ent
	go generate ./cmd/server

test:
	go test ./...
	golangci-lint run ./...

test-unit:
	go test -tags=unit ./...

# 回归防线定向圈定（不变量/特征化测试）。CI 的 test-unit 已包含这些测试，
# 本目标用于本地开发与重构 PR 审计时快速验证行为锁未被破坏。
test-invariants:
	go test -tags=unit -count=1 -run 'Invariant|Characterization' ./internal/...

test-integration:
	go test -tags=integration ./...

test-e2e:
	./scripts/e2e-test.sh

test-e2e-local:
	go test -tags=e2e -v -timeout=300s ./internal/integration/...
