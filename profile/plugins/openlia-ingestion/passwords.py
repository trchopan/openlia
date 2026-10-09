"""Small, deliberately narrow document-password profile store."""

from __future__ import annotations

import json
import os
import re
import tempfile
import tomllib
from contextlib import contextmanager
from pathlib import Path


PROFILE_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$")
FORBIDDEN_LABEL = re.compile(r"(?i)(oauth|api[ _-]?key|token|private[ _-]?key|session|cookie|service[ _-]?credential)")


class PasswordProfileError(ValueError):
    pass


class PasswordProfiles:
    def __init__(self, workspace: Path):
        self.path = workspace / "sources" / "document-passwords.toml"
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.lock_path = Path(os.environ.get("HERMES_HOME", str(workspace.parent))) / "ingestion" / "document-passwords.lock"
        self.lock_path.parent.mkdir(parents=True, exist_ok=True)

    def _read(self) -> dict:
        if not self.path.exists():
            return {"version": 1, "profiles": {}}
        try:
            data = tomllib.loads(self.path.read_text(encoding="utf-8"))
        except (OSError, UnicodeError, tomllib.TOMLDecodeError) as exc:
            raise PasswordProfileError(f"password profile file is invalid: {exc}") from exc
        profiles = data.get("profiles", {})
        if not isinstance(profiles, dict):
            raise PasswordProfileError("password profiles must be a TOML table")
        return {"version": 1, "profiles": profiles}

    def save(self, profile_id: str, label: str, password: str) -> dict:
        if not isinstance(profile_id, str) or not PROFILE_RE.fullmatch(profile_id):
            raise PasswordProfileError("profile_id must be a safe identifier")
        if not isinstance(label, str) or not label.strip() or FORBIDDEN_LABEL.search(label):
            raise PasswordProfileError("profile label is empty or names a service credential")
        if not isinstance(password, str) or not password:
            raise PasswordProfileError("document password must not be empty")
        with self._locked():
            data = self._read()
            profiles = data["profiles"]
            profiles[profile_id] = {"label": label.strip(), "password": password}
            lines = ["version = 1", ""]
            for key in sorted(profiles):
                profile = profiles[key]
                lines.append(f"[profiles.{key}]")
                lines.append(f"label = {json.dumps(profile['label'], ensure_ascii=True)}")
                lines.append(f"password = {json.dumps(profile['password'], ensure_ascii=True)}")
                lines.append("")
            self._atomic_write("\n".join(lines).encode("utf-8"))
        return {"profile_id": profile_id, "label": label.strip(), "updated": True}

    def list(self) -> list[dict]:
        with self._locked():
            data = self._read()
        result = []
        for profile_id, profile in sorted(data["profiles"].items()):
            if isinstance(profile, dict) and isinstance(profile.get("label"), str):
                result.append({"profile_id": profile_id, "label": profile["label"]})
        return result

    def resolve(self, profile_id: str) -> str:
        if not isinstance(profile_id, str) or not PROFILE_RE.fullmatch(profile_id):
            raise PasswordProfileError("invalid password profile")
        with self._locked():
            profile = self._read()["profiles"].get(profile_id)
        if not isinstance(profile, dict) or not isinstance(profile.get("password"), str):
            raise PasswordProfileError("password profile was not found")
        return profile["password"]

    def _atomic_write(self, data: bytes) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        descriptor, temporary = tempfile.mkstemp(prefix=".document-passwords.", dir=self.path.parent)
        try:
            os.fchmod(descriptor, 0o600)
            with os.fdopen(descriptor, "wb") as output:
                output.write(data)
                output.flush()
                os.fsync(output.fileno())
            os.replace(temporary, self.path)
            os.chmod(self.path, 0o600)
            directory = os.open(self.path.parent, os.O_RDONLY)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)

    @contextmanager
    def _locked(self):
        with self.lock_path.open("a+b") as lock:
            os.chmod(self.lock_path, 0o600)
            try:
                import fcntl
                fcntl.flock(lock.fileno(), fcntl.LOCK_EX)
            except (ImportError, OSError):
                pass
            try:
                yield
            finally:
                try:
                    import fcntl
                    fcntl.flock(lock.fileno(), fcntl.LOCK_UN)
                except (ImportError, OSError):
                    pass
