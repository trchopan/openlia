#!/usr/bin/env python3
"""Run the safe local OpenLia smoke gate or an explicit live deployment check."""

from __future__ import annotations

import argparse
import json
import os
import re
import shlex
import shutil
import stat
import subprocess
import tempfile
import time
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[1]
SECRET_PATTERNS = (
    re.compile(r"(?i)bearer\s+[A-Za-z0-9._~+/=-]+"),
    re.compile(r"\b(?:sk|ghp|gho|ghu|github_pat)_[A-Za-z0-9_-]+"),
    re.compile(r"(?i)[A-Z][A-Z0-9_]*(?:KEY|TOKEN|SECRET|PASSWORD)[A-Z0-9_]*\s*=\s*[^\s,;]+"),
    re.compile(r"(?i)(capabilit(?:y|ies)[\"']?\s*[:=]\s*[\"']?)[^\s,\"']+"),
)


def redact(value: str) -> str:
    for pattern in SECRET_PATTERNS:
        value = pattern.sub("[REDACTED]", value)
    return value


def run_case(
    case_id: str,
    command: list[str],
    timeout: int = 120,
    env: dict[str, str] | None = None,
) -> dict[str, Any]:
    started = time.time()
    try:
        result = subprocess.run(command, cwd=ROOT, env=env, capture_output=True, text=True, timeout=timeout)
        output = redact((result.stdout + result.stderr)[-2000:])
        status = "PASS" if result.returncode == 0 else "FAIL"
        return {
            "case_id": case_id,
            "status": status,
            "started_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(started)),
            "duration_seconds": round(time.time() - started, 3),
            "exit_code": result.returncode,
            "evidence": [output] if output else [],
            "error": None if result.returncode == 0 else "command failed",
        }
    except subprocess.TimeoutExpired:
        return {
            "case_id": case_id,
            "status": "FAIL",
            "started_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(started)),
            "duration_seconds": round(time.time() - started, 3),
            "exit_code": None,
            "evidence": [],
            "error": "timeout",
        }
    except OSError as exc:
        return {
            "case_id": case_id,
            "status": "FAIL",
            "started_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(started)),
            "duration_seconds": round(time.time() - started, 3),
            "exit_code": None,
            "evidence": [redact(str(exc))],
            "error": "command could not start",
        }


def not_applicable(case_id: str, reason: str) -> dict[str, Any]:
    return {"case_id": case_id, "status": "N/A", "evidence": [], "error": reason}


def manual_case(case_id: str, question: str) -> dict[str, Any]:
    if not os.isatty(0):
        return not_applicable(case_id, "interactive confirmation requires a terminal")
    try:
        answer = input(question).strip().lower()
    except EOFError:
        return not_applicable(case_id, "interactive confirmation was not provided")
    status = "PASS" if answer in {"y", "yes"} else "FAIL"
    return {"case_id": case_id, "status": status, "evidence": [], "error": None if status == "PASS" else "operator did not confirm"}


def quote_toml_string(value: str) -> str:
    return json.dumps(value, ensure_ascii=True)


def require_live_path(value: Path, label: str) -> Path:
    path = value.expanduser().resolve()
    if not path.is_file():
        raise ValueError(f"{label} must be a regular file: {path}")
    os.chmod(path, 0o600)
    if stat.S_IMODE(path.stat().st_mode) != 0o600:
        raise ValueError(f"{label} must have mode 0600: {path}")
    return path


