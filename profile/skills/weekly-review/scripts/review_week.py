#!/usr/bin/env python3
"""Render a deterministic weekly review from explicit JSON inputs."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any


def _list(data: dict[str, Any], key: str) -> list[dict[str, Any]]:
    values = data.get(key, [])
    if not isinstance(values, list) or not all(isinstance(value, dict) for value in values):
        raise ValueError(f"{key} must be a list of objects")
    return values


def _title(item: dict[str, Any]) -> str:
    return str(item.get("title") or item.get("name") or item.get("claim") or "Untitled")


def _ordered(values: list[dict[str, Any]]) -> list[dict[str, Any]]:
    return sorted(values, key=lambda value: (_title(value).casefold(), str(value.get("id") or "")))


def _bullets(values: list[dict[str, Any]], empty: str) -> list[str]:
    return [f"- {_title(value)}" for value in _ordered(values)] or [f"- {empty}"]


def render_review(data: dict[str, Any]) -> str:
    if not isinstance(data, dict):
        raise ValueError("input must be a JSON object")
    projects = _list(data, "projects")
    tasks = _list(data, "tasks")
    decisions = _list(data, "decisions")
    claims = _list(data, "claims")
    completed = [task for task in tasks if str(task.get("status", "")).lower() in {"done", "completed"}]
    open_tasks = [task for task in tasks if task not in completed]
    active_projects = [project for project in projects if str(project.get("status", "active")).lower() not in {"done", "completed", "archived"}]
    revisit = [decision for decision in decisions if decision.get("revisit") is True or decision.get("outcome") in (None, "")]
    claim_review = [claim for claim in claims if claim.get("needs_review") is True or str(claim.get("status", "")).lower() in {"candidate", "stale", "contested"}]

    lines = [
        "# Weekly Review",
        "",
        f"Week: {str(data.get('week') or 'unspecified')}",
        "",
        "## Completed",
        "",
        *_bullets(completed, "No completed tasks supplied."),
        "",
        "## Active Projects",
        "",
        *_bullets(active_projects, "No active projects supplied."),
        "",
        "## Open Loops",
        "",
        *_bullets(open_tasks, "No open tasks supplied."),
        "",
        "## Decisions To Revisit",
        "",
        *_bullets(revisit, "No decisions need review."),
        "",
        "## Claims To Review",
        "",
        *_bullets(claim_review, "No claims need review."),
        "",
        "## Next Week",
        "",
        "- Choose a small number of outcomes before adding more tasks.",
        "- Confirm owners and dates for any open loop that matters.",
        "",
    ]
    return "\n".join(lines)


def self_test() -> None:
    output = render_review(
        {
            "week": "2026-W01",
            "projects": [{"title": "Active project", "status": "active"}],
            "tasks": [{"title": "Finished task", "status": "done"}, {"title": "Open task", "status": "open"}],
            "decisions": [{"title": "Unresolved choice"}],
            "claims": [{"claim": "A stale preference", "status": "stale", "needs_review": True}],
        }
    )
    assert "Finished task" in output
    assert "Open task" in output
    assert "Unresolved choice" in output
    assert "A stale preference" in output


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input", nargs="?", type=Path, help="JSON weekly review input")
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
        rendered = render_review(json.loads(args.input.read_text(encoding="utf-8")))
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
