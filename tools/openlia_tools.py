#!/usr/bin/env python3
"""Private OpenLia browser-job API and single-worker queue."""

from __future__ import annotations

import hashlib
import json
import os
import queue
import re
import signal
import sqlite3
import subprocess
import sys
import threading
import traceback
import time
import urllib.parse
import uuid
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any


TOOLS = {
    "chatgpt-chat": "chatgpt_conversation.py",
    "gemini-chat": "gemini_conversation.py",
}
MAX_BODY_BYTES = 1024 * 1024
DEFAULT_TIMEOUT_SECONDS = 300
MAX_TIMEOUT_SECONDS = 600


def timestamp() -> str:
    return datetime.now(timezone.utc).isoformat()


def safe_slug(value: str) -> str:
    value = re.sub(r"[^A-Za-z0-9_-]+", "_", value.strip().lower()).strip("_")
    return value[:50] or "chat"


def redact(value: str) -> str:
    patterns = (
        (r"(?i)bearer\s+[A-Za-z0-9._~+/=-]+", "Bearer [REDACTED]"),
        (r"\b(?:sk|ghp|gho|ghu|github_pat)_[A-Za-z0-9_-]+", "[REDACTED]"),
        (r"(?i)([A-Z][A-Z0-9_]*(?:KEY|TOKEN|SECRET|PASSWORD)[A-Z0-9_]*)\s*=\s*[^\s,;]+", r"\1=[REDACTED]"),
    )
    for pattern, replacement in patterns:
        value = re.sub(pattern, replacement, value)
    return value


class JobStore:
    def __init__(self, path: Path):
        path.parent.mkdir(parents=True, exist_ok=True)
        self.path = path
        self.lock = threading.RLock()
        self.db = sqlite3.connect(path, check_same_thread=False)
        self.db.row_factory = sqlite3.Row
        self.db.execute("PRAGMA journal_mode=WAL")
        self.db.execute(
            """CREATE TABLE IF NOT EXISTS jobs (
                id TEXT PRIMARY KEY,
                tool TEXT NOT NULL,
                prompt TEXT NOT NULL,
                topic TEXT NOT NULL,
                output_path TEXT NOT NULL,
                fingerprint TEXT NOT NULL,
                idempotency_key TEXT NOT NULL,
                timeout_seconds INTEGER NOT NULL,
                status TEXT NOT NULL,
                phase TEXT NOT NULL,
                error TEXT NOT NULL,
                created_at TEXT NOT NULL,
                updated_at TEXT NOT NULL
            )"""
        )
        self.db.commit()
        try:
            os.chmod(path, 0o600)
        except OSError:
            pass

    def recover_running(self) -> None:
        with self.lock:
            now = timestamp()
            self.db.execute(
                "UPDATE jobs SET status='failed', phase='worker_restart', error=?, updated_at=? WHERE status='running'",
                ("worker restarted while job was running; prompt was not retried", now),
            )
            self.db.commit()

    def prune_older_than(self, days: int = 14) -> int:
        with self.lock:
            cutoff = (datetime.now(timezone.utc) - timedelta(days=days)).isoformat()
            cursor = self.db.execute(
                "DELETE FROM jobs WHERE status IN ('completed', 'failed', 'cancelled') AND created_at < ?",
                (cutoff,),
            )
            deleted = cursor.rowcount
            self.db.commit()
            return deleted

    def active_duplicate(self, fingerprint: str, idempotency_key: str) -> sqlite3.Row | None:
        with self.lock:
            if idempotency_key:
                row = self.db.execute(
                    "SELECT * FROM jobs WHERE idempotency_key=? AND status IN ('queued','running') ORDER BY created_at DESC LIMIT 1",
                    (idempotency_key,),
                ).fetchone()
            else:
                row = self.db.execute(
                    "SELECT * FROM jobs WHERE fingerprint=? AND status IN ('queued','running') ORDER BY created_at DESC LIMIT 1",
                    (fingerprint,),
                ).fetchone()
            return row

    def create(self, values: dict[str, Any]) -> None:
        with self.lock:
            self.db.execute(
                """INSERT INTO jobs
                (id, tool, prompt, topic, output_path, fingerprint, idempotency_key,
                 timeout_seconds, status, phase, error, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'queued', 'queued', '', ?, ?)""",
                (
                    values["id"], values["tool"], values["prompt"], values["topic"],
                    values["output_path"], values["fingerprint"], values["idempotency_key"],
                    values["timeout_seconds"], values["created_at"], values["created_at"],
                ),
            )
            self.db.commit()

    def get(self, job_id: str) -> sqlite3.Row | None:
        with self.lock:
            return self.db.execute("SELECT * FROM jobs WHERE id=?", (job_id,)).fetchone()

    def queued(self) -> list[sqlite3.Row]:
        with self.lock:
            return list(self.db.execute("SELECT * FROM jobs WHERE status='queued' ORDER BY created_at"))

    def update(self, job_id: str, status: str, phase: str, error: str = "") -> None:
        with self.lock:
            self.db.execute(
                "UPDATE jobs SET status=?, phase=?, error=?, updated_at=? WHERE id=?",
                (status, phase, redact(error)[:2000], timestamp(), job_id),
            )
            self.db.commit()

    def output_path(self, job_id: str) -> str | None:
        row = self.get(job_id)
        return str(row["output_path"]) if row else None


