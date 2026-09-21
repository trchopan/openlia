#!/usr/bin/env python3
"""Deterministic validation and live verification helper for the browser-pilot skill."""

from __future__ import annotations

import json
import os
import sys
import threading
import urllib.parse
import urllib.request

BLOCKED_HOSTS = {
    "localhost",
    "127.0.0.1",
    "0.0.0.0",
    "169.254.169.254",  # Cloud metadata
    "metadata.google.internal",
}


def is_safe_target_url(url: str) -> bool:
    """Validate that target URL is a safe, non-internal HTTP/HTTPS address."""
    if not url or not isinstance(url, str):
        return False
    parsed = urllib.parse.urlparse(url.strip())
    if parsed.scheme not in ("http", "https"):
        return False
    hostname = (parsed.hostname or "").lower()
    if not hostname or hostname in BLOCKED_HOSTS:
        return False
    if hostname.startswith("127.") or hostname.startswith("10.") or hostname.startswith("192.168."):
        return False
    return True


def detect_bot_challenge(page_text: str) -> bool:
    """Detect common anti-bot / verification challenges."""
    lowered = page_text.lower()
    indicators = (
        "please verify you are human",
        "cloudflare",
        "turnstile",
        "slider verification",
        "kéo thanh trượt để xác minh",
        "xác minh bảo mật",
        "security check",
        "robot",
    )
    return any(ind in lowered for ind in indicators)


def prune_playwright_mcp_logs(
    directory: str = ".playwright-mcp",
    max_files: int = 20,
    max_age_hours: float = 24.0,
) -> int:
    """Prune old Playwright MCP logs and snapshots to prevent disk bloat.
    
    Removes files older than max_age_hours, and caps the total number of files to max_files
    retaining only the most recently modified ones.
    Returns the count of deleted files.
    """
    if not os.path.exists(directory) or not os.path.isdir(directory):
        return 0

    now = os.path.getmtime(directory)
    # Use current epoch time
    import time
    now_ts = time.time()
    max_age_sec = max_age_hours * 3600.0

    entries: list[tuple[str, float]] = []
    deleted_count = 0

    for fname in os.listdir(directory):
        fpath = os.path.join(directory, fname)
        if not os.path.isfile(fpath):
            continue
        try:
            mtime = os.path.getmtime(fpath)
            # Evict if older than max_age_hours
            if now_ts - mtime > max_age_sec:
                os.remove(fpath)
                deleted_count += 1
            else:
                entries.append((fpath, mtime))
        except OSError:
            pass

    # Evict oldest files if count still exceeds max_files
    if len(entries) > max_files:
        # Sort newest first
        entries.sort(key=lambda x: x[1], reverse=True)
        to_delete = entries[max_files:]
        for fpath, _ in to_delete:
            try:
                os.remove(fpath)
                deleted_count += 1
            except OSError:
                pass

    return deleted_count


def enforce_tab_cap(client: PlaywrightMcpClient, max_tabs: int = 2) -> int:
    """Check open tabs and close any excess tabs beyond max_tabs to avoid leaks when unattended."""
    try:
        res = client.call_tool("browser_tabs", {"action": "list"}, timeout=10.0)
    except Exception:
        return 0

    text = ""
    for item in res.get("content", []):
        if item.get("type") == "text":
            text += item.get("text", "")

    # Tabs listed as: - <index>: (current) [title](url) or - <index>: [title](url)
    indices: list[int] = []
    for line in text.splitlines():
        line = line.strip()
        if line.startswith("- "):
            parts = line.split(":", 1)
            try:
                idx = int(parts[0].replace("-", "").strip())
                indices.append(idx)
            except ValueError:
                continue

    closed_count = 0
    if len(indices) > max_tabs:
        # Close from highest index downwards to keep early/active tabs stable
        for idx in sorted(indices, reverse=True)[: len(indices) - max_tabs]:
            try:
                client.call_tool("browser_tabs", {"action": "close", "index": idx}, timeout=5.0)
                closed_count += 1
            except Exception:
                pass

    return closed_count


def close_current_tab(client: PlaywrightMcpClient) -> bool:
    """Close the current tab cleanly via browser_tabs."""
    try:
        client.call_tool("browser_tabs", {"action": "close"}, timeout=10.0)
        return True
    except Exception:
        return False


