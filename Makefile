GOFILES := $(shell find . -name '*.go' -not -path './vendor/*')
COVERAGE_THRESHOLD ?= 80.0
DOCKER_PLATFORM ?= linux/$(shell go env GOARCH)

.PHONY: build ci docker fmt fmt-check test test-cover tidy-check vet

build:
	go build .

ci: fmt-check tidy-check vet test-cover build docker

docker:
	@set -eu; \
	platform="$(DOCKER_PLATFORM)"; \
	os="$${platform%%/*}"; \
	arch="$${platform##*/}"; \
	context="$$(mktemp -d)"; \
	trap 'rm -rf "$$context"' EXIT INT TERM; \
	mkdir -p "$$context/$$platform"; \
	cp Dockerfile "$$context/Dockerfile"; \
	CGO_ENABLED=0 GOOS="$$os" GOARCH="$$arch" go build -o "$$context/$$platform/infoblox-exporter" .; \
	docker build --platform "$$platform" -t infoblox-exporter:local "$$context"

fmt:
	gofmt -w $(GOFILES)

fmt-check:
	@test -z "$$(gofmt -l $(GOFILES))"

test:
	go test ./...

test-cover:
	go test ./... -coverprofile=coverage.out -covermode=count
	go tool cover -func=coverage.out
	@go tool cover -func=coverage.out | awk -v threshold="$(COVERAGE_THRESHOLD)" '/^total:/ { coverage = $$3; sub(/%$$/, "", coverage); if (coverage + 0 < threshold + 0) { printf "coverage %.1f%% is below %.1f%% threshold\n", coverage, threshold; exit 1 } printf "coverage %.1f%% meets %.1f%% threshold\n", coverage, threshold }'

tidy-check:
	go mod tidy
	git diff --exit-code -- go.mod go.sum

vet:
	go vet ./...
