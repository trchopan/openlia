#!/usr/bin/env python3
"""Validate YAML-front-matter claim records without contacting external services."""

from __future__ import annotations

import argparse
import json
import re
import sys
from datetime import date, datetime, timezone
from pathlib import Path
from typing import Any

import yaml


KINDS = {"reported", "observed", "inferred", "hypothesis"}
STATUSES = {"candidate", "active", "stale", "contested", "superseded", "retracted"}
DATE_FIELDS = ("asserted_at", "observed_at", "valid_from", "valid_until", "review_after", "reviewed_at")
FRONT_MATTER = re.compile(r"\A---[ \t]*\r?\n(?P<metadata>.*?)(?:\r?\n)---[ \t]*(?:\r?\n|\Z)", re.DOTALL)


class UniqueKeySafeLoader(yaml.SafeLoader):
    """Safe YAML loader that refuses ambiguous duplicate metadata keys."""


def _construct_unique_mapping(loader: UniqueKeySafeLoader, node: yaml.nodes.MappingNode, deep: bool = False) -> dict[Any, Any]:
    mapping: dict[Any, Any] = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node, deep=deep)
        if key in mapping:
            raise ValueError(f"duplicate YAML key: {key}")
        mapping[key] = loader.construct_object(value_node, deep=deep)
    return mapping


UniqueKeySafeLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, _construct_unique_mapping)


def _normalize_yaml(value: Any) -> Any:
    if isinstance(value, datetime):
        return value.isoformat()
    if isinstance(value, date):
        return value.isoformat()
    if isinstance(value, dict):
        return {key: _normalize_yaml(item) for key, item in value.items()}
    if isinstance(value, list):
        return [_normalize_yaml(item) for item in value]
    return value


def parse_claim_markdown(text: str) -> dict[str, Any]:
    match = FRONT_MATTER.match(text)
    if not match:
        raise ValueError("claim file must begin with YAML front matter")
    try:
        metadata = yaml.load(match.group("metadata"), Loader=UniqueKeySafeLoader)
    except yaml.YAMLError as exc:
        raise ValueError(f"invalid YAML front matter: {exc}") from exc
    if not isinstance(metadata, dict):
        raise ValueError("YAML front matter must contain an object")
    return _normalize_yaml(metadata)


def load_claim_file(path: Path) -> dict[str, Any]:
    return parse_claim_markdown(path.read_text(encoding="utf-8"))


def _as_nonempty_string(value: Any, field: str, errors: list[str]) -> None:
    if not isinstance(value, str) or not value.strip():
        errors.append(f"{field} must be a non-empty string")


def _parse_date(value: Any, field: str, errors: list[str]) -> datetime | None:
    if value is None or value == "":
        return None
    if isinstance(value, datetime):
        return value if value.tzinfo is not None else value.replace(tzinfo=timezone.utc)
    if isinstance(value, date):
        return datetime(value.year, value.month, value.day, tzinfo=timezone.utc)
    if not isinstance(value, str):
        errors.append(f"{field} must be an ISO-8601 date or timestamp")
        return None
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
        return parsed if parsed.tzinfo is not None else parsed.replace(tzinfo=timezone.utc)
    except ValueError:
        errors.append(f"{field} must be an ISO-8601 date or timestamp")
        return None


def _validate_references(value: Any, field: str, errors: list[str], required: bool = True) -> None:
    if value is None:
        if required:
            errors.append(f"{field} must be a non-empty list")
        return
    valid_list = isinstance(value, list) and (not required or bool(value))
    valid_items = valid_list and all(isinstance(item, str) and item.strip() for item in value)
    if not valid_items:
        errors.append(f"{field} must be a {'non-empty ' if required else ''}list of strings")


def validate_claim(claim: Any) -> list[str]:
    if not isinstance(claim, dict):
        return ["claim must be an object"]

    errors: list[str] = []
    for field in ("id", "claim"):
        _as_nonempty_string(claim.get(field), field, errors)

    kind = claim.get("kind")
    if kind not in KINDS:
        errors.append(f"kind must be one of: {', '.join(sorted(KINDS))}")

    status = claim.get("status")
    if status not in STATUSES:
        errors.append(f"status must be one of: {', '.join(sorted(STATUSES))}")

    source = claim.get("source")
    if not isinstance(source, dict):
        errors.append("source must be an object with type and ref")
    else:
        _as_nonempty_string(source.get("type"), "source.type", errors)
        _as_nonempty_string(source.get("ref"), "source.ref", errors)

    provenance = claim.get("provenance")
    if not isinstance(provenance, dict):
        errors.append("provenance must be an object with evidence_refs")
    else:
        _validate_references(provenance.get("evidence_refs"), "provenance.evidence_refs", errors)
        _validate_references(provenance.get("derived_from"), "provenance.derived_from", errors, required=False)

    parsed_dates: dict[str, datetime] = {}
    for field in DATE_FIELDS:
        parsed = _parse_date(claim.get(field), field, errors)
        if parsed is not None:
            parsed_dates[field] = parsed
    if not parsed_dates.get("asserted_at") and not parsed_dates.get("observed_at"):
        errors.append("asserted_at or observed_at is required")
    if parsed_dates.get("valid_from") and parsed_dates.get("valid_until"):
        if parsed_dates["valid_from"] > parsed_dates["valid_until"]:
            errors.append("valid_from must not be after valid_until")

    confidence = claim.get("confidence")
    if confidence is not None:
        if isinstance(confidence, bool) or not isinstance(confidence, (int, float)) or not 0 <= confidence <= 1:
            errors.append("confidence must be a number between 0 and 1")
    if kind in {"inferred", "hypothesis"} and confidence is None:
        errors.append("confidence is required for inferred and hypothesis claims")
    if confidence is not None or kind in {"inferred", "hypothesis"}:
        _as_nonempty_string(claim.get("confidence_basis"), "confidence_basis", errors)
    if kind in {"inferred", "hypothesis"} and status == "active":
        if claim.get("reviewed_at") in (None, ""):
            errors.append("reviewed_at is required for an active inferred or hypothesis claim")
        else:
            _parse_date(claim.get("reviewed_at"), "reviewed_at", errors)
        _as_nonempty_string(claim.get("reviewed_by"), "reviewed_by", errors)

    return errors


