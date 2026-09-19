#!/usr/bin/env bash
set -Eeuo pipefail

openlia_json_quote() {
    python3 -c 'import json, sys; print(json.dumps(sys.stdin.read().rstrip("\n"), ensure_ascii=True))' <<<"$1"
}

openlia_json_bool() {
    if [[ "$1" == true ]]; then
        printf 'true\n'
    else
        printf 'false\n'
    fi
}
