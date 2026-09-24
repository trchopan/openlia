---
name: gemini-chat
description: MANDATORY isolated Temporary Chat automated research via Google Gemini web UI with ephemeral banner gatekeeper, authentic Markdown extraction, Google Search grounding, and YAML export.
version: 0.2.0
platforms: [linux, macos]
required_environment_variables: []
required_credential_files: []
metadata:
  hermes:
    tags: [personal-os, browser, gemini, ai-ui, google, research, grounding]
    category: automation
---

# Gemini Chat (Automated Script Execution)

## When to Use

Use this skill to conduct deep research, code review, large-context analysis (up to 1M+ tokens), and Google Search-grounded inquiries using the official **Google Gemini** web interface (`gemini.google.com`), saving each completed query and response into standardized OpenLia YAML knowledge files.

- **Google Search Grounding**: Leverage Gemini's real-time Google search integration for recent developments, documentation, and live web facts.
- **Large Context & Reasoning**: Offload long-document review or complex technical reasoning to Gemini 1.5 Pro / 2.0 Flash Thinking.
- **Durable Knowledge Ingestion**: Serialize verbatim dialogues into `workspace/knowledge/gemini/` matching the OpenLia frontend-ready schema with sanitized citations.

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
To query Gemini, run the bundled script via terminal:

```bash
bun /opt/data/skills/browser-pilot/scripts/openlia_job.ts submit gemini-chat \
  --prompt "Research WebAssembly garbage collection status in 2026. Use Google Search grounding." \
  --topic "Wasm GC 2026 Status"
```

The command returns a job ID immediately. Poll it with `status JOB_ID` and fetch
the completed YAML with `result JOB_ID`; do not hold one terminal invocation open
for the full remote-browser conversation.

#### What the Script Handles Automatically:
1. **MCP Connection**: Connects to the host-local browser-tools MCP proxy through the configured browser attachment.
2. **Ephemeral Navigation**: Navigates to `https://gemini.google.com/app`.
3. **Gatekeeper Verification**: Toggles the Temporary Chat button in the sidebar and verifies the ephemeral banner (*"Chats in this window won't appear in Recent Chats"*). Fails closed if unverified.
4. **Prompt Submission**: Inputs text into `div.ql-editor[contenteditable="true"]` and clicks the send button.
5. **Streaming Completion**: Monitors generation until the response completes and action buttons appear.
6. **Authentic Markdown & Grounding Extraction**: Intercepts `navigator.clipboard.writeText` and triggers Gemini's Copy response button to extract 100% authentic Markdown (tables, code blocks), unwraps Google redirect URLs (`https://www.google.com/url?q=...`), and derives clean citation titles.
7. **YAML Serialization**: Writes the conversation to `workspace/knowledge/gemini/YYYYMMDD_HHMMSS_<slug>.yaml`.
8. **Session Cleanup**: Resets the browser tab to `about:blank`.

---

## Developer Verification Workflow

Before deploying skill updates to a live agent, developers must verify skill health:

### 1. Offline Self-Test (Fast / CI)
```bash
bun profile/skills/browser-pilot/scripts/openlia_job.ts --self-test
```
Validates citation sanitization, redirect unwrapping, filename generation, and YAML schema compliance with zero external dependencies.

### 2. Live Browser Verification (Local Playwright MCP)
```bash
bun profile/skills/browser-pilot/scripts/openlia_job.ts --verify
```
Checks the private browser-job service and reports pass/fail without submitting a prompt or contaminating user workspace data. Run a real canary job separately when validating provider UI selectors.

---

## UI Selector Reference

| Element | Selector / Identifier | Purpose |
| :--- | :--- | :--- |
| Temporary Chat Button | `button[aria-label="Temporary chat"]` | Switches session to ephemeral mode |
| Ephemeral Banner | `div:has-text("Chats in this window won't appear in Recent Chats")` | Verifies zero-retention mode |
| Prompt Input | `div.ql-editor[contenteditable="true"]` | Text input area |
| Send Button | `button[aria-label="Send message"]` | Submits prompt |
| Stop Button | `button[aria-label="Stop response"]` | Indicates streaming in progress |
| Copy Button | `button[aria-label*="Copy"], button[data-test-id="copy-button"]` | Intercepted for authentic Markdown |
| Grounding Links | `a.source-chip, a[href*="google.com/url"]` | Citation links unwrapped by helper |