def _claim_result(claim: Any, errors: list[str], index: int = 1, path: str | None = None) -> dict[str, Any]:
    claim_id = claim.get("id") if isinstance(claim, dict) else None
    result: dict[str, Any] = {
        "index": index,
        "id": str(claim_id or f"claim-{index:03d}"),
        "valid": not errors,
        "errors": errors,
    }
    if path:
        result["path"] = path
    return result


def validate_claims(data: Any) -> dict[str, Any]:
    if isinstance(data, list):
        claims = data
    elif isinstance(data, dict) and isinstance(data.get("claims"), list):
        claims = data["claims"]
    else:
        raise ValueError("input must be a JSON list or an object with a claims list")

    seen: set[str] = set()
    results: list[dict[str, Any]] = []
    for index, claim in enumerate(claims, start=1):
        errors = validate_claim(claim)
        claim_id = claim.get("id") if isinstance(claim, dict) else None
        if isinstance(claim_id, str) and claim_id:
            if claim_id in seen:
                errors.append("duplicate claim id")
            seen.add(claim_id)
        results.append(_claim_result(claim, errors, index))
    return {
        "valid": bool(results) and all(result["valid"] for result in results),
        "count": len(results),
        "claims": results,
        "policy": "Validation checks structure only; evidence and truth require human review.",
    }


def validate_claim_file(path: Path) -> dict[str, Any]:
    try:
        claim = load_claim_file(path)
    except (OSError, ValueError) as exc:
        return _claim_result({}, [str(exc)], path=str(path))
    return _claim_result(claim, validate_claim(claim), path=str(path))


def self_test() -> None:
    parsed = parse_claim_markdown(
        "---\n"
        "id: claim-date\n"
        "claim: Date parsing\n"
        "kind: reported\n"
        "status: active\n"
        "source:\n  type: user\n  ref: test\n"
        "provenance:\n  evidence_refs: [test]\n"
        "asserted_at: 2026-08-12\n"
        "---\n"
        "# Notes\n"
    )
    assert parsed["asserted_at"] == "2026-08-12"
    try:
        parse_claim_markdown("---\nid: one\nid: two\n---\n")
    except ValueError as exc:
        assert "duplicate YAML key" in str(exc)
    else:
        raise AssertionError("duplicate YAML keys must be rejected")
    valid = validate_claims(
        {
            "claims": [
                {
                    "id": "claim-laptop-plan",
                    "claim": "User plans to replace their laptop this year.",
                    "kind": "reported",
                    "status": "active",
                    "source": {"type": "user", "ref": "conversation-2026-08-12"},
                    "provenance": {"evidence_refs": ["conversation-2026-08-12"]},
                    "asserted_at": "2026-08-12",
                    "valid_until": "2026-12-31",
                },
                {
                    "id": "claim-apple-preference",
                    "claim": "User appears to prefer Apple laptops.",
                    "kind": "inferred",
                    "status": "candidate",
                    "source": {"type": "service", "ref": "shopping-history"},
                    "provenance": {
                        "evidence_refs": ["shopping/2026-laptop-research.md"],
                        "derived_from": ["claim-laptop-plan"],
                    },
                    "observed_at": "2026-08-12",
                    "confidence": 0.82,
                    "confidence_basis": "Two independent purchase records and one explicit comparison.",
                },
            ]
        }
    )
    assert valid["valid"] is True
    invalid = validate_claims(
        {
            "claims": [
                {
                    "id": "claim-bad",
                    "claim": "An unsupported inference.",
                    "kind": "inferred",
                    "status": "active",
                    "source": {"type": "assistant", "ref": "chat"},
                    "provenance": {"evidence_refs": []},
                    "observed_at": "not-a-date",
                    "confidence": 1.2,
                }
            ]
        }
    )
    assert invalid["valid"] is False
    assert len(invalid["claims"][0]["errors"]) >= 3
    assert any("reviewed_at" in error for error in invalid["claims"][0]["errors"])


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input", nargs="?", type=Path, help="Markdown claim file or JSON claim candidates")
    parser.add_argument("--output", type=Path, help="Write the validation report to a file")
    parser.add_argument("--self-test", action="store_true", help="Run the built-in deterministic test")
    args = parser.parse_args(argv)
    if args.self_test:
        self_test()
        print("ok")
        return 0
    if args.input is None:
        parser.error("input is required unless --self-test is used")
    try:
        if args.input.suffix.lower() == ".md":
            claim_result = validate_claim_file(args.input)
            result = {"valid": claim_result["valid"], "count": 1, "claims": [claim_result]}
        else:
            result = validate_claims(json.loads(args.input.read_text(encoding="utf-8")))
        rendered = json.dumps(result, indent=2, sort_keys=True, ensure_ascii=True) + "\n"
        if args.output:
            args.output.write_text(rendered, encoding="utf-8")
        else:
            sys.stdout.write(rendered)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    return 0 if result["valid"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