class JobService:
    def __init__(self):
        self.data_root = Path(os.getenv("OPENLIA_TOOLS_DATA_ROOT", "/opt/data")).resolve()
        self.workspace_root = self.data_root / "workspace"
        self.job_root = self.data_root / "openlia"
        self.store = JobStore(self.job_root / "jobs.sqlite3")
        self.store.recover_running()
        self.store.prune_older_than(14)
        self.jobs: queue.Queue[str] = queue.Queue()
        self.browser_client: Any = None
        self.browser_client_lock = threading.RLock()
        self.active_job_id: str | None = None
        self.worker = threading.Thread(target=self._worker_loop, name="openlia-tools-worker", daemon=True)
        self.worker.start()
        for row in self.store.queued():
            self.jobs.put(str(row["id"]))

    def submit(self, tool: str, payload: dict[str, Any]) -> tuple[dict[str, Any], int]:
        if tool not in TOOLS:
            return {"ok": False, "error": "unknown browser tool"}, 400
        prompt = payload.get("prompt")
        if not isinstance(prompt, str) or not prompt.strip() or len(prompt) > 200_000:
            return {"ok": False, "error": "prompt must be a non-empty string under 200000 characters"}, 400
        topic = payload.get("topic", "")
        if not isinstance(topic, str):
            return {"ok": False, "error": "topic must be a string"}, 400
        try:
            timeout = int(payload.get("timeout_seconds", DEFAULT_TIMEOUT_SECONDS))
        except (TypeError, ValueError):
            return {"ok": False, "error": "timeout_seconds must be an integer"}, 400
        timeout = max(30, min(timeout, MAX_TIMEOUT_SECONDS))
        idempotency_key = payload.get("idempotency_key", "")
        if not isinstance(idempotency_key, str):
            return {"ok": False, "error": "idempotency_key must be a string"}, 400
        fingerprint = hashlib.sha256((tool + "\0" + prompt + "\0" + topic).encode()).hexdigest()
        existing = self.store.active_duplicate(fingerprint, idempotency_key)
        if existing:
            return self.public_job(existing), 202

        job_id = "job-" + uuid.uuid4().hex
        platform = "chatgpt" if tool == "chatgpt-chat" else "gemini"
        filename = datetime.now(timezone.utc).strftime("%Y%m%d_%H%M%S") + "_" + safe_slug(topic or prompt[:40]) + "_" + job_id[4:12] + ".yaml"
        output_path = self.workspace_root / "knowledge" / platform / filename
        created = timestamp()
        self.store.create({
            "id": job_id, "tool": tool, "prompt": prompt, "topic": topic,
            "output_path": str(output_path), "fingerprint": fingerprint,
            "idempotency_key": idempotency_key, "timeout_seconds": timeout,
            "created_at": created,
        })
        self.jobs.put(job_id)
        return self.public_job(self.store.get(job_id)), 202

    def public_job(self, row: sqlite3.Row | None) -> dict[str, Any]:
        if row is None:
            return {"ok": False, "error": "job not found"}
        output = Path(str(row["output_path"]))
        try:
            relative = output.relative_to(self.workspace_root).as_posix()
        except ValueError:
            relative = ""
        result = {
            "ok": True,
            "job_id": row["id"],
            "tool": row["tool"],
            "status": row["status"],
            "phase": row["phase"],
            "created_at": row["created_at"],
            "updated_at": row["updated_at"],
            "result_path": relative or None,
        }
        if row["error"]:
            result["error"] = row["error"]
        return result

    def status(self, job_id: str) -> tuple[dict[str, Any], int]:
        row = self.store.get(job_id)
        if row is None:
            return {"ok": False, "error": "job not found"}, 404
        return self.public_job(row), 200

    def result(self, job_id: str) -> tuple[bytes, str, int]:
        row = self.store.get(job_id)
        if row is None:
            return b'{"ok":false,"error":"job not found"}', "application/json", 404
        if row["status"] != "completed":
            return json.dumps(self.public_job(row)).encode(), "application/json", 409
        try:
            data = Path(str(row["output_path"])).read_bytes()
        except OSError:
            return b'{"ok":false,"error":"job result is missing"}', "application/json", 500
        return data, "application/yaml", 200

    def cancel(self, job_id: str) -> tuple[dict[str, Any], int]:
        row = self.store.get(job_id)
        if row is None:
            return {"ok": False, "error": "job not found"}, 404
        if row["status"] == "queued":
            self.store.update(job_id, "cancelled", "cancelled", "cancelled before browser submission")
            return self.public_job(self.store.get(job_id)), 200
        if row["status"] == "running":
            return {"ok": False, "error": "running browser jobs cannot be cancelled safely after prompt submission"}, 409
        return self.public_job(row), 200

    def health(self) -> dict[str, Any]:
        active = 1 if self.active_job_id else 0
        return {"ok": True, "service": "openlia-tools", "active_jobs": active, "browser_mcp_url": os.getenv("OPENLIA_BROWSER_MCP_URL", "")}

    def _worker_loop(self) -> None:
        while True:
            job_id = self.jobs.get()
            try:
                self._run_job(job_id)
            finally:
                self.jobs.task_done()

    def _run_job(self, job_id: str) -> None:
        row = self.store.get(job_id)
        if row is None or row["status"] != "queued":
            return
        self.store.update(job_id, "running", "running")
        self.active_job_id = job_id
        script = self.data_root / "skills" / str(row["tool"]) / "scripts" / TOOLS[str(row["tool"])]
        # The path above is kept relative to the managed skill tree and is
        # validated before execution; user input never controls the command.
        if not script.is_file():
            self.store.update(job_id, "failed", "worker", "skill script is missing")
            return
        try:
            with self.browser_client_lock:
                module_name = "gemini_conversation" if row["tool"] == "gemini-chat" else "chatgpt_conversation"
                scripts_root = self.data_root / "skills" / row["tool"] / "scripts"
                sys.path.insert(0, str(scripts_root))
                module = __import__(module_name)
                if self.browser_client is None:
                    self.browser_client = module.PlaywrightMcpClient(os.getenv("OPENLIA_BROWSER_MCP_URL", ""))
                os.environ["HERMES_HOME"] = str(self.data_root)
                os.environ["OPENLIA_TOOLS_WORKER"] = "1"
                os.environ["OPENLIA_BROWSER_REUSE_TAB"] = "1"
                execute = module.execute_gemini_chat if row["tool"] == "gemini-chat" else module.execute_chatgpt_chat
                execute(
                    prompt=str(row["prompt"]),
                    topic=str(row["topic"]),
                    output_path=str(row["output_path"]),
                    mcp_url=os.getenv("OPENLIA_BROWSER_MCP_URL", ""),
                    keep_tab=True,
                    timeout=int(row["timeout_seconds"]),
                    client=self.browser_client,
                )
            if not Path(str(row["output_path"])).is_file():
                self.store.update(job_id, "failed", "worker", "browser job completed without a result file")
            else:
                self.store.update(job_id, "completed", "completed")
        except Exception:
            if self.browser_client is not None:
                try:
                    self.browser_client.call_tool("browser_navigate", {"url": "about:blank"})
                except Exception:
                    pass
            self.browser_client = None
            self.store.update(job_id, "failed", "worker", redact(traceback.format_exc()))
        finally:
            self.active_job_id = None


