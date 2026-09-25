package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type openliaBrowserTarget struct {
	Mode               string
	Target             string
	SSHPort            int
	Root               string
	ExtensionTokenFile string
}

var defaultOpenLIABrowserAllowlist = []string{
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

func commandOpenLIABrowser(options Options, args []string, assets fs.FS) int {
	if len(args) == 0 {
		return fail(options, ExitUsage, "browser requires configure, install, start, stop, restart, status, logs, or uninstall", nil)
	}
	action := args[0]
	args = args[1:]
	if action == "configure" {
		return configureOpenLIABrowser(options, args)
	}
	config, code := configOrError(options)
	if code != ExitOK {
		return code
	}
	if !config.OpenLIABrowser.Configured {
		return fail(options, ExitPrereq, "browser is not configured; run `openlia browser configure`", nil)
	}
	target := openliaBrowserTarget{Mode: config.OpenLIABrowser.Mode, Target: config.OpenLIABrowser.Target, SSHPort: config.OpenLIABrowser.SSHPort, Root: config.OpenLIABrowser.Root, ExtensionTokenFile: config.OpenLIABrowser.ExtensionTokenFile}
	if err := validateOpenLIABrowserTarget(target); err != nil {
		return fail(options, ExitUsage, err.Error(), nil)
	}
	ctx, cancel := remoteContext()
	defer cancel()
	switch action {
	case "install":
		return installOpenLIABrowser(options, ctx, target, assets)
	case "start", "stop", "restart":
		return lifecycleOpenLIABrowser(options, ctx, target, action)
	case "status":
		return statusOpenLIABrowser(options, ctx, target)
	case "logs":
		return logsOpenLIABrowser(options, ctx, target, args)
	case "uninstall":
		return uninstallOpenLIABrowser(options, ctx, target)
	default:
		return fail(options, ExitUsage, fmt.Sprintf("unknown browser action %q", action), nil)
	}
}

func configureOpenLIABrowser(options Options, args []string) int {
	set := newFlagSet("browser configure")
	local := set.Bool("local", false, "manage openlia-browser on this machine")
	target := set.String("target", "", "SSH destination such as user@host")
	root := set.String("root", "", "openlia-browser installation root on the selected machine")
	sshPort := set.Int("ssh-port", 22, "SSH port")
	tokenFile := set.String("extension-token-file", "", "Playwright extension token file on the selected machine")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return fail(options, ExitUsage, "browser configure requires --local or --target and --root", nil)
	}
	if (*local && *target != "") || (!*local && *target == "") {
		return fail(options, ExitUsage, "browser configure requires exactly one of --local or --target", nil)
	}
	config, err := loadConfigUnchecked()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	if err != nil {
		config = defaultConfig()
	}
	config.OpenLIABrowser = OpenLIABrowserConfig{
		Configured:         true,
		Mode:               "local",
		Target:             "",
		SSHPort:            *sshPort,
		Root:               *root,
		ExtensionTokenFile: *tokenFile,
	}
	if !*local {
		config.OpenLIABrowser.Mode = "ssh"
		config.OpenLIABrowser.Target = *target
	}
	if err := validateConfig(config); err != nil {
		return fail(options, ExitUsage, err.Error(), nil)
	}
	if err := saveConfig(config); err != nil {
		return fail(options, ExitInternal, err.Error(), nil)
	}
	return writeResult(options, map[string]any{
		"schema":    1,
		"ok":        true,
		"action":    "configure",
		"component": "openlia-browser",
		"mode":      config.OpenLIABrowser.Mode,
		"target":    config.OpenLIABrowser.Target,
		"ssh_port":  config.OpenLIABrowser.SSHPort,
		"root":      config.OpenLIABrowser.Root,
	}, "openlia-browser configuration saved")
}

func validateOpenLIABrowserTarget(target openliaBrowserTarget) error {
	config := OpenLIABrowserConfig{Configured: true, Mode: target.Mode, Target: target.Target, SSHPort: target.SSHPort, Root: target.Root, ExtensionTokenFile: target.ExtensionTokenFile}
	return validateOpenLIABrowser(config)
}

func openliaBrowserCommand(target openliaBrowserTarget, command string) ([]byte, error) {
	if target.Mode == "local" {
		process := exec.Command("/bin/sh", "-c", command)
		var stdout, stderr bytes.Buffer
		process.Stdout = &stdout
		process.Stderr = &stderr
		if err := process.Run(); err != nil {
			return stdout.Bytes(), fmt.Errorf("local openlia-browser command failed: %s", strings.TrimSpace(redact(stderr.String())))
		}
		return stdout.Bytes(), nil
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		return nil, fmt.Errorf("ssh is required: %w", err)
	}
	process := exec.Command(ssh, "-p", strconv.Itoa(target.SSHPort), "--", target.Target, command)
	var stdout, stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	if err := process.Run(); err != nil {
		message := strings.TrimSpace(redact(stderr.String()))
		if message == "" {
			message = err.Error()
		}
		return stdout.Bytes(), fmt.Errorf("remote openlia-browser command failed: %s", message)
	}
	return stdout.Bytes(), nil
}

