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


KINDS = {"reported", "observed", "inferred", "hypothesis", "hypothetical"}
STATUSES = {"candidate", "active", "stale", "contested", "superseded", "retracted", "rejected"}
DATE_FIELDS = ("asserted_at", "observed_at", "valid_from", "valid_until", "review_after", "reviewed_at")
METADATA_FIELDS = {
    "id",
    "kind",
    "status",
    "source",
    "provenance",
    *DATE_FIELDS,
    "confidence",
    "confidence_basis",
    "reviewed_by",
    "supersedes",
}
CLAIM_FIELDS = METADATA_FIELDS | {"claim"}
LEGACY_FIELD_MIGRATIONS = {
    "claim_id": "rename it to `id`",
    "statement": "move its text into the required `## Claim` Markdown section",
    "first_recorded": "rename it to `asserted_at` or `observed_at`, as appropriate",
    "last_reviewed": "rename it to `reviewed_at`",
    "review_due": "rename it to `review_after`",
    "temporal_scope": "map its meaning to `valid_from`, `valid_until`, or `review_after`, or explain it under `## Notes`",
    "related_claims": "review each reference and move derivation links to `provenance.derived_from`",
}
FRONT_MATTER = re.compile(r"\A---[ \t]*\r?\n(?P<metadata>.*?)(?:\r?\n)---[ \t]*(?:\r?\n|\Z)", re.DOTALL)
MARKDOWN_HEADING = re.compile(r"^[ \t]{0,3}(#{1,6})[ \t]+(.+?)[ \t]*#*[ \t]*$")
CODE_FENCE = re.compile(r"^[ \t]{0,3}(`{3,}|~{3,})")


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
    legacy_text_fields = [field for field in ("claim", "statement") if field in metadata]
    if legacy_text_fields:
        fields = ", ".join(f"`{field}`" for field in legacy_text_fields)
        raise ValueError(
            f"front matter field {fields} is no longer supported; move the claim text into the required `## Claim` section and remove the field"
        )
    claim_text = _extract_claim_section(match.string[match.end() :])
    metadata["claim"] = claim_text
    return _normalize_yaml(metadata)


def _extract_claim_section(body: str) -> str:
    sections: list[list[str]] = []
    current: list[str] | None = None
    fence_character: str | None = None
    fence_length = 0

    for line in body.splitlines():
        fence_match = CODE_FENCE.match(line)
        if fence_character is not None:
            if fence_match and fence_match.group(1)[0] == fence_character and len(fence_match.group(1)) >= fence_length:
                fence_character = None
                fence_length = 0
            if current is not None:
                current.append(line)
            continue
        if fence_match:
            fence_character = fence_match.group(1)[0]
            fence_length = len(fence_match.group(1))
            if current is not None:
                current.append(line)
            continue

        heading = MARKDOWN_HEADING.match(line)
        if heading:
            level = len(heading.group(1))
            title = re.sub(r"[ \t]+#+[ \t]*$", "", heading.group(2)).strip()
            if current is not None and level <= 2:
                sections.append(current)
                current = None
            if level == 2 and title == "Claim":
                current = []
            continue
        if current is not None:
            current.append(line)

    if current is not None:
        sections.append(current)
    if not sections:
        raise ValueError(
            "missing required Markdown section `## Claim`; move legacy claim text from front matter or rename `## Statement` to `## Claim`"
        )
    if len(sections) > 1:
        raise ValueError("Markdown record must contain exactly one `## Claim` section")
    claim_text = "\n".join(sections[0]).strip()
    if not claim_text:
        raise ValueError("required Markdown section `## Claim` must contain non-whitespace text")
    return claim_text


def load_claim_file(path: Path) -> dict[str, Any]:
    return parse_claim_markdown(path.read_text(encoding="utf-8"))


def _as_nonempty_string(value: Any, field: str, errors: list[str]) -> None:
    if not isinstance(value, str) or not value.strip():
        errors.append(f"{field} must be a non-empty string")


def _parse_date(value: Any, field: str, errors: list[str]) -> datetime | None:
    if value is None:
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
        errors.append(f"{field} must be a {'non-empty ' if required else ''}list of strings")
        return
    valid_list = isinstance(value, list) and (not required or bool(value))
    valid_items = valid_list and all(isinstance(item, str) and item.strip() for item in value)
    if not valid_items:
        errors.append(f"{field} must be a {'non-empty ' if required else ''}list of strings")