def validate_live_inputs(args: argparse.Namespace) -> tuple[Path, Path]:
    if not args.local and (not args.target or any(character in args.target for character in " \t\r\n'\";$&|()<>`")):
        raise ValueError("target must be a non-empty SSH destination without shell metacharacters")
    if not args.project or not re.fullmatch(r"[A-Za-z0-9_-]+", args.project):
        raise ValueError("project must contain only letters, numbers, underscore, or hyphen")
    root = args.root.rstrip("/")
    if (
        not root.startswith("/")
        or root in {"", "/", "/home", "/home/ubuntu", "/srv", "/srv/openlia", "/tmp"}
        or any(part in {"", ".", ".."} for part in root.split("/")[1:])
        or any(character in root for character in "\t\r\n'\";$&|()<>`")
    ):
        raise ValueError("root must be a specific safe absolute path")
    if not args.locho_host or not re.fullmatch(r"[A-Za-z0-9_-]+", args.locho_host):
        raise ValueError("locho-host must contain only letters, numbers, underscore, or hyphen")
    env_file = require_live_path(args.env_file, "env-file")
    attachments_file = require_live_path(args.attachments_file, "attachments-file")
    required_keys = {"OPENAI_API_KEY", "OPENAI_BASE_URL", "TELEGRAM_BOT_TOKEN", "TELEGRAM_ALLOWED_USERS"}
    seen_keys: set[str] = set()
    for raw_line in env_file.read_text(encoding="utf-8").splitlines():
        line = raw_line.rstrip("\r")
        if not line or line.startswith("#"):
            continue
        if "=" not in line:
            raise ValueError("env-file is not dotenv-shaped")
        key, value = line.split("=", 1)
        if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key) or not value:
            raise ValueError("env-file contains an invalid or empty assignment")
        seen_keys.add(key)
    missing_keys = sorted(required_keys - seen_keys)
    if missing_keys:
        raise ValueError("env-file is missing required keys: " + ", ".join(missing_keys))
    attachment_text = attachments_file.read_text(encoding="utf-8")
    if "host_id" not in attachment_text or "listen_host" not in attachment_text or "[[services]]" not in attachment_text:
        raise ValueError("attachments-file is missing required Locho fields")
    return env_file, attachments_file


def live_result(case_id: str, status: str, error: str | None = None, evidence: list[str] | None = None) -> dict[str, Any]:
    return {"case_id": case_id, "status": status, "evidence": evidence or [], "error": error}


def remote_skill_check(target: str, project: str, remote_root: str, skill: str) -> list[str]:
    compose_directory = f"{remote_root}/current/docker"
    compose_file = f"{compose_directory}/compose.yaml"
    generated_compose = f"{compose_directory}/compose.generated.yaml"
    check = f"test -s /opt/data/skills/{skill}/SKILL.md"
    command = " ".join(
        [
            "docker",
            "compose",
            "--project-name",
            shlex.quote(project),
            "--project-directory",
            shlex.quote(compose_directory),
            "-f",
            shlex.quote(compose_file),
            "-f",
            shlex.quote(generated_compose),
            "exec",
            "-T",
            "hermes",
            "sh",
            "-c",
            shlex.quote(check),
        ]
    )
    return ["ssh", "--", target, command]


def local_skill_check(root: str, project: str, skill: str) -> list[str]:
    compose_directory = f"{root}/current/docker"
    command = [
        "docker",
        "compose",
        "--project-name",
        project,
        "--project-directory",
        compose_directory,
        "-f",
        f"{compose_directory}/compose.yaml",
    ]
    generated = Path(compose_directory) / "compose.generated.yaml"
    if generated.is_file():
        command.extend(["-f", str(generated)])
    command.extend(["exec", "-T", "hermes", "sh", "-c", f"test -s /opt/data/skills/{skill}/SKILL.md"])
    return command


