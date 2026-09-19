#!/usr/bin/env python3
"""Classify inbox records with deterministic, local-only rules."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import Any


ROUTES = ("task", "decision", "idea", "research", "archive")
ROUTE_ORDER = {name: index for index, name in enumerate(ROUTES)}


def _text(item: dict[str, Any]) -> str:
    parts = [item.get("title", ""), item.get("subject", ""), item.get("content", "")]
    tags = item.get("tags", [])
    if isinstance(tags, list):
        parts.extend(str(tag) for tag in tags)
    return " ".join(str(part) for part in parts if part).lower()


def _explicit_route(item: dict[str, Any]) -> str | None:
    for key in ("route", "type", "category"):
        value = item.get(key)
        if isinstance(value, str) and value.lower() in ROUTES:
            return value.lower()
    return None


def classify(item: dict[str, Any]) -> tuple[str, str]:
    """Return a route and a short reason without using the current date."""
    explicit = _explicit_route(item)
    if explicit:
        return explicit, "preserved explicit route"

    text = _text(item)
    if re.search(r"\b(should|decide|choose|trade[- ]off|option|compare)\b", text):
        return "decision", "contains a choice or trade-off"
    if re.search(r"\b(todo|to-do|action|follow[- ]?up|need to|remember to|deadline)\b", text):
        return "task", "contains an action or commitment"
    if re.search(r"\b(idea|maybe|what if|learn|interesting)\b", text):
        return "idea", "looks like a possibility or observation"
    if re.search(r"\b(research|read|source|article|paper|reference)\b", text):
        return "research", "points to knowledge work"
    return "archive", "no action signal was detected"


def triage(items: list[dict[str, Any]]) -> dict[str, Any]:
    result: list[dict[str, Any]] = []
    counts = {route: 0 for route in ROUTES}
    for index, item in enumerate(items, start=1):
        if not isinstance(item, dict):
            raise ValueError(f"item {index} must be an object")
        route, reason = classify(item)
        record = {
            "id": str(item.get("id") or f"item-{index:03d}"),
            "title": str(item.get("title") or item.get("subject") or "Untitled item"),
            "route": route,
            "reason": reason,
            "next_step": {
                "task": "Define one concrete next action.",
                "decision": "Record options, constraints, and evidence.",
                "idea": "Capture why it matters and one small experiment.",
                "research": "Record the question and the sources to inspect.",
                "archive": "Keep the record only if it may be useful later.",
            }[route],
        }
        result.append(record)
        counts[route] += 1
    return {"counts": counts, "items": result}


def load_items(path: Path) -> list[dict[str, Any]]:
    data = json.loads(path.read_text(encoding="utf-8"))
    if isinstance(data, list):
        return data
    if isinstance(data, dict) and isinstance(data.get("items"), list):
        return data["items"]
    raise ValueError("input must be a JSON list or an object with an items list")


def self_test() -> None:
    output = triage(
        [
            {"id": "a", "title": "Decide which option fits", "content": "Compare two paths."},
            {"id": "b", "title": "Follow up on the draft"},
            {"id": "c", "title": "Interesting reading", "type": "research"},
            {"id": "d", "title": "A quiet note"},
        ]
    )
    assert [item["route"] for item in output["items"]] == ["decision", "task", "research", "archive"]
    assert output["counts"]["task"] == 1


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input", nargs="?", type=Path, help="JSON file containing inbox items")
    parser.add_argument("--output", type=Path, help="Write JSON output to this existing directory")
    parser.add_argument("--self-test", action="store_true", help="Run the built-in deterministic test")
    args = parser.parse_args(argv)
    if args.self_test:
        self_test()
        print("ok")
        return 0
    if args.input is None:
        parser.error("input is required unless --self-test is used")
    try:
        payload = triage(load_items(args.input))
        rendered = json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=True) + "\n"
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