def validate_claim(claim: Any) -> list[str]:
    if not isinstance(claim, dict):
        return ["claim must be an object"]

    errors: list[str] = []
    for field in sorted(claim, key=str):
        if field in CLAIM_FIELDS:
            continue
        migration = LEGACY_FIELD_MIGRATIONS.get(str(field))
        if migration:
            errors.append(f"legacy field {field!r} is not supported; {migration}")
        else:
            errors.append(f"unsupported claim field {field!r}")

    _as_nonempty_string(claim.get("id"), "id", errors)
    _as_nonempty_string(claim.get("claim"), "claim text from `## Claim`", errors)

    kind = claim.get("kind")
    if not isinstance(kind, str) or kind not in KINDS:
        errors.append(f"kind must be one of: {', '.join(sorted(KINDS))}")

    status = claim.get("status")
    if not isinstance(status, str) or status not in STATUSES:
        errors.append(f"status must be one of: {', '.join(sorted(STATUSES))}")

    source = claim.get("source")
    if isinstance(source, dict):
        for field in sorted(source, key=str):
            if field not in {"type", "ref"}:
                errors.append(f"unsupported source field {field!r}")
        _as_nonempty_string(source.get("type"), "source.type", errors)
        _as_nonempty_string(source.get("ref"), "source.ref", errors)
    else:
        errors.append("source must be an object with non-empty `type` and `ref` strings")

    provenance = claim.get("provenance")
    if isinstance(provenance, dict):
        for field in sorted(provenance, key=str):
            if field not in {"evidence_refs", "derived_from"}:
                errors.append(f"unsupported provenance field {field!r}")
        _validate_references(provenance.get("evidence_refs"), "provenance.evidence_refs", errors)
        if "derived_from" not in provenance:
            errors.append("provenance.derived_from must be a list of strings (use [] when there are no derived claims)")
        else:
            _validate_references(provenance.get("derived_from"), "provenance.derived_from", errors, required=False)
    else:
        errors.append("provenance must be an object with `evidence_refs` and `derived_from` lists")

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
        if isinstance(confidence, str) and confidence.lower() in {"low", "medium", "high"}:
            pass
        elif isinstance(confidence, bool) or not isinstance(confidence, (int, float)) or not 0 <= confidence <= 1:
            errors.append("confidence must be a number between 0 and 1 or low/medium/high")
    requires_confidence = isinstance(kind, str) and kind in {"inferred", "hypothesis", "hypothetical"}
    if requires_confidence and confidence is None:
        errors.append("confidence is required for inferred, hypothesis, and hypothetical claims")
    if confidence is not None or requires_confidence:
        _as_nonempty_string(claim.get("confidence_basis"), "confidence_basis", errors)
    elif claim.get("confidence_basis") is not None:
        _as_nonempty_string(claim.get("confidence_basis"), "confidence_basis", errors)

    if claim.get("supersedes") is not None:
        _as_nonempty_string(claim.get("supersedes"), "supersedes", errors)
    if claim.get("reviewed_by") is not None:
        _as_nonempty_string(claim.get("reviewed_by"), "reviewed_by", errors)

    if requires_confidence and status == "active":
        if claim.get("reviewed_at") is None:
            errors.append("reviewed_at is required before an inferred, hypothesis, or hypothetical claim can be active")
        if claim.get("reviewed_by") is None:
            errors.append("reviewed_by is required before an inferred, hypothesis, or hypothetical claim can be active")

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
    template_path = Path(__file__).parent.parent / "templates" / "claim-record.md"
    template = template_path.read_text(encoding="utf-8")
    completed_template = (
        template.replace("claim-YYYYMMDD-short-slug", "claim-template-example")
        .replace('"replace-with-stable-source-reference"', '"conversation:example:1"')
        .replace('"replace-with-evidence-reference"', '"conversation:example:1"')
        .replace('"YYYY-MM-DD"', '"2026-08-12"')
        .replace("[Write one specific, falsifiable claim statement here.]", "The template record validates after required values are filled.")
    )
    parsed = parse_claim_markdown(completed_template)
    assert parsed["asserted_at"] == "2026-08-12"
    assert parsed["claim"] == "The template record validates after required values are filled."
    assert validate_claim(parsed) == []

    for markdown, expected_error in (
        (completed_template.replace("## Claim", "## Statement"), "missing required Markdown section `## Claim`"),
        (completed_template.replace("## Claim\n\nThe template record validates after required values are filled.", "## Claim\n\n## Evidence"), "must contain non-whitespace text"),
    ):
        try:
            parse_claim_markdown(markdown)
        except ValueError as exc:
            assert expected_error in str(exc)
        else:
            raise AssertionError(f"expected parsing to fail with: {expected_error}")

    try:
        parse_claim_markdown("---\nid: one\nid: two\n---\n")
    except ValueError as exc:
        assert "duplicate YAML key" in str(exc)
    else:
        raise AssertionError("duplicate YAML keys must be rejected")

    try:
        parse_claim_markdown(
            "---\nclaim_id: legacy-id\nstatement: Legacy statement\n---\n\n## Claim\n\nNew statement.\n"
        )
    except ValueError as exc:
        assert "front matter field `statement`" in str(exc)
        assert "move the claim text into the required `## Claim` section" in str(exc)
    else:
        raise AssertionError("legacy frontmatter claim text must produce a migration error")

    valid = validate_claims(
        {
            "claims": [
                {
                    "id": "claim-laptop-plan",
                    "claim": "User plans to replace their laptop this year.",
                    "kind": "reported",
                    "status": "active",
                    "source": {"type": "user", "ref": "conversation-2026-08-12"},
                    "provenance": {"evidence_refs": ["conversation-2026-08-12"], "derived_from": []},
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
                    "provenance": {"evidence_refs": [], "derived_from": []},
                    "observed_at": "not-a-date",
                    "confidence": 1.2,
                }
            ]
        }
    )
    assert invalid["valid"] is False
    assert len(invalid["claims"][0]["errors"]) >= 3
    assert any("confidence_basis" in error for error in invalid["claims"][0]["errors"])
    assert any("reviewed_at" in error for error in invalid["claims"][0]["errors"])

    legacy = validate_claim(
        {
            "claim_id": "legacy-id",
            "claim": "Legacy statement",
            "kind": "reported",
            "status": "candidate",
            "source": "legacy source",
            "provenance": "legacy provenance",
            "first_recorded": "2026-08-12",
        }
    )
    assert any("rename it to `id`" in error for error in legacy)
    assert any("source must be an object" in error for error in legacy)
    assert any("provenance must be an object" in error for error in legacy)


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
