#!/usr/bin/env python3
"""Deterministic helper, YAML serializer, and Playwright MCP automation for the gemini-chat skill."""

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
from typing import Any

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
    "sa",
    "ved",
    "usg",
}


def clean_url(url: str) -> str:
    """Clean and canonicalize URLs, unwrapping Google redirect links and removing tracking queries."""
    if not url or not isinstance(url, str):
        return ""

    target_url = url.strip()

    # Step 1: Unwrap Google redirect URLs (e.g. https://www.google.com/url?q=<dest>&...)
    parsed = urllib.parse.urlparse(target_url)
    hostname = (parsed.hostname or "").lower()
    if "google." in hostname and parsed.path.startswith("/url"):
        qs = urllib.parse.parse_qs(parsed.query)
        q_dest = qs.get("q", []) or qs.get("url", [])
        if q_dest and q_dest[0].startswith("http"):
            target_url = q_dest[0]
            parsed = urllib.parse.urlparse(target_url)

    # Step 2: Strip tracking parameters
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
    if "arxiv.org" in hostname:
        return (hostname, "arXiv")

    clean_domain = hostname.removeprefix("www.")
    site_name = clean_domain.split(".")[0].capitalize() if clean_domain else "Web"
    return (hostname, site_name)


def clean_title(raw_title: str, url: str) -> str:
    """Clean noisy badge strings and derive clean titles."""
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

    if not cleaned or cleaned.lower() in ("gemini", "google", "web", "source", "link"):
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
    return text.strip("_") or "gemini_chat"


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


def detect_temporary_chat_state(page_text: str) -> bool:
    """Detect presence of Temporary Chat indicator markers in page text or snapshot.
    
    Note: Do NOT match bare 'temporary chat' because that matches the sidebar
    button '<button aria-label="Temporary chat">' before temporary mode is activated.
    """
    if not page_text or not isinstance(page_text, str):
        return False
    lowered = page_text.lower()
    indicators = (
        "chats in this window won't appear in recent chats",
        "chats in this window won't appear in your gemini apps activity",
        "chats won't appear in recent",
        "just stopping by",
        "gemini apps activity is off",
        "cuộc trò chuyện tạm thời",
    )
    return any(ind in lowered for ind in indicators)


