#!/usr/bin/env python3
"""Check bundled workspace and skill templates against their user-facing contracts."""

from __future__ import annotations

import argparse
import importlib.util
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
BUNDLE_ROOT = ROOT / "workspace-template"
SKILLS_ROOT = ROOT / "profile" / "skills"
CLAIM_TEMPLATE = BUNDLE_ROOT / "knowledge" / "claims" / "claim-template.md"
RUNTIME_VALIDATOR = (
    ROOT
    / "profile"
    / "system-skills"
    / "workspace-template-customization"
    / "scripts"
    / "validate_workspace.py"
)
_spec = importlib.util.spec_from_file_location("workspace_template_validator", RUNTIME_VALIDATOR)
if _spec is None or _spec.loader is None:
    raise RuntimeError(f"cannot load workspace validator: {RUNTIME_VALIDATOR}")
validator = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = validator
_spec.loader.exec_module(validator)


TEMPLATE_HEADINGS = {
    "goals/goal-template.md": (
        "<Goal Title>",
        (
            "Objective",
            "Linked Areas & Projects",
            "Key Results & Milestones",
            "Current Focus & Next Actions",
        ),
    ),
    "areas/area-template.md": (
        "<Area Title>",
        (
            "Scope & Purpose",
            "Standards & Principles",
            "Recurring Responsibilities",
            "Active Projects",
            "Key People & Resources",
        ),
    ),
    "projects/project-template.md": (
        "<Project Title>",
        (
            "Objective",
            "Constraints",
            "Milestones",
            "Next Tasks",
            "Risks & Open Issues",
            "Key Decisions",
        ),
    ),
    "decisions/decision-template.md": (
        "<Decision Title>",
        (
            "Question",
            "Context And Constraints",
            "Options Considered",
            "Criteria And Weights",
            "Evidence & Claims",
            "Chosen Option & Rationale",
            "Review Outcome",
        ),
    ),
    "monitors/monitor-template.md": (
        "<Monitor Title>",
        ("Target Condition", "Trigger Rule", "Recommended Action", "Check Log"),
    ),
    "tasks/task-template.md": (
        "<Task Title>",
        ("Action", "Checklist / Substeps", "Completion Note"),
    ),
    "people/person-template.md": (
        "<Person Name>",
        ("Open Loops & Commitments", "Interaction Notes & Context"),
    ),
    "ideas/idea-template.md": (
        "<Idea Title>",
        (
            "Concept",
            "Potential Value & Opportunity",
            "Key Questions & Uncertainties",
            "Next Exploration Step",
        ),
    ),
    "travel/trip-template.md": (
        "<Trip Title>",
        (
            "Itinerary Outline",
            "Reservations & Confirmations",
            "Packing & Preparation Checklist",
            "Budget & Expenses",
        ),
    ),
    "shopping/item-template.md": (
        "<Item / Purchase Title>",
        (
            "Budget & Price Targets",
            "Candidate Options",
            "Decision Criteria",
            "Purchase History",
        ),
    ),
    "finance/finance-template.md": (
        "<Finance Review / Plan Title>",
        (
            "Spending Summary & Targets",
            "Accounts & Net Assets (Non-Secret)",
            "Active Financial Goals",
            "Recurring Subscriptions & Audits",
        ),
    ),
    "calendar/event-note-template.md": (
        "<Event Title>",
        ("Purpose & Agenda", "Discussion & Raw Notes", "Follow-up Action Items"),
    ),
    "knowledge/claims/claim-template.md": (
        "<Claim Record>",
        ("Claim", "Evidence", "Notes"),
    ),
}

