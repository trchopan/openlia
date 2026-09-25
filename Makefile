BUILD_DIR ?= dist
GO ?= go
VENV ?= .venv
PYTHON ?= $(shell if [ -f "$(VENV)/bin/python3" ]; then echo "$(VENV)/bin/python3"; else echo "python3"; fi)

.PHONY: help build build-host build-cli build-operator build-operator-linux-amd64 build-operator-linux-arm64 test lint compose-config venv bun-install bun-check bun-build skills-test skills-verify clean-logs smoke smoke-local smoke-local-live

help:
	@printf '%s\n' \
		'build              Build the host CLI and Linux operator artifacts' \
		'build-host         Build the host openlia CLI' \
		'build-cli          Alias for build-host' \
		'build-operator     Build Linux amd64 and arm64 operator artifacts' \
		'build-operator-linux-amd64  Build the Linux amd64 operator artifact' \
		'build-operator-linux-arm64  Build the Linux arm64 operator artifact' \
		'test               Run Go, Python, skill, and operator tests' \
		'venv               Create local virtualenv and install dependencies from requirements-dev.txt' \
		'skills-test        Run offline skill self-tests' \
		'skills-verify      Run bundled skill and browser relay tests' \
		'clean-logs         Purge local .playwright-mcp and verification test logs' \
		'deploy-dev         Verify skills and deploy to local dev stack' \
		'deploy-prod        Verify skills and promote to production stack' \
		'lint               Run formatting and shell checks' \
		'compose-config     Validate the base Compose file' \
		'smoke              Run provider-free static smoke checks' \
		'smoke-local        Deploy and remove a provider-free local stack' \
		'smoke-local-live   Deploy a credential-backed local stack; cleanup is opt-in'

build:
	$(MAKE) bun-install bun-build build-host build-operator

build-host build-cli: bun-install bun-build
	$(GO) build -o openlia .

build-operator: build-operator-linux-amd64 build-operator-linux-arm64

build-operator-linux-amd64:
	mkdir -p "$(BUILD_DIR)"
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -o "$(BUILD_DIR)/openlia-operator-linux-amd64" ./cmd/operator

build-operator-linux-arm64:
	mkdir -p "$(BUILD_DIR)"
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -o "$(BUILD_DIR)/openlia-operator-linux-arm64" ./cmd/operator

venv:
	@if command -v uv >/dev/null 2>&1; then \
		echo "Creating virtual environment using uv in $(VENV)..."; \
		uv venv $(VENV) && uv pip install --python $(VENV)/bin/python -r requirements-dev.txt; \
	else \
		echo "Creating virtual environment using python3 -m venv in $(VENV)..."; \
		python3 -m venv $(VENV) && $(VENV)/bin/pip install -r requirements-dev.txt; \
	fi
	@echo "Virtual environment ready in $(VENV)."

test:
	$(MAKE) bun-install bun-check bun-build
	go test ./...
	go vet ./...
	$(PYTHON) -m py_compile tests/smoke.py
	for script in profile/skills/*/scripts/*.py; do $(PYTHON) "$$script" --self-test; done

bun-install:
	bun install --frozen-lockfile --ignore-scripts

bun-check:
	bun run check
	bun run typecheck
	bun run test

bun-build:
	bun run build

lint:
	bun run format:check
	bun run lint
	gofmt -d main.go cli operator cmd
	bash -n docker/*.sh profile/cron/scripts/*.sh
	shellcheck docker/*.sh profile/cron/scripts/*.sh

compose-config:
	OPENLIA_DATA_ROOT=/tmp/openlia-compose-check/runtime/hermes \
	OPENLIA_SYSTEM_SKILLS_ROOT=/tmp/openlia-compose-check/runtime/system-skills \
	OPENLIA_SKILLS_CACHE_ROOT=/tmp/openlia-compose-check/runtime/skill-cache \
	OPENLIA_SKILLS_ENV_ROOT=/tmp/openlia-compose-check/runtime/skill-envs \
	OPENLIA_SECRET_DIR=/tmp/openlia-compose-check/runtime/secrets \
	OPENLIA_NETWORK_NAME=openlia-compose-check-private \
	docker compose -f docker/compose.yaml config --quiet

skills-test:
	$(PYTHON) tests/verify_skills.py --offline

skills-verify:
	$(PYTHON) tests/verify_skills.py --offline
	bun test packages/openlia-browser

clean-logs:
	rm -rf .playwright-mcp/ /tmp/openlia_verify/

deploy-dev: skills-test
	OPENLIA_CONFIG=$$HOME/.config/openlia/dev/openlia_dev.toml ./openlia deploy

deploy-prod: skills-test
	OPENLIA_CONFIG=$$HOME/.config/openlia/config.toml ./openlia update openlia

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