func openliaBrowserUpload(target openliaBrowserTarget, destination string, data []byte, mode string) error {
	temporary := destination + ".tmp-openlia"
	command := "set -eu; mkdir -p " + shellQuote(filepath.Dir(destination)) + "; umask 077; cat > " + shellQuote(temporary) + "; chmod " + shellQuote(mode) + " " + shellQuote(temporary) + "; mv -f " + shellQuote(temporary) + " " + shellQuote(destination)
	if _, err := openliaBrowserCommandWithInput(target, command, data); err != nil {
		return err
	}
	return nil
}

func openliaBrowserCommandWithInput(target openliaBrowserTarget, command string, data []byte) ([]byte, error) {
	if target.Mode == "local" {
		process := exec.Command("/bin/sh", "-c", command)
		process.Stdin = bytes.NewReader(data)
		var stdout, stderr bytes.Buffer
		process.Stdout = &stdout
		process.Stderr = &stderr
		if err := process.Run(); err != nil {
			return stdout.Bytes(), fmt.Errorf("local openlia-browser command failed: %s", strings.TrimSpace(redact(stderr.String())))
		}
		return stdout.Bytes(), nil
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		return nil, fmt.Errorf("ssh is required: %w", err)
	}
	process := exec.Command(ssh, "-p", strconv.Itoa(target.SSHPort), "--", target.Target, command)
	process.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	if err := process.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("remote openlia-browser command failed: %s", strings.TrimSpace(redact(stderr.String())))
	}
	return stdout.Bytes(), nil
}

func openliaBrowserRootCommand(target openliaBrowserTarget, command string) string {
	return "root=" + shellQuote(target.Root) + "; " + command
}

func installOpenLIABrowser(options Options, ctx context.Context, target openliaBrowserTarget, assets fs.FS) int {
	bundle, err := fs.ReadFile(assets, "packages/openlia-browser/dist/server.js")
	if err != nil {
		return fail(options, ExitInternal, fmt.Sprintf("openlia-browser bundle is missing: %v", err), nil)
	}
	digest := sha256.Sum256(bundle)
	digestText := hex.EncodeToString(digest[:])
	if ctx.Err() != nil {
		return fail(options, ExitFailure, ctx.Err().Error(), nil)
	}
	root := shellQuote(target.Root)
	setup := "set -eu; mkdir -p " + root + "/data " + root + "/logs " + root + "/run; chmod 700 " + root + " " + root + "/data " + root + "/logs " + root + "/run; printf '%s\\n' " + shellQuote(`{"schema":1,"service":"openlia-browser","bundle_sha256":"`+digestText+`"}`) + " > " + root + "/install.json; chmod 600 " + root + "/install.json"
	if _, err := openliaBrowserCommand(target, setup); err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	if err := openliaBrowserUpload(target, filepath.Join(target.Root, "server.js"), bundle, "600"); err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	nodePath := "node"
	if output, err := openliaBrowserCommand(target, "for candidate in /opt/homebrew/bin/node /usr/local/bin/node /usr/bin/node; do if [ -x \"$candidate\" ]; then printf '%s\\n' \"$candidate\"; exit 0; fi; done; command -v node"); err == nil && strings.TrimSpace(string(output)) != "" {
		nodePath = strings.TrimSpace(string(output))
	}
	tokenPath := target.ExtensionTokenFile
	nodeDir := filepath.Dir(nodePath)
	runner := renderOpenLIABrowserRunner(target, nodePath, nodeDir, tokenPath)
	if err := openliaBrowserUpload(target, filepath.Join(target.Root, "run.sh"), []byte(runner), "700"); err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	return lifecycleOpenLIABrowser(options, ctx, target, "restart")
}

