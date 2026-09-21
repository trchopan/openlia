# syntax=docker/dockerfile:1.7

ARG TOOLS_BASE_IMAGE=debian
ARG TOOLS_BASE_TAG=13.4-slim
ARG TOOLS_BASE_DIGEST=sha256:109e2c65005bf160609e4ba6acf7783752f8502ad218e298253428690b9eaa4b
FROM ${TOOLS_BASE_IMAGE}:${TOOLS_BASE_TAG}@${TOOLS_BASE_DIGEST}

USER root

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates python3 python3-yaml \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10000 --home-dir /nonexistent --shell /usr/sbin/nologin openlia

COPY --chmod=0755 tools/openlia_tools.py /opt/openlia/tools/openlia_tools.py

USER openlia
ENV PYTHONUNBUFFERED=1
