#!/usr/bin/env python3
"""Build a deterministic, read-only index of workspace claim records."""

from __future__ import annotations

import argparse
import json
import sys
import tempfile
from datetime import date, datetime
from pathlib import Path
from typing import Any

from validate_claims import load_claim_file, validate_claim


REVIEW_STATES = {"candidate", "stale", "contested", "expired", "review_due", "not_yet_valid"}


def _as_of(value: str) -> date:
    try:
        return date.fromisoformat(value)
    except ValueError as exc:
        raise ValueError("as-of must be an ISO-8601 date") from exc


def _claim_date(value: Any) -> date | None:
    if value in (None, ""):
        return None
    if isinstance(value, datetime):
        return value.date()
    if isinstance(value, date):
        return value
    if isinstance(value, str):
        try:
            return datetime.fromisoformat(value.replace("Z", "+00:00")).date()
        except ValueError:
            return None
    return None


def _state(claim: dict[str, Any], as_of: date) -> str:
    valid_from = _claim_date(claim.get("valid_from"))
    if valid_from and as_of < valid_from:
        return "not_yet_valid"
    valid_until = _claim_date(claim.get("valid_until"))
    if valid_until and as_of > valid_until:
        return "expired"
    status = str(claim.get("status") or "")
    if status in {"candidate", "stale", "contested", "superseded", "retracted"}:
        return status
    review_after = _claim_date(claim.get("review_after"))
    if status == "active" and review_after and as_of >= review_after:
        return "review_due"
    return "active"


def _public_record(claim: dict[str, Any], path: Path, workspace: Path, as_of: date) -> dict[str, Any]:
    state = _state(claim, as_of)
    fields = (
        "id",
        "claim",
        "kind",
        "status",
        "source",
        "provenance",
        "asserted_at",
        "observed_at",
        "valid_from",
        "valid_until",
        "review_after",
        "reviewed_at",
        "reviewed_by",
        "confidence",
        "confidence_basis",
        "supersedes",
    )
    record = {field: claim[field] for field in fields if field in claim}
    record.update(
        {
            "path": str(path.relative_to(workspace)),
            "state": state,
            "needs_review": state in REVIEW_STATES,
            "valid": True,
            "errors": [],
        }
    )
    return record


def index_claims(workspace: Path, as_of_value: str) -> dict[str, Any]:
    workspace = workspace.expanduser().resolve()
    as_of = _as_of(as_of_value)
    claims_directory = workspace / "knowledge" / "claims"
    if not claims_directory.is_dir():
        raise ValueError(f"claims directory does not exist: {claims_directory}")

    records: list[dict[str, Any]] = []
    issues: list[dict[str, Any]] = []
    for path in sorted(claims_directory.rglob("*.md")):
        relative_path = str(path.relative_to(workspace))
        try:
            claim = load_claim_file(path)
        except (OSError, ValueError) as exc:
            issue = {"path": relative_path, "errors": [str(exc)]}
            issues.append(issue)
            records.append(
                {
                    "path": relative_path,
                    "state": "invalid",
                    "needs_review": True,
                    "valid": False,
                    "errors": issue["errors"],
                }
            )
            continue

        errors = validate_claim(claim)
        if errors:
            issues.append({"path": relative_path, "id": str(claim.get("id") or ""), "errors": errors})
            records.append(
                {
                    "id": str(claim.get("id") or ""),
                    "claim": str(claim.get("claim") or ""),
                    "path": relative_path,
                    "state": "invalid",
                    "needs_review": True,
                    "valid": False,
                    "errors": errors,
                }
            )
            continue
        records.append(_public_record(claim, path, workspace, as_of))

    ids: dict[str, list[dict[str, Any]]] = {}
    for record in records:
        claim_id = record.get("id")
        if record["valid"] and claim_id:
            ids.setdefault(str(claim_id), []).append(record)
    for claim_id, duplicates in ids.items():
        if len(duplicates) < 2:
            continue
        message = f"duplicate claim id: {claim_id}"
        for record in duplicates:
            record["valid"] = False
            record["state"] = "invalid"
            record["needs_review"] = True
            record["errors"].append(message)
            issues.append({"path": record["path"], "id": claim_id, "errors": [message]})

    by_status: dict[str, int] = {}
    by_state: dict[str, int] = {}
    for record in records:
        if record["valid"]:
            status = str(record.get("status") or "unknown")
            by_status[status] = by_status.get(status, 0) + 1
        state = str(record.get("state") or "unknown")
        by_state[state] = by_state.get(state, 0) + 1
    return {
        "workspace": str(workspace),
        "claims_directory": str(claims_directory),
        "as_of": as_of.isoformat(),
        "claims": records,
        "issues": issues,
        "summary": {
            "total": len(records),
            "valid": sum(1 for record in records if record["valid"]),
            "invalid": sum(1 for record in records if not record["valid"]),
            "needs_review": sum(1 for record in records if record["needs_review"]),
            "by_status": dict(sorted(by_status.items())),
            "by_state": dict(sorted(by_state.items())),
        },
        "policy": "Read-only index; claim truth and evidence still require human review.",
    }


