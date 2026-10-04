#!/usr/bin/env python3
"""Deterministic workspace hygiene and integrity audit linter."""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import tempfile
from pathlib import Path
from typing import Any

CANONICAL_DOMAINS = {
    "inbox",
    "goals",
    "areas",
    "projects",
    "knowledge",
    "ideas",
    "decisions",
    "monitors",
    "tasks",
    "calendar",
    "people",
    "shopping",
    "travel",
    "finance",
    "archive",
}

ALLOWED_ROOT_FILES = {
    "README.md",
    "AGENTS.md",
    ".gitignore",
    ".git",
    ".DS_Store",
}

MD_LINK_PATTERN = re.compile(r"\[([^\]]+)\]\((?!https?://|mailto:|/files/)([^)#]+)(?:#[^)]+)?\)")
WIKI_LINK_PATTERN = re.compile(r"\[\[([^\]|]+)(?:\|[^\]]+)?\]\]")
URI_SCHEME_PATTERN = re.compile(r"^[a-z][a-z0-9+.-]*:", re.IGNORECASE)
FENCE_PATTERN = re.compile(r"^ {0,3}(`{3,}|~{3,})(.*)$")
INLINE_CODE_PATTERN = re.compile(r"(?<!`)(`+)(?!`).*?(?<!`)\1(?!`)", re.DOTALL)
CHECKBOX_PATTERN = re.compile(r"^\s*-\s*\[([ xX])\]", re.MULTILINE)
STATUS_PATTERN = re.compile(r"(?i)^status:\s*([a-z0-9_-]+)", re.MULTILINE)


def _blank_code_text(text: str) -> str:
    return "".join(
        "\n" if char == "\n" else "\r" if char == "\r" else " "
        for char in text
    )


def _blank_code(match: re.Match[str]) -> str:
    return _blank_code_text(match.group())


def mask_markdown_code(content: str) -> str:
    """Blank fenced and inline code so their examples are not treated as links."""
    masked_lines = []
    fence_char = None
    fence_length = 0

    for line in content.splitlines(keepends=True):
        line_content = line.rstrip("\r\n")
        if fence_char:
            closing_fence = re.match(
                rf"^ {{0,3}}{re.escape(fence_char)}{{{fence_length},}}[ \t]*$",
                line_content,
            )
            masked_lines.append(_blank_code_text(line))
            if closing_fence:
                fence_char = None
                fence_length = 0
            continue

        opening_fence = FENCE_PATTERN.match(line_content)
        if opening_fence:
            marker = opening_fence.group(1)
            if marker[0] == "~" or "`" not in opening_fence.group(2):
                fence_char = marker[0]
                fence_length = len(marker)
                masked_lines.append(_blank_code_text(line))
                continue

        masked_lines.append(line)

    masked = "".join(masked_lines)
    return INLINE_CODE_PATTERN.sub(_blank_code, masked)


def audit_root_files(workspace: Path) -> list[dict[str, Any]]:
    issues = []
    try:
        for entry in workspace.iterdir():
            if entry.name in ALLOWED_ROOT_FILES or entry.name.startswith("."):
                continue
            if entry.is_file():
                issues.append({
                    "type": "stray-root-file",
                    "file": entry.name,
                    "observation": f"File '{entry.name}' is located at workspace root outside canonical domains.",
                    "suggestion": "Move file into an appropriate domain (e.g. inbox/, knowledge/, or archive/).",
                })
            elif entry.is_dir() and entry.name not in CANONICAL_DOMAINS:
                issues.append({
                    "type": "non-canonical-directory",
                    "file": entry.name,
                    "observation": f"Directory '{entry.name}/' is not one of the 15 canonical Personal OS domains.",
                    "suggestion": "Review whether contents belong inside a canonical domain or archive/.",
                })
    except Exception as e:
        issues.append({"type": "audit-error", "file": ".", "observation": str(e), "suggestion": "Check permissions."})
    return issues


def audit_inbox(workspace: Path) -> list[dict[str, Any]]:
    issues = []
    inbox_dir = workspace / "inbox"
    if not inbox_dir.is_dir():
        return issues
    for root, _, files in os.walk(inbox_dir):
        for file in files:
            if file.startswith(".") or file == "README.md":
                continue
            rel_path = Path(root, file).relative_to(workspace).as_posix()
            issues.append({
                "type": "inbox-unfiled",
                "file": rel_path,
                "observation": f"Captured item '{rel_path}' is pending review and filing.",
                "suggestion": "Triage into tasks/, calendar/, knowledge/claims/, or archive/.",
            })
    return issues


