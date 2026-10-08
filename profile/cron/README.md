# Optional Cron Jobs

No cron jobs are enabled by this profile. Scheduled agent work must be
reviewed after the interactive workflows and credentials are configured.

When ready, create additional jobs through the Hermes cron interface and keep
them paused until reviewed. Scheduled work must read `assistant-policy.yaml`.
The starter policy permits routine local workspace actions and report writes,
but external side effects remain gated. A scheduled daily briefing may save a
dated report only when `scheduled_work.enabled` and its action list permit
`generate_reports`; otherwise return the report in the job output. An explicit
read-only instruction always takes precedence. Cron is configured to deny
dangerous commands and unattended approval requests.

Example review flow:

```text
hermes cron create "every 1d at 09:00" "Run /daily-briefing within assistant-policy.yaml; save the dated report only if scheduled_work permits generate_reports, otherwise return it in chat." --skill daily-briefing --paused
hermes cron list
hermes cron resume <job-id>
```

Keep credentials, personal destinations, and generated runtime state outside
this directory. Manage generated cron state through the Hermes cron interface.
