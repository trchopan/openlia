#!/bin/sh
set -eu

: "${OPENLIA_RUNTIME_UID:?OPENLIA_RUNTIME_UID is required}"
: "${OPENLIA_RUNTIME_GID:?OPENLIA_RUNTIME_GID is required}"

case "$OPENLIA_RUNTIME_UID:$OPENLIA_RUNTIME_GID" in
    *[!0-9:]*|*:|:*|*:*:*)
        printf '%s\n' 'OPENLIA_RUNTIME_UID and OPENLIA_RUNTIME_GID must be numeric' >&2
        exit 2
        ;;
esac

mkdir -p /run/openlia /tmp/openlia-backup

aws_credentials=/root/.aws/credentials
aws_config=/root/.aws/config
if [ -f "$aws_credentials" ]; then
    cp "$aws_credentials" /tmp/openlia-backup/aws-credentials
    chown "$OPENLIA_RUNTIME_UID:$OPENLIA_RUNTIME_GID" /tmp/openlia-backup/aws-credentials
    chmod 0600 /tmp/openlia-backup/aws-credentials
    aws_credentials=/tmp/openlia-backup/aws-credentials
else
    aws_credentials=/dev/null
fi
if [ -f "$aws_config" ]; then
    cp "$aws_config" /tmp/openlia-backup/aws-config
    chown "$OPENLIA_RUNTIME_UID:$OPENLIA_RUNTIME_GID" /tmp/openlia-backup/aws-config
    chmod 0600 /tmp/openlia-backup/aws-config
    aws_config=/tmp/openlia-backup/aws-config
else
    aws_config=/dev/null
fi

# Existing host schedules may have created root-owned state and archives. The
# scheduler drops privileges for the actual operator, so make only its two
# writable trees accessible to the configured runtime identity.
chown -R "$OPENLIA_RUNTIME_UID:$OPENLIA_RUNTIME_GID" /runtime/meta /runtime/backups 2>/dev/null || true

cat > /run/openlia/scheduler.env <<EOF
OPENLIA_OPERATOR_CONFIG_FILE=${OPENLIA_OPERATOR_CONFIG_FILE}
OPENLIA_BACKUP_KNOWN_HOSTS=/etc/ssh/known_hosts
OPENLIA_RUNTIME_UID=${OPENLIA_RUNTIME_UID}
OPENLIA_RUNTIME_GID=${OPENLIA_RUNTIME_GID}
AWS_SHARED_CREDENTIALS_FILE=${aws_credentials}
AWS_CONFIG_FILE=${aws_config}
AWS_SDK_LOAD_CONFIG=1
AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID-}
AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY-}
AWS_SESSION_TOKEN=${AWS_SESSION_TOKEN-}
AWS_PROFILE=${AWS_PROFILE-}
AWS_REGION=${AWS_REGION-}
AWS_DEFAULT_REGION=${AWS_DEFAULT_REGION-}
TZ=${TZ:-Asia/Ho_Chi_Minh}
EOF
chmod 0600 /run/openlia/scheduler.env

# Use the same persisted environment for the startup tick as cron uses.
set -a
. /run/openlia/scheduler.env
set +a

# A release update can leave a due schedule behind while the Compose project
# is being recreated. Run once immediately; the schedule state prevents a
# duplicate run when cron reaches the same minute.
/usr/local/bin/openlia-backup-tick || true

exec cron -f -L 8
