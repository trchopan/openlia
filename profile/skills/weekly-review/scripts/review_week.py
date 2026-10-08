#!/usr/bin/env python3
"""Build deterministic weekly review data from explicit JSON inputs."""

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


def build_review(data: dict[str, Any]) -> dict[str, Any]:
    if not isinstance(data, dict):
        raise ValueError("input must be a JSON object")
    projects = _list(data, "projects")
    tasks = _list(data, "tasks")
    decisions = _list(data, "decisions")
    claims = _list(data, "claims")
    completed = [task for task in tasks if str(task.get("status", "")).lower() in {"done", "completed"}]
    closed_tasks = [
        task
        for task in tasks
        if str(task.get("status", "")).lower() in {"cancelled", "canceled", "archived"}
    ]
    open_tasks = [
        task
        for task in tasks
        if str(task.get("status", "")).lower()
        not in {"done", "completed", "cancelled", "canceled", "archived"}
    ]
    active_projects = [
        project
        for project in projects
        if str(project.get("status") or "active").lower() == "active"
    ]
    revisit = [
        decision
        for decision in decisions
        if decision.get("revisit") is True
        or str(decision.get("status") or "").lower() in {"proposed", "pending", ""}
    ]
    claim_review = [claim for claim in claims if claim.get("needs_review") is True or str(claim.get("status", "")).lower() in {"candidate", "stale", "contested"}]

    return {
        "week": str(data.get("week") or "unspecified"),
        "completed": _ordered(completed),
        "closed_tasks": _ordered(closed_tasks),
        "active_projects": _ordered(active_projects),
        "open_tasks": _ordered(open_tasks),
        "decisions_to_revisit": _ordered(revisit),
        "claims_to_review": _ordered(claim_review),
    }


def self_test() -> None:
    review = build_review(
        {
            "week": "2026-W01",
            "projects": [
                {"title": "Active project", "status": "active"},
                {"title": "Unspecified project", "status": None},
                {"title": "Paused project", "status": "paused"},
            ],
            "tasks": [
                {"title": "Finished task", "status": "done"},
                {"title": "Cancelled task", "status": "cancelled"},
                {"title": "Open task", "status": "open"},
            ],
            "decisions": [
                {"title": "Unresolved choice", "status": None},
                {"title": "Abandoned decision", "status": "abandoned"},
            ],
            "claims": [{"claim": "A stale preference", "status": "stale", "needs_review": True}],
        }
    )
    assert review["completed"][0]["title"] == "Finished task"
    assert review["closed_tasks"][0]["title"] == "Cancelled task"
    assert review["open_tasks"][0]["title"] == "Open task"
    assert review["decisions_to_revisit"][0]["title"] == "Unresolved choice"
    assert review["claims_to_review"][0]["claim"] == "A stale preference"
    assert {project["title"] for project in review["active_projects"]} == {
        "Active project",
        "Unspecified project",
    }
    assert all(project.get("title") != "Paused project" for project in review["active_projects"])
    assert all(decision.get("title") != "Abandoned decision" for decision in review["decisions_to_revisit"])


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input", nargs="?", type=Path, help="JSON weekly review input")
    parser.add_argument("--output", type=Path, help="Write JSON output to a file")
    parser.add_argument("--self-test", action="store_true", help="Run the built-in deterministic test")
    args = parser.parse_args(argv)
    if args.self_test:
        self_test()
        print("ok")
        return 0
    if args.input is None:
        parser.error("input is required unless --self-test is used")
    try:
        rendered = json.dumps(
            build_review(json.loads(args.input.read_text(encoding="utf-8"))),
            indent=2,
            sort_keys=True,
            ensure_ascii=True,
        ) + "\n"
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
