# Locho Host Attachments

Copy `attachments.toml.example` to the runtime path on the selected deployment:

```text
<install-root>/runtime/locho/<host-name>/attachments.toml
```

The runtime file must contain the host ID, `listen_host = "0.0.0.0"`, and one
or more `[[services]]` entries. Replace capability placeholders out of band;
never commit them or pass them as ordinary command arguments. Use
`openlia attachments rotate <host> --source /path/to/attachments.toml` to
validate and replace one host without restarting unrelated sidecars.
