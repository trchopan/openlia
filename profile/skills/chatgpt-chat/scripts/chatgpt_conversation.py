#!/usr/bin/env python3
"""Deterministic helper, YAML serializer, and Playwright MCP automation for the chatgpt-chat skill."""

from __future__ import annotations

import argparse
import datetime
import json
import os
import re
import sys
import threading
import time
import urllib.parse
import urllib.request
from typing import Any, Callable

import yaml


_pilot_dir = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", "browser-pilot", "scripts"))
if _pilot_dir not in sys.path:
    sys.path.insert(0, _pilot_dir)
try:
    from check_browser_pilot import prune_playwright_mcp_logs, enforce_tab_cap, close_current_tab
except ImportError:
    def prune_playwright_mcp_logs(*args, **kwargs): return 0
    def enforce_tab_cap(*args, **kwargs): return 0
    def close_current_tab(*args, **kwargs): return False


class BlockScalarDumper(yaml.SafeDumper):
    """YAML dumper that represents multi-line strings as literal block scalars (|) without anchors."""

    def ignore_aliases(self, data: Any) -> bool:
        return True


def _str_presenter(dumper: yaml.Dumper, data: str) -> yaml.Node:
    cleaned = "\n".join(line.rstrip() for line in data.replace("\r\n", "\n").split("\n")).strip()
    cleaned = cleaned.replace("\t", "    ")
    if "\n" in cleaned:
        return dumper.represent_scalar("tag:yaml.org,2002:str", cleaned, style="|")
    return dumper.represent_scalar("tag:yaml.org,2002:str", cleaned)


BlockScalarDumper.add_representer(str, _str_presenter)


TRACKING_PARAMS = {
    "utm_source",
    "utm_medium",
    "utm_campaign",
    "utm_term",
    "utm_content",
    "utm_id",
    "fbclid",
    "gclid",
}


def clean_url(url: str) -> str:
    """Clean and canonicalize URLs by removing tracking query parameters."""
    if not url or not isinstance(url, str):
        return ""
    parsed = urllib.parse.urlparse(url.strip())
    query_params = urllib.parse.parse_qsl(parsed.query, keep_blank_values=True)
    filtered = [
        (k, v)
        for k, v in query_params
        if k.lower() not in TRACKING_PARAMS and not k.lower().startswith("utm_")
    ]
    new_query = urllib.parse.urlencode(filtered)
    return urllib.parse.urlunparse((
        parsed.scheme,
        parsed.netloc,
        parsed.path,
        parsed.params,
        new_query,
        parsed.fragment,
    ))


def infer_site_metadata(url: str, title: str = "") -> tuple[str, str]:
    """Infer clean domain and friendly site/publisher name from URL and title."""
    if not url:
        return ("", "Web")
    parsed = urllib.parse.urlparse(url.strip())
    hostname = (parsed.hostname or "").lower()
    path = parsed.path.lower()

    if "doc.rust-lang.org" in hostname:
        if "/book/" in path:
            return (hostname, "The Rust Book")
        if "/std/" in path:
            return (hostname, "Rust Standard Library")
        return (hostname, "Rust Documentation")
    if "docs.rs" in hostname:
        parts = [p for p in path.split("/") if p]
        if parts:
            return (hostname, f"{parts[0].capitalize()} Docs")
        return (hostname, "Docs.rs")
    if "go.dev" in hostname or "golang.org" in hostname:
        if "/ref/spec" in path:
            return (hostname, "Go Language Specification")
        if "/ref/mem" in path:
            return (hostname, "Go Memory Model")
        if "/blog/" in path:
            return (hostname, "Go Blog")
        return (hostname, "Go Documentation")
    if "github.com" in hostname:
        return (hostname, "GitHub")
    if "stackoverflow.com" in hostname:
        return (hostname, "Stack Overflow")
    if "wikipedia.org" in hostname:
        return (hostname, "Wikipedia")

    clean_domain = hostname.removeprefix("www.")
    site_name = clean_domain.split(".")[0].capitalize() if clean_domain else "Web"
    return (hostname, site_name)


