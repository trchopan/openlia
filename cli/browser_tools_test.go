package cli

import (
	"strings"
	"testing"
)

func TestBrowserToolsRunnerIncludesOutputLanguage(t *testing.T) {
	runner := renderBrowserToolsRunner(browserToolsTarget{
		Root:           "/srv/browser-tools",
		OutputLanguage: "vi",
	}, "/usr/bin/node", "/usr/bin", "/srv/playwright-token")
	if !strings.Contains(runner, "export OPENLIA_OUTPUT_LANGUAGE='vi'") {
		t.Fatalf("browser-tools runner does not include output language: %s", runner)
	}
}
