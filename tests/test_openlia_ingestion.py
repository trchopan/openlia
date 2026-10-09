from __future__ import annotations

import importlib.util
import json
import os
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest
import urllib.request
from pathlib import Path
from unittest.mock import patch


REPO_ROOT = Path(__file__).resolve().parents[1]
PLUGIN_ROOT = REPO_ROOT / "profile/plugins/openlia-ingestion"
FIXTURES_DIR = REPO_ROOT / "tests/fixtures/ingestion"
TEST_DOC_KEY = "openlia"
WRONG_DOC_KEY = "wrong"


def load_plugin_package():
    name = "openlia_ingestion_test"
    spec = importlib.util.spec_from_file_location(
        name,
        PLUGIN_ROOT / "__init__.py",
        submodule_search_locations=[str(PLUGIN_ROOT)],
    )
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return name


class MockVisionResult:
    def __init__(self, text: str, warnings: list[str] | None = None):
        self.parsed = {"text": text, "warnings": warnings or []}


class MockVisionLLM:
    def __init__(self, text: str = "Extracted document OCR text", warnings: list[str] | None = None):
        self.text = text
        self.warnings = warnings or []
        self.calls = []

    def complete_structured(self, **kwargs):
        self.calls.append(kwargs)
        return MockVisionResult(self.text, self.warnings)


class MockHermesContext:
    def __init__(self, llm=None):
        self.tools = {}
        self.llm = llm
        self.injected_messages = []
        self.configs = {"max_concurrent_jobs": 1}

    def register_tool(self, name, toolset, schema, handler, is_async, description):
        self.tools[name] = handler

    def register_auxiliary_task(self, name, display_name, description, defaults):
        pass

    def get_config(self, key, default):
        return self.configs.get(key, default)

    def inject_message(self, message, role="user", session_key=None):
        self.injected_messages.append({"message": message, "role": role, "session_key": session_key})
        return True


