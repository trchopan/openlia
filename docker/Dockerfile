# syntax=docker/dockerfile:1.7

# Keep the upstream release tag visible while requiring an immutable digest.
ARG HERMES_BASE_IMAGE=nousresearch/hermes-agent
ARG HERMES_BASE_TAG=v2026.9.14
ARG HERMES_BASE_DIGEST=sha256:99641e57ec762c59e54cb44aa6746b7fc68c18b3c5ddb088af54234c613d9294
FROM ${HERMES_BASE_IMAGE}:${HERMES_BASE_TAG}@${HERMES_BASE_DIGEST}

ARG TARGETARCH
ARG BUN_VERSION=1.2.22
ARG BUN_X86_64_SHA256=4c446af1a01d7b40e1e11baebc352f9b2bfd12887e51b97dd3b59879cee2743a
ARG BUN_AARCH64_SHA256=a97c687fb5e54de4e2fb0869a7ac9a2d9c3af75ac182e2b68138c1dd8f98131b
ARG UV_VERSION=0.8.14
ARG UV_X86_64_SHA256=954add045f29f93191523175e4aea066996840e86c1b6339dee25f48b15b5ddb
ARG UV_AARCH64_SHA256=69616218470b2ad053617efb9e7027b1518ea38918d933c2791e113d99cec507

USER root

# The upstream image owns the entrypoint and s6 tree. Only add immutable
# command-line runtimes, timezone data, and the OpenLia secret helper here.
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates curl git hledger python3-yaml tzdata unzip xz-utils \
    && rm -rf /var/lib/apt/lists/* \
    && hledger --version \
    && set -eu; \
    case "${TARGETARCH:-amd64}" in \
        amd64) \
            bun_arch='x64'; \
            bun_sha="${BUN_X86_64_SHA256}"; \
            uv_target='x86_64-unknown-linux-gnu'; \
            uv_sha="${UV_X86_64_SHA256}"; \
            ;; \
        arm64) \
            bun_arch='aarch64'; \
            bun_sha="${BUN_AARCH64_SHA256}"; \
            uv_target='aarch64-unknown-linux-gnu'; \
            uv_sha="${UV_AARCH64_SHA256}"; \
            ;; \
        *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    bun_archive="/tmp/bun-linux-${bun_arch}.zip"; \
    uv_archive="/tmp/uv-${uv_target}.tar.gz"; \
    curl --fail --silent --show-error --location --retry 3 \
        --output "${bun_archive}" \
        "https://github.com/oven-sh/bun/releases/download/bun-v${BUN_VERSION}/bun-linux-${bun_arch}.zip"; \
    printf '%s  %s\n' "${bun_sha}" "${bun_archive}" | sha256sum -c -; \
    mkdir -p /tmp/bun; \
    unzip -q "${bun_archive}" -d /tmp/bun; \
    install -m 0755 "/tmp/bun/bun-linux-${bun_arch}/bun" /usr/local/bin/bun; \
    ln -sf /usr/local/bin/bun /usr/local/bin/bunx; \
    curl --fail --silent --show-error --location --retry 3 \
        --output "${uv_archive}" \
        "https://github.com/astral-sh/uv/releases/download/${UV_VERSION}/uv-${uv_target}.tar.gz"; \
    printf '%s  %s\n' "${uv_sha}" "${uv_archive}" | sha256sum -c -; \
    mkdir -p /tmp/uv; \
    tar -xzf "${uv_archive}" -C /tmp/uv; \
    install -m 0755 "/tmp/uv/uv-${uv_target}/uv" /usr/local/bin/uv; \
    install -m 0755 "/tmp/uv/uv-${uv_target}/uvx" /usr/local/bin/uvx; \
    rm -rf /tmp/bun /tmp/uv "${bun_archive}" "${uv_archive}"

COPY profile/skills/claim-review/requirements.txt /tmp/claim-req.txt
COPY profile/skills/gemini-chat/requirements.txt /tmp/gemini-req.txt
COPY profile/skills/chatgpt-chat/requirements.txt /tmp/chatgpt-req.txt
RUN uv pip install --python /opt/hermes/.venv/bin/python --no-cache \
    --requirement /tmp/claim-req.txt \
    --requirement /tmp/gemini-req.txt \
    --requirement /tmp/chatgpt-req.txt \
    && rm -f /tmp/*-req.txt

COPY --chmod=0755 docker/secret-source.sh /usr/local/bin/openlia-secret-source
COPY --chmod=0755 docker/git-askpass.sh /usr/local/bin/openlia-git-askpass

# Keep the upstream default USER and ENTRYPOINT unchanged. s6 must start as
# root so the upstream volume setup can drop supervised processes to hermes.
ENV HERMES_TIMEZONE="Asia/Ho_Chi_Minh"
ENV TZ="Asia/Ho_Chi_Minh"
ENV PATH="/usr/local/bin:/opt/hermes/bin:/opt/hermes/.venv/bin:/opt/data/.local/bin:${PATH}"