class PlaywrightMcpClient:
    """Lightweight standard-library MCP SSE client."""

    def __init__(self, base_url: str = "http://localhost:8931"):
        self.base_url = base_url.rstrip("/")
        self.sse_url = f"{self.base_url}/sse"
        parsed = urllib.parse.urlparse(self.base_url)
        self.host_header = f"localhost:{parsed.port or 8931}"
        self.post_url: str | None = None
        self.req_id = 0
        self.lock = threading.Lock()
        self.pending: dict[int, tuple[threading.Event, list[dict]]] = {}
        self.running = True
        self._connect()

    def _connect(self) -> None:
        req = urllib.request.Request(self.sse_url, headers={"Accept": "text/event-stream", "Host": self.host_header})
        self.resp = urllib.request.urlopen(req, timeout=10)

        while True:
            line = self.resp.readline().decode("utf-8")
            if not line:
                raise RuntimeError("SSE stream closed before endpoint received")
            if line.startswith("data:"):
                endpoint_path = line.split(":", 1)[1].strip()
                self.post_url = urllib.parse.urljoin(self.base_url, endpoint_path)
                break

        self.thread = threading.Thread(target=self._reader_loop, daemon=True)
        self.thread.start()

        self.call("initialize", {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "browser-pilot-check", "version": "1.0"},
        })
        self.notify("notifications/initialized")

    def _reader_loop(self) -> None:
        while self.running:
            try:
                line = self.resp.readline().decode("utf-8")
                if not line:
                    break
                line = line.strip()
                if line.startswith("data:"):
                    payload = json.loads(line[5:].strip())
                    msg_id = payload.get("id")
                    if msg_id in self.pending:
                        event, result_box = self.pending[msg_id]
                        result_box.append(payload)
                        event.set()
            except Exception:
                break

    def call(self, method: str, params: dict | None = None, timeout: float = 30.0) -> dict:
        with self.lock:
            self.req_id += 1
            cid = self.req_id
        evt = threading.Event()
        res_box: list[dict] = []
        self.pending[cid] = (evt, res_box)

        body = {"jsonrpc": "2.0", "id": cid, "method": method}
        if params is not None:
            body["params"] = params

        req = urllib.request.Request(
            self.post_url,
            data=json.dumps(body).encode("utf-8"),
            headers={"Content-Type": "application/json", "Host": self.host_header},
        )
        urllib.request.urlopen(req, timeout=timeout)

        if not evt.wait(timeout=timeout):
            self.pending.pop(cid, None)
            raise TimeoutError(f"RPC call {method} (id={cid}) timed out after {timeout}s")

        self.pending.pop(cid, None)
        res = res_box[0]
        if "error" in res:
            raise RuntimeError(f"RPC error: {res['error']}")
        return res.get("result", {})

    def notify(self, method: str, params: dict | None = None) -> None:
        body = {"jsonrpc": "2.0", "method": method}
        if params is not None:
            body["params"] = params
        req = urllib.request.Request(
            self.post_url,
            data=json.dumps(body).encode("utf-8"),
            headers={"Content-Type": "application/json", "Host": self.host_header},
        )
        urllib.request.urlopen(req, timeout=5)

    def call_tool(self, name: str, arguments: dict, timeout: float = 60.0) -> dict:
        return self.call("tools/call", {"name": name, "arguments": arguments}, timeout=timeout)

    def get_tool_text(self, res: dict) -> str:
        text = ""
        for item in res.get("content", []):
            if item.get("type") == "text":
                text += item.get("text", "")
        return text


def live_verify(mcp_url: str = "") -> None:
    """Live verification against Playwright MCP."""
    target_url = mcp_url or os.getenv("OPENLIA_BROWSER_MCP_URL", "http://localhost:8931")
    pruned = prune_playwright_mcp_logs(".playwright-mcp", max_files=20, max_age_hours=24)
    if pruned > 0:
        print(f"Pruned {pruned} old/excess .playwright-mcp log file(s).")

    print(f"Connecting to Playwright MCP at {target_url}...")
    try:
        client = PlaywrightMcpClient(target_url)
        closed_tabs = enforce_tab_cap(client, max_tabs=2)
        if closed_tabs > 0:
            print(f"Enforced tab cap: closed {closed_tabs} excess background tab(s).")

        res = client.call_tool("browser_tabs", {"action": "list"})
        tabs_text = ""
        for item in res.get("content", []):
            if item.get("type") == "text":
                tabs_text += item.get("text", "")
        print("Playwright MCP connection successful.")
        print(f"Active tabs summary:\n{tabs_text.strip()}")
        print("browser-pilot: live verification PASSED")
    except Exception as e:
        print(f"browser-pilot: live verification FAILED: {e}", file=sys.stderr)
        sys.exit(1)


def self_test() -> None:
    # URL Safety checks
    assert is_safe_target_url("https://maps.google.com")
    assert is_safe_target_url("https://gemini.google.com")
    assert is_safe_target_url("https://shopee.vn")
    assert not is_safe_target_url("http://localhost:8931")
    assert not is_safe_target_url("http://127.0.0.1:8931")
    assert not is_safe_target_url("http://169.254.169.254/computeMetadata/v1")
    assert not is_safe_target_url("file:///etc/passwd")

    # Challenge detection checks
    assert detect_bot_challenge("Cloudflare Ray ID: ... Please verify you are human")
    assert detect_bot_challenge("Shopee Security Check: Kéo thanh trượt để xác minh")
    assert not detect_bot_challenge("Google Maps: 25 mins in light traffic to Tan Son Nhat Airport")

    # Log pruning tests
    import tempfile
    import time
    with tempfile.TemporaryDirectory() as tmpdir:
        # Create 5 files with varying ages
        now = time.time()
        for i in range(5):
            p = os.path.join(tmpdir, f"test-{i}.log")
            with open(p, "w") as f:
                f.write("log")
            # Set mtimes: 0 is oldest, 4 is newest
            os.utime(p, (now - (10 - i) * 100, now - (10 - i) * 100))

        # Cap at 3 files
        deleted = prune_playwright_mcp_logs(tmpdir, max_files=3, max_age_hours=100)
        assert deleted == 2, f"Expected 2 deleted, got {deleted}"
        remaining = sorted(os.listdir(tmpdir))
        assert len(remaining) == 3
        # Ensure newest files (test-2, test-3, test-4) were retained
        assert "test-4.log" in remaining
        assert "test-3.log" in remaining
        assert "test-2.log" in remaining


if __name__ == "__main__":
    if len(sys.argv) == 2 and sys.argv[1] == "--self-test":
        self_test()
        print("ok")
        sys.exit(0)
    elif len(sys.argv) >= 2 and sys.argv[1] == "--verify":
        url = sys.argv[2] if len(sys.argv) > 2 else ""
        live_verify(url)
        sys.exit(0)
    else:
        raise SystemExit("usage: check_browser_pilot.py [--self-test | --verify [MCP_URL]]")