def run_live(args: argparse.Namespace) -> list[dict[str, Any]]:
    results: list[dict[str, Any]] = []
    cleanup_eligible = False
    env_file, attachments_file = validate_live_inputs(args)
    root = args.root.rstrip("/")
    base_env = os.environ.copy()

    with tempfile.TemporaryDirectory(prefix="openlia-live-") as temporary_directory:
        temporary = Path(temporary_directory)
        staged_env = temporary / "hermes.env"
        staged_attachments = temporary / "attachments.toml"
        shutil.copyfile(env_file, staged_env)
        shutil.copyfile(attachments_file, staged_attachments)
        os.chmod(staged_env, 0o600)
        os.chmod(staged_attachments, 0o600)
        config = temporary / "config.toml"
        config.write_text(
            "[openlia]\n"
            "schema = 1\n"
            "[secrets]\n"
            f"source = {quote_toml_string(str(staged_env))}\n",
            encoding="utf-8",
        )
        os.chmod(config, 0o600)
        live_env = {**base_env, "OPENLIA_CONFIG": str(config)}

        root_check = (
            run_case("LIVE-LOCAL-ROOT", ["test", "!", "-e", root], timeout=30)
            if args.local
            else run_case("LIVE-REMOTE-ROOT", ["ssh", "--", args.target, "test", "!", "-e", root], timeout=30)
        )
        results.append(root_check)
        if root_check["status"] != "PASS":
            results.append(live_result("LIVE-PREFLIGHT", "FAIL", "local root already exists or target is unreachable"))
            return results
        cleanup_eligible = True
        results.append(live_result("LIVE-PREFLIGHT", "PASS", evidence=["local inputs validated; credentials redacted"]))

        init_arguments = ["go", "run", ".", "init"]
        if args.local:
            init_arguments.extend(["--local", "--root", root])
        else:
            init_arguments.extend(["--target", args.target, "--root", root])
        init_arguments.extend(["--project", args.project, "--json"])
        init = run_case("LIVE-INIT", init_arguments, timeout=1800, env=live_env)
        results.append(init)
        if init["status"] == "PASS":
            results.append(
                run_case(
                    "LIVE-ATTACHMENTS",
                    [
                        "go",
                        "run",
                        ".",
                        "attachments",
                        "rotate",
                        args.locho_host,
                        "--source",
                        str(staged_attachments),
                        "--json",
                    ],
                    timeout=300,
                    env=live_env,
                )
            )
            results.append(
                run_case(
                    "LIVE-LOCHO",
                    ["go", "run", ".", "update", "locho", "--json"],
                    timeout=1800,
                    env=live_env,
                )
            )
            results.append(
                run_case(
                    "LIVE-HEALTH",
                    ["go", "run", ".", "doctor", "--check-providers", "--json"],
                    timeout=300,
                    env=live_env,
                )
            )
            results.append(
                run_case(
                    "LIVE-ATTACHMENT-LIST",
                    ["go", "run", ".", "attachments", "list", "--json"],
                    timeout=120,
                    env=live_env,
                )
            )
            results.append(
                run_case(
                    "LIVE-PERSONAL-FINANCE",
                    local_skill_check(root, args.project, "personal-finance")
                    if args.local
                    else remote_skill_check(args.target, args.project, root, "personal-finance"),
                    timeout=30,
                )
            )
            results.append(
                manual_case(
                    "TELEGRAM-COMMUNICATION",
                    "Send this exact Telegram message to the bot:\n"
                    '  I spent 15,000 VND on a Banh Mi today\n'
                    "Did the agent reply? [y/N] ",
                )
            )
            results.append(
                manual_case(
                    "TELEGRAM-PERSONAL-FINANCE",
                    "Did the reply recognize the spending request and follow the finance skill's safe workflow "
                    "(for example, request a journal path or approval rather than silently writing)? [y/N] ",
                )
            )

        if args.cleanup and cleanup_eligible:
            results.append(
                run_case(
                    "LIVE-UNINSTALL",
                    [
                        "go",
                        "run",
                        ".",
                        "uninstall",
                        "--local" if args.local else "--target",
                        *([] if args.local else [args.target]),
                        "--project",
                        args.project,
                        "--root",
                        root,
                        "--non-interactive",
                        "--json",
                    ],
                    timeout=300,
                    env=live_env,
                )
            )
        elif args.cleanup:
            results.append(live_result("LIVE-CLEANUP", "FAIL", "cleanup was not eligible for this target"))
    return results