def build_conversation_yaml(
    session_meta: dict[str, Any],
    messages: list[dict[str, Any]],
) -> str:
    """Build standardized YAML representation of Gemini conversation with frontend-ready references."""
    if not isinstance(session_meta, dict):
        raise ValueError("session_meta must be a dict")
    if not isinstance(messages, list):
        raise ValueError("messages must be a list of dicts")

    now = datetime.datetime.now(datetime.timezone.utc).isoformat()
    meta = {
        "platform": session_meta.get("platform", "gemini"),
        "mode": "temporary",
        "started_at": session_meta.get("started_at", now),
        "model": session_meta.get("model", "Gemini"),
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
        if role not in ("user", "assistant", "model", "system"):
            raise ValueError(f"Invalid message role '{role}' at index {idx}")
        normalized_role = "assistant" if role == "model" else role
        content = str(msg.get("content", "")).strip()

        msg_data: dict[str, Any] = {
            "role": normalized_role,
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
    if session.get("platform") != "gemini":
        raise ValueError("Session platform must be 'gemini'")
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
    validate_turn_sequence(messages)
    return data


def validate_turn_sequence(turns: Any) -> None:
    """Reject duplicated UI nodes and incomplete conversations."""
    if not isinstance(turns, list) or len(turns) < 2:
        raise ValueError("Conversation must contain a user and assistant message")
    previous_role = ""
    for index, turn in enumerate(turns, start=1):
        if not isinstance(turn, dict):
            raise ValueError(f"Turn {index} must be a mapping")
        role = str(turn.get("role", "")).lower()
        if role == "model":
            role = "assistant"
        if role not in ("user", "assistant"):
            raise ValueError(f"Turn {index} has unsupported role {role!r}")
        if not str(turn.get("content", "")).strip():
            raise ValueError(f"Turn {index} has empty content")
        if role == "assistant" and index > 1 and " ".join(str(turns[index - 2].get("content", "")).split()) == " ".join(str(turn.get("content", "")).split()):
            raise ValueError(f"Turn {index} duplicates the preceding user prompt")
        if role == previous_role:
            raise ValueError(f"Turn {index} repeats role {role!r}; transcript is not alternating")
        previous_role = role
    if str(turns[0].get("role", "")).lower() != "user":
        raise ValueError("Conversation must start with a user message")
    if str(turns[-1].get("role", "")).lower() not in ("assistant", "model"):
        raise ValueError("Conversation must end with an assistant response")


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
            "clientInfo": {"name": "gemini-chat-script", "version": "1.0"},
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

    def call_tool(self, name: str, arguments: dict, timeout: float = 90.0) -> dict:
        return self.call("tools/call", {"name": name, "arguments": arguments}, timeout=timeout)

    def get_tool_text(self, res: dict) -> str:
        text = ""
        for item in res.get("content", []):
            if item.get("type") == "text":
                text += item.get("text", "")
        return text


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
  const expectedPrompt = __OPENLIA_EXPECTED_PROMPT__;
  const normalize = value => (value || '').replace(/\\s+/g, ' ').trim();
  const cleanUser = value => normalize(value).replace(/^You said\\s*/i, '').trim();
  const userSelector = "user-query, .user-query-container, div[data-test-id='user-query']";
  const userElements = Array.from(new Set(Array.from(document.querySelectorAll(userSelector))))
    .filter((element, index, all) => !all.some((other, otherIndex) => otherIndex !== index && other.contains(element)));
  const modelCandidates = Array.from(new Set(Array.from(document.querySelectorAll("model-response, div[data-test-id='model-response']"))));
  let modelElements = modelCandidates.filter((element, index, all) => !all.some((other, otherIndex) => otherIndex !== index && other.contains(element)));
  const expectedText = normalize(expectedPrompt);
  const userTexts = new Set(userElements.map(element => cleanUser(element.innerText)));
  modelElements = modelElements.filter(element => {
    const text = normalize(element.innerText);
    return text !== expectedText && !userTexts.has(text);
  });
  if (!modelElements.length) {
    modelElements = Array.from(document.querySelectorAll("message-content"))
      .filter(element => !element.closest(userSelector))
      .filter(element => {
        const text = normalize(element.innerText);
        return text !== expectedText && !userTexts.has(text);
      });
  }
  const lastModel = modelElements.length ? modelElements[modelElements.length - 1] : null;
  const stopButton = document.querySelector("button[aria-label*='Stop'], [aria-label*='Stop response']");
  const sendButton = document.querySelector("button[aria-label*='Send'], button[data-test-id='send-button']");
  return JSON.stringify({
    user_count: userElements.length,
    model_count: modelElements.length,
    last_model_text: lastModel ? (lastModel.innerText || '').trim() : '',
    stop_visible: Boolean(stopButton),
    send_present: Boolean(sendButton)
  });
}"""


def browser_script(template: str, prompt: str) -> str:
    return template.replace("__OPENLIA_EXPECTED_PROMPT__", json.dumps(prompt))


def read_completion_state(client: PlaywrightMcpClient, prompt: str) -> dict[str, Any]:
    result = client.call_tool("browser_evaluate", {"function": browser_script(COMPLETION_STATE_JS, prompt)})
    state = extract_eval_json(client.get_tool_text(result))
    if not isinstance(state, dict):
        raise RuntimeError("Gemini completion state was not a JSON object")
    return state


def completion_ready(state: dict[str, Any], initial_model_count: int) -> bool:
    try:
        model_count = int(state.get("model_count", 0))
    except (TypeError, ValueError):
        return False
    return (
        model_count > initial_model_count
        and bool(str(state.get("last_model_text", "")).strip())
        and not bool(state.get("stop_visible"))
    )


def wait_for_completion(client: PlaywrightMcpClient, prompt: str, initial_model_count: int, timeout: float) -> None:
    start_time = time.time()
    previous_text = ""
    stable_polls = 0
    while time.time() - start_time < timeout:
        state = read_completion_state(client, prompt)
        text = str(state.get("last_model_text", "")).strip()
        if completion_ready(state, initial_model_count):
            if text == previous_text:
                stable_polls += 1
            else:
                stable_polls = 0
            previous_text = text
            if stable_polls >= 2:
                return
        else:
            previous_text = ""
            stable_polls = 0
        time.sleep(1.5)
    raise TimeoutError(f"Gemini generation did not reach a stable completed response within {timeout}s")


EXTRACTION_JS = """async () => {
  const turns = [];
  const expectedPrompt = __OPENLIA_EXPECTED_PROMPT__;
  const normalize = value => (value || '').replace(/\\s+/g, ' ').trim();
  const cleanUser = value => normalize(value).replace(/^You said\\s*/i, '').trim();
  const userSelector = "user-query, .user-query-container, div[data-test-id='user-query']";
  const userElements = Array.from(new Set(Array.from(document.querySelectorAll(userSelector))))
    .filter((element, index, all) => !all.some((other, otherIndex) => otherIndex !== index && other.contains(element)));
  const modelCandidates = Array.from(new Set(Array.from(document.querySelectorAll("model-response, div[data-test-id='model-response']"))));
  let modelElements = modelCandidates.filter((element, index, all) => !all.some((other, otherIndex) => otherIndex !== index && other.contains(element)));
  const expectedText = normalize(expectedPrompt);
  const userTexts = new Set(userElements.map(element => cleanUser(element.innerText)));
  modelElements = modelElements.filter(element => {
    const text = normalize(element.innerText);
    return text !== expectedText && !userTexts.has(text);
  });
  if (!modelElements.length) {
    modelElements = Array.from(document.querySelectorAll("message-content"))
      .filter(element => !element.closest(userSelector))
      .filter(element => {
        const text = normalize(element.innerText);
        return text !== expectedText && !userTexts.has(text);
      });
  }

  const entries = [
    ...userElements.map(node => ({role: "user", node})),
    ...modelElements.map(node => ({role: "assistant", node}))
  ].sort((left, right) => {
    if (left.node === right.node) return 0;
    return left.node.compareDocumentPosition(right.node) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1;
  });

  const clipboard = navigator.clipboard;
  const orig = clipboard && clipboard.writeText;
  let lastCopied = null;
  if (clipboard && orig) clipboard.writeText = function(text) {
    lastCopied = text;
    return Promise.resolve();
  };

  for (const entry of entries) {
    const block = entry.node;
    if (entry.role === "user") {
      let userText = cleanUser(block.innerText);
      if (expectedText && userText === expectedText + ' ' + expectedText) userText = expectedPrompt;
      turns.push({role: "user", content: userText, references: []});
      continue;
    }
    const container = block.closest(".conversation-turn") || block.parentElement;
    const copyBtn = container ? container.querySelector("button[aria-label*='Copy response'], button[aria-label='Copy'], button[data-test-id='copy-button']") : null;
    lastCopied = null;
    if (copyBtn) {
      copyBtn.click();
      await new Promise(resolve => setTimeout(resolve, 250));
    }
    let markdown = (lastCopied || block.innerText || '').replace(/^Gemini said\\s*/i, '').trim();
    if (normalize(markdown) === expectedText || userTexts.has(normalize(markdown))) {
      markdown = (block.innerText || '').replace(/^Gemini said\\s*/i, '').trim();
    }
    const links = Array.from(block.querySelectorAll("a[href]")).map(a => ({
      title: (a.innerText || a.getAttribute("aria-label") || "").trim(),
      url: a.href
    })).filter(l => l.url.startsWith("http") && !l.url.includes("gemini.google.com/app"));
    turns.push({role: "assistant", content: markdown, references: links});
  }

  if (!turns.some(turn => turn.role === "user")) {
    turns.unshift({role: "user", content: expectedPrompt, references: []});
  }
  if (clipboard && orig) clipboard.writeText = orig;
  const deduplicated = turns.filter((turn, index, all) => {
    const previous = all[index - 1];
    return !previous || previous.role !== turn.role || previous.content !== turn.content;
  });
  deduplicated.forEach((turn, index) => { turn.turn = Math.floor(index / 2) + 1; });
  return JSON.stringify(deduplicated);
}"""


def execute_gemini_chat(
    prompt: str,
    topic: str = "",
    output_path: str = "",
    mcp_url: str = "",
    continue_session: bool = False,
    keep_tab: bool = False,
    timeout: float = 180.0,
    client: PlaywrightMcpClient | None = None,
) -> str:
    """Execute Gemini chat query via Playwright MCP and output standardized YAML."""
    prune_playwright_mcp_logs(".playwright-mcp", max_files=20, max_age_hours=24)
    target_mcp_url = mcp_url or os.getenv("OPENLIA_BROWSER_MCP_URL", "http://localhost:8931")
    client = client or PlaywrightMcpClient(target_mcp_url)
    enforce_tab_cap(client, max_tabs=2)

    if not continue_session:
        # Step 1: Reuse the connected tab for the persistent worker. Standalone
        # runs create a fresh tab so they do not disturb an existing session.
        if os.getenv("OPENLIA_BROWSER_REUSE_TAB") == "1":
            client.call_tool("browser_navigate", {"url": "https://gemini.google.com/app"})
        else:
            client.call_tool("browser_tabs", {"action": "new", "url": "https://gemini.google.com/app"})
        time.sleep(3)

        # Step 2: Gatekeeper Check
        snap = client.call_tool("browser_snapshot", {})
        snap_text = client.get_tool_text(snap)

        if not detect_temporary_chat_state(snap_text):
            # Click sidebar Temporary Chat toggle
            client.call_tool("browser_click", {"target": 'button[aria-label="Temporary chat"]'})
            confirmed = False
            for _ in range(6):
                time.sleep(1)
                snap = client.call_tool("browser_snapshot", {})
                snap_text = client.get_tool_text(snap)
                if detect_temporary_chat_state(snap_text):
                    confirmed = True
                    break
            if not confirmed:
                raise RuntimeError("Zero-Tolerance Gatekeeper: Gemini Temporary Chat mode could not be verified. Aborting query.")

    # Capture the existing response count before submitting this prompt.
    initial_state = read_completion_state(client, prompt)
    initial_model_count = int(initial_state.get("model_count", 0))

    # Step 3: Type and submit prompt
    client.call_tool("browser_type", {
        "target": 'div.ql-editor[contenteditable="true"]',
        "text": prompt,
    })
    time.sleep(0.5)
    try:
        client.call_tool("browser_click", {"target": 'button[aria-label="Send message"]'})
    except Exception:
        client.call_tool("browser_press_key", {"key": "Enter"})

    # Step 4: Wait for a new model response to finish and stabilize.
    wait_for_completion(client, prompt, initial_model_count, timeout)

    # Step 5: Extract turns via clipboard interception
    eval_res = client.call_tool("browser_evaluate", {"function": browser_script(EXTRACTION_JS, prompt)})
    raw_eval_text = client.get_tool_text(eval_res)
    turns = extract_eval_json(raw_eval_text)

    validate_turn_sequence(turns)

    # Step 5b: Verify URL did not branch into persistent conversation
    url_eval = client.call_tool("browser_evaluate", {"function": "() => window.location.href"})
    curr_url_raw = client.get_tool_text(url_eval)
    try:
        curr_url = extract_eval_json(curr_url_raw)
    except Exception:
        curr_url = curr_url_raw
    if isinstance(curr_url, str) and re.search(r"/app/[a-zA-Z0-9_-]{10,}", curr_url):
        raise RuntimeError(f"Gatekeeper Leak Detected: Gemini persisted chat into URL: {curr_url}")

    # Step 6: Assemble YAML
    topic_str = topic or prompt[:40]
    meta = {
        "platform": "gemini",
        "topic": topic_str,
        "model": "Gemini",
        "url": "https://gemini.google.com/app",
    }
    yaml_data = build_conversation_yaml(meta, turns)

    # Step 7: Save output
    if not output_path:
        filename = generate_timestamped_filename(topic_str)
        workspace_dir = os.path.join(resolve_workspace_root(), "knowledge", "gemini")
        os.makedirs(workspace_dir, exist_ok=True)
        output_path = os.path.join(workspace_dir, filename)
    else:
        os.makedirs(os.path.dirname(os.path.abspath(output_path)), exist_ok=True)

    with open(output_path, "w", encoding="utf-8") as f:
        f.write(yaml_data)

    # Step 8: Session Teardown on success
    if not keep_tab:
        close_current_tab(client)

    return output_path


def live_verify(mcp_url: str = "") -> None:
    """Developer live verification in isolated temporary directory."""
    verify_dir = "/tmp/openlia_verify/gemini"
    os.makedirs(verify_dir, exist_ok=True)
    out_file = os.path.join(verify_dir, generate_timestamped_filename("Verify Canary"))

    print(f"Running Gemini live verification (isolated output -> {out_file})...")
    test_prompt = "Reply with exactly: LIA_VERIFY_OK"
    try:
        saved_path = execute_gemini_chat(
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
        print(f"gemini-chat: live verification PASSED (turns={len(parsed['messages'])}, file={saved_path})")
    except Exception as e:
        print(f"gemini-chat: live verification FAILED: {e}", file=sys.stderr)
        sys.exit(1)


def self_test() -> None:
    """Deterministic validation test."""
    sample_text = "Chats in this window won't appear in Recent Chats or Gemini Apps Activity."
    assert detect_temporary_chat_state(sample_text) is True
    assert detect_temporary_chat_state("Regular chat history active") is False
    # Crucial assertion: sidebar button label alone must NOT trigger detection
    assert detect_temporary_chat_state('button "Temporary chat" aria-label="Temporary chat"') is False
    assert detect_temporary_chat_state("Just stopping by? Temporary chat") is True

    raw_google_redirect = "https://www.google.com/url?q=https://go.dev/ref/spec%3Futm_source%3Dchatgpt.com&sa=U&ved=2ahUKEwj"
    clean_u = clean_url(raw_google_redirect)
    assert clean_u == "https://go.dev/ref/spec", f"Unexpected cleaned redirect URL: {clean_u}"

    domain, site = infer_site_metadata("https://arxiv.org/abs/2401.0001")
    assert domain == "arxiv.org"
    assert site == "arXiv"
    assert clean_title("Google", "https://github.com/google/gemini-cookbook") == "GitHub: Gemini Cookbook"

    test_dt = datetime.datetime(2026, 9, 21, 0, 15, 0, tzinfo=datetime.timezone.utc)
    fname = generate_timestamped_filename("Gemini 1.5 Pro Architecture", test_dt)
    assert fname == "20260921_001500_gemini_15_pro_architecture.yaml"

    meta = {
        "platform": "gemini",
        "topic": "Gemini Reasoning Capabilities",
        "model": "Gemini 2.0 Flash Thinking",
        "url": "https://gemini.google.com/app",
    }
    messages = [
        {"role": "user", "turn": 1, "content": "Explain Gemini Thinking mode."},
        {
            "role": "model",
            "turn": 1,
            "content": "# Gemini Thinking\n\nGemini generates intermediate thought tokens.",
            "references": [
                {"title": "Gemini Overview", "url": "https://www.google.com/url?q=https://ai.google.dev/gemini-api/docs&sa=U"},
            ],
        },
    ]

    yaml_output = build_conversation_yaml(meta, messages)
    assert "schema: 1" in yaml_output
    assert "platform: gemini" in yaml_output
    assert "mode: temporary" in yaml_output
    assert "&id" not in yaml_output
    assert "*id" not in yaml_output
    assert "https://ai.google.dev/gemini-api/docs" in yaml_output
    assert "role: assistant" in yaml_output

    parsed = validate_conversation_yaml(yaml_output)
    assert parsed["session"]["platform"] == "gemini"
    assert parsed["session"]["mode"] == "temporary"
    assert len(parsed["messages"]) == 2
    assert len(parsed["references"]) == 1
    assert parsed["references"][0]["id"] == "ref-1"

    assert resolve_workspace_root("/opt/data/workspace", "/opt/data") == "/opt/data/workspace"
    assert resolve_workspace_root("/repo/workspace", "") == "/repo/workspace"
    assert resolve_workspace_root("/repo", "") == "/repo/workspace"
    assert resolve_workspace_root("/ignored", "", "/custom/workspace") == "/custom/workspace"

    assert completion_ready(
        {
            "model_count": 1,
            "last_model_text": "complete response",
            "send_present": True,
            "stop_visible": False,
        },
        0,
    )
    assert not completion_ready(
        {
            "model_count": 1,
            "last_model_text": "still generating",
            "send_present": True,
            "stop_visible": True,
        },
        0,
    )
    try:
        validate_turn_sequence([
            {"role": "user", "content": "prompt"},
            {"role": "assistant", "content": "answer"},
            {"role": "user", "content": "prompt"},
            {"role": "user", "content": "prompt"},
        ])
    except ValueError:
        pass
    else:
        raise AssertionError("malformed Gemini turn sequence was accepted")


def main() -> None:
    parser = argparse.ArgumentParser(description="Gemini Conversation automation & YAML helper")
    parser.add_argument("--self-test", action="store_true", help="Run deterministic offline self-test")
    parser.add_argument("--verify", action="store_true", help="Run developer live verification in /tmp/openlia_verify/")
    parser.add_argument("--prompt", "-p", type=str, help="Prompt text to submit to Gemini")
    parser.add_argument("--topic", "-t", type=str, default="", help="Conversation topic for metadata and filename")
    parser.add_argument("--output", "-o", type=str, default="", help="Output YAML file path")
    parser.add_argument("--mcp-url", type=str, default="", help="Playwright MCP server base URL")
    parser.add_argument("--continue", dest="continue_session", action="store_true", help="Continue in existing temporary chat tab")
    parser.add_argument("--keep-tab", action="store_true", help="Keep browser tab open after completion")
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
            meta = {"platform": "gemini", "topic": args.topic or "Gemini Conversation", "url": "https://gemini.google.com/app"}
            messages = data
        else:
            meta = data.get("session", {})
            meta["platform"] = "gemini"
            if args.topic:
                meta["topic"] = args.topic
            messages = data.get("messages", [])

        yaml_out = build_conversation_yaml(meta, messages)
        with open(out_path, "w", encoding="utf-8") as f:
            f.write(yaml_out)
        print(f"saved: {out_path}")
        sys.exit(0)

    if args.prompt:
        out = execute_gemini_chat(
            prompt=args.prompt,
            topic=args.topic,
            output_path=args.output,
            mcp_url=args.mcp_url,
            continue_session=args.continue_session,
            keep_tab=args.keep_tab,
            timeout=args.timeout,
        )
        print(f"saved: {out}")
        sys.exit(0)

    parser.print_help()
    sys.exit(1)


if __name__ == "__main__":
    main()