def audit_broken_links(workspace: Path) -> list[dict[str, Any]]:
    issues = []
    for root, _, files in os.walk(workspace):
        # Skip git or hidden dirs
        if "/." in root or "\\." in root:
            continue
        for file in files:
            # Skip templates as they contain placeholder links like [[Project Name]]
            if not file.endswith(".md") or file.endswith("-template.md"):
                continue
            file_path = Path(root, file)
            rel_file_path = file_path.relative_to(workspace).as_posix()
            try:
                content = file_path.read_text(encoding="utf-8", errors="replace")
            except Exception:
                continue

            content = mask_markdown_code(content)

            # Check markdown links
            for match in MD_LINK_PATTERN.finditer(content):
                target = match.group(2).strip()
                if (
                    not target
                    or "<" in target
                    or ">" in target
                    or URI_SCHEME_PATTERN.match(target)
                ):
                    continue
                # Resolve relative target
                target_path = (file_path.parent / target).resolve()
                try:
                    if not target_path.exists():
                        issues.append({
                            "type": "broken-link",
                            "file": rel_file_path,
                            "observation": f"Reference to '{target}' does not resolve to an existing file.",
                            "suggestion": f"Fix broken link target or update reference in {rel_file_path}.",
                        })
                except Exception:
                    pass

            # Check wikilinks
            for match in WIKI_LINK_PATTERN.finditer(content):
                target_name = match.group(1).strip()
                if not target_name or "<" in target_name or ">" in target_name:
                    continue
                # Wikilinks typically resolve by filename across workspace
                found = False
                for candidate in (target_name, f"{target_name}.md"):
                    if (workspace / candidate).exists():
                        found = True
                        break
                    # Search across subdirectories
                    matches = list(workspace.glob(f"**/{candidate}"))
                    if matches:
                        found = True
                        break
                if not found:
                    issues.append({
                        "type": "broken-wikilink",
                        "file": rel_file_path,
                        "observation": f"Wikilink '[[{target_name}]]' does not resolve to any workspace document.",
                        "suggestion": f"Check if target document was renamed, moved, or deleted.",
                    })
    return issues


def audit_completed_projects(workspace: Path) -> list[dict[str, Any]]:
    issues = []
    projects_dir = workspace / "projects"
    if not projects_dir.is_dir():
        return issues
    for root, _, files in os.walk(projects_dir):
        for file in files:
            if not file.endswith(".md") or file in ("project-template.md", "README.md"):
                continue
            file_path = Path(root, file)
            rel_path = file_path.relative_to(workspace).as_posix()
            try:
                content = file_path.read_text(encoding="utf-8", errors="replace")
            except Exception:
                continue

            # Check status header
            status_match = STATUS_PATTERN.search(content)
            is_completed_status = status_match and status_match.group(1).lower() in ("completed", "done", "closed")

            # Check task checkboxes
            boxes = CHECKBOX_PATTERN.findall(content)
            all_checked = len(boxes) > 0 and all(b.lower() == "x" for b in boxes)

            if is_completed_status or all_checked:
                issues.append({
                    "type": "completed-project",
                    "file": rel_path,
                    "observation": f"Project '{rel_path}' appears finished (all tasks checked or status completed).",
                    "suggestion": f"Move to archive/projects/{file} to keep active project list clean.",
                })
    return issues


def audit_claims(workspace: Path) -> list[dict[str, Any]]:
    issues = []
    claims_dir = workspace / "knowledge" / "claims"
    if not claims_dir.is_dir():
        return issues
    for root, _, files in os.walk(claims_dir):
        for file in files:
            if not file.endswith(".md") or file in ("claim-record.md", "README.md"):
                continue
            file_path = Path(root, file)
            rel_path = file_path.relative_to(workspace).as_posix()
            try:
                content = file_path.read_text(encoding="utf-8", errors="replace")
            except Exception:
                continue
            status_match = STATUS_PATTERN.search(content)
            if status_match and status_match.group(1).lower() in ("provisional", "candidate", "disputed"):
                issues.append({
                    "type": "provisional-claim",
                    "file": rel_path,
                    "observation": f"Claim in '{rel_path}' is '{status_match.group(1)}' and requires verification.",
                    "suggestion": "Review supporting evidence or update status to active/retracted.",
                })
    return issues


def run_audit(workspace_dir: str | Path) -> dict[str, Any]:
    workspace = Path(workspace_dir).resolve()
    if not workspace.is_dir():
        raise FileNotFoundError(f"Workspace directory not found: {workspace}")

    root_issues = audit_root_files(workspace)
    inbox_issues = audit_inbox(workspace)
    broken_link_issues = audit_broken_links(workspace)
    project_issues = audit_completed_projects(workspace)
    claim_issues = audit_claims(workspace)

    all_issues = root_issues + inbox_issues + broken_link_issues + project_issues + claim_issues
    return {
        "workspace": str(workspace),
        "total_issues": len(all_issues),
        "summary": {
            "stray_root_files": len(root_issues),
            "unfiled_inbox_items": len(inbox_issues),
            "broken_links": len(broken_link_issues),
            "completed_projects": len(project_issues),
            "claims_requiring_review": len(claim_issues),
        },
        "issues": all_issues,
    }


