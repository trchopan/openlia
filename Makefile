.PHONY: build test lint compose-config skills-test smoke

build:
	go build -o openlia .

test:
	go test ./...
	go vet ./...
	python3 -m py_compile tests/smoke.py
	for script in profile/skills/*/scripts/*.py; do python3 "$$script" --self-test; done
	./tests/ops_test.sh

lint:
	gofmt -d main.go cli
	bash -n docker/secret-source.sh ops/*.sh ops/lib/*.sh tests/ops_test.sh
	shellcheck docker/secret-source.sh ops/*.sh ops/lib/*.sh tests/ops_test.sh

compose-config:
	docker compose -f docker/compose.yaml config --quiet

skills-test:
	for script in profile/skills/*/scripts/*.py; do python3 "$$script" --self-test; done

smoke:
	python3 tests/smoke.py --mode cli
