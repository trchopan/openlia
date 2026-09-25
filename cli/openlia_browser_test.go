package cli

import (
	"strings"
	"testing"
)

func TestOpenLIABrowserRunnerSupervisesPlaywright(t *testing.T) {
	runner := renderOpenLIABrowserRunner(openliaBrowserTarget{
		Root: "/srv/openlia-browser",
	}, "/usr/bin/node", "/usr/bin", "/srv/playwright-token")
	for _, expected := range []string{
		"export OPENLIA_BROWSER_SUPERVISE_PLAYWRIGHT=1",
		"export OPENLIA_BROWSER_ALLOWED_TOOLS='" + strings.Join(defaultOpenLIABrowserAllowlist, ",") + "'",
		"export OPENLIA_BROWSER_PLAYWRIGHT_TOKEN_FILE='/srv/playwright-token'",
		"exec '/usr/bin/node' '/srv/openlia-browser/server.js'",
	} {
		if !strings.Contains(runner, expected) {
			t.Fatalf("openlia-browser runner is missing %q: %s", expected, runner)
		}
	}
	if strings.Contains(runner, "OPENLIA_OUTPUT_LANGUAGE") || strings.Contains(runner, "OPENLIA_BROWSER_JOB") || strings.Contains(runner, "OPENLIA_BROWSER_JOBS") {
		t.Fatalf("openlia-browser runner contains chat-job configuration: %s", runner)
	}
}