def _label(record: dict[str, Any]) -> str:
    statement = " ".join(str(record.get("claim") or "Untitled claim").split())
    return f"- [{record.get('state', 'unknown')}] {statement} ({record.get('path', 'unknown path')})"


def render_markdown(result: dict[str, Any]) -> str:
    summary = result["summary"]
    review = [record for record in result["claims"] if record["needs_review"]]
    active = [record for record in result["claims"] if record["valid"] and record["state"] == "active"]
    invalid = [record for record in result["claims"] if not record["valid"]]
    lines = [
        "# Claim Index",
        "",
        f"As of: {result['as_of']}",
        f"Workspace: {result['workspace']}",
        "",
        "## Summary",
        "",
        f"- Total records: {summary['total']}",
        f"- Valid records: {summary['valid']}",
        f"- Invalid records: {summary['invalid']}",
        f"- Needs review: {summary['needs_review']}",
        "",
        "## Claims To Review",
        "",
    ]
    if review:
        lines.extend(_label(record) for record in review)
    else:
        lines.append("- None")
    lines.extend(["", "## Active Claims", ""])
    if active:
        lines.extend(_label(record) for record in active)
    else:
        lines.append("- None")
    lines.extend(["", "## Invalid Records", ""])
    if invalid:
        lines.extend(_label(record) for record in invalid)
    else:
        lines.append("- None")
    lines.append("")
    return "\n".join(lines)


def self_test() -> None:
    valid = """---
id: claim-active
claim: User prefers local backups.
kind: reported
status: active
source:
  type: user
  ref: conversation-1
provenance:
  evidence_refs:
    - conversation-1
asserted_at: 2026-08-01
---
# Notes
"""
    candidate = valid.replace("claim-active", "claim-candidate").replace("status: active", "status: candidate")
    expired = valid.replace("claim-active", "claim-expired").replace("valid_until", "valid_until") + "\n"
    expired = expired.replace("asserted_at: 2026-08-01", "asserted_at: 2026-08-01\nvalid_until: 2026-08-10")
    with tempfile.TemporaryDirectory(prefix="openlia-claims-") as directory:
        workspace = Path(directory)
        claims_directory = workspace / "knowledge" / "claims"
        claims_directory.mkdir(parents=True)
        (claims_directory / "active.md").write_text(valid, encoding="utf-8")
        (claims_directory / "candidate.md").write_text(candidate, encoding="utf-8")
        (claims_directory / "expired.md").write_text(expired, encoding="utf-8")
        (claims_directory / "invalid.md").write_text("# Missing front matter\n", encoding="utf-8")
        result = index_claims(workspace, "2026-08-12")
        assert result["summary"]["total"] == 4
        assert result["summary"]["invalid"] == 1
        assert result["summary"]["needs_review"] == 3
        assert result["summary"]["by_state"]["expired"] == 1


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("workspace", nargs="?", type=Path, help="OpenLia workspace root")
    parser.add_argument("--as-of", required=False, help="ISO-8601 date used for validity and review checks")
    parser.add_argument("--format", choices=("json", "markdown"), default="json")
    parser.add_argument("--output", type=Path, help="Write the index to a file")
    parser.add_argument("--self-test", action="store_true", help="Run the built-in deterministic test")
    args = parser.parse_args(argv)
    if args.self_test:
        self_test()
        print("ok")
        return 0
    if args.workspace is None:
        parser.error("workspace is required unless --self-test is used")
    if not args.as_of:
        parser.error("--as-of is required unless --self-test is used")
    try:
        result = index_claims(args.workspace, args.as_of)
        rendered = json.dumps(result, indent=2, sort_keys=True, ensure_ascii=True) + "\n" if args.format == "json" else render_markdown(result)
        if args.output:
            args.output.write_text(rendered, encoding="utf-8")
        else:
            sys.stdout.write(rendered)
    except (OSError, ValueError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    return 0 if not result["issues"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