class Handler(BaseHTTPRequestHandler):
    service: JobService

    def log_message(self, _format: str, *_args: Any) -> None:
        return

    def send_json(self, status: int, payload: dict[str, Any]) -> None:
        data = json.dumps(payload, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def read_json(self) -> dict[str, Any] | None:
        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            return None
        if length <= 0 or length > MAX_BODY_BYTES:
            return None
        try:
            value = json.loads(self.rfile.read(length))
        except (json.JSONDecodeError, OSError):
            return None
        return value if isinstance(value, dict) else None

    def do_GET(self) -> None:
        path = urllib.parse.urlsplit(self.path).path.rstrip("/")
        if path == "/health":
            self.send_json(200, self.service.health())
            return
        if path.startswith("/openlia/jobs/"):
            parts = path.split("/")
            if len(parts) == 4:
                payload, status = self.service.status(parts[3])
                self.send_json(status, payload)
                return
            if len(parts) == 5 and parts[4] == "result":
                data, content_type, status = self.service.result(parts[3])
                self.send_response(status)
                self.send_header("Content-Type", content_type)
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
                return
        self.send_json(404, {"ok": False, "error": "not found"})

    def do_POST(self) -> None:
        path = urllib.parse.urlsplit(self.path).path.rstrip("/")
        payload = self.read_json()
        if payload is None:
            self.send_json(400, {"ok": False, "error": "request body must be a JSON object under 1 MiB"})
            return
        if path.startswith("/openlia/tools/"):
            tool = path.rsplit("/", 1)[-1]
            result, status = self.service.submit(tool, payload)
            self.send_json(status, result)
            return
        if path.startswith("/openlia/jobs/") and path.endswith("/cancel"):
            job_id = path.split("/")[3]
            result, status = self.service.cancel(job_id)
            self.send_json(status, result)
            return
        self.send_json(404, {"ok": False, "error": "not found"})


def main() -> None:
    service = JobService()
    Handler.service = service
    bind = os.getenv("OPENLIA_TOOLS_BIND", "0.0.0.0")
    port = int(os.getenv("OPENLIA_TOOLS_PORT", "8787"))
    server = ThreadingHTTPServer((bind, port), Handler)
    def stop_server(_signum: int, _frame: Any) -> None:
        threading.Thread(target=server.shutdown, daemon=True).start()

    for signal_name in (signal.SIGTERM, signal.SIGINT):
        signal.signal(signal_name, stop_server)
    server.serve_forever()


if __name__ == "__main__":
    main()
