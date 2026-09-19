#!/usr/bin/env python3
"""Score decision options with explicit, weighted criteria."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any


def _number(value: Any, label: str) -> float:
    if isinstance(value, bool):
        raise ValueError(f"{label} must be a number")
    try:
        number = float(value)
    except (TypeError, ValueError) as exc:
        raise ValueError(f"{label} must be a number") from exc
    if not 0 <= number <= 10:
        raise ValueError(f"{label} must be between 0 and 10")
    return number


def score_options(data: dict[str, Any]) -> dict[str, Any]:
    if not isinstance(data, dict):
        raise ValueError("input must be a JSON object")
    criteria = data.get("criteria", [])
    options = data.get("options", [])
    if not isinstance(criteria, list) or not criteria:
        raise ValueError("criteria must be a non-empty list")
    if not isinstance(options, list) or not options:
        raise ValueError("options must be a non-empty list")

    normalized_criteria: list[dict[str, Any]] = []
    total_weight = 0.0
    for index, criterion in enumerate(criteria, start=1):
        if not isinstance(criterion, dict) or not criterion.get("name"):
            raise ValueError(f"criterion {index} must have a name")
        weight = _number(criterion.get("weight", 1), f"weight for {criterion['name']}")
        if weight == 0:
            raise ValueError(f"weight for {criterion['name']} must be greater than zero")
        normalized_criteria.append({"name": str(criterion["name"]), "weight": weight})
        total_weight += weight

    ranked: list[dict[str, Any]] = []
    for index, option in enumerate(options, start=1):
        if not isinstance(option, dict) or not option.get("name"):
            raise ValueError(f"option {index} must have a name")
        scores = option.get("scores", {})
        if not isinstance(scores, dict):
            raise ValueError(f"scores for {option['name']} must be an object")
        weighted_total = 0.0
        missing: list[str] = []
        details: dict[str, float] = {}
        for criterion in normalized_criteria:
            name = criterion["name"]
            if name not in scores:
                missing.append(name)
                value = 0.0
            else:
                value = _number(scores[name], f"score for {option['name']} / {name}")
            details[name] = value
            weighted_total += value * criterion["weight"]
        ranked.append(
            {
                "name": str(option["name"]),
                "weighted_score": round(weighted_total / total_weight, 4),
                "scores": details,
                "missing_scores": missing,
                "risks": [str(risk) for risk in option.get("risks", [])] if isinstance(option.get("risks", []), list) else [],
            }
        )
    ranked.sort(key=lambda option: (-option["weighted_score"], option["name"].casefold()))
    return {
        "question": str(data.get("question") or ""),
        "criteria": sorted(normalized_criteria, key=lambda criterion: criterion["name"].casefold()),
        "options": ranked,
        "top_option": ranked[0]["name"],
        "caveat": "Scores are decision support, not an automatic action.",
    }


def render_markdown(result: dict[str, Any]) -> str:
    lines = ["# Decision Analysis", "", f"Question: {result['question'] or 'Unspecified'}", "", "## Ranking", ""]
    for index, option in enumerate(result["options"], start=1):
        gaps = f"; missing: {', '.join(option['missing_scores'])}" if option["missing_scores"] else ""
        lines.append(f"{index}. {option['name']} - {option['weighted_score']:.4f}{gaps}")
    lines.extend(["", "## Guardrail", "", result["caveat"], ""])
    return "\n".join(lines)


def self_test() -> None:
    result = score_options(
        {
            "question": "Which path is clearer?",
            "criteria": [{"name": "fit", "weight": 2}, {"name": "cost", "weight": 1}],
            "options": [
                {"name": "A", "scores": {"fit": 8, "cost": 5}},
                {"name": "B", "scores": {"fit": 6, "cost": 9}},
            ],
        }
    )
    assert result["top_option"] == "A"
    assert result["options"][0]["weighted_score"] == 7.0
    assert "Decision Analysis" in render_markdown(result)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input", nargs="?", type=Path, help="JSON decision matrix")
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
        result = score_options(json.loads(args.input.read_text(encoding="utf-8")))
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