class IngestionTests(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.root = Path(self.tempdir.name)
        self.workspace = self.root / "workspace"
        self.runtime_dir = self.root / "runtime"
        self.ingestion_dir = self.runtime_dir / "ingestion"
        self.workspace.mkdir(parents=True, exist_ok=True)
        self.ingestion_dir.mkdir(parents=True, exist_ok=True)

        os.environ["HERMES_HOME"] = str(self.runtime_dir)
        os.environ["OPENLIA_WORKSPACE_ROOT"] = str(self.workspace)
        os.environ["OPENLIA_UPLOAD_ROOT"] = str(self.root)

        self.package = load_plugin_package()
        self.publication = sys.modules[f"{self.package}.publication"]
        self.queue_mod = sys.modules[f"{self.package}.queue"]
        self.passwords_mod = sys.modules[f"{self.package}.passwords"]
        self.adapters = sys.modules[f"{self.package}.adapters"]
        self.runtime_mod = sys.modules[f"{self.package}.runtime"]

        self.publisher = self.publication.WorkspacePublisher(self.workspace)
        self.queue = self.queue_mod.IngestionQueue(self.ingestion_dir)
        self.passwords = self.passwords_mod.PasswordProfiles(self.workspace)

    def tearDown(self):
        os.environ.pop("HERMES_HOME", None)
        os.environ.pop("OPENLIA_WORKSPACE_ROOT", None)
        os.environ.pop("OPENLIA_UPLOAD_ROOT", None)
        self.tempdir.cleanup()

    def _copy_fixture(self, name: str) -> Path:
        source = FIXTURES_DIR / name
        self.assertTrue(source.exists(), f"Fixture file {name} is missing")
        target = self.root / name
        shutil.copy2(source, target)
        return target

    # =========================================================================
    # Queue, Leases, and Continuation Tests
    # =========================================================================

    def test_capture_and_idempotent_submission(self):
        captured = self.publisher.capture(
            {"kind": "text", "title": "Example", "content": "one\ntwo"}
        )
        request = {
            "idempotency_key": "same-request",
            "intake_id": captured["intake"]["intake_id"],
            "source_id": captured["source"]["id"],
            "version_id": captured["version"]["id"],
            "operation": "extract",
            "requested_outputs": ["text"],
            "continuation": {},
        }
        first = self.queue.submit(request)
        second = self.queue.submit(request)
        self.assertEqual(first["job_id"], second["job_id"])
        self.assertTrue(
            (self.workspace / "sources/artifacts" / captured["source"]["id"] / captured["version"]["id"] / "original.txt").exists()
        )

    def test_lease_generation_fences_second_worker(self):
        captured = self.publisher.capture(
            {"kind": "text", "title": "Example", "content": "one"}
        )
        self.queue.submit(
            {
                "idempotency_key": "lease",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["version"]["id"],
                "operation": "extract",
                "requested_outputs": ["text"],
                "continuation": {},
            }
        )
        claimed = self.queue.claim("worker-a")
        self.assertIsNotNone(claimed)
        self.assertTrue(self.queue.heartbeat(claimed["job_id"], "worker-a", claimed["lease_generation"]))
        self.assertFalse(self.queue.heartbeat(claimed["job_id"], "worker-b", claimed["lease_generation"]))

    def test_continuation_action_is_durable_and_idempotent(self):
        first = self.queue.begin_action("ingestion:evt_1", "evt_1", "intake_1")
        second = self.queue.begin_action("ingestion:evt_1", "evt_1", "intake_1")
        self.assertEqual(first["status"], "started")
        self.assertEqual(second["status"], "started")
        self.assertTrue(self.queue.complete_action("ingestion:evt_1", ["tasks/task_1.md"]))
        self.assertFalse(self.queue.complete_action("ingestion:evt_1", ["tasks/task_1.md"]))

    def test_password_profile_never_returns_value_from_listing(self):
        result = self.passwords.save("bank_a", "Bank A statement", TEST_DOC_KEY)  # ggignore
        self.assertEqual(result["profile_id"], "bank_a")
        self.assertEqual(self.passwords.resolve("bank_a"), TEST_DOC_KEY)
        self.assertEqual(self.passwords.list(), [{"profile_id": "bank_a", "label": "Bank A statement"}])
        mode = stat.S_IMODE(self.passwords.path.stat().st_mode)
        self.assertEqual(mode, 0o600)
        self.assertNotIn(TEST_DOC_KEY, str(self.passwords.list()))

    # =========================================================================
    # Supported File Formats Tests (Fixtures)
    # =========================================================================

    def test_text_fixture_extraction(self):
        fixture_path = self._copy_fixture("sample.txt")
        captured = self.publisher.capture(
            {"kind": "text", "title": "Text Note", "file_path": str(fixture_path)}
        )
        job = self.queue.submit(
            {
                "idempotency_key": "job-text-file",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["version"]["id"],
                "operation": "extract",
                "requested_outputs": ["text"],
                "continuation": {},
            }
        )
        claimed = self.queue.claim("worker-text")
        result = self.adapters.process_job(claimed, self.publisher, self.passwords, None)
        self.assertEqual(result["status"], "completed")
        self.assertGreater(result["summary"]["lines"], 3)

        # Check extracted markdown artifact
        extraction_file = list(self.workspace.glob(f"sources/artifacts/{captured['source']['id']}/**/text.md"))
        self.assertTrue(len(extraction_file) == 1)
        self.assertIn("Hermes Agent ingestion runtime", extraction_file[0].read_text(encoding="utf-8"))

    def test_csv_fixture_extraction(self):
        fixture_path = self._copy_fixture("sample.csv")
        captured = self.publisher.capture(
            {"kind": "csv", "title": "Sample Expenses", "file_path": str(fixture_path)}
        )
        self.queue.submit(
            {
                "idempotency_key": "job-csv",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["version"]["id"],
                "operation": "extract",
                "requested_outputs": ["tables"],
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-csv")
        result = self.adapters.process_job(job, self.publisher, self.passwords, None)
        self.assertEqual(result["status"], "completed")
        self.assertEqual(result["summary"]["rows"], 4)  # header + 3 data rows

        # Check extracted JSON tables
        extraction_file = list(self.workspace.glob(f"sources/artifacts/{captured['source']['id']}/**/tables.json"))
        self.assertTrue(len(extraction_file) == 1)
        table_data = json.loads(extraction_file[0].read_text(encoding="utf-8"))
        self.assertEqual(table_data["delimiter"], ",")
        self.assertEqual(table_data["rows"][0]["values"], ["date", "category", "item", "amount", "currency"])

    def test_xlsx_fixture_extraction(self):
        fixture_path = self._copy_fixture("sample.xlsx")
        captured = self.publisher.capture(
            {"kind": "excel", "title": "Workbook", "file_path": str(fixture_path)}
        )
        self.queue.submit(
            {
                "idempotency_key": "job-xlsx",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["version"]["id"],
                "operation": "extract",
                "requested_outputs": ["tables"],
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-xlsx")
        result = self.adapters.process_job(job, self.publisher, self.passwords, None)
        self.assertEqual(result["status"], "completed")
        self.assertEqual(result["summary"]["sheets"], 2)

        # Check extracted JSON sheets
        extraction_file = list(self.workspace.glob(f"sources/artifacts/{captured['source']['id']}/**/tables.json"))
        self.assertTrue(len(extraction_file) == 1)
        data = json.loads(extraction_file[0].read_text(encoding="utf-8"))
        sheet_names = [s["name"] for s in data["sheets"]]
        self.assertEqual(sheet_names, ["Summary", "Expenses"])
        # Check formula preservation
        expenses_rows = data["sheets"][1]["rows"]
        total_row = expenses_rows[3]["values"]
        self.assertEqual(total_row[1]["formula"], "=SUM(B2:B3)")

    def test_xls_legacy_fixture_extraction(self):
        fixture_path = self._copy_fixture("sample.xls")
        captured = self.publisher.capture(
            {"kind": "excel", "title": "Legacy Workbook", "file_path": str(fixture_path)}
        )
        self.queue.submit(
            {
                "idempotency_key": "job-xls",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["version"]["id"],
                "operation": "extract",
                "requested_outputs": ["tables"],
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-xls")
        result = self.adapters.process_job(job, self.publisher, self.passwords, None)
        self.assertEqual(result["status"], "completed")
        self.assertEqual(result["summary"]["sheets"], 1)

        extraction_file = list(self.workspace.glob(f"sources/artifacts/{captured['source']['id']}/**/tables.json"))
        self.assertTrue(len(extraction_file) == 1)
        data = json.loads(extraction_file[0].read_text(encoding="utf-8"))
        self.assertEqual(data["sheets"][0]["name"], "Quarterly")
        self.assertEqual(data["sheets"][0]["rows"][0]["values"], ["Quarter", "Revenue"])

    def test_encrypted_xlsx_with_password_profile(self):
        fixture_path = self._copy_fixture("sample_protected.xlsx")
        self.passwords.save("excel_cred", "Quarterly secret", TEST_DOC_KEY)  # ggignore

        captured = self.publisher.capture(
            {"kind": "excel", "title": "Protected Workbook", "file_path": str(fixture_path), "password_profile": "excel_cred"}
        )
        self.queue.submit(
            {
                "idempotency_key": "job-enc-xlsx",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["version"]["id"],
                "operation": "extract",
                "password_profile": "excel_cred",
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-enc-xlsx")
        result = self.adapters.process_job(job, self.publisher, self.passwords, None)
        self.assertEqual(result["status"], "completed")

        extraction_file = list(self.workspace.glob(f"sources/artifacts/{captured['source']['id']}/**/tables.json"))
        self.assertTrue(len(extraction_file) == 1)
        data = json.loads(extraction_file[0].read_text(encoding="utf-8"))
        self.assertEqual(data["sheets"][0]["name"], "Confidential")
        self.assertEqual(data["sheets"][0]["rows"][1]["values"][0]["value"], "Checking")

    def test_encrypted_xlsx_missing_password_error(self):
        fixture_path = self._copy_fixture("sample_protected.xlsx")
        captured = self.publisher.capture(
            {"kind": "excel", "title": "Protected Workbook", "file_path": str(fixture_path)}
        )
        self.queue.submit(
            {
                "idempotency_key": "job-enc-xlsx-nopass",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["version"]["id"],
                "operation": "extract",
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-enc-xlsx-nopass")
        with self.assertRaises(self.adapters.AdapterError) as ctx:
            self.adapters.process_job(job, self.publisher, self.passwords, None)
        self.assertEqual(ctx.exception.code, "password_required")

    def test_encrypted_xlsx_wrong_password_error(self):
        fixture_path = self._copy_fixture("sample_protected.xlsx")
        self.passwords.save("wrong_cred", "Wrong password", WRONG_DOC_KEY)  # ggignore
        captured = self.publisher.capture(
            {"kind": "excel", "title": "Protected Workbook", "file_path": str(fixture_path), "password_profile": "wrong_cred"}
        )
        self.queue.submit(
            {
                "idempotency_key": "job-enc-xlsx-wrong",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["version"]["id"],
                "operation": "extract",
                "password_profile": "wrong_cred",
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-enc-xlsx-wrong")
        with self.assertRaises(self.adapters.AdapterError) as ctx:
            self.adapters.process_job(job, self.publisher, self.passwords, None)
        self.assertIn(ctx.exception.code, {"password_invalid", "unlock_failed"})

    def test_text_pdf_fixture_extraction(self):
        fixture_path = self._copy_fixture("sample_text.pdf")
        captured = self.publisher.capture(
            {"kind": "pdf", "title": "PDF Report", "file_path": str(fixture_path)}
        )
        self.queue.submit(
            {
                "idempotency_key": "job-pdf-text",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["version"]["id"],
                "operation": "extract",
                "requested_outputs": ["text"],
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-pdf")
        result = self.adapters.process_job(job, self.publisher, self.passwords, None)
        self.assertEqual(result["status"], "completed")

        extraction_file = list(self.workspace.glob(f"sources/artifacts/{captured['source']['id']}/**/text.md"))
        self.assertTrue(len(extraction_file) == 1)
        self.assertIn("Hermes Agent Ingestion Report", extraction_file[0].read_text(encoding="utf-8"))

    def test_encrypted_pdf_with_password_profile(self):
        fixture_path = self._copy_fixture("sample_protected.pdf")
        self.passwords.save("pdf_cred", "PDF unlock code", TEST_DOC_KEY)  # ggignore
        captured = self.publisher.capture(
            {"kind": "pdf", "title": "Encrypted PDF", "file_path": str(fixture_path), "password_profile": "pdf_cred"}
        )
        self.queue.submit(
            {
                "idempotency_key": "job-pdf-enc",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["version"]["id"],
                "operation": "extract",
                "password_profile": "pdf_cred",
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-pdf-enc")
        result = self.adapters.process_job(job, self.publisher, self.passwords, None)
        self.assertEqual(result["status"], "completed")

        extraction_file = list(self.workspace.glob(f"sources/artifacts/{captured['source']['id']}/**/text.md"))
        self.assertTrue(len(extraction_file) == 1)
        self.assertIn("Hermes Agent Ingestion Report", extraction_file[0].read_text(encoding="utf-8"))

    def test_encrypted_pdf_missing_password_error(self):
        fixture_path = self._copy_fixture("sample_protected.pdf")
        captured = self.publisher.capture(
            {"kind": "pdf", "title": "Encrypted PDF", "file_path": str(fixture_path)}
        )
        self.queue.submit(
            {
                "idempotency_key": "job-pdf-enc-nopass",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["version"]["id"],
                "operation": "extract",
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-pdf-enc-nopass")
        with self.assertRaises(self.adapters.AdapterError) as ctx:
            self.adapters.process_job(job, self.publisher, self.passwords, None)
        self.assertEqual(ctx.exception.code, "password_required")

    def test_scanned_pdf_vision_fallback(self):
        fixture_path = self._copy_fixture("sample_scanned.pdf")
        captured = self.publisher.capture(
            {"kind": "pdf", "title": "Scanned Document", "file_path": str(fixture_path)}
        )
        self.queue.submit(
            {
                "idempotency_key": "job-scanned-pdf",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["version"]["id"],
                "operation": "extract",
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-scanned-pdf")
        mock_llm = MockVisionLLM(text="Scanned Invoice Line Items")
        result = self.adapters.process_job(job, self.publisher, self.passwords, mock_llm)
        self.assertEqual(result["status"], "completed")
        self.assertEqual(result["summary"]["pages"], 1)
        self.assertEqual(len(mock_llm.calls), 1)

        extraction_file = list(self.workspace.glob(f"sources/artifacts/{captured['source']['id']}/**/text.md"))
        self.assertTrue(len(extraction_file) == 1)
        self.assertIn("Scanned Invoice Line Items", extraction_file[0].read_text(encoding="utf-8"))

    def test_scanned_pdf_requires_vision_model(self):
        fixture_path = self._copy_fixture("sample_scanned.pdf")
        captured = self.publisher.capture(
            {"kind": "pdf", "title": "Scanned Document", "file_path": str(fixture_path)}
        )
        self.queue.submit(
            {
                "idempotency_key": "job-scanned-pdf-nollm",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["version"]["id"],
                "operation": "extract",
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-scanned-pdf-nollm")
        with self.assertRaises(self.adapters.AdapterError) as ctx:
            self.adapters.process_job(job, self.publisher, self.passwords, None)
        self.assertEqual(ctx.exception.code, "vision_unavailable")

    def test_image_png_and_jpg_extraction(self):
        mock_llm = MockVisionLLM(text="OpenLia Visual Capture: Total Due $99.95")
        for ext in ["sample.png", "sample.jpg"]:
            with self.subTest(file=ext):
                fixture_path = self._copy_fixture(ext)
                captured = self.publisher.capture(
                    {"kind": "image", "title": f"Image {ext}", "file_path": str(fixture_path)}
                )
                self.queue.submit(
                    {
                        "idempotency_key": f"job-img-{ext}",
                        "intake_id": captured["intake"]["intake_id"],
                        "source_id": captured["source"]["id"],
                        "version_id": captured["version"]["id"],
                        "operation": "extract",
                        "continuation": {},
                    }
                )
                job = self.queue.claim(f"worker-{ext}")
                result = self.adapters.process_job(job, self.publisher, self.passwords, mock_llm)
                self.assertEqual(result["status"], "completed")

                extraction_file = list(self.workspace.glob(f"sources/artifacts/{captured['source']['id']}/**/text.md"))
                self.assertTrue(len(extraction_file) == 1)
                self.assertIn("Total Due $99.95", extraction_file[0].read_text(encoding="utf-8"))

    def test_generic_file_kind_dispatch(self):
        # A file captured with generic kind='file' should automatically resolve
        # to its proper adapter (e.g. PDF, CSV)
        fixture_path = self._copy_fixture("sample_text.pdf")
        captured = self.publisher.capture(
            {"kind": "file", "title": "Generic File PDF", "file_path": str(fixture_path)}
        )
        self.queue.submit(
            {
                "idempotency_key": "job-generic-file",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["version"]["id"],
                "operation": "extract",
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-generic")
        result = self.adapters.process_job(job, self.publisher, self.passwords, None)
        self.assertEqual(result["status"], "completed")
        self.assertTrue(result["extraction_refs"])

    # =========================================================================
    # YouTube Ingestion Tests (Offline & Live)
    # =========================================================================

    def test_youtube_offline_fixture_extraction(self):
        url = "https://www.youtube.com/watch?v=jNQXAC9IVRw"
        captured = self.publisher.capture({"kind": "youtube", "title": "Me at the zoo", "url": url})
        self.queue.submit(
            {
                "idempotency_key": "job-yt-offline",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["intake"]["version_id"],
                "operation": "extract",
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-yt-offline")

        metadata_fixture = (FIXTURES_DIR / "sample_youtube_metadata.json").read_text(encoding="utf-8")
        vtt_fixture = (FIXTURES_DIR / "sample_subtitles.vtt").read_text(encoding="utf-8")

        def mock_command(command, code):
            if any("dump-single-json" in str(arg) for arg in command):
                return metadata_fixture
            return ""

        def mock_subprocess_run(command, *args, **kwargs):
            # When yt-dlp attempts to write subtitles into temporary directory, write fixture vtt
            out_pattern = None
            for idx, arg in enumerate(command):
                if arg == "--output" and idx + 1 < len(command):
                    out_pattern = command[idx + 1]
                    break
            if out_pattern:
                out_path = Path(out_pattern.replace("%(ext)s", "en.vtt"))
                out_path.parent.mkdir(parents=True, exist_ok=True)
                out_path.write_text(vtt_fixture, encoding="utf-8")
            return subprocess.CompletedProcess(command, 0, stdout="", stderr="")

        with patch.object(self.adapters, "_command", side_effect=mock_command), \
             patch.object(self.adapters.subprocess, "run", side_effect=mock_subprocess_run):
            result = self.adapters.process_job(job, self.publisher, self.passwords, None)

        self.assertEqual(result["status"], "completed")
        extraction_file = list(self.workspace.glob(f"sources/artifacts/{captured['source']['id']}/**/transcript.md"))
        self.assertTrue(len(extraction_file) == 1)
        transcript_content = extraction_file[0].read_text(encoding="utf-8")
        self.assertIn("Welcome to OpenLia", transcript_content)
        self.assertNotIn("WEBVTT", transcript_content)
        self.assertNotIn("Kind:", transcript_content)

    def test_youtube_missing_subtitles_partial_status(self):
        url = "https://www.youtube.com/watch?v=aqz-KE-bpKQ"
        captured = self.publisher.capture({"kind": "youtube", "title": "Big Buck Bunny", "url": url})
        self.queue.submit(
            {
                "idempotency_key": "job-yt-nosubs",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["intake"]["version_id"],
                "operation": "extract",
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-yt-nosubs")

        metadata_fixture = json.dumps({"id": "aqz-KE-bpKQ", "title": "Big Buck Bunny", "channel": "Blender", "webpage_url": url, "duration": 60})

        with patch.object(self.adapters, "_command", return_value=metadata_fixture), \
             patch.object(self.adapters.subprocess, "run", return_value=subprocess.CompletedProcess([], 0)):
            result = self.adapters.process_job(job, self.publisher, self.passwords, None)

        self.assertEqual(result["status"], "partial")
        self.assertIn("transcript_unavailable", result["summary"]["warnings"])

    def test_youtube_live_extraction(self):
        # Sample video: "Me at the zoo" (first YouTube video, 19 seconds, permanent captions)
        sample_url = "https://www.youtube.com/watch?v=jNQXAC9IVRw"
        try:
            req = urllib.request.Request(sample_url, headers={"User-Agent": "OpenLia-Test"})
            with urllib.request.urlopen(req, timeout=5):
                pass
        except Exception as exc:
            self.skipTest(f"Live YouTube network test skipped (network unreachable: {exc})")

        captured = self.publisher.capture(
            {"kind": "youtube", "title": "Me at the zoo Live", "url": sample_url}
        )
        self.queue.submit(
            {
                "idempotency_key": "job-yt-live",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["intake"]["version_id"],
                "operation": "extract",
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-yt-live")
        try:
            result = self.adapters.process_job(job, self.publisher, self.passwords, None)
        except self.adapters.AdapterError as exc:
            self.skipTest(f"Live yt-dlp extraction skipped due to provider rate limit/environment: {exc}")

        self.assertEqual(result["status"], "completed")
        self.assertTrue(result["extraction_refs"])
        extraction_file = list(self.workspace.glob(f"sources/artifacts/{captured['source']['id']}/**/transcript.md"))
        self.assertTrue(len(extraction_file) == 1)
        transcript = extraction_file[0].read_text(encoding="utf-8")
        self.assertIn("elephants", transcript.lower())

    # =========================================================================
    # Hermes Agent Runtime Tools Integration Tests
    # =========================================================================

    def test_runtime_tools_full_workflow(self):
        ctx = MockHermesContext()
        runtime = self.runtime_mod.IngestionRuntime(ctx)
        runtime.register_tools()

        # 1. ingestion_capture
        fixture_path = self._copy_fixture("sample.csv")
        capture_resp = json.loads(
            runtime.capture({"kind": "csv", "title": "Tool Capture", "file_path": str(fixture_path)})
        )
        self.assertTrue(capture_resp["ok"])
        self.assertEqual(capture_resp["state"], "queued")
        job_id = capture_resp["job_id"]
        intake_id = capture_resp["intake_id"]

        # 2. ingestion_status
        status_resp = json.loads(runtime.status({"job_id": job_id}))
        self.assertTrue(status_resp["ok"])
        self.assertEqual(status_resp["state"], "queued")

        # 3. Simulate worker execution
        job = self.queue.claim("test-tool-worker")
        result = self.adapters.process_job(job, self.publisher, self.passwords, None)
        event_id = self.queue.finish(job, result)
        self.assertIsNotNone(event_id)

        # 4. Status after completion
        status_after = json.loads(runtime.status({"intake_id": intake_id}))
        self.assertEqual(status_after["state"], "completed")

        # 5. ingestion_acknowledge
        ack_resp = json.loads(
            runtime.acknowledge({"event_id": event_id, "intake_id": intake_id, "action_id": "act_test_1"})
        )
        self.assertTrue(ack_resp["ok"])
        self.assertTrue(ack_resp["acknowledged"])

        # 6. ingestion_complete_action
        complete_resp = json.loads(
            runtime.complete_action({"action_id": "act_test_1", "record_refs": ["knowledge/note.md"]})
        )
        self.assertTrue(complete_resp["ok"])
        self.assertTrue(complete_resp["completed"])

    def test_runtime_password_management_tools(self):
        ctx = MockHermesContext()
        runtime = self.runtime_mod.IngestionRuntime(ctx)
        runtime.register_tools()

        # save_document_password
        save_resp = json.loads(
            runtime.save_password({"profile_id": "fin_2026", "label": "Tax documents 2026", "password": TEST_DOC_KEY})  # ggignore
        )
        self.assertTrue(save_resp["ok"])
        self.assertEqual(save_resp["profile_id"], "fin_2026")

        # list_document_password_profiles (must not reveal password)
        list_resp = json.loads(runtime.list_passwords({}))
        self.assertTrue(list_resp["ok"])
        self.assertEqual(list_resp["profiles"], [{"label": "Tax documents 2026", "profile_id": "fin_2026"}])
        self.assertNotIn(TEST_DOC_KEY, json.dumps(list_resp))

        # retry_ingestion_with_password_profile
        fixture_path = self._copy_fixture("sample_protected.xlsx")
        captured = self.publisher.capture({"kind": "excel", "title": "Locked", "file_path": str(fixture_path)})
        self.queue.submit(
            {
                "idempotency_key": "job-retry-test",
                "intake_id": captured["intake"]["intake_id"],
                "source_id": captured["source"]["id"],
                "version_id": captured["version"]["id"],
                "operation": "extract",
                "continuation": {},
            }
        )
        job = self.queue.claim("worker-fail")
        self.queue.block_for_password(job, "password_required", "need password")

        retry_resp = json.loads(
            runtime.retry_with_password({"intake_id": captured["intake"]["intake_id"], "profile_id": "fin_2026"})
        )
        self.assertTrue(retry_resp["ok"])
        self.assertEqual(retry_resp["state"], "queued")


if __name__ == "__main__":
    unittest.main()
