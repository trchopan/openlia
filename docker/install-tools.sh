#!/bin/sh
set -eu

enabled_tools=${OPENLIA_ENABLED_TOOLS:-}
if [ -z "$enabled_tools" ]; then
    exit 0
fi
case "$enabled_tools" in
    ,*|*,|*,,*)
        printf 'invalid OpenLia tool capability list: %s\n' "$enabled_tools" >&2
        exit 1
        ;;
esac

packages=""
old_ifs=$IFS
IFS=,
for tool in $enabled_tools; do
    case "$tool" in
        pdf)
            packages="$packages poppler-utils"
            ;;
        office)
            packages="$packages libreoffice-calc libreoffice-writer"
            ;;
        ocr)
            packages="$packages tesseract-ocr tesseract-ocr-eng"
            ;;
        media-transcripts)
            packages="$packages ffmpeg yt-dlp"
            ;;
        *)
            printf 'unknown OpenLia tool capability: %s\n' "$tool" >&2
            exit 1
            ;;
    esac
done
IFS=$old_ifs

apt-get update
# Package names come only from the capability map above.
# shellcheck disable=SC2086
DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends $packages
rm -rf /var/lib/apt/lists/*

IFS=,
for tool in $enabled_tools; do
    case "$tool" in
        pdf) pdftotext -v >/dev/null 2>&1 ;;
        office) libreoffice --headless --version >/dev/null ;;
        ocr) tesseract --version >/dev/null ;;
        media-transcripts)
            ffmpeg -version >/dev/null
            yt-dlp --version >/dev/null
            ;;
    esac
done
IFS=$old_ifs
