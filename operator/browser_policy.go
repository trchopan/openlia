package operator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	managedBrowserPolicyStart = "# BEGIN OPENLIA MANAGED BROWSER TOOLSET"
	managedBrowserPolicyEnd   = "# END OPENLIA MANAGED BROWSER TOOLSET"
)

var managedBrowserToolAllowlist = []string{
	"openlia_browser_session_request",
	"openlia_browser_session_status",
	"openlia_browser_session_touch",
	"openlia_browser_session_release",
	"openlia_browser_session_cancel",
	"browser_navigate",
	"browser_navigate_back",
	"browser_snapshot",
	"browser_find",
	"browser_click",
	"browser_type",
	"browser_fill_form",
	"browser_press_key",
	"browser_select_option",
	"browser_hover",
	"browser_tabs",
	"browser_wait_for",
	"browser_take_screenshot",
	"browser_handle_dialog",
	"browser_close",
}

func managedBrowserPolicy(endpoint string) []string {
	mcpEndpoint := strings.TrimRight(endpoint, "/") + "/mcp"
	lines := []string{
		managedBrowserPolicyStart,
		"tools:",
		"  tool_search:",
		"    enabled: off",
		"agent:",
		"  disabled_toolsets:",
		"    - browser",
		"mcp_servers:",
		"  openlia-playwright:",
		"    url: " + strconv.Quote(mcpEndpoint),
		"    connect_timeout: 30",
		"    timeout: 120",
	}
	lines = append(lines, "    tools:", "      include:")
	for _, tool := range managedBrowserToolAllowlist {
		lines = append(lines, "        - "+tool)
	}
	return append(lines, managedBrowserPolicyEnd)
}

// ReconcileBrowserPolicy removes Hermes' native browser toolset when OpenLia
// owns an explicit openlia-browser attachment. The policy is deliberately separate
// from browser endpoint discovery so an unassigned legacy endpoint cannot
// silently change the model's tool surface.
func ReconcileBrowserPolicy(config Config, playwrightRole bool, browserEndpoint string) error {
	path := filepath.Join(config.DataRoot, "config.yaml")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read Hermes config for browser policy: %w", err)
	}
	updated, changed, err := renderBrowserPolicy(data, playwrightRole, browserEndpoint)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return AtomicWriteFile(path, updated, 0o600)
}

func renderBrowserPolicy(data []byte, playwrightRole bool, browserEndpoint string) ([]byte, bool, error) {
	if playwrightRole && browserEndpoint == "" {
		return nil, false, fmt.Errorf("openlia-browser policy requires a browser MCP endpoint")
	}
	lines := strings.Split(string(data), "\n")
	start, end := -1, -1
	for index, line := range lines {
		switch strings.TrimSpace(line) {
		case managedBrowserPolicyStart:
			if start >= 0 {
				return nil, false, fmt.Errorf("Hermes config contains duplicate managed browser policy blocks")
			}
			start = index
		case managedBrowserPolicyEnd:
			if start < 0 || end >= 0 {
				return nil, false, fmt.Errorf("Hermes config contains an invalid managed browser policy block")
			}
			end = index
		}
	}
	if (start >= 0) != (end >= 0) {
		return nil, false, fmt.Errorf("Hermes config contains an incomplete managed browser policy block")
	}
	if start >= 0 {
		replacement := []string{}
		if playwrightRole {
			replacement = managedBrowserPolicy(browserEndpoint)
		}
		updated := append([]string{}, lines[:start]...)
		updated = append(updated, replacement...)
		updated = append(updated, lines[end+1:]...)
		value := strings.Join(updated, "\n")
		return []byte(value), value != string(data), nil
	}
	if !playwrightRole {
		return data, false, nil
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if (trimmed == "agent:" || trimmed == "mcp_servers:" || trimmed == "tools:") && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			return nil, false, fmt.Errorf("Hermes config already contains a user-owned %s mapping; refusing to overwrite it while enabling the Playwright browser policy", strings.TrimSuffix(trimmed, ":"))
		}
	}
	updated := append([]string{}, lines...)
	if len(updated) > 0 && updated[len(updated)-1] != "" {
		updated = append(updated, "")
	}
	updated = append(updated, managedBrowserPolicy(browserEndpoint)...)
	return []byte(strings.Join(updated, "\n")), true, nil
}