def self_test() -> None:
    with tempfile.TemporaryDirectory() as tmp_str:
        tmp = Path(tmp_str)
        # Create canonical dirs
        for d in ("inbox", "projects", "knowledge/claims"):
            (tmp / d).mkdir(parents=True)

        # 1. Stray root file
        (tmp / "scratch_notes.txt").write_text("random scratch note", encoding="utf-8")

        # 2. Unfiled inbox item
        (tmp / "inbox" / "2026-09-trip-notes.md").write_text("Trip notes", encoding="utf-8")

        # 3. Broken markdown link
        (tmp / "projects" / "website.md").write_text(
            "status: active\n- [ ] Design [missing doc](missing_spec.md)\n- [x] Wireframe\n",
            encoding="utf-8",
        )

        # 4. Completed project
        (tmp / "projects" / "garden.md").write_text(
            "status: completed\n- [x] Buy seeds\n- [x] Plant seeds\n",
            encoding="utf-8",
        )

        # 5. Provisional claim
        (tmp / "knowledge" / "claims" / "c001.md").write_text(
            "---\nstatus: provisional\n---\nUser prefers window seats.\n",
            encoding="utf-8",
        )

        result = run_audit(tmp)
        assert result["total_issues"] == 5, f"Expected 5 issues, got {result['total_issues']}: {result['issues']}"
        assert result["summary"]["stray_root_files"] == 1
        assert result["summary"]["unfiled_inbox_items"] == 1
        assert result["summary"]["broken_links"] == 1
        assert result["summary"]["completed_projects"] == 1
        assert result["summary"]["claims_requiring_review"] == 1

        # Also test on a clean directory
        clean_dir = tmp / "clean"
        clean_dir.mkdir()
        (clean_dir / "README.md").write_text("# Clean", encoding="utf-8")
        clean_result = run_audit(clean_dir)
        assert clean_result["total_issues"] == 0, f"Expected 0 issues for clean workspace, got {clean_result}"

        # Example links in code and non-file URIs are not workspace file references.
        link_test = tmp / "link-test"
        (link_test / "goals").mkdir(parents=True)
        (link_test / "goals" / "career.md").write_text("Career goal", encoding="utf-8")
        (link_test / "AGENTS.md").write_text(
            "`[Career Goal](openlia://workspace/goals/career.md)`\n"
            "`[Inbox Triage](openlia://skills/inbox-triage)`\n"
            "`[Specs](./specs.md)`\n"
            "[Canonical URI](openlia://workspace/goals/career.md)\n"
            "[Existing relative link](goals/career.md)\n"
            "[Broken relative link](goals/missing.md)\n"
            "```markdown\n"
            "[Fenced example](./fenced-missing.md)\n"
            "```\n",
            encoding="utf-8",
        )
        link_result = run_audit(link_test)
        broken_links = [issue for issue in link_result["issues"] if issue["type"] == "broken-link"]
        assert len(broken_links) == 1, (
            f"Expected only the active broken relative link, got {broken_links}"
        )
        assert "goals/missing.md" in broken_links[0]["observation"]


def main() -> None:
    parser = argparse.ArgumentParser(description="Deterministic workspace organize audit linter")
    parser.add_argument("workspace", nargs="?", default=".", help="Workspace path to audit (default: .)")
    parser.add_argument("--self-test", action="store_true", help="Run deterministic offline self-tests")
    parser.add_argument("--json", action="store_true", help="Output findings as JSON")
    args = parser.parse_args()

    if args.self_test:
        self_test()
        print("ok")
        sys.exit(0)

    try:
        report = run_audit(args.workspace)
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)

    if args.json:
        print(json.dumps(report, indent=2))
    else:
        print(f"=== Workspace Organize Audit ===")
        print(f"Workspace: {report['workspace']}")
        print(f"Total issues found: {report['total_issues']}")
        for k, v in report["summary"].items():
            print(f"  - {k.replace('_', ' ').capitalize()}: {v}")
        if report["issues"]:
            print("\nFindings:")
            for idx, issue in enumerate(report["issues"], 1):
                print(f"  {idx}. [{issue['type']}] {issue['file']}")
                print(f"     Observation: {issue['observation']}")
                print(f"     Suggestion:  {issue['suggestion']}")


if __name__ == "__main__":
    main()
