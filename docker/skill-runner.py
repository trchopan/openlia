#!/usr/bin/env python3
"""Run a command with an installed skill's audited dependency environment."""

import os
import pathlib
import subprocess
import sys


def main() -> int:
    if len(sys.argv) < 4 or sys.argv[1] != "run" or sys.argv[3] != "--":
        print("usage: openlia-skill run SKILL -- COMMAND [ARG ...]", file=sys.stderr)
        return 2
    skill = sys.argv[2]
    if not skill or any(not (character.isalnum() or character in "_-") for character in skill):
        print("invalid skill name", file=sys.stderr)
        return 2
    command = sys.argv[4:]
    if not command:
        print("missing skill command", file=sys.stderr)
        return 2
    environment = pathlib.Path("/opt/openlia/skill-envs/by-skill") / skill
    path = "/usr/local/bin:/usr/bin:/bin"
    python = environment / ".venv" / "bin" / "python"
    if python.is_file():
        path = f"{python.parent}:{path}"
    node_modules = environment / "node_modules" / ".bin"
    if node_modules.is_dir():
        path = f"{node_modules}:{path}"
    env = {
        "HOME": "/tmp",
        "LANG": "C.UTF-8",
        "PATH": path,
        "PYTHONNOUSERSITE": "1",
        "PYTHONDONTWRITEBYTECODE": "1",
        "OPENLIA_WORKSPACE_ROOT": "/opt/data/workspace",
    }
    return subprocess.run(command, cwd=f"/opt/data/skills/{skill}", env=env, check=False).returncode


if __name__ == "__main__":
    raise SystemExit(main())
