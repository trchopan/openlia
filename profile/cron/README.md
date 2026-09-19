# Optional Cron Jobs

The workspace Git pull is the only job OpenLia enables automatically. It is a
static no-agent job created by `openlia init` after the workspace remote has
been configured. It runs the bundled `scripts/openlia-workspace-git-sync.sh`
script on the configured schedule and does not spend model tokens.

Other jobs are not enabled by this profile. Scheduled agent work must be
reviewed after the interactive workflows and credentials are configured.

When ready, create additional jobs through the Hermes cron interface and keep
them paused until reviewed. Safe candidates are read-only daily briefings and
weekly reviews that write no files and deliver only a report. Cron is configured
to deny dangerous commands and unattended approval requests.

The Git pull uses no-agent mode so its fixed script can run unattended while
the normal approval policy remains fail-closed for agent cron sessions. It does
not commit or push workspace changes.

Example review flow:

```text
hermes cron create "every 1d at 09:00" "Run /daily-briefing using read-only inputs." --skill daily-briefing --paused
hermes cron list
hermes cron resume <job-id>
```

Do not place credentials, personal destinations, or generated runtime state in
this directory. Do not edit Hermes' generated `jobs.json` directly.
