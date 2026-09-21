#!/usr/bin/env python3
"""Submit and inspect OpenLia browser jobs without holding a terminal open."""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request


def base_url() -> str:
    return os.getenv("OPENLIA_TOOLS_URL", "http://openlia-tools:8787").rstrip("/")


def request(method: str, path: str, body: dict | None = None) -> tuple[int, bytes, str]:
    data = None if body is None else json.dumps(body).encode()
    headers = {"Accept": "application/json"}
    if data is not None:
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(base_url() + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=15) as response:
            return response.status, response.read(), response.headers.get_content_type()
    except urllib.error.HTTPError as error:
        return error.code, error.read(), error.headers.get_content_type()


def self_test() -> None:
    assert base_url() == "http://openlia-tools:8787" or base_url().startswith("http")
    print("ok")


def main() -> int:
    if "--self-test" in sys.argv[1:]:
        self_test()
        return 0

    parser = argparse.ArgumentParser(description="OpenLia browser job client")
    subparsers = parser.add_subparsers(dest="action", required=True)

    submit = subparsers.add_parser("submit")
    submit.add_argument("tool", choices=("chatgpt-chat", "gemini-chat"))
    submit.add_argument("--prompt", required=True)
    submit.add_argument("--topic", default="")
    submit.add_argument("--idempotency-key", default="")
    submit.add_argument("--timeout-seconds", type=int, default=300)

    for action in ("status", "result", "cancel"):
        command = subparsers.add_parser(action)
        command.add_argument("job_id")

    args = parser.parse_args()
    if args.action == "submit":
        status, data, content_type = request(
            "POST",
            "/openlia/tools/" + args.tool,
            {
                "prompt": args.prompt,
                "topic": args.topic,
                "idempotency_key": args.idempotency_key,
                "timeout_seconds": args.timeout_seconds,
            },
        )
    elif args.action == "status":
        status, data, content_type = request("GET", "/openlia/jobs/" + args.job_id)
    elif args.action == "result":
        status, data, content_type = request("GET", "/openlia/jobs/" + args.job_id + "/result")
    else:
        status, data, content_type = request("POST", "/openlia/jobs/" + args.job_id + "/cancel", {})

    if content_type == "application/json":
        try:
            print(json.dumps(json.loads(data), indent=2))
        except json.JSONDecodeError:
            print(data.decode(errors="replace"))
    else:
        sys.stdout.buffer.write(data)
        if data and not data.endswith(b"\n"):
            sys.stdout.buffer.write(b"\n")
    return 0 if 200 <= status < 300 else 1


if __name__ == "__main__":
    raise SystemExit(main())
