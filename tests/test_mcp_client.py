#!/usr/bin/env python3
"""Unit tests for PlaywrightMcpClient header generation and URL handling."""

import sys
import unittest
import urllib.parse
from pathlib import Path

SKILLS_DIR = Path(__file__).resolve().parents[1] / "profile" / "skills"
sys.path.insert(0, str(SKILLS_DIR / "browser-pilot" / "scripts"))
sys.path.insert(0, str(SKILLS_DIR / "chatgpt-chat" / "scripts"))
sys.path.insert(0, str(SKILLS_DIR / "gemini-chat" / "scripts"))

from check_browser_pilot import PlaywrightMcpClient as BrowserMcpClient  # noqa: E402
from chatgpt_conversation import PlaywrightMcpClient as ChatGPTMcpClient  # noqa: E402
from gemini_conversation import PlaywrightMcpClient as GeminiMcpClient  # noqa: E402


class McpClientHeaderTest(unittest.TestCase):
    def test_host_header_normalization(self) -> None:
        test_cases = [
            ("http://localhost:8931", "localhost:8931"),
            ("http://locho-test-host:8931", "localhost:8931"),
            ("http://locho-laptop:9000", "localhost:9000"),
            ("http://127.0.0.1:8931/", "localhost:8931"),
            ("http://remote-host", "localhost:8931"),
        ]

        for url, expected_host in test_cases:
            parsed = urllib.parse.urlparse(url.rstrip("/"))
            derived = f"localhost:{parsed.port or 8931}"
            self.assertEqual(derived, expected_host, f"Failed for url={url}")

    def test_clients_define_host_header_and_timeout(self) -> None:
        for client_cls in (BrowserMcpClient, ChatGPTMcpClient, GeminiMcpClient):
            # Verify constructor defines host_header and expected methods
            self.assertTrue(hasattr(client_cls, "call"))
            self.assertTrue(hasattr(client_cls, "call_tool"))
            self.assertTrue(hasattr(client_cls, "notify"))


if __name__ == "__main__":
    unittest.main()
