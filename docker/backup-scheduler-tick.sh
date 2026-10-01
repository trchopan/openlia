#!/bin/sh
set -eu

: "${OPENLIA_OPERATOR_CONFIG_FILE:?OPENLIA_OPERATOR_CONFIG_FILE is required}"
: "${OPENLIA_RUNTIME_UID:?OPENLIA_RUNTIME_UID is required}"
: "${OPENLIA_RUNTIME_GID:?OPENLIA_RUNTIME_GID is required}"

if [ ! -r "$OPENLIA_OPERATOR_CONFIG_FILE" ]; then
    exit 0
fi

touch /run/openlia/last-tick

# The host operator may protect this mounted configuration with mode 0600.
# Copying it as root into tmpfs lets the operator run as the runtime identity.
config_copy=/tmp/openlia-backup/schedule-config.json
cp "$OPENLIA_OPERATOR_CONFIG_FILE" "$config_copy"
chmod 0644 "$config_copy"

exec 9>/run/openlia/operation.lock
if ! flock -n 9; then
    exit 0
fi

case "$(uname -m)" in
    x86_64|amd64) operator=/usr/local/lib/openlia/openlia-operator-amd64 ;;
    aarch64|arm64) operator=/usr/local/lib/openlia/openlia-operator-arm64 ;;
    *) printf 'unsupported scheduler architecture: %s\n' "$(uname -m)" >&2; exit 2 ;;
esac

if setpriv --reuid="$OPENLIA_RUNTIME_UID" --regid="$OPENLIA_RUNTIME_GID" --clear-groups --bounding-set=-all \
    env OPENLIA_OPERATOR_CONFIG_FILE="$config_copy" "$operator" backup tick; then
    status=0
else
    status=$?
fi
if [ "$status" -eq 0 ]; then
    touch /run/openlia/last-success
fi
exit "$status"