SKILL_TEMPLATE_HEADINGS = {
    "deep-research/templates/research-brief.md": (
        "Research Brief",
        (
            "Question",
            "Scope And Decision Relevance",
            "Executive Summary",
            "Claims And Evidence",
            "Evidence Gaps",
            "Next Checks",
            "Sources",
        ),
    ),
    "project-review/templates/project-record.md": (
        "Project Record",
        ("Objective", "Status", "Milestones", "Next Tasks", "Risks", "Decisions"),
    ),
    "decision-analysis/templates/decision-record.md": (
        "Decision Record",
        (
            "Question",
            "Context And Constraints",
            "Options",
            "Criteria And Weights",
            "Evidence Gaps",
            "Chosen Option",
            "Review Date And Outcome",
        ),
    ),
    "daily-briefing/templates/briefing.md": (
        "Daily Briefing",
        ("Today", "What Changed", "Important-Urgent Matrix"),
    ),
    "weekly-review/templates/weekly-review.md": (
        "Weekly Review",
        (
            "Completed",
            "Active Projects",
            "Open Loops",
            "Decisions To Revisit",
            "Claims To Review",
            "Next Week",
        ),
    ),
    "workspace-organize/templates/organize-report.md": (
        "Workspace Organize Scout Report - YYYY-MM-DD",
        (
            "Scope and method",
            "Proposals",
            "Summary",
            "Items not proposed due to insufficient evidence",
        ),
    ),
    "inbox-triage/templates/inbox-item.md": ("Inbox Item", ()),
}
SKILL_TEMPLATE_NESTED_HEADINGS = {
    "daily-briefing/templates/briefing.md": {
        "Today": ("Focus", "Calendar", "Tasks", "Follow-ups", "Monitors"),
        "What Changed": ("Facts", "Signal", "Missing evidence", "Questions"),
        "Important-Urgent Matrix": (
            "Do first",
            "Schedule",
            "Delegate / Coordinate",
            "Defer / Drop",
        ),
    },
    "workspace-organize/templates/organize-report.md": {
        "Proposals": ("O-YYYY-MM-DD-001",),
    },
}


def _claim_schema_errors() -> list[str]:
    errors: list[str] = []
    try:
        metadata, _ = validator.parse_frontmatter(CLAIM_TEMPLATE.read_text(encoding="utf-8"))
        schema_path = validator._resolve_template_schema(
            metadata.get("$schema"), CLAIM_TEMPLATE, BUNDLE_ROOT
        )
        schema = validator.load_schema(schema_path)
    except (OSError, ValueError) as exc:
        return [f"knowledge/claims/claim-template.md: {exc}"]

    inferred = validator._without_schema_directive(metadata)
    inferred["kind"] = "inferred"
    inferred["confidence"] = "medium"
    inferred["confidence_basis"] = "Supported by repeated observations."
    if validator.validate_frontmatter(inferred, schema):
        errors.append("claim schema rejected an inferred candidate with confidence and basis")

    inferred["status"] = "active"
    if not validator.validate_frontmatter(inferred, schema):
        errors.append("claim schema accepted an active inference without review metadata")
    inferred["reviewed_at"] = "2026-10-06"
    inferred["reviewed_by"] = "user"
    if validator.validate_frontmatter(inferred, schema):
        errors.append("claim schema rejected an active inference with review metadata")

    unsupported = validator._without_schema_directive(metadata)
    unsupported["legacy_field"] = "not allowed"
    if not validator.validate_frontmatter(unsupported, schema):
        errors.append("claim schema accepted an unsupported frontmatter field")
    return errors


