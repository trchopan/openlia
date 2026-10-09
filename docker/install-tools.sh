#!/bin/sh
set -eu

packages="ffmpeg poppler-utils"

apt-get -o Acquire::Check-Valid-Until=false update
# Package names come only from the capability map above.
# shellcheck disable=SC2086
DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends $packages
rm -rf /var/lib/apt/lists/*

uv pip install --python /opt/hermes/.venv/bin/python --no-cache --require-hashes --no-deps \
	--requirement /usr/local/share/openlia/yt-dlp-requirements.txt

ffmpeg -version >/dev/null
ffprobe -version >/dev/null
pdftotext -v >/dev/null 2>&1
pdfinfo -v >/dev/null 2>&1
pdftoppm -v >/dev/null 2>&1
yt-dlp --version >/dev/null