def clean_title(raw_title: str, url: str) -> str:
    """Clean noisy badge strings (+1, +2, duplicate words) from citation titles."""
    if not raw_title:
        raw_title = ""

    cleaned = re.sub(r"\+\d+", "", raw_title)
    cleaned = re.sub(r"\s+", " ", cleaned).strip()

    words = cleaned.split()
    deduped: list[str] = []
    for w in words:
        if not deduped or deduped[-1].lower() != w.lower():
            deduped.append(w)
    cleaned = " ".join(deduped).strip()

    if not cleaned or cleaned.lower() in ("go", "docs.rs", "rust documentation", "github"):
        domain, site_name = infer_site_metadata(url, raw_title)
        parsed = urllib.parse.urlparse(url)
        path_parts = [p for p in parsed.path.split("/") if p]
        if path_parts:
            last_part = path_parts[-1].removesuffix(".html").replace("-", " ").replace("_", " ").strip()
            if last_part and last_part not in ("index", "mod", "fn") and last_part.lower() not in site_name.lower():
                return f"{site_name}: {last_part.title()}"
        return site_name

    return cleaned


def slugify(text: str) -> str:
    """Convert a title or topic string into a clean lowercase slug."""
    text = text.strip().lower()
    text = re.sub(r"[^\w\s-]", "", text)
    text = re.sub(r"[\s_-]+", "_", text)
    return text.strip("_") or "chat"


def resolve_workspace_root(
    cwd: str | None = None,
    hermes_home: str | None = None,
    configured_root: str | None = None,
) -> str:
    """Resolve the Personal OS workspace without appending workspace twice."""
    if configured_root is None:
        configured_root = os.getenv("OPENLIA_WORKSPACE_ROOT", "")
    if configured_root:
        return os.path.abspath(configured_root)
    if hermes_home is None:
        hermes_home = os.getenv("HERMES_HOME", "")
    if hermes_home:
        return os.path.join(os.path.abspath(hermes_home), "workspace")
    if cwd is None:
        cwd = os.getcwd()
    cwd = os.path.abspath(cwd)
    if os.path.basename(cwd) == "workspace":
        return cwd
    return os.path.join(cwd, "workspace")


def generate_timestamped_filename(topic: str, dt: datetime.datetime | None = None) -> str:
    """Generate canonical timestamp-prefixed filename: YYYYMMDD_HHMMSS_<slug>.yaml."""
    if dt is None:
        dt = datetime.datetime.now(datetime.timezone.utc)
    prefix = dt.strftime("%Y%m%d_%H%M%S")
    slug = slugify(topic)[:50]
    return f"{prefix}_{slug}.yaml"


def verify_temporary_chat_url(url: str) -> bool:
    """Validate that target URL points to ChatGPT with temporary-chat enabled."""
    if not url or not isinstance(url, str):
        return False
    parsed = urllib.parse.urlparse(url.strip())
    hostname = (parsed.hostname or "").lower()
    if hostname not in ("chatgpt.com", "chat.openai.com"):
        return False
    query_params = urllib.parse.parse_qs(parsed.query)
    temp_vals = query_params.get("temporary-chat", [])
    return "true" in temp_vals or "1" in temp_vals


def detect_temporary_chat_state(page_text: str) -> bool:
    """Detect presence of Temporary Chat indicator markers in page text or snapshot."""
    if not page_text or not isinstance(page_text, str):
        return False
    lowered = page_text.lower()
    indicators = (
        "temporary chat",
        "chat history is turned off",
        "chat history is off",
        "this chat won't appear in history",
        "won't be used to train our models",
        "tin nhắn tạm thời",
    )
    return any(ind in lowered for ind in indicators)