def validate_bundle() -> list[str]:
    errors = validator.validate_workspace(BUNDLE_ROOT)
    errors.extend(validator.validate_skill_templates(SKILLS_ROOT))
    expected = set(TEMPLATE_HEADINGS)
    discovered = {
        path.relative_to(BUNDLE_ROOT).as_posix()
        for path in BUNDLE_ROOT.rglob("*-template.md")
    }
    if discovered != expected:
        missing = sorted(expected - discovered)
        unregistered = sorted(discovered - expected)
        if missing:
            errors.append(f"missing registered templates: {', '.join(missing)}")
        if unregistered:
            errors.append(f"unregistered template files: {', '.join(unregistered)}")

    expected_schemas: set[str] = set()
    for path in BUNDLE_ROOT.rglob("*-template.md"):
        try:
            metadata, _ = validator.parse_frontmatter(path.read_text(encoding="utf-8"))
            schema_path = validator._resolve_template_schema(
                metadata.get("$schema"), path, BUNDLE_ROOT
            )
        except (OSError, ValueError) as exc:
            errors.append(f"{path.relative_to(BUNDLE_ROOT).as_posix()}: {exc}")
            continue
        expected_schemas.add(schema_path.relative_to(BUNDLE_ROOT).as_posix())
    discovered_schemas = {
        path.relative_to(BUNDLE_ROOT).as_posix()
        for path in BUNDLE_ROOT.rglob("*.schema.json")
        if path.name != "workspace.schema.json"
    }
    if discovered_schemas != expected_schemas:
        missing = sorted(expected_schemas - discovered_schemas)
        unregistered = sorted(discovered_schemas - expected_schemas)
        if missing:
            errors.append(f"missing referenced template schemas: {', '.join(missing)}")
        if unregistered:
            errors.append(f"unregistered template schemas: {', '.join(unregistered)}")

    for relative, (title, sections) in TEMPLATE_HEADINGS.items():
        path = BUNDLE_ROOT / relative
        try:
            _, body = validator.parse_frontmatter(path.read_text(encoding="utf-8"))
        except (OSError, ValueError) as exc:
            errors.append(f"{relative}: {exc}")
            continue
        expected_headings = [(1, title)] + [(2, section) for section in sections]
        actual_headings = validator.extract_headings(body)
        if actual_headings != expected_headings:
            errors.append(
                f"{relative}: headings must be exactly {expected_headings!r}; "
                f"found {actual_headings!r}"
            )
        if relative == "daily-briefing/templates/briefing.md" and ("{{" in body or "}}" in body):
            errors.append(f"{relative}: renderer-style placeholders are not allowed in direct-write templates")

    discovered_skill_templates = {
        path.relative_to(SKILLS_ROOT).as_posix()
        for path in SKILLS_ROOT.glob("*/templates/*.md")
    }
    expected_skill_templates = set(SKILL_TEMPLATE_HEADINGS)
    if discovered_skill_templates != expected_skill_templates:
        missing = sorted(expected_skill_templates - discovered_skill_templates)
        unregistered = sorted(discovered_skill_templates - expected_skill_templates)
        if missing:
            errors.append(f"missing registered skill templates: {', '.join(missing)}")
        if unregistered:
            errors.append(f"unregistered skill templates: {', '.join(unregistered)}")

    for relative, (title, sections) in SKILL_TEMPLATE_HEADINGS.items():
        path = SKILLS_ROOT / relative
        try:
            _, body = validator.parse_optional_frontmatter(path.read_text(encoding="utf-8"))
        except (OSError, ValueError) as exc:
            errors.append(f"{relative}: {exc}")
            continue
        expected_headings = [(1, title)]
        nested = SKILL_TEMPLATE_NESTED_HEADINGS.get(relative, {})
        for section in sections:
            expected_headings.append((2, section))
            expected_headings.extend((3, heading) for heading in nested.get(section, ()))
        actual_headings = validator.extract_headings(body)
        if actual_headings != expected_headings:
            errors.append(
                f"{relative}: headings must be exactly {expected_headings!r}; "
                f"found {actual_headings!r}"
            )

    errors.extend(_claim_schema_errors())
    return sorted(set(errors))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--self-test", action="store_true", help="run validator self-tests")
    args = parser.parse_args(argv)
    if args.self_test:
        validator.self_test()
        print("ok")
        return 0

    errors = validate_bundle()
    if errors:
        for error in errors:
            print(f"error: {error}", file=sys.stderr)
        return 1
    print(
        f"Validated {len(TEMPLATE_HEADINGS)} bundled workspace templates and "
        f"{len(SKILL_TEMPLATE_HEADINGS)} bundled skill templates."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
