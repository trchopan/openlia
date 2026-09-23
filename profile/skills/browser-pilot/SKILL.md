---
name: browser-pilot
description: Pilot a shared remote desktop browser for interactive web apps, map routing, AI chat UI delegation, and bot-protected e-commerce research.
version: 0.1.0
platforms: [linux, macos]
required_environment_variables: []
required_credential_files: []
metadata:
  hermes:
    tags: [personal-os, browser, playwright, web-apps, maps, ecommerce]
    category: automation
---

# Browser Pilot

## When to Use

Use when a task requires driving a live, headed browser session with existing user logins, dynamic Single-Page Application (SPA) interactions, or sites with bot protection:
- **Navigation & Maps**: Looking up routes, commute options, and live traffic on Google Maps.
- **AI UI Delegation**: Querying external chat interfaces (Gemini Advanced, ChatGPT) for deep reasoning or web research and capturing the synthesis.
- **E-Commerce Discovery**: Browsing platforms like Shopee, Lazada, or Amazon where user session cookies and cooperative bot resolution are required.

## Tool Selection

When an attached service is mapped to the `playwright-browser` role, Hermes'
native `browser_*` toolset is intentionally disabled and the attached Playwright
MCP server is registered directly with Hermes. Use the attached Playwright MCP
tools for Maps, shopping, and generic browser workflows. Use the queued OpenLia
browser-job client for the supported ChatGPT and Gemini workflows below.

The current MCP server name is `openlia-playwright`, so its tool names are
prefixed `mcp__openlia_playwright__`, for example:
`mcp__openlia_playwright__browser_navigate`,
`mcp__openlia_playwright__browser_snapshot`, and
`mcp__openlia_playwright__browser_click`. Do not invent terminal scripts such as
`maps_client.py`, and do not call the disabled native `browser_*` names.

When no `playwright-browser` role is configured, use the browser automation tools
to pilot the native browser session:
- `browser_navigate`: Navigate to a URL
- `browser_snapshot`: Read the current page text or accessibility tree
- `browser_click`: Click on links, buttons, or elements
- `browser_type` / `browser_fill_form`: Enter text into inputs or textareas
- `browser_tabs`: List, open, or switch browser tabs
- `browser_wait_for`: Wait for dynamic content, text, or elements to render
- `browser_take_screenshot`: Capture visual screenshots

## Safe Defaults & Approval Boundaries

| Action | Mode | Policy |
| :--- | :--- | :--- |
| Read DOM, take screenshot, extract text/prices | Automatic | Always permitted |
| Dismiss standard cookie/privacy banners | Automatic | Permitted |
| Type queries in search bars, Google Maps destination, or AI chat prompt | Automatic | Permitted |
| Anti-bot checkpoint (Cloudflare, Shopee slider, CAPTCHA, 2FA) | **User Handoff** | **Pause and prompt user to solve in their open browser** |
| Form submission with sensitive personal info or credentials | **Ask** | Never submit without explicit user confirmation |
| Add to cart, place order, payment, financial action | **Always ask** | Strict approval required; never checkout autonomously |

## Operational Procedures

### 1. Cooperative Anti-Bot Handshake (Shopee, Lazada, Cloudflare)
1. Navigate to the requested search/product page.
2. Inspect the page snapshot for challenge markers (e.g. slider puzzle, verification dialog, Cloudflare Turnstile, 403 blocks).
3. If blocked:
   - Do **not** attempt automated clicking on the puzzle pieces.
   - Alert the user: *"Verification challenge detected on [Domain]. Please complete the slider/CAPTCHA in your Vivaldi window, then reply to continue."*
   - Once the user confirms or after brief poll, re-inspect the page.
4. Scroll incrementally to trigger lazy-loaded product cards and reviews.
5. Extract titles, prices, voucher conditions, and seller ratings.

### 2. External AI UI Delegation (ChatGPT, Gemini)
> [!IMPORTANT]
> **Mandatory Script Execution & Zero-Tolerance Temporary Chat**:
> Never navigate to or type queries into `chatgpt.com` or `gemini.google.com` using low-level browser tool loops. Manual browser orchestration is brittle, slow, and risks leaking queries into persistent history.
  > - **ChatGPT Research**: Submit a `chatgpt-chat` job through the OpenLia browser-job client, then poll and fetch its result. The worker runs the dedicated script, enforces Temporary Chat, and captures authentic Markdown.
  > - **Gemini Research**: Submit a `gemini-chat` job through the OpenLia browser-job client, then poll and fetch its result. The worker runs the dedicated script, toggles ephemeral Temporary Chat, and formats YAML.
> - Both scripts output clean, standardized YAML transcripts with timestamp prefixes to `workspace/knowledge/<platform>/`.

### 3. Route & Traffic Intelligence (Google Maps)
1. Navigate to Google Maps with start and destination parameters.
2. Ensure transit/driving mode is correctly selected.
3. Extract route distance, travel duration, current traffic delay notices (orange/red congestion warnings), and suggested alternate routes.
4. Record the travel advisory into `workspace/travel/` or task context.

### 4. Durable Workspace Recording
- Save verified facts (e.g., historical price point, commute benchmark, external AI research brief) to `workspace/knowledge/claims/` or relevant domain folder.
- Close auxiliary tabs and return to a blank/neutral page when finished.

## Pitfalls

- Do not leave multiple tabs accumulating in the user's browser.
- Do not attempt to guess or brute-force bot verification sliders.
- Never record session tokens or raw credentials into workspace markdown files.
