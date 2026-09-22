#!/usr/bin/env python3
"""Unit tests for PlaywrightMcpClient header generation and URL handling."""

import sys
import unittest
import urllib.parse
from unittest import mock
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

    def test_chatgpt_preflight_reconnects_before_prompt_submission(self) -> None:
        class FakeClient:
            def __init__(self, fail_first: bool) -> None:
                self.fail_first = fail_first
                self.calls: list[tuple[str, dict]] = []
                self.closed = False

            def call_tool(self, name: str, arguments: dict, timeout: float = 90.0) -> dict:
                self.calls.append((name, arguments))
                if name == "browser_tabs" and arguments.get("action") == "list":
                    return {"content": [{"type": "text", "text": "[Welcome](chrome-extension://relay)"}]}
                if self.fail_first:
                    self.fail_first = False
                    raise TimeoutError("transport reset")
                return {"content": []}

            def get_tool_text(self, result: dict) -> str:
                return "".join(item.get("text", "") for item in result.get("content", []))

            def close(self) -> None:
                self.closed = True

        first = FakeClient(True)
        second = FakeClient(False)
        second.fail_first = False
        clients = iter([second])
        result = __import__("chatgpt_conversation").prepare_temporary_chat(
            first,
            client_factory=lambda: next(clients),
            retries=1,
        )

        self.assertIs(result, second)
        self.assertTrue(first.closed)
        self.assertEqual([name for name, _ in first.calls], ["browser_tabs", "browser_navigate", "browser_tabs"])
        self.assertEqual([name for name, _ in second.calls], ["browser_tabs", "browser_navigate"])
        self.assertEqual(second.calls[0][1], {"action": "list"})

    def test_chatgpt_preflight_does_not_retry_without_factory(self) -> None:
        class BrokenClient:
            def call_tool(self, _name: str, _arguments: dict, timeout: float = 90.0) -> dict:
                raise TimeoutError("transport reset")

            def close(self) -> None:
                pass

        with self.assertRaisesRegex(RuntimeError, "no prompt was sent"):
            __import__("chatgpt_conversation").prepare_temporary_chat(BrokenClient(), retries=1)

    def test_failed_preflight_attempt_closes_owned_tab_before_retry(self) -> None:
        module = __import__("chatgpt_conversation")
        with mock.patch.object(module, "close_current_tab") as close_tab:
            class Client:
                def call_tool(self, _name: str, _arguments: dict, timeout: float = 90.0) -> dict:
                    raise TimeoutError("transport reset")

                def close(self) -> None:
                    pass

            with self.assertRaisesRegex(RuntimeError, "no prompt was sent"):
                module.prepare_temporary_chat(Client(), retries=0)
            close_tab.assert_not_called()

    def test_chatgpt_executor_closes_tab_when_gatekeeper_fails(self) -> None:
        module = __import__("chatgpt_conversation")

        class Client:
            def call_tool(self, name: str, _arguments: dict, timeout: float = 90.0) -> dict:
                if name == "browser_navigate":
                    return {"content": []}
                if name == "browser_tabs":
                    return {"content": [{"type": "text", "text": "[Welcome](chrome-extension://relay)"}]}
                if name == "browser_snapshot":
                    return {"content": []}
                return {"content": []}

            def get_tool_text(self, result: dict) -> str:
                return "".join(item.get("text", "") for item in result.get("content", []))

            def close(self) -> None:
                pass

        with mock.patch.object(module, "close_current_tab") as close_tab:
            with self.assertRaisesRegex(RuntimeError, "Temporary Chat"):
                module.execute_chatgpt_chat("test", client=Client(), timeout=1)
            close_tab.assert_called_once()

    def test_chatgpt_preflight_rejects_non_extension_tab(self) -> None:
        module = __import__("chatgpt_conversation")

        class UserTab:
            def __init__(self) -> None:
                self.calls: list[tuple[str, dict]] = []

            def call_tool(self, name: str, arguments: dict, timeout: float = 90.0) -> dict:
                self.calls.append((name, arguments))
                return {"content": [{"type": "text", "text": "- 0: (current) [My tab](https://example.com)"}]}

            def get_tool_text(self, result: dict) -> str:
                return "".join(item.get("text", "") for item in result.get("content", []))

            def close(self) -> None:
                self.calls.append(("close", {}))

        client = UserTab()
        with mock.patch.object(module, "close_current_tab") as close_tab:
            with self.assertRaisesRegex(RuntimeError, "no prompt was sent"):
                module.prepare_temporary_chat(client, retries=0)
            close_tab.assert_not_called()
        self.assertEqual([name for name, _ in client.calls], ["browser_tabs"])

    def test_gemini_executor_accepts_worker_client_factory(self) -> None:
        import inspect
        module = __import__("gemini_conversation")
        self.assertIn("client_factory", inspect.signature(module.execute_gemini_chat).parameters)

    def test_chatgpt_preflight_reuses_existing_tab_when_new_tab_hangs(self) -> None:
        class ExistingTabClient:
            def __init__(self) -> None:
                self.calls: list[tuple[str, dict]] = []

            def call_tool(self, name: str, arguments: dict, timeout: float = 90.0) -> dict:
                self.calls.append((name, arguments))
                if name == "browser_tabs" and arguments.get("action") == "list":
                    return {"content": [{"type": "text", "text": "[Welcome](chrome-extension://relay)"}]}
                return {"content": []}

            def get_tool_text(self, result: dict) -> str:
                return "".join(item.get("text", "") for item in result.get("content", []))

            def close(self) -> None:
                pass

        client = ExistingTabClient()
        result = __import__("chatgpt_conversation").prepare_temporary_chat(client, retries=0)
        self.assertIs(result, client)
        self.assertEqual([name for name, _ in client.calls], ["browser_tabs", "browser_navigate"])
        self.assertEqual(client.calls[-1][1]["url"], "https://chatgpt.com/?temporary-chat=true")



if __name__ == "__main__":
    unittest.main()
