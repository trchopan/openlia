# Optional Cron Jobs

No jobs are enabled by this profile. This is intentional: scheduled work must
be reviewed after the interactive workflows and credentials are configured.

When ready, create jobs through the Hermes cron interface and keep them paused
until reviewed. Safe candidates are read-only daily briefings and weekly
reviews that write no files and deliver only a report. Cron is configured to
deny dangerous commands and unattended approval requests.

Example review flow:

```text
hermes cron create "every 1d at 09:00" "Run /daily-briefing using read-only inputs." --skill daily-briefing --paused
hermes cron list
hermes cron resume <job-id>
```

Do not place credentials, personal destinations, or generated runtime state in
this directory. Do not edit Hermes' generated `jobs.json` directly.
