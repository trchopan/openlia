# Security Policy

OpenLia is an early-stage operations and setup layer for Hermes Agent. This
policy covers vulnerabilities in OpenLia's source code, CLI, operations
scripts, Docker build files, release metadata, profile, and generated runtime
configuration.

## Supported Versions

OpenLia is pre-1.0. Security fixes are prioritized for the latest release and
the default branch. Older releases should be treated as unsupported unless a
release note explicitly says otherwise. Users should update to the latest
release before reporting an issue when possible.

## Reporting a Vulnerability

Report suspected vulnerabilities privately through [GitHub's private
vulnerability reporting form](https://github.com/trchopan/openlia/security/advisories/new).
Include:

- A short description of the vulnerability and its potential impact.
- The affected OpenLia version, commit, or release manifest.
- Reproduction steps or a minimal proof of concept.
- Relevant configuration, deployment mode, and host/container details.
- Any proposed mitigation or patch, if available.

Do not include live credentials, personal workspace data, private deployment
identifiers, or unredacted logs. OpenLia's secret-handling rules are documented
in the [README](README.md), and reports should follow the same redaction
standard.

Please do not disclose a suspected vulnerability through a public issue, pull
request, discussion, or social-media post until a coordinated disclosure plan
has been agreed. There is no guaranteed response time, but reports will be
reviewed and follow-up will be coordinated through the private security
advisory.

If you accidentally expose a credential while investigating, revoke or rotate
it immediately and report only the minimum sanitized details needed to identify
the exposure.

## Scope and Upstream Issues

The following are in scope when the security boundary is controlled by
OpenLia:

- Secret ingestion, redaction, rotation, and credential handling.
- CLI validation, remote-operation boundaries, and generated Compose files.
- Backup and restore behavior, including path and archive validation.
- Container isolation, exposed listeners, and generated service configuration.
- Workspace Git automation and credential handling.
- Release integrity, provenance, and checksum verification.

Report vulnerabilities in Hermes Agent, Locho, Debian, Docker, Go, Python,
Bun, uv, PyYAML, or another upstream dependency to that project's maintainers
as well. Notify OpenLia through the private GitHub report form when the issue
affects OpenLia's integration, configuration, packaging, or documented security
boundary.

The following are not OpenLia vulnerabilities by themselves:

- Provider outages, service abuse, rate limits, or provider-side account
  compromise.
- Vulnerabilities in a user's host, Docker installation, SSH configuration, or
  external service that OpenLia does not control.
- Model behavior or prompt-injection concerns that do not bypass an OpenLia
  security boundary or cause unauthorized access or execution.

## Disclosure Process

OpenLia will work with the reporter to validate the issue, assess affected
versions, prepare a fix or mitigation, and coordinate public disclosure. A
reporter will be credited in release notes or the security advisory when they
request credit and it is safe to do so.

Security fixes should preserve the repository's existing safeguards, including
protected out-of-repository secrets, private-by-default listeners, read-only
container filesystems, restricted capabilities, and manual approval boundaries.
See the [security boundaries in the README](README.md#security-boundaries) and
the [contribution guidelines](CONTRIBUTING.md) for additional requirements.
