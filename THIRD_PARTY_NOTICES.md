# Third-Party Notices

OpenLia source code is distributed under the [MIT License](LICENSE). OpenLia
also builds or uses software from other projects. Those projects remain subject
to their own licenses and notices. This file is an inventory of the
third-party artifacts referenced directly by this repository; it is not a
replacement for the license files shipped by those projects or images.

## Directly Referenced Artifacts

| Component | Version or pin | License | Source or notice |
| --- | --- | --- | --- |
| Hermes Agent | `nousresearch/hermes-agent:v2026.9.14` at `sha256:99641e57ec762c59e54cb44aa6746b7fc68c18b3c5ddb088af54234c613d9294` | MIT | [Upstream repository](https://github.com/NousResearch/hermes-agent) |
| Locho | `v1.2.0-beta.1`; architecture-specific SHA-256 values are recorded in [`release/manifest.json`](release/manifest.json) | MIT | [Upstream repository](https://github.com/trchopan/locho) and [release downloads](https://github.com/trchopan/locho/releases/tag/v1.2.0-beta.1) |
| Bun | `1.2.22`; architecture-specific SHA-256 values are recorded in [`docker/Dockerfile`](docker/Dockerfile) | MIT, with additional bundled component licenses | [Upstream licensing information](https://github.com/oven-sh/bun/blob/main/LICENSE.md) |
| uv | `0.8.14`; architecture-specific SHA-256 values are recorded in [`docker/Dockerfile`](docker/Dockerfile) | MIT and Apache-2.0 | [MIT License](https://github.com/astral-sh/uv/blob/0.8.14/LICENSE-MIT) and [Apache License 2.0](https://github.com/astral-sh/uv/blob/0.8.14/LICENSE-APACHE) |
| PyYAML | `6.0.2` | MIT | [Upstream license](https://github.com/yaml/pyyaml/blob/6.0.2/LICENSE) |

OpenLia has no external Go modules. The Python dependency above is declared in
[`profile/skills/claim-review/requirements.txt`](profile/skills/claim-review/requirements.txt)
and is also used by the development requirements file.

## Container Base Images and Packages

The derived images use these pinned base images:

- Hermes image base: `nousresearch/hermes-agent:v2026.9.14` at the digest
  listed above.
- Locho image base: `debian:13.4-slim` at
  `sha256:109e2c65005bf160609e4ba6acf7783752f8502ad218e298253428690b9eaa4b`.

The Dockerfiles install distribution packages including `ca-certificates`,
`curl`, `git`, `hledger`, `passwd`, `tzdata`, `unzip`, and `xz-utils`. These
packages and their transitive dependencies have package-specific licenses and
copyright notices. The exact package versions are resolved by the Debian image
at build time rather than pinned individually in this repository. For a built
image, inspect the package metadata and files under `/usr/share/doc/*/copyright`
and consult [Debian's license information](https://www.debian.org/legal/licenses/).

## Bundled and Transitive Notices

- Bun statically links or embeds additional libraries and polyfills with
  licenses including LGPL, BSD, Apache, MIT, zlib, and other licenses. Refer to
  Bun's upstream licensing information for the complete component inventory.
- Hermes Agent and Locho may include their own dependencies and notices inside
  their respective images or release archives. OpenLia does not replace those
  notices.
- The final image also contains dependencies supplied by the upstream Hermes
  image and the Debian base image. A complete notice set for a particular image
  must be generated from that image, including its package database and
  upstream-provided notice files.

When redistributing an OpenLia-built image, binary, or release archive, retain
the applicable upstream license texts and attribution notices from every
included artifact. Verify the current pins in the Dockerfiles and release
manifest before making a redistribution.

## Host Tools and External Services

Docker Engine, Docker Compose, Go, Python, Bash, SSH, GitHub, and configured
OpenAI-compatible providers are prerequisites or external services. They are
not bundled by this repository and remain subject to their own terms and
licenses. Optional Locho capabilities are also supplied and licensed by their
respective service or software providers.
