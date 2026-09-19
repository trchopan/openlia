#!/usr/bin/env python3
"""Summarize project health without changing project files."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any


DONE = {"done", "completed", "closed"}
PRIORITY = {"urgent": 0, "high": 1, "normal": 2, "low": 3}


def _objects(project: dict[str, Any], key: str) -> list[dict[str, Any]]:
    values = project.get(key, [])
    if not isinstance(values, list) or not all(isinstance(value, dict) for value in values):
        raise ValueError(f"{key} must be a list of objects")
    return values


def _name(value: dict[str, Any]) -> str:
    return str(value.get("title") or value.get("name") or "Untitled")


def project_report(project: dict[str, Any]) -> dict[str, Any]:
    if not isinstance(project, dict):
        raise ValueError("project input must be a JSON object")
    milestones = _objects(project, "milestones")
    tasks = _objects(project, "tasks")
    risks = _objects(project, "risks")
    decisions = _objects(project, "decisions")
    completed = sum(str(item.get("status", "")).lower() in DONE for item in milestones)
    completion = round((completed / len(milestones)) * 100, 1) if milestones else 0.0
    open_tasks = [item for item in tasks if str(item.get("status", "open")).lower() not in DONE]
    open_tasks.sort(
        key=lambda item: (
            PRIORITY.get(str(item.get("priority", "normal")).lower(), 2),
            str(item.get("due") or ""),
            _name(item).casefold(),
            str(item.get("id") or ""),
        )
    )
    overdue = [_name(item) for item in milestones + tasks if item.get("overdue") is True]
    return {
        "title": _name(project),
        "objective": str(project.get("objective") or ""),
        "status": str(project.get("status") or "unspecified"),
        "completion_percent": completion,
        "milestone_count": len(milestones),
        "completed_milestone_count": completed,
        "open_tasks": [_name(item) for item in open_tasks],
        "overdue_items": sorted(overdue, key=str.casefold),
        "risk_count": len(risks),
        "decision_count": len(decisions),
    }


def render_markdown(report: dict[str, Any]) -> str:
    lines = [
        f"# Project Review: {report['title']}",
        "",
        f"Status: {report['status']}",
        f"Completion: {report['completion_percent']}%",
        "",
        "## Objective",
        "",
        report["objective"] or "No objective supplied.",
        "",
        "## Next Actions",
        "",
        *([f"- {item}" for item in report["open_tasks"]] or ["- No open tasks supplied."]),
        "",
        "## Risks And Decisions",
        "",
        f"- Risks recorded: {report['risk_count']}",
        f"- Decisions recorded: {report['decision_count']}",
        "",
        "## Overdue",
        "",
        *([f"- {item}" for item in report["overdue_items"]] or ["- None marked overdue."]),
        "",
    ]
    return "\n".join(lines)


def self_test() -> None:
    report = project_report(
        {
            "title": "Example project",
            "objective": "Reach a defined outcome",
            "status": "active",
            "milestones": [{"title": "First step", "status": "completed"}, {"title": "Next step", "status": "open"}],
            "tasks": [{"title": "High priority action", "priority": "high", "status": "open"}],
        }
    )
    assert report["completion_percent"] == 50.0
    assert report["open_tasks"] == ["High priority action"]
    assert "Example project" in render_markdown(report)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input", nargs="?", type=Path, help="JSON project record")
    parser.add_argument("--format", choices=("json", "markdown"), default="json")
    parser.add_argument("--output", type=Path, help="Write the report to a file")
    parser.add_argument("--self-test", action="store_true", help="Run the built-in deterministic test")
    args = parser.parse_args(argv)
    if args.self_test:
        self_test()
        print("ok")
        return 0
    if args.input is None:
        parser.error("input is required unless --self-test is used")
    try:
        report = project_report(json.loads(args.input.read_text(encoding="utf-8")))
        rendered = (
            json.dumps(report, indent=2, sort_keys=True, ensure_ascii=True) + "\n"
            if args.format == "json"
            else render_markdown(report)
        )
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
