.PHONY: help build test lint compose-config skills-test smoke smoke-local smoke-local-live

help:
	@printf '%s\n' \
		'build              Build the openlia CLI' \
		'test               Run Go, Python, skill, and operations tests' \
		'lint               Run formatting and shell checks' \
		'compose-config     Validate the base Compose file' \
		'smoke              Run provider-free static smoke checks' \
		'smoke-local        Deploy and remove a provider-free local stack' \
		'smoke-local-live   Deploy a credential-backed local stack; cleanup is opt-in'

build:
	go build -o openlia .

test:
	go test ./...
	go vet ./...
	python3 -m py_compile tests/smoke.py
	for script in profile/skills/*/scripts/*.py; do python3 "$$script" --self-test; done
	./tests/ops_test.sh
	./tests/workspace_git_test.sh

lint:
	gofmt -d main.go cli
	bash -n docker/*.sh ops/*.sh ops/lib/*.sh profile/cron/scripts/*.sh tests/ops_test.sh tests/workspace_git_test.sh
	shellcheck docker/*.sh ops/*.sh ops/lib/*.sh profile/cron/scripts/*.sh tests/ops_test.sh tests/workspace_git_test.sh

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
