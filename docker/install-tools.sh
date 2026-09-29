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
media_requested=false
seen_tools=,
old_ifs=$IFS
IFS=,
for tool in $enabled_tools; do
	case "$seen_tools" in
		*,"$tool",*)
			printf 'duplicate OpenLia tool capability: %s\n' "$tool" >&2
			exit 1
			;;
	esac
	seen_tools="$seen_tools$tool,"
	case "$tool" in
        pdf)
            packages="$packages poppler-utils"
            ;;
		office)
			packages="$packages libreoffice-calc-nogui libreoffice-writer-nogui"
            ;;
        ocr)
            packages="$packages tesseract-ocr tesseract-ocr-eng"
            ;;
		media-transcripts)
			packages="$packages ffmpeg"
			media_requested=true
            ;;
        *)
            printf 'unknown OpenLia tool capability: %s\n' "$tool" >&2
            exit 1
            ;;
    esac
done
IFS=$old_ifs

apt-get -o Acquire::Check-Valid-Until=false update
# Package names come only from the capability map above.
# shellcheck disable=SC2086
DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends $packages
rm -rf /var/lib/apt/lists/*

if [ "$media_requested" = true ]; then
	uv pip install --python /opt/hermes/.venv/bin/python --no-cache --require-hashes --no-deps \
		--requirement /usr/local/share/openlia/yt-dlp-requirements.txt
fi

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
