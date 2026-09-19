#!/usr/bin/env python3
"""Small deterministic checks for the workspace-git skill."""

from __future__ import annotations

import re
import sys


SAFE_PATH = re.compile(r"^[A-Za-z0-9._/-]+$")


def is_safe_workspace_path(value: str) -> bool:
    if not value or value.startswith("/") or ".." in value:
        return False
    if not SAFE_PATH.fullmatch(value):
        return False
    blocked = (".env", ".pem", ".key", ".p12", ".pfx", "auth.json")
    return not value.endswith(blocked) and not any(value.startswith(prefix) for prefix in ("sessions/", "logs/", "cache/"))


def self_test() -> None:
    assert is_safe_workspace_path("projects/example.md")
    assert not is_safe_workspace_path(".env")
    assert not is_safe_workspace_path("people/../secret.md")
    assert not is_safe_workspace_path("logs/gateway.log")


if __name__ == "__main__":
    if len(sys.argv) != 2 or sys.argv[1] != "--self-test":
        raise SystemExit("usage: check_workspace_git.py --self-test")
    self_test()
    print("ok")
