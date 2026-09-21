#!/usr/bin/env python3
"""Unified Skill Verification Suite for OpenLia developers.

Separates offline unit self-tests from live Playwright MCP verifications,
ensuring skills are robustly tested before deployment to live agents.
"""

from __future__ import annotations

import argparse
import glob
import os
import subprocess
import sys
import time

SKILLS_DIR = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "profile", "skills")
LIVE_VERIFIABLE_SKILLS = ["browser-pilot", "gemini-chat", "chatgpt-chat"]


def run_command(cmd: list[str], timeout: float = 120.0) -> tuple[int, str, str, float]:
    """Execute command with timing and captured output."""
    t0 = time.time()
    try:
        proc = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=timeout)
        duration = time.time() - t0
        return proc.returncode, proc.stdout, proc.stderr, duration
    except subprocess.TimeoutExpired:
        return 124, "", f"Timed out after {timeout}s", time.time() - t0
    except Exception as e:
        return 1, "", str(e), time.time() - t0


def run_offline_tests(selected_skill: str = "") -> bool:
    """Run deterministic offline self-tests across all skill scripts."""
    print("\n" + "=" * 65)
    print("  RUNNING OFFLINE SKILL SELF-TESTS (--self-test)")
    print("=" * 65)

    pattern = os.path.join(SKILLS_DIR, selected_skill or "*", "scripts", "*.py")
    scripts = sorted(glob.glob(pattern))

    if not scripts:
        print(f"No skill scripts found matching pattern: {pattern}")
        return False

    all_passed = True
    results = []

    for script in scripts:
        skill_name = os.path.basename(os.path.dirname(os.path.dirname(script)))
        script_name = os.path.basename(script)
        code, stdout, stderr, duration = run_command([sys.executable, script, "--self-test"], timeout=15)
        passed = (code == 0)
        if not passed:
            all_passed = False
        results.append((skill_name, script_name, passed, duration, stderr.strip() or stdout.strip()))

    # Print table
    print(f"{'Skill':<20} {'Script':<26} {'Status':<10} {'Duration':<10}")
    print("-" * 68)
    for skill, script, passed, dur, _ in results:
        status_str = "\033[92mPASS\033[0m" if passed else "\033[91mFAIL\033[0m"
        print(f"{skill:<20} {script:<26} {status_str:<19} {dur:.2f}s")

    # Print failures if any
    for skill, script, passed, _, details in results:
        if not passed:
            print(f"\n[FAIL DETAILED TRACE] {skill} -> {script}:")
            print(details)

    print("=" * 65 + "\n")
    return all_passed


def run_live_verifications(selected_skill: str = "", mcp_url: str = "") -> bool:
    """Run live verification against Playwright MCP."""
    print("\n" + "=" * 65)
    print("  RUNNING LIVE BROWSER VERIFICATIONS (--verify)")
    print("=" * 65)

    target_skills = [selected_skill] if selected_skill else LIVE_VERIFIABLE_SKILLS
    all_passed = True
    results = []

    for skill in target_skills:
        pattern = os.path.join(SKILLS_DIR, skill, "scripts", "*.py")
        scripts = sorted(glob.glob(pattern))
        if not scripts:
            print(f"Skipping {skill}: script not found")
            continue

        script = scripts[0]
        script_name = os.path.basename(script)
        cmd = [sys.executable, script, "--verify"]
        if mcp_url:
            cmd.append(mcp_url)

        print(f"Verifying {skill} via {script_name}...")
        code, stdout, stderr, duration = run_command(cmd, timeout=90)
        passed = (code == 0)
        if not passed:
            all_passed = False
        output_snippet = (stdout.strip() or stderr.strip())
        results.append((skill, script_name, passed, duration, output_snippet))

    print("-" * 68)
    print(f"{'Skill':<20} {'Script':<26} {'Status':<10} {'Duration':<10}")
    print("-" * 68)
    for skill, script, passed, dur, _ in results:
        status_str = "\033[92mPASS\033[0m" if passed else "\033[91mFAIL\033[0m"
        print(f"{skill:<20} {script:<26} {status_str:<19} {dur:.2f}s")

    for skill, script, passed, _, details in results:
        if not passed:
            print(f"\n[FAIL DETAILED TRACE] {skill} -> {script}:")
            print(details)

    print("=" * 65 + "\n")
    return all_passed


def check_environment_dependencies() -> bool:
    """Pre-flight check for required packages declared in requirements-dev.txt."""
    missing = []
    try:
        import yaml  # noqa: F401
    except ImportError:
        missing.append("PyYAML (import yaml)")

    if missing:
        print("\n\033[91m" + "=" * 65)
        print("  [ERROR] MISSING REQUIRED PYTHON DEPENDENCIES")
        print("=" * 65 + "\033[0m")
        for m in missing:
            print(f"  - {m}")
        print("\nOpenLia requires dependencies from requirements-dev.txt.")
        print("To create and setup a ready virtual environment, run:")
        print("    \033[96mmake venv\033[0m")
        print("Or install into your active Python environment:")
        print("    \033[96mpip install -r requirements-dev.txt\033[0m")
        print("=" * 65 + "\n")
        return False
    return True


def main() -> None:
    parser = argparse.ArgumentParser(description="OpenLia Skill Verification Suite")
    parser.add_argument("--offline", action="store_true", help="Run offline unit self-tests")
    parser.add_argument("--live", action="store_true", help="Run live browser verifications against Playwright MCP")
    parser.add_argument("--skill", type=str, default="", help="Run tests for a specific skill only")
    parser.add_argument("--mcp-url", type=str, default="", help="Override default Playwright MCP URL")

    args = parser.parse_args()

    # Pre-flight check
    if not check_environment_dependencies():
        sys.exit(1)

    # Default to running offline tests if no mode specified
    # Automatically prune .playwright-mcp logs
    pilot_scripts_dir = os.path.join(SKILLS_DIR, "browser-pilot", "scripts")
    if pilot_scripts_dir not in sys.path:
        sys.path.insert(0, pilot_scripts_dir)
    try:
        from check_browser_pilot import prune_playwright_mcp_logs
        pruned = prune_playwright_mcp_logs(".playwright-mcp", max_files=20, max_age_hours=24)
        if pruned > 0:
            print(f"Pruned {pruned} old/excess .playwright-mcp log file(s).")
    except Exception:
        pass

    run_all = not args.offline and not args.live

    success = True
    if args.offline or run_all:
        if not run_offline_tests(args.skill):
            success = False

    if args.live or run_all:
        if not run_live_verifications(args.skill, args.mcp_url):
            success = False

    try:
        from check_browser_pilot import prune_playwright_mcp_logs
        prune_playwright_mcp_logs(".playwright-mcp", max_files=20, max_age_hours=24)
    except Exception:
        pass

    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()
