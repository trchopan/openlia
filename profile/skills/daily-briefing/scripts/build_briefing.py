#!/usr/bin/env python3
"""Render a deterministic daily briefing from explicit JSON inputs."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any


SECTIONS = (
    ("calendar", "Calendar"),
    ("tasks", "Tasks"),
    ("follow_ups", "Follow-ups"),
    ("monitors", "Monitors"),
)


def _entries(data: dict[str, Any], key: str) -> list[dict[str, Any]]:
    values = data.get(key, [])
    if not isinstance(values, list):
        raise ValueError(f"{key} must be a list")
    if not all(isinstance(value, dict) for value in values):
        raise ValueError(f"{key} entries must be objects")
    return sorted(
        values,
        key=lambda value: (
            str(value.get("date") or value.get("time") or value.get("due") or ""),
            str(value.get("priority") or "normal"),
            str(value.get("title") or value.get("name") or "").casefold(),
            str(value.get("id") or ""),
        ),
    )


def _label(entry: dict[str, Any]) -> str:
    title = str(entry.get("title") or entry.get("name") or "Untitled")
    detail = str(entry.get("detail") or entry.get("description") or "").strip()
    when = str(entry.get("time") or entry.get("due") or "").strip()
    suffix = f" ({when})" if when else ""
    return f"- {title}{suffix}: {detail}" if detail else f"- {title}{suffix}"


def render_briefing(data: dict[str, Any]) -> str:
    if not isinstance(data, dict):
        raise ValueError("input must be a JSON object")
    date = str(data.get("date") or "unspecified date")
    lines = ["# Daily Briefing", "", f"Date: {date}", "", "## Focus", ""]
    focus = data.get("focus", [])
    if not isinstance(focus, list):
        raise ValueError("focus must be a list")
    if focus:
        lines.extend(f"- {str(item)}" for item in focus)
    else:
        lines.append("- No focus items supplied.")
    for key, heading in SECTIONS:
        lines.extend(["", f"## {heading}", ""])
        entries = _entries(data, key)
        lines.extend(_label(entry) for entry in entries) if entries else lines.append("- None supplied.")
    lines.extend(["", "## Safety", "", "This briefing is read-only; review any proposed action before approval.", ""])
    return "\n".join(lines)


def self_test() -> None:
    output = render_briefing(
        {
            "date": "2026-01-02",
            "focus": ["Protect a block for deep work"],
            "tasks": [{"title": "Write outline", "due": "09:00"}],
            "calendar": [{"title": "Planning", "time": "10:00"}],
        }
    )
    assert output.index("Planning") < output.index("Write outline")
    assert "No focus items" not in output


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input", nargs="?", type=Path, help="JSON briefing input")
    parser.add_argument("--output", type=Path, help="Write Markdown output to a file")
    parser.add_argument("--self-test", action="store_true", help="Run the built-in deterministic test")
    args = parser.parse_args(argv)
    if args.self_test:
        self_test()
        print("ok")
        return 0
    if args.input is None:
        parser.error("input is required unless --self-test is used")
    try:
        rendered = render_briefing(json.loads(args.input.read_text(encoding="utf-8")))
        rendered += "\n"
        if args.output:
            args.output.write_text(rendered, encoding="utf-8")
        else:
            sys.stdout.write(rendered)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
