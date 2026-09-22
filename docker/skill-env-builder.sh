#!/bin/sh
set -eu

action=${OPENLIA_SKILL_ACTION:-${1:-}}
source_path=${OPENLIA_SKILL_SOURCE:-}
environment_path=${OPENLIA_SKILL_ENV:-}

case "$source_path" in
  /opt/openlia/skill-cache/*|/opt/openlia/migration-source/*) ;;
  *) printf '%s\n' 'invalid skill source path' >&2; exit 2 ;;
esac
case "$environment_path" in
  /opt/openlia/skill-envs/*) ;;
  *) printf '%s\n' 'invalid skill environment path' >&2; exit 2 ;;
esac

if [ "$action" = audit ]; then
  mkdir -p "$HOME" "$environment_path"
  case ${OPENLIA_SKILL_DEPENDENCY:-} in
    python)
      test -f "$source_path/pyproject.toml"
      test -f "$source_path/uv.lock"
      UV_PROJECT_ENVIRONMENT="$environment_path/.venv" \
        uv sync --frozen --no-dev --no-install-project --project "$source_path"
      requirements=$(mktemp)
      uv export --frozen --no-dev --no-emit-project --project "$source_path" --output-file "$requirements"
      pip-audit --requirement "$requirements" --progress-spinner off
      rm -f "$requirements"
      ;;
    javascript)
      test -f "$source_path/package.json"
      test -f "$source_path/bun.lock"
      work=$(mktemp -d)
      trap 'rm -rf "$work"' EXIT INT TERM
      cp "$source_path/package.json" "$source_path/bun.lock" "$work/"
      bun install --frozen-lockfile --production --ignore-scripts --cwd "$work"
      bun audit --production --cwd "$work"
      cp -R "$work/." "$environment_path/"
      ;;
    *) printf '%s\n' 'invalid dependency kind' >&2; exit 2 ;;
  esac
  exit 0
fi

if [ "$action" = test ]; then
  test_json=${OPENLIA_SKILL_TEST_JSON:-}
  test -n "$test_json"
  runtime_path="/usr/local/bin:/usr/bin:/bin"
  if [ -x "$environment_path/.venv/bin/python" ]; then
    runtime_path="$environment_path/.venv/bin:$runtime_path"
  fi
  if [ -d "$environment_path/node_modules/.bin" ]; then
    runtime_path="$environment_path/node_modules/.bin:$runtime_path"
  fi
  export OPENLIA_SKILL_RUNTIME_PATH="$runtime_path"
  exec python3 -I -c 'import json, os, subprocess, sys
command = json.loads(os.environ["OPENLIA_SKILL_TEST_JSON"])
if not isinstance(command, list) or not command or not all(isinstance(v, str) and v for v in command):
    raise SystemExit("invalid test command")
raise SystemExit(subprocess.run(command, cwd=os.environ["OPENLIA_SKILL_SOURCE"], env={"HOME": "/tmp/openlia-home", "PATH": os.environ["OPENLIA_SKILL_RUNTIME_PATH"], "PYTHONNOUSERSITE": "1", "PYTHONDONTWRITEBYTECODE": "1"}).returncode)'
fi

printf '%s\n' 'expected audit or test action' >&2
exit 2
