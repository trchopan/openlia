#!/usr/bin/env python3
"""Build an evidence matrix from researcher-supplied source notes."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any


WEIGHTS = {"authority": 0.4, "quality": 0.3, "recency": 0.1, "relevance": 0.2}


def _score(source: dict[str, Any], key: str) -> float:
    value = source.get(key)
    if value is None:
        return 0.0
    if isinstance(value, bool):
        raise ValueError(f"{key} score must be a number")
    try:
        number = float(value)
    except (TypeError, ValueError) as exc:
        raise ValueError(f"{key} score must be a number") from exc
    if not 0 <= number <= 5:
        raise ValueError(f"{key} score must be between 0 and 5")
    return number


def build_matrix(data: dict[str, Any]) -> dict[str, Any]:
    if not isinstance(data, dict):
        raise ValueError("input must be a JSON object")
    sources = data.get("sources", [])
    if not isinstance(sources, list) or not all(isinstance(source, dict) for source in sources):
        raise ValueError("sources must be a list of objects")
    rows: list[dict[str, Any]] = []
    for index, source in enumerate(sources, start=1):
        title = str(source.get("title") or f"Source {index}")
        scores = {key: _score(source, key) for key in WEIGHTS}
        missing = [key for key in WEIGHTS if key not in source]
        total = round(sum(scores[key] * WEIGHTS[key] for key in WEIGHTS), 3)
        rows.append(
            {
                "id": str(source.get("id") or f"source-{index:03d}"),
                "title": title,
                "url": str(source.get("url") or ""),
                "score": total,
                "missing_dimensions": missing,
                "notes": str(source.get("notes") or ""),
            }
        )
    rows.sort(key=lambda row: (-row["score"], row["title"].casefold(), row["id"]))
    gaps = []
    if not rows:
        gaps.append("No sources were supplied.")
    if rows and all(row["score"] < 3 for row in rows):
        gaps.append("No source reached a weighted confidence score of 3.0.")
    if any(row["missing_dimensions"] for row in rows):
        gaps.append("One or more source dimensions are missing; verify before relying on the ranking.")
    return {
        "question": str(data.get("question") or ""),
        "sources": rows,
        "evidence_gaps": gaps,
        "method": "Weighted source notes only; this helper does not fetch or verify URLs.",
    }


def render_markdown(result: dict[str, Any]) -> str:
    lines = ["# Research Matrix", "", f"Question: {result['question'] or 'Unspecified'}", "", "## Sources", ""]
    if result["sources"]:
        for source in result["sources"]:
            url = f" - {source['url']}" if source["url"] else ""
            lines.append(f"- {source['title']}: {source['score']:.3f}{url}")
    else:
        lines.append("- No sources supplied.")
    lines.extend(["", "## Evidence Gaps", ""])
    lines.extend(f"- {gap}" for gap in result["evidence_gaps"] or ["No mechanical gaps detected; assess the claims manually."])
    lines.extend(["", "## Method", "", result["method"], ""])
    return "\n".join(lines)


def self_test() -> None:
    result = build_matrix(
        {
            "question": "What evidence is strongest?",
            "sources": [
                {"title": "Primary source", "authority": 5, "quality": 4, "recency": 4, "relevance": 5},
                {"title": "Partial source", "authority": 2, "quality": 2, "relevance": 2},
            ],
        }
    )
    assert result["sources"][0]["title"] == "Primary source"
    assert "recency" in result["sources"][1]["missing_dimensions"]
    assert "Research Matrix" in render_markdown(result)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input", nargs="?", type=Path, help="JSON source notes")
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
        result = build_matrix(json.loads(args.input.read_text(encoding="utf-8")))
        rendered = json.dumps(result, indent=2, sort_keys=True, ensure_ascii=True) + "\n" if args.format == "json" else render_markdown(result)
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