def run_local_smoke(args: argparse.Namespace) -> list[dict[str, Any]]:
    results: list[dict[str, Any]] = []
    temporary_root: str | None = None
    root = args.root
    if root is None:
        temporary_root = tempfile.mkdtemp(prefix="openlia-local-")
        shutil.rmtree(temporary_root)
        root = temporary_root
    root = str(Path(root).expanduser().resolve())
    project = args.project or "openlia_local_smoke"
    cleanup_eligible = False
    config_directory = tempfile.TemporaryDirectory(prefix="openlia-local-config-")
    config = Path(config_directory.name) / "config.toml"
    config.write_text("[openlia]\nschema = 1\n[secrets]\n", encoding="utf-8")
    local_env = {**os.environ, "OPENLIA_CONFIG": str(config)}
    try:
        root_check = run_case("LOCAL-ROOT", ["test", "!", "-e", root], timeout=30)
        results.append(root_check)
        if root_check["status"] != "PASS":
            results.append(live_result("LOCAL-PREFLIGHT", "FAIL", "local root already exists"))
            return results
        cleanup_eligible = True
        results.append(live_result("LOCAL-PREFLIGHT", "PASS", evidence=["local root is absent; credentials are not used"]))

        init = run_case(
            "LOCAL-INIT",
            ["go", "run", ".", "init", "--local", "--root", root, "--project", project, "--json"],
            timeout=1800,
            env=local_env,
        )
        results.append(init)
        if init["status"] == "PASS":
            for case_id, command, timeout in (
                ("LOCAL-DOCTOR", ["go", "run", ".", "doctor", "--json"], 300),
                ("LOCAL-STOP", ["go", "run", ".", "stop", "--json"], 120),
                ("LOCAL-START", ["go", "run", ".", "start", "--json"], 300),
                ("LOCAL-SKILL-BUNDLED", ["go", "run", ".", "skills", "test", "personal-finance"], 120),
            ):
                results.append(run_case(case_id, command, timeout=timeout, env=local_env))
            results.append(
                run_case(
                    "LOCAL-SKILL-RUNTIME",
                    local_skill_check(root, project, "personal-finance"),
                    timeout=120,
                    env=local_env,
                )
            )
    finally:
        if args.cleanup and cleanup_eligible:
            results.append(
                run_case(
                    "LOCAL-UNINSTALL",
                    [
                        "go",
                        "run",
                        ".",
                        "uninstall",
                        "--local",
                        "--project",
                        project,
                        "--root",
                        root,
                        "--non-interactive",
                        "--json",
                    ],
                    timeout=300,
                    env=local_env,
                )
            )
        config_directory.cleanup()
        if temporary_root is not None and not Path(root).exists():
            shutil.rmtree(temporary_root, ignore_errors=True)
    return results


