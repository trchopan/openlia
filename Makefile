BUILD_DIR ?= dist
GO ?= go

.PHONY: help build build-host build-cli build-operator build-operator-linux-amd64 build-operator-linux-arm64 test lint compose-config skills-test smoke smoke-local smoke-local-live

help:
	@printf '%s\n' \
		'build              Build the host CLI and Linux operator artifacts' \
		'build-host         Build the host openlia CLI' \
		'build-cli          Alias for build-host' \
		'build-operator     Build Linux amd64 and arm64 operator artifacts' \
		'build-operator-linux-amd64  Build the Linux amd64 operator artifact' \
		'build-operator-linux-arm64  Build the Linux arm64 operator artifact' \
		'test               Run Go, Python, skill, and operator tests' \
		'lint               Run formatting and shell checks' \
		'compose-config     Validate the base Compose file' \
		'smoke              Run provider-free static smoke checks' \
		'smoke-local        Deploy and remove a provider-free local stack' \
		'smoke-local-live   Deploy a credential-backed local stack; cleanup is opt-in'

build:
	$(MAKE) build-host build-operator

build-host build-cli:
	$(GO) build -o openlia .

build-operator: build-operator-linux-amd64 build-operator-linux-arm64

build-operator-linux-amd64:
	mkdir -p "$(BUILD_DIR)"
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -o "$(BUILD_DIR)/openlia-operator-linux-amd64" ./cmd/operator

build-operator-linux-arm64:
	mkdir -p "$(BUILD_DIR)"
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -o "$(BUILD_DIR)/openlia-operator-linux-arm64" ./cmd/operator

test:
	go test ./...
	go vet ./...
	python3 -m py_compile tests/smoke.py
	for script in profile/skills/*/scripts/*.py; do python3 "$$script" --self-test; done

lint:
	gofmt -d main.go cli operator cmd
	bash -n docker/*.sh profile/cron/scripts/*.sh
	shellcheck docker/*.sh profile/cron/scripts/*.sh

compose-config:
	docker compose -f docker/compose.yaml config --quiet

skills-test:
	for script in profile/skills/*/scripts/*.py; do python3 "$$script" --self-test; done

smoke:
	python3 tests/smoke.py --mode cli

smoke-local:
	python3 tests/smoke.py --mode local

smoke-local-live:
	@test -n "$(OPENLIA_SMOKE_ENV_FILE)" || (printf '%s\n' 'OPENLIA_SMOKE_ENV_FILE is required' >&2; exit 2)
	@test -n "$(OPENLIA_SMOKE_ATTACHMENTS_FILE)" || (printf '%s\n' 'OPENLIA_SMOKE_ATTACHMENTS_FILE is required' >&2; exit 2)
	@test -n "$(OPENLIA_SMOKE_LOCHO_HOST)" || (printf '%s\n' 'OPENLIA_SMOKE_LOCHO_HOST is required' >&2; exit 2)
	python3 tests/smoke.py --mode live --local \
		--project "$(or $(OPENLIA_SMOKE_PROJECT),openlia_smoke)" \
		--root "$(or $(OPENLIA_SMOKE_ROOT),/tmp/openlia_smoke)" \
		--env-file "$(OPENLIA_SMOKE_ENV_FILE)" \
		--attachments-file "$(OPENLIA_SMOKE_ATTACHMENTS_FILE)" \
		--locho-host "$(OPENLIA_SMOKE_LOCHO_HOST)"
