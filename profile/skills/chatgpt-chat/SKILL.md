---
name: chatgpt-chat
description: MANDATORY isolated Temporary Chat (?temporary-chat=true) research via ChatGPT web UI with zero-tolerance gatekeeper, authentic Markdown extraction, and YAML export.
version: 0.2.0
platforms: [linux, macos]
required_environment_variables: []
required_credential_files: []
metadata:
  hermes:
    tags: [personal-os, browser, chatgpt, ai-ui, temporary-chat, research]
    category: automation
---

# ChatGPT Chat (Automated Script Execution)

## When to Use

Use this skill to leverage the ChatGPT web interface (including ChatGPT Search, Advanced Data Analysis, or thinking models) to research, draft, compare technical architectures, or offload complex inquiries, while guaranteeing user privacy through **Temporary Chat** mode and saving each completed query and response into structured YAML.

- **Deep Reasoning & Comparison**: Offload complex comparative analysis or code generation to specialized ChatGPT models.
- **Privacy-Preserving Research**: Enforce Temporary Chat mode so discussions are neither saved to user chat history nor used for model training.
- **Knowledge Base Ingestion**: Serialize the completed query and response into standard YAML containing rich Markdown for durable storage in `workspace/knowledge/chatgpt/`.

---

## Strict Safety & Gatekeeper Rules

> [!IMPORTANT]
> **Zero-Tolerance Temporary Chat Policy**:
> - **NEVER** post queries into a standard, persistent chat session.
> - The automated script explicitly verifies that Temporary Chat mode is active before typing or submitting any prompt.
> - If Temporary Chat cannot be confirmed, the script **HALTS IMMEDIATELY**, closes only the owned browser tab, and raises an error.

---

## Agent Operational Procedures

### Primary Execution: Dedicated Automation Script
To query ChatGPT, run the bundled script via terminal:

```bash
bun /opt/data/skills/browser-pilot/scripts/openlia_job.ts submit chatgpt-chat \
  --prompt "Explain the key differences between Go channels and Rust channels with code examples." \
  --topic "Go vs Rust Channels"
```

The command returns a job ID immediately. Poll it with `status JOB_ID` and fetch
the completed YAML with `result JOB_ID`; do not hold one terminal invocation open
for the full remote-browser conversation.

#### What the Script Handles Automatically:
1. **MCP Connection**: Connects to the host-local browser-tools MCP proxy through the configured browser attachment.
2. **Temporary Navigation**: Navigates to `https://chatgpt.com/?temporary-chat=true`.
3. **Gatekeeper Verification**: Verifies the "Temporary Chat" badge or "Chat history is turned off" text in the accessibility tree before typing. Fails closed if unverified.
4. **Prompt Submission**: Inputs text into `#prompt-textarea` and clicks the send button (or presses Enter).
5. **Streaming Completion**: Monitors generation until streaming finishes, the stop button disappears, and the send button is re-enabled.
6. **Authentic Markdown & Citation Extraction**: Intercepts `navigator.clipboard.writeText` and triggers ChatGPT's "Copy response" button to capture 100% authentic Markdown (tables, code blocks, headers), cleans tracking query parameters, and derives clean citation titles.
7. **YAML Serialization**: Writes the conversation to `workspace/knowledge/chatgpt/YYYYMMDD_HHMMSS_<slug>.yaml`.
8. **Session Cleanup**: Resets the browser tab to `about:blank`.

---

## Developer Verification Workflow

Before deploying skill updates to a live agent, developers must verify skill health:

### 1. Offline Self-Test (Fast / CI)
```bash
bun profile/skills/browser-pilot/scripts/openlia_job.ts --self-test
```
Validates URL cleaning, tracking parameter stripping, title cleaning, site metadata inference, and YAML schema compliance with zero external dependencies.

### 2. Live Browser Verification (Local Playwright MCP)
```bash
bun profile/skills/browser-pilot/scripts/openlia_job.ts --verify
```
Checks the private browser-job service and reports pass/fail without submitting a prompt or contaminating user workspace data. Run a real canary job separately when validating provider UI selectors.

---

## UI Selector Reference

| Element | Selector / Identifier | Purpose |
| :--- | :--- | :--- |
| Temporary Marker | `div:has-text("Temporary Chat")`, `[aria-label*="Temporary"]` | Verifies privacy mode |
| Prompt Input | `#prompt-textarea, div[contenteditable="true"]` | Text input area |
| Send Button | `button[data-testid="send-button"], button[aria-label*="Send"]` | Submits prompt |
| Stop Button | `button[data-testid="stop-button"], button[aria-label*="Stop"]` | Indicates streaming in progress |
| User Message | `[data-message-author-role="user"]` | User turn container |
| Assistant Message | `[data-message-author-role="assistant"]` | AI response container |
| Copy Button | `button[data-testid="copy-turn-action-button"], button[aria-label*="Copy"]` | Intercepted for authentic Markdown |