def run(mode: str, args: argparse.Namespace) -> list[dict[str, Any]]:
    results: list[dict[str, Any]] = []
    if mode in {"cli", "static"}:
        results.append(run_case("CLI-001", ["go", "run", ".", "--help"]))
        results.append(run_case("CLI-002", ["go", "run", ".", "--version", "--json"]))
        results.append(run_case("CLI-006", ["go", "run", ".", "skills", "list", "--json"]))
        for skill in (
            "inbox-triage",
            "daily-briefing",
            "weekly-review",
            "project-review",
            "decision-analysis",
            "deep-research",
            "personal-finance",
        ):
            results.append(run_case(f"SKILL-{skill}", ["go", "run", ".", "skills", "test", skill]))
        results.append(run_case("DEP-001", ["docker", "compose", "-f", "docker/compose.yaml", "config", "--quiet"]))
        scripts = ["docker/secret-source.sh", *map(str, sorted((ROOT / "ops").glob("*.sh")))]
        results.append(run_case("STATIC-OPS", ["bash", "-n", *scripts]))
    if mode == "live":
        try:
            results.extend(run_live(args))
        except (OSError, ValueError) as exc:
            results.append(live_result("LIVE-PREFLIGHT", "FAIL", redact(str(exc))))
    if mode == "local":
        try:
            results.extend(run_local_smoke(args))
        except (OSError, ValueError) as exc:
            results.append(live_result("LOCAL-PREFLIGHT", "FAIL", redact(str(exc))))
    if mode in {"cli", "api", "locho", "recovery", "human"} and not os.environ.get("OPENLIA_SMOKE_TARGET"):
        results.extend(
            [
                not_applicable("CLI-002-LIVE", "OPENLIA_SMOKE_TARGET is not configured"),
                not_applicable("AUTH-002", "disposable provider credentials are not configured"),
                not_applicable("LOCHO-001", "disposable Locho capabilities are not configured"),
                not_applicable("REC-001", "disposable recovery target is not configured"),
                not_applicable("HUMAN-001", "human OAuth acceptance is not an unattended test"),
            ]
        )
    return results


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mode", choices=("cli", "static", "api", "locho", "recovery", "human", "local", "live"), default="cli")
    parser.add_argument("--local", action="store_true", help="run live mode against the local machine")
    parser.add_argument("--target", help="SSH destination for live mode")
    parser.add_argument("--project", help="Compose project name for live mode")
    parser.add_argument("--root", help="Specific absolute OpenLia installation root")
    parser.add_argument("--env-file", type=Path, help="Mode-0600 dotenv source for live mode")
    parser.add_argument("--attachments-file", type=Path, help="Mode-0600 Locho attachment source for live mode")
    parser.add_argument("--locho-host", help="Locho host name matching the attachment source")
    parser.add_argument("--cleanup", action="store_true", help="Stop and remove the live installation root after the run")
    parser.add_argument("--report", type=Path, help="Write the redacted report outside the checkout")
    args = parser.parse_args(argv)
    if args.mode == "live":
        if args.local and args.target is not None:
            parser.error("--local and --target are mutually exclusive")
        required_live_arguments = {
            "--project": args.project,
            "--root": args.root,
            "--env-file": args.env_file,
            "--attachments-file": args.attachments_file,
            "--locho-host": args.locho_host,
        }
        if not args.local:
            required_live_arguments["--target"] = args.target
        missing = [name for name, value in required_live_arguments.items() if value is None]
        if missing:
            parser.error("live mode requires " + ", ".join(missing))
    elif args.mode == "local":
        if args.local or args.target or args.env_file or args.attachments_file or args.locho_host:
            parser.error("local mode does not accept live target or credential arguments")
        if args.cleanup is False:
            args.cleanup = True
    elif args.cleanup or args.local or any(
        value is not None for value in (args.target, args.project, args.root, args.env_file, args.attachments_file, args.locho_host)
    ):
        parser.error("live deployment arguments and --cleanup require --mode live")
    report_path = args.report
    temporary_directory: str | None = None
    if report_path is None:
        temporary_directory = tempfile.mkdtemp(prefix="openlia-smoke-")
        report_path = Path(temporary_directory) / "report.json"
    else:
        report_path = report_path.expanduser().resolve()
        try:
            report_path.relative_to(ROOT)
        except ValueError:
            pass
        else:
            parser.error("report must be outside the repository checkout")
        report_path.parent.mkdir(parents=True, exist_ok=True)

    results = run(args.mode, args)
    payload = {"schema": 1, "mode": args.mode, "run_id": report_path.stem, "cases": results}
    report_path.write_text(json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=True) + "\n", encoding="utf-8")
    os.chmod(report_path, 0o600)
    print(json.dumps({"schema": 1, "report": str(report_path), "cases": len(results), "failed": sum(item["status"] == "FAIL" for item in results)}, ensure_ascii=True))
    return 1 if any(item["status"] == "FAIL" for item in results) else 0


if __name__ == "__main__":
    raise SystemExit(main())