def build_conversation_yaml(
    session_meta: dict[str, Any],
    messages: list[dict[str, Any]],
) -> str:
    """Build standardized YAML representation of ChatGPT conversation with frontend-ready references."""
    if not isinstance(session_meta, dict):
        raise ValueError("session_meta must be a dict")
    if not isinstance(messages, list):
        raise ValueError("messages must be a list of dicts")

    now = datetime.datetime.now(datetime.timezone.utc).isoformat()
    meta = {
        "platform": session_meta.get("platform", "chatgpt"),
        "mode": "temporary",
        "started_at": session_meta.get("started_at", now),
        "model": session_meta.get("model", "ChatGPT"),
        "topic": session_meta.get("topic", "unnamed-chat"),
        "url": clean_url(session_meta.get("url", "")),
        "turns_count": len(messages),
    }

    # Step 1: Discover all unique references and collect turn appearances
    global_refs_map: dict[str, dict[str, Any]] = {}
    ref_counter = 0

    for idx, msg in enumerate(messages, start=1):
        turn_num = msg.get("turn", (idx + 1) // 2)
        raw_refs = msg.get("references") or []
        for ref in raw_refs:
            if isinstance(ref, dict):
                r_url = clean_url(ref.get("url", ""))
                r_title = ref.get("title", "")
            elif isinstance(ref, str):
                r_url = clean_url(ref)
                r_title = ""
            else:
                continue

            if not r_url or not r_url.startswith("http"):
                continue

            if r_url not in global_refs_map:
                ref_counter += 1
                domain, site_name = infer_site_metadata(r_url, r_title)
                c_title = clean_title(r_title, r_url)
                global_refs_map[r_url] = {
                    "id": f"ref-{ref_counter}",
                    "index": ref_counter,
                    "title": c_title,
                    "url": r_url,
                    "domain": domain,
                    "site_name": site_name,
                    "turns": {turn_num},
                }
            else:
                global_refs_map[r_url]["turns"].add(turn_num)

    # Step 2: Build messages array with independent reference copies
    formatted_messages = []
    for idx, msg in enumerate(messages, start=1):
        role = msg.get("role", "unknown").lower()
        if role not in ("user", "assistant", "system"):
            raise ValueError(f"Invalid message role '{role}' at index {idx}")
        content = str(msg.get("content", "")).strip()

        msg_data: dict[str, Any] = {
            "role": role,
            "turn": msg.get("turn", (idx + 1) // 2),
            "content": content,
        }

        raw_refs = msg.get("references") or []
        msg_refs = []
        msg_seen_urls = set()
        for ref in raw_refs:
            r_url = clean_url(ref.get("url", "") if isinstance(ref, dict) else str(ref))
            if r_url in global_refs_map and r_url not in msg_seen_urls:
                msg_seen_urls.add(r_url)
                item = global_refs_map[r_url]
                msg_refs.append({
                    "id": item["id"],
                    "index": item["index"],
                    "title": item["title"],
                    "url": item["url"],
                    "domain": item["domain"],
                    "site_name": item["site_name"],
                })

        if msg_refs:
            msg_data["references"] = msg_refs

        formatted_messages.append(msg_data)

    # Step 3: Build root references list
    root_references = []
    for r_url, item in global_refs_map.items():
        root_references.append({
            "id": item["id"],
            "index": item["index"],
            "title": item["title"],
            "url": item["url"],
            "domain": item["domain"],
            "site_name": item["site_name"],
            "turns": sorted(list(item["turns"])),
        })

    doc: dict[str, Any] = {
        "schema": 1,
        "session": meta,
        "messages": formatted_messages,
    }

    if root_references:
        doc["references"] = root_references

    return yaml.dump(
        doc,
        Dumper=BlockScalarDumper,
        sort_keys=False,
        allow_unicode=True,
        default_flow_style=False,
    )


def validate_conversation_yaml(yaml_content: str) -> dict[str, Any]:
    """Parse and validate conversation YAML against expected schema."""
    data = yaml.safe_load(yaml_content)
    if not isinstance(data, dict):
        raise ValueError("Invalid YAML: root must be a mapping")
    if data.get("schema") != 1:
        raise ValueError(f"Unsupported schema version: {data.get('schema')}")
    session = data.get("session")
    if not isinstance(session, dict):
        raise ValueError("Invalid YAML: 'session' must be a mapping")
    if session.get("mode") != "temporary":
        raise ValueError("Session mode must be 'temporary'")
    messages = data.get("messages")
    if not isinstance(messages, list):
        raise ValueError("Invalid YAML: 'messages' must be a list")
    for idx, msg in enumerate(messages, start=1):
        if not isinstance(msg, dict):
            raise ValueError(f"Message {idx} must be a mapping")
        if "role" not in msg or "content" not in msg:
            raise ValueError(f"Message {idx} missing 'role' or 'content'")
    return data


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
            "clientInfo": {"name": "chatgpt-chat-script", "version": "1.0"},
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

    def close(self) -> None:
        """Close the local SSE connection without making another MCP call."""
        self.running = False
        response = getattr(self, "resp", None)
        if response is not None:
            try:
                response.close()
            except Exception:
                pass

    def call_tool(self, name: str, arguments: dict, timeout: float = 90.0) -> dict:
        return self.call("tools/call", {"name": name, "arguments": arguments}, timeout=timeout)

    def get_tool_text(self, res: dict) -> str:
        text = ""
        for item in res.get("content", []):
            if item.get("type") == "text":
                text += item.get("text", "")
        return text


def prepare_temporary_chat(
    client: PlaywrightMcpClient,
    client_factory: Callable[[], PlaywrightMcpClient] | None = None,
    retries: int = 2,
) -> PlaywrightMcpClient:
    """Preflight MCP and open Temporary Chat before any prompt is typed.

    A reconnect is safe here because the prompt has not been sent. Once this
    function returns, callers must not retry the browser workflow.
    """
    last_error: Exception | None = None
    for attempt in range(retries + 1):
        try:
            tabs = client.call_tool("browser_tabs", {"action": "list"}, timeout=15.0)
            tabs_text = client.get_tool_text(tabs)
            if "chrome-extension://" not in tabs_text or "Welcome" not in tabs_text:
                raise RuntimeError("no OpenLia-owned extension Welcome tab is available")
            # Ownership is established by the extension Welcome marker before
            # navigation, so a navigation timeout can still clean up safely.
            client._openlia_tab_owned = True
            client.call_tool(
                "browser_navigate",
                {"url": "https://chatgpt.com/?temporary-chat=true"},
                timeout=30.0,
            )
            return client
        except Exception as error:
            last_error = error
            if getattr(client, "_openlia_tab_owned", False):
                close_current_tab(client)
            if client_factory is None or attempt >= retries:
                break
            client.close()
            time.sleep(1.0)
            client = client_factory()
    raise RuntimeError(
        "Browser MCP transport was unavailable before ChatGPT prompt submission; "
        "no prompt was sent"
    ) from last_error


def extract_eval_json(raw_text: str) -> Any:
    """Extract and parse JSON returned from browser_evaluate tool output."""
    cleaned = raw_text.strip()
    if "### Result" in cleaned:
        part = cleaned.split("### Result", 1)[1]
        if "### Ran Playwright" in part:
            part = part.split("### Ran Playwright", 1)[0]
        cleaned = part.strip()
    try:
        val = json.loads(cleaned)
        if isinstance(val, str):
            val = json.loads(val)
        return val
    except Exception:
        match = re.search(r"(\[.*\]|\{.*\})", cleaned, re.DOTALL)
        if match:
            val = json.loads(match.group(1))
            if isinstance(val, str):
                val = json.loads(val)
            return val
        raise ValueError(f"Could not parse evaluate result as JSON: {raw_text[:200]}")


COMPLETION_STATE_JS = """() => {
  const assistants = Array.from(document.querySelectorAll("[data-message-author-role='assistant']"));
  const lastAssistant = assistants.length ? assistants[assistants.length - 1] : null;
  const sendButton = document.querySelector("button[data-testid='send-button'], button[aria-label*='Send']");
  const stopButton = document.querySelector(
    "button[data-testid='stop-button'], button[aria-label*='Stop'], [aria-label*='Stop generating']"
  );
  return JSON.stringify({
    assistant_count: assistants.length,
    last_assistant_text: lastAssistant ? (lastAssistant.innerText || '').trim() : '',
    send_present: Boolean(sendButton),
    send_disabled: Boolean(sendButton && (sendButton.disabled || sendButton.getAttribute('aria-disabled') === 'true')),
    stop_visible: Boolean(stopButton)
  });
}"""


def read_completion_state(client: PlaywrightMcpClient) -> dict[str, Any]:
    """Read DOM state used to distinguish a finished response from a partial stream."""
    result = client.call_tool("browser_evaluate", {"function": COMPLETION_STATE_JS})
    raw_text = client.get_tool_text(result)
    state = extract_eval_json(raw_text)
    if not isinstance(state, dict):
        raise RuntimeError("ChatGPT completion state was not a JSON object")
    return state


def completion_ready(state: dict[str, Any], initial_assistant_count: int) -> bool:
    """Return true only when a new, non-empty assistant response is present."""
    try:
        assistant_count = int(state.get("assistant_count", 0))
    except (TypeError, ValueError):
        return False
    return (
        assistant_count > initial_assistant_count
        and bool(str(state.get("last_assistant_text", "")).strip())
        and bool(state.get("send_present"))
        and not bool(state.get("stop_visible"))
    )


def wait_for_completion(
    client: PlaywrightMcpClient,
    initial_assistant_count: int,
    timeout: float,
) -> dict[str, Any]:
    """Wait for a new assistant response to become stable across multiple polls."""
    start_time = time.time()
    previous_text = ""
    stable_polls = 0
    while time.time() - start_time < timeout:
        state = read_completion_state(client)
        text = str(state.get("last_assistant_text", "")).strip()
        if completion_ready(state, initial_assistant_count):
            if text == previous_text:
                stable_polls += 1
            else:
                stable_polls = 0
            previous_text = text
            if stable_polls >= 2:
                return state
        else:
            previous_text = ""
            stable_polls = 0
        time.sleep(1.5)
    raise TimeoutError(f"ChatGPT generation did not reach a stable completed response within {timeout}s")


def validate_complete_turns(turns: Any) -> None:
    """Reject transcripts that contain only a prompt or a partial assistant turn."""
    if not isinstance(turns, list) or not turns:
        raise RuntimeError("No turns extracted from ChatGPT conversation.")
    assistant_turns = [turn for turn in turns if isinstance(turn, dict) and turn.get("role") == "assistant"]
    if not assistant_turns or not str(assistant_turns[-1].get("content", "")).strip():
        raise RuntimeError("ChatGPT response was incomplete; no non-empty assistant turn was extracted.")
    if not isinstance(turns[-1], dict) or turns[-1].get("role") != "assistant":
        raise RuntimeError("ChatGPT conversation did not end with an assistant response.")


EXTRACTION_JS = """async () => {
  const turns = [];
  const clipboard = navigator.clipboard;
  const orig = clipboard && clipboard.writeText;
  let lastCopied = null;
  if (clipboard && orig) clipboard.writeText = function(text) {
    lastCopied = text;
    return Promise.resolve();
  };

  const messageBlocks = document.querySelectorAll("[data-message-author-role]");
  for (const [idx, block] of messageBlocks.entries()) {
    const role = block.getAttribute("data-message-author-role");
    if (role === "user") {
      turns.push({
        role: "user",
        turn: Math.floor(idx / 2) + 1,
        content: block.innerText.trim(),
        references: []
      });
    } else {
      const container = block.closest("[data-testid^='conversation-turn-']") || block.parentElement;
      const copyBtn = container ? container.querySelector("button[data-testid='copy-turn-action-button'], button[aria-label*='Copy']") : null;
      lastCopied = null;
      if (copyBtn) {
        copyBtn.click();
        await new Promise(resolve => setTimeout(resolve, 250));
      }
      const markdownContent = lastCopied || block.innerText.trim();

      const links = Array.from(block.querySelectorAll("a[href]")).map(a => ({
        title: (a.innerText || a.getAttribute("aria-label") || "").trim(),
        url: a.href
      })).filter(l => l.url.startsWith("http") && !l.url.includes("chatgpt.com/c/"));

      turns.push({
        role: "assistant",
        turn: Math.floor(idx / 2) + 1,
        content: markdownContent,
        references: links
      });
    }
  }

  if (clipboard && orig) clipboard.writeText = orig;
  return JSON.stringify(turns);
}"""


def execute_chatgpt_chat(
    prompt: str,
    topic: str = "",
    output_path: str = "",
    mcp_url: str = "",
    timeout: float = 180.0,
    client: PlaywrightMcpClient | None = None,
    client_factory: Callable[[], PlaywrightMcpClient] | None = None,
) -> str:
    """Execute ChatGPT chat query via Playwright MCP and output standardized YAML."""
    prune_playwright_mcp_logs(".playwright-mcp", max_files=20, max_age_hours=24)
    target_mcp_url = mcp_url or os.getenv("OPENLIA_BROWSER_MCP_URL", "http://localhost:8931")
    client = client or PlaywrightMcpClient(target_mcp_url)
    tab_owned = False
    try:
        client = prepare_temporary_chat(client, client_factory=client_factory)
        tab_owned = bool(getattr(client, "_openlia_tab_owned", False))
        enforce_tab_cap(client, max_tabs=2)

        # The temporary tab was opened by prepare_temporary_chat before any prompt
        # was submitted.
        time.sleep(3)

        # Gatekeeper Check
        snap = client.call_tool("browser_snapshot", {})
        snap_text = client.get_tool_text(snap)

        if not detect_temporary_chat_state(snap_text):
            # Attempt to click model selector / temporary switch if visible
            client.call_tool("browser_click", {"target": 'button[data-testid="model-selector-dropdown"], button[aria-label*="Model"]'})
            time.sleep(1)
            client.call_tool("browser_click", {"target": 'button[role="switch"], div:has-text("Temporary chat")'})
            time.sleep(2)
            snap = client.call_tool("browser_snapshot", {})
            snap_text = client.get_tool_text(snap)
            if not detect_temporary_chat_state(snap_text):
                raise RuntimeError("Zero-Tolerance Gatekeeper: ChatGPT Temporary Chat mode could not be verified. Aborting query.")

        # Capture the existing conversation length before submitting this prompt.
        initial_state = read_completion_state(client)
        initial_assistant_count = int(initial_state.get("assistant_count", 0))

        # Step 3: Type and submit prompt
        client.call_tool("browser_type", {
            "target": '#prompt-textarea, div[contenteditable="true"]',
            "text": prompt,
        })
        time.sleep(0.5)
        try:
            client.call_tool("browser_click", {
                "target": 'button[data-testid="send-button"], button[aria-label*="Send"]',
            })
        except Exception:
            client.call_tool("browser_press_key", {"key": "Enter"})

        # Step 4: Wait for a new assistant response to finish and stabilize.
        wait_for_completion(client, initial_assistant_count, timeout)

        # Step 5: Extract turns via clipboard interception
        eval_res = client.call_tool("browser_evaluate", {"function": EXTRACTION_JS})
        raw_eval_text = client.get_tool_text(eval_res)
        turns = extract_eval_json(raw_eval_text)

        validate_complete_turns(turns)

        # Step 6: Assemble YAML
        topic_str = topic or prompt[:40]
        meta = {
            "platform": "chatgpt",
            "topic": topic_str,
            "model": "ChatGPT",
            "url": "https://chatgpt.com/?temporary-chat=true",
        }
        yaml_data = build_conversation_yaml(meta, turns)

        # Step 7: Save output
        if not output_path:
            filename = generate_timestamped_filename(topic_str)
            workspace_dir = os.path.join(resolve_workspace_root(), "knowledge", "chatgpt")
            os.makedirs(workspace_dir, exist_ok=True)
            output_path = os.path.join(workspace_dir, filename)
        else:
            os.makedirs(os.path.dirname(os.path.abspath(output_path)), exist_ok=True)

        with open(output_path, "w", encoding="utf-8") as f:
            f.write(yaml_data)

        return output_path
    finally:
        if tab_owned:
            close_current_tab(client)


def live_verify(mcp_url: str = "") -> None:
    """Developer live verification in isolated temporary directory."""
    verify_dir = "/tmp/openlia_verify/chatgpt"
    os.makedirs(verify_dir, exist_ok=True)
    out_file = os.path.join(verify_dir, generate_timestamped_filename("Verify Canary"))

    print(f"Running ChatGPT live verification (isolated output -> {out_file})...")
    test_prompt = "Reply with exactly: LIA_VERIFY_OK"
    try:
        saved_path = execute_chatgpt_chat(
            prompt=test_prompt,
            topic="Verify Canary",
            output_path=out_file,
            mcp_url=mcp_url,
            timeout=60,
        )
        with open(saved_path, "r", encoding="utf-8") as f:
            content = f.read()
        parsed = validate_conversation_yaml(content)
        assert len(parsed["messages"]) >= 2, "Expected at least 2 messages (user + assistant)"
        print(f"chatgpt-chat: live verification PASSED (turns={len(parsed['messages'])}, file={saved_path})")
    except Exception as e:
        print(f"chatgpt-chat: live verification FAILED: {e}", file=sys.stderr)
        sys.exit(1)


def self_test() -> None:
    """Deterministic validation test."""
    raw_u = "https://go.dev/ref/spec?utm_source=chatgpt.com&utm_medium=referral&tab=1"
    clean_u = clean_url(raw_u)
    assert clean_u == "https://go.dev/ref/spec?tab=1", f"Unexpected cleaned URL: {clean_u}"

    assert clean_title("Rust Documentation\n+1", "https://doc.rust-lang.org/book/ch16-04.html") == "The Rust Book: Ch16 04"
    assert clean_title("Go\n+2\nGo\n+2", "https://go.dev/ref/spec") == "Go Language Specification"
    assert clean_title("Crossbeam Benchmark Suite", "https://github.com/crossbeam-rs") == "Crossbeam Benchmark Suite"

    domain, site = infer_site_metadata("https://doc.rust-lang.org/std/sync/mpsc/index.html")
    assert domain == "doc.rust-lang.org"
    assert site == "Rust Standard Library"

    domain2, site2 = infer_site_metadata("https://docs.rs/tokio/latest/tokio/sync/mpsc/index.html")
    assert domain2 == "docs.rs"
    assert site2 == "Tokio Docs"

    test_dt = datetime.datetime(2026, 9, 20, 23, 15, 0, tzinfo=datetime.timezone.utc)
    fname = generate_timestamped_filename("Compare Go & Rust Channels!", test_dt)
    assert fname == "20260920_231500_compare_go_rust_channels.yaml"

    meta = {
        "topic": "Go vs Rust Concurrency",
        "model": "GPT-4o",
        "url": "https://chatgpt.com/c/example?utm_source=chatgpt.com",
    }
    messages = [
        {"role": "user", "turn": 1, "content": "Explain concurrency."},
        {
            "role": "assistant",
            "turn": 1,
            "content": "# Concurrency Comparison\n\n```go\nch := make(chan int)\n```",
            "references": [
                {"title": "Go\n+2", "url": "https://go.dev/ref/mem?utm_source=chatgpt.com"},
                {"title": "Rust Documentation\n+1", "url": "https://doc.rust-lang.org/book/ch16-04.html?utm_source=chatgpt.com"},
            ],
        },
    ]

    yaml_output = build_conversation_yaml(meta, messages)
    assert "schema: 1" in yaml_output
    assert "mode: temporary" in yaml_output
    assert "utm_source" not in yaml_output
    assert "&id" not in yaml_output
    assert "*id" not in yaml_output
    assert "domain: go.dev" in yaml_output
    assert "site_name: Go Memory Model" in yaml_output
    assert "turns:" in yaml_output

    parsed = validate_conversation_yaml(yaml_output)
    assert parsed["session"]["topic"] == "Go vs Rust Concurrency"
    assert len(parsed["messages"]) == 2
    assert len(parsed["references"]) == 2
    assert parsed["references"][0]["id"] == "ref-1"
    assert parsed["references"][0]["turns"] == [1]

    assert completion_ready(
        {
            "assistant_count": 1,
            "last_assistant_text": "complete response",
            "send_present": True,
            "send_disabled": False,
            "stop_visible": False,
        },
        0,
    )
    assert not completion_ready(
        {
            "assistant_count": 1,
            "last_assistant_text": "still streaming",
            "send_present": True,
            "send_disabled": False,
            "stop_visible": True,
        },
        0,
    )
    try:
        validate_complete_turns([{"role": "user", "content": "prompt"}])
    except RuntimeError:
        pass
    else:
        raise AssertionError("incomplete ChatGPT turns were accepted")

    assert resolve_workspace_root("/opt/data/workspace", "/opt/data") == "/opt/data/workspace"
    assert resolve_workspace_root("/repo/workspace", "") == "/repo/workspace"
    assert resolve_workspace_root("/repo", "") == "/repo/workspace"
    assert resolve_workspace_root("/ignored", "", "/custom/workspace") == "/custom/workspace"


def main() -> None:
    parser = argparse.ArgumentParser(description="ChatGPT Conversation automation & YAML helper")
    parser.add_argument("--self-test", action="store_true", help="Run deterministic offline self-test")
    parser.add_argument("--verify", action="store_true", help="Run developer live verification in /tmp/openlia_verify/")
    parser.add_argument("--prompt", "-p", type=str, help="Prompt text to submit to ChatGPT")
    parser.add_argument("--topic", "-t", type=str, default="", help="Conversation topic for metadata and filename")
    parser.add_argument("--output", "-o", type=str, default="", help="Output YAML file path")
    parser.add_argument("--mcp-url", type=str, default="", help="Playwright MCP server base URL")
    parser.add_argument("--timeout", type=float, default=180.0, help="Response completion timeout in seconds")

    # Legacy compatibility helpers
    parser.add_argument("--validate", type=str, help="Path to YAML file to validate")
    parser.add_argument("--build", nargs=2, metavar=("INPUT_JSON", "OUTPUT_YAML"), help="Build YAML from JSON turns")

    args = parser.parse_args()

    if args.self_test:
        self_test()
        print("ok")
        sys.exit(0)

    if args.verify:
        live_verify(args.mcp_url)
        sys.exit(0)

    if args.validate:
        with open(args.validate, "r", encoding="utf-8") as f:
            content = f.read()
        validate_conversation_yaml(content)
        print("valid")
        sys.exit(0)

    if args.build:
        in_path, out_path = args.build
        with open(in_path, "r", encoding="utf-8") as f:
            data = json.load(f)

        if isinstance(data, list):
            meta = {"topic": args.topic, "url": args.mcp_url}
            messages = data
        else:
            meta = data.get("session", {})
            if args.topic != "ChatGPT Conversation":
                meta["topic"] = args.topic
            messages = data.get("messages", [])

        yaml_out = build_conversation_yaml(meta, messages)
        with open(out_path, "w", encoding="utf-8") as f:
            f.write(yaml_out)
        print(f"saved: {out_path}")
        sys.exit(0)

    if args.prompt:
        out = execute_chatgpt_chat(
            prompt=args.prompt,
            topic=args.topic,
            output_path=args.output,
            mcp_url=args.mcp_url,
            timeout=args.timeout,
        )
        print(f"saved: {out}")
        sys.exit(0)

    parser.print_help()
    sys.exit(1)


if __name__ == "__main__":
    main()
