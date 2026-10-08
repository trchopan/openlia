# Optional Cron Jobs

No cron jobs are enabled by this profile. Scheduled agent work must be
reviewed after the interactive workflows and credentials are configured.

When ready, create additional jobs through the Hermes cron interface and keep
them paused until reviewed. Scheduled daily briefings and weekly reviews must
explicitly request read-only/no-write delivery and return their reports in the
job output; the interactive daily-briefing default writes a dated workspace
report. Cron is configured to deny dangerous commands and unattended approval
requests.

Example review flow:

```text
hermes cron create "every 1d at 09:00" "Run /daily-briefing using read-only inputs; return the structured report in chat only and do not write workspace files." --skill daily-briefing --paused
hermes cron list
hermes cron resume <job-id>
```

Keep credentials, personal destinations, and generated runtime state outside
this directory. Manage generated cron state through the Hermes cron interface.
