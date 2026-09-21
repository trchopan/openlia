#!/usr/bin/env python3
import sys
import tempfile
import unittest
from pathlib import Path


sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools"))
from openlia_tools import JobStore, redact, safe_slug  # noqa: E402


class OpenLiaToolsTest(unittest.TestCase):
    def test_store_deduplicates_active_jobs(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            store = JobStore(Path(directory) / "jobs.sqlite3")
            values = {
                "id": "job-one",
                "tool": "gemini-chat",
                "prompt": "hello",
                "topic": "test",
                "output_path": "/opt/data/workspace/result.yaml",
                "fingerprint": "fingerprint",
                "idempotency_key": "telegram-1",
                "timeout_seconds": 300,
                "created_at": "2026-09-21T00:00:00+00:00",
            }
            store.create(values)
            duplicate = store.active_duplicate("fingerprint", "telegram-1")
            self.assertIsNotNone(duplicate)
            self.assertEqual(duplicate["id"], "job-one")
            store.update("job-one", "completed", "completed")
            self.assertIsNone(store.active_duplicate("fingerprint", "telegram-1"))

    def test_running_jobs_fail_closed_on_worker_restart(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            store = JobStore(Path(directory) / "jobs.sqlite3")
            values = {
                "id": "job-running",
                "tool": "chatgpt-chat",
                "prompt": "hello",
                "topic": "test",
                "output_path": "/opt/data/workspace/result.yaml",
                "fingerprint": "fingerprint",
                "idempotency_key": "telegram-2",
                "timeout_seconds": 300,
                "created_at": "2026-09-21T00:00:00+00:00",
            }
            store.create(values)
            store.update("job-running", "running", "running")
            store.recover_running()
            row = store.get("job-running")
            self.assertEqual(row["status"], "failed")
            self.assertIn("not retried", row["error"])

    def test_helpers_redact_and_slug(self) -> None:
        self.assertEqual(safe_slug("Latest Go / Gemini!"), "latest_go_gemini")
        self.assertNotIn("sk-secret", redact("OPENAI_API_KEY=sk-secret"))

    def test_store_prunes_old_jobs(self) -> None:
        from datetime import datetime, timedelta, timezone
        with tempfile.TemporaryDirectory() as directory:
            store = JobStore(Path(directory) / "jobs.sqlite3")
            now = datetime.now(timezone.utc)
            old_date = (now - timedelta(days=20)).isoformat()
            recent_date = (now - timedelta(days=2)).isoformat()

            # Job 1: Old completed job (should be pruned)
            store.create({
                "id": "job-old-completed", "tool": "chatgpt-chat", "prompt": "p1", "topic": "t1",
                "output_path": "/path/1.yaml", "fingerprint": "fp1", "idempotency_key": "",
                "timeout_seconds": 300, "created_at": old_date,
            })
            store.update("job-old-completed", "completed", "completed")

            # Job 2: Old queued job (should NOT be pruned)
            store.create({
                "id": "job-old-queued", "tool": "gemini-chat", "prompt": "p2", "topic": "t2",
                "output_path": "/path/2.yaml", "fingerprint": "fp2", "idempotency_key": "",
                "timeout_seconds": 300, "created_at": old_date,
            })

            # Job 3: Recent completed job (should NOT be pruned)
            store.create({
                "id": "job-recent-completed", "tool": "gemini-chat", "prompt": "p3", "topic": "t3",
                "output_path": "/path/3.yaml", "fingerprint": "fp3", "idempotency_key": "",
                "timeout_seconds": 300, "created_at": recent_date,
            })
            store.update("job-recent-completed", "completed", "completed")

            pruned = store.prune_older_than(days=14)
            self.assertEqual(pruned, 1)
            self.assertIsNone(store.get("job-old-completed"))
            self.assertIsNotNone(store.get("job-old-queued"))
            self.assertIsNotNone(store.get("job-recent-completed"))


if __name__ == "__main__":
    unittest.main()