func renderOpenLIABrowserRunner(target openliaBrowserTarget, nodePath, nodeDir, tokenPath string) string {
	return "#!/bin/sh\nset -eu\nexport PATH=" + shellQuote(nodeDir) + ":${PATH:-/usr/bin:/bin}\nexport OPENLIA_BROWSER_BIND=127.0.0.1\nexport OPENLIA_BROWSER_PORT=8932\nexport OPENLIA_BROWSER_MCP_URL=http://localhost:8931/mcp\nexport OPENLIA_BROWSER_ALLOWED_TOOLS=" + shellQuote(strings.Join(defaultOpenLIABrowserAllowlist, ",")) + "\nexport OPENLIA_BROWSER_PLAYWRIGHT_PORT=8931\nexport OPENLIA_BROWSER_SUPERVISE_PLAYWRIGHT=1\nexport OPENLIA_BROWSER_PLAYWRIGHT_TOKEN_FILE=" + shellQuote(tokenPath) + "\nexport OPENLIA_BROWSER_PLAYWRIGHT_COMMAND=" + shellQuote(filepath.Join(nodeDir, "npx")) + "\nexport OPENLIA_BROWSER_DATA_ROOT=" + shellQuote(filepath.Join(target.Root, "data")) + "\nexec " + shellQuote(nodePath) + " " + shellQuote(filepath.Join(target.Root, "server.js")) + "\n"
}

func lifecycleOpenLIABrowser(options Options, ctx context.Context, target openliaBrowserTarget, action string) int {
	root := shellQuote(target.Root)
	pid := target.Root + "/run/openlia-browser.pid"
	commands := map[string]string{
		"start":   "set -eu; if [ -f " + shellQuote(pid) + " ] && kill -0 \"$(cat " + shellQuote(pid) + ")\" 2>/dev/null; then exit 0; fi; nohup " + root + "/run.sh > " + root + "/logs/openlia-browser.log 2>&1 < /dev/null & echo $! > " + shellQuote(pid) + "; chmod 600 " + shellQuote(pid),
		"stop":    "set -eu; if [ -f " + shellQuote(pid) + " ]; then kill \"$(cat " + shellQuote(pid) + ")\" 2>/dev/null || true; rm -f " + shellQuote(pid) + "; fi",
		"restart": "set -eu; if [ -f " + shellQuote(pid) + " ]; then kill \"$(cat " + shellQuote(pid) + ")\" 2>/dev/null || true; rm -f " + shellQuote(pid) + "; fi; nohup " + root + "/run.sh > " + root + "/logs/openlia-browser.log 2>&1 < /dev/null & echo $! > " + shellQuote(pid) + "; chmod 600 " + shellQuote(pid),
	}
	command, ok := commands[action]
	if !ok {
		return fail(options, ExitUsage, "unsupported openlia-browser lifecycle action", nil)
	}
	if _, err := openliaBrowserCommand(target, command); err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	return writeResult(options, map[string]any{"schema": 1, "ok": true, "component": "openlia-browser", "action": action, "mode": target.Mode, "target": target.Target, "ssh_port": target.SSHPort, "root": target.Root}, "openlia-browser "+action+" requested")
}

func statusOpenLIABrowser(options Options, ctx context.Context, target openliaBrowserTarget) int {
	output, err := openliaBrowserCommand(target, "curl -fsS http://127.0.0.1:8932/health")
	if err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	return renderRemote(options, output, fmt.Sprintf("openlia-browser status: %s", strings.TrimSpace(string(output))))
}

func logsOpenLIABrowser(options Options, ctx context.Context, target openliaBrowserTarget, args []string) int {
	if len(args) != 0 {
		return fail(options, ExitUsage, "openlia-browser logs takes no positional arguments", nil)
	}
	output, err := openliaBrowserCommand(target, "tail -n 200 "+shellQuote(filepath.Join(target.Root, "logs", "openlia-browser.log")))
	if err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	if options.JSON {
		return writeResult(options, map[string]any{"schema": 1, "ok": true, "component": "openlia-browser", "action": "logs", "logs": redact(string(output))}, "")
	}
	fmt.Print(redact(string(output)))
	return ExitOK
}

func uninstallOpenLIABrowser(options Options, ctx context.Context, target openliaBrowserTarget) int {
	if !options.NonInteractive {
		expected := "uninstall " + target.Root
		fmt.Fprintf(os.Stderr, "Permanently remove openlia-browser from %s? Type %q to continue: ", target.Root, expected)
		answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil || strings.TrimSpace(answer) != expected {
			return fail(options, ExitFailure, "openlia-browser uninstall cancelled", nil)
		}
	}
	command := "set -eu; if [ -f " + shellQuote(filepath.Join(target.Root, "run", "openlia-browser.pid")) + " ]; then kill \"$(cat " + shellQuote(filepath.Join(target.Root, "run", "openlia-browser.pid")) + ")\" 2>/dev/null || true; fi; rm -rf -- " + shellQuote(target.Root)
	if _, err := openliaBrowserCommand(target, command); err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	return writeResult(options, map[string]any{"schema": 1, "ok": true, "component": "openlia-browser", "action": "uninstall", "state": "removed", "root": target.Root}, "openlia-browser uninstalled")
}
