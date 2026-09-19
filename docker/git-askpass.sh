#!/usr/bin/env bash
set -Eeuo pipefail

secret_file=${OPENLIA_GIT_SECRET_FILE_IN_CONTAINER:-/run/openlia-secrets/hermes.env}
[[ -f "$secret_file" && -r "$secret_file" ]] || exit 1

prompt=${1:-}
case "$prompt" in
    *[Uu]sername*)
        printf '%s\n' 'x-access-token'
        exit 0
        ;;
esac

while IFS= read -r line || [[ -n "$line" ]]; do
    line=${line%$'\r'}
    [[ "$line" == OPENLIA_GIT_TOKEN=* ]] || continue
    value=${line#OPENLIA_GIT_TOKEN=}
    [[ -n "$value" ]] || exit 1
    printf '%s\n' "$value"
    exit 0
done <"$secret_file"

exit 1
