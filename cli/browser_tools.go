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

type browserToolsTarget struct {
	Mode               string
	Target             string
	SSHPort            int
	Root               string
	ExtensionTokenFile string
}

func commandBrowserTools(options Options, args []string, assets fs.FS) int {
	if len(args) == 0 {
		return fail(options, ExitUsage, "browser-tools requires configure, install, start, stop, restart, status, logs, or uninstall", nil)
	}
	action := args[0]
	args = args[1:]
	if action == "configure" {
		return configureBrowserTools(options, args)
	}
	config, code := configOrError(options)
	if code != ExitOK {
		return code
	}
	if !config.BrowserTools.Configured {
		return fail(options, ExitPrereq, "browser-tools is not configured; run `openlia browser-tools configure`", nil)
	}
	target := browserToolsTarget{Mode: config.BrowserTools.Mode, Target: config.BrowserTools.Target, SSHPort: config.BrowserTools.SSHPort, Root: config.BrowserTools.Root, ExtensionTokenFile: config.BrowserTools.ExtensionTokenFile}
	if err := validateBrowserToolsTarget(target); err != nil {
		return fail(options, ExitUsage, err.Error(), nil)
	}
	ctx, cancel := remoteContext()
	defer cancel()
	switch action {
	case "install":
		return installBrowserTools(options, ctx, target, assets)
	case "start", "stop", "restart":
		return lifecycleBrowserTools(options, ctx, target, action)
	case "status":
		return statusBrowserTools(options, ctx, target)
	case "logs":
		return logsBrowserTools(options, ctx, target, args)
	case "uninstall":
		return uninstallBrowserTools(options, ctx, target)
	default:
		return fail(options, ExitUsage, fmt.Sprintf("unknown browser-tools action %q", action), nil)
	}
}

func configureBrowserTools(options Options, args []string) int {
	set := newFlagSet("browser-tools configure")
	local := set.Bool("local", false, "manage browser-tools on this machine")
	target := set.String("target", "", "SSH destination such as user@host")
	root := set.String("root", "", "browser-tools installation root on the selected machine")
	sshPort := set.Int("ssh-port", 22, "SSH port")
	tokenFile := set.String("extension-token-file", "", "Playwright extension token file on the selected machine")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return fail(options, ExitUsage, "browser-tools configure requires --local or --target and --root", nil)
	}
	if (*local && *target != "") || (!*local && *target == "") {
		return fail(options, ExitUsage, "browser-tools configure requires exactly one of --local or --target", nil)
	}
	config, err := loadConfigUnchecked()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	if err != nil {
		config = defaultConfig()
	}
	config.BrowserTools = BrowserToolsConfig{
		Configured:         true,
		Mode:               "local",
		Target:             "",
		SSHPort:            *sshPort,
		Root:               *root,
		ExtensionTokenFile: *tokenFile,
	}
	if !*local {
		config.BrowserTools.Mode = "ssh"
		config.BrowserTools.Target = *target
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
		"component": "browser-tools",
		"mode":      config.BrowserTools.Mode,
		"target":    config.BrowserTools.Target,
		"ssh_port":  config.BrowserTools.SSHPort,
		"root":      config.BrowserTools.Root,
	}, "browser-tools configuration saved")
}

func validateBrowserToolsTarget(target browserToolsTarget) error {
	config := BrowserToolsConfig{Configured: true, Mode: target.Mode, Target: target.Target, SSHPort: target.SSHPort, Root: target.Root, ExtensionTokenFile: target.ExtensionTokenFile}
	return validateBrowserTools(config)
}

func browserToolsCommand(target browserToolsTarget, command string) ([]byte, error) {
	if target.Mode == "local" {
		process := exec.Command("/bin/sh", "-c", command)
		var stdout, stderr bytes.Buffer
		process.Stdout = &stdout
		process.Stderr = &stderr
		if err := process.Run(); err != nil {
			return stdout.Bytes(), fmt.Errorf("local browser-tools command failed: %s", strings.TrimSpace(redact(stderr.String())))
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
		return stdout.Bytes(), fmt.Errorf("remote browser-tools command failed: %s", message)
	}
	return stdout.Bytes(), nil
}

func browserToolsUpload(target browserToolsTarget, destination string, data []byte, mode string) error {
	temporary := destination + ".tmp-openlia"
	command := "set -eu; mkdir -p " + shellQuote(filepath.Dir(destination)) + "; umask 077; cat > " + shellQuote(temporary) + "; chmod " + shellQuote(mode) + " " + shellQuote(temporary) + "; mv -f " + shellQuote(temporary) + " " + shellQuote(destination)
	if _, err := browserToolsCommandWithInput(target, command, data); err != nil {
		return err
	}
	return nil
}

func browserToolsCommandWithInput(target browserToolsTarget, command string, data []byte) ([]byte, error) {
	if target.Mode == "local" {
		process := exec.Command("/bin/sh", "-c", command)
		process.Stdin = bytes.NewReader(data)
		var stdout, stderr bytes.Buffer
		process.Stdout = &stdout
		process.Stderr = &stderr
		if err := process.Run(); err != nil {
			return stdout.Bytes(), fmt.Errorf("local browser-tools command failed: %s", strings.TrimSpace(redact(stderr.String())))
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
		return stdout.Bytes(), fmt.Errorf("remote browser-tools command failed: %s", strings.TrimSpace(redact(stderr.String())))
	}
	return stdout.Bytes(), nil
}

func browserToolsRootCommand(target browserToolsTarget, command string) string {
	return "root=" + shellQuote(target.Root) + "; " + command
}

func installBrowserTools(options Options, ctx context.Context, target browserToolsTarget, assets fs.FS) int {
	bundle, err := fs.ReadFile(assets, "packages/browser-tools/dist/server.js")
	if err != nil {
		return fail(options, ExitInternal, fmt.Sprintf("browser-tools bundle is missing: %v", err), nil)
	}
	digest := sha256.Sum256(bundle)
	digestText := hex.EncodeToString(digest[:])
	if ctx.Err() != nil {
		return fail(options, ExitFailure, ctx.Err().Error(), nil)
	}
	root := shellQuote(target.Root)
	setup := "set -eu; mkdir -p " + root + "/data " + root + "/logs " + root + "/run; chmod 700 " + root + " " + root + "/data " + root + "/logs " + root + "/run; printf '%s\\n' " + shellQuote(`{"schema":1,"service":"browser-tools","bundle_sha256":"`+digestText+`"}`) + " > " + root + "/install.json; chmod 600 " + root + "/install.json"
	if _, err := browserToolsCommand(target, setup); err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	if err := browserToolsUpload(target, filepath.Join(target.Root, "server.js"), bundle, "600"); err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	nodePath := "node"
	if output, err := browserToolsCommand(target, "for candidate in /opt/homebrew/bin/node /usr/local/bin/node /usr/bin/node; do if [ -x \"$candidate\" ]; then printf '%s\\n' \"$candidate\"; exit 0; fi; done; command -v node"); err == nil && strings.TrimSpace(string(output)) != "" {
		nodePath = strings.TrimSpace(string(output))
	}
	tokenPath := target.ExtensionTokenFile
	nodeDir := filepath.Dir(nodePath)
	runner := "#!/bin/sh\nset -eu\nexport PATH=" + shellQuote(nodeDir) + ":${PATH:-/usr/bin:/bin}\nexport BROWSER_TOOLS_BIND=127.0.0.1\nexport BROWSER_TOOLS_PORT=8932\nexport BROWSER_TOOLS_MCP_URL=http://localhost:8931/mcp\nexport BROWSER_TOOLS_PLAYWRIGHT_PORT=8931\nexport BROWSER_TOOLS_SUPERVISE_PLAYWRIGHT=1\nexport BROWSER_TOOLS_PLAYWRIGHT_TOKEN_FILE=" + shellQuote(tokenPath) + "\nexport BROWSER_TOOLS_PLAYWRIGHT_COMMAND=" + shellQuote(filepath.Join(nodeDir, "npx")) + "\nexport BROWSER_TOOLS_DATA_ROOT=" + shellQuote(filepath.Join(target.Root, "data")) + "\nexec " + shellQuote(nodePath) + " " + shellQuote(filepath.Join(target.Root, "server.js")) + "\n"
	if err := browserToolsUpload(target, filepath.Join(target.Root, "run.sh"), []byte(runner), "700"); err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	return lifecycleBrowserTools(options, ctx, target, "restart")
}

func lifecycleBrowserTools(options Options, ctx context.Context, target browserToolsTarget, action string) int {
	root := shellQuote(target.Root)
	pid := target.Root + "/run/browser-tools.pid"
	commands := map[string]string{
		"start":   "set -eu; if [ -f " + shellQuote(pid) + " ] && kill -0 \"$(cat " + shellQuote(pid) + ")\" 2>/dev/null; then exit 0; fi; nohup " + root + "/run.sh > " + root + "/logs/browser-tools.log 2>&1 < /dev/null & echo $! > " + shellQuote(pid) + "; chmod 600 " + shellQuote(pid),
		"stop":    "set -eu; if [ -f " + shellQuote(pid) + " ]; then kill \"$(cat " + shellQuote(pid) + ")\" 2>/dev/null || true; rm -f " + shellQuote(pid) + "; fi",
		"restart": "set -eu; if [ -f " + shellQuote(pid) + " ]; then kill \"$(cat " + shellQuote(pid) + ")\" 2>/dev/null || true; rm -f " + shellQuote(pid) + "; fi; nohup " + root + "/run.sh > " + root + "/logs/browser-tools.log 2>&1 < /dev/null & echo $! > " + shellQuote(pid) + "; chmod 600 " + shellQuote(pid),
	}
	command, ok := commands[action]
	if !ok {
		return fail(options, ExitUsage, "unsupported browser-tools lifecycle action", nil)
	}
	if _, err := browserToolsCommand(target, command); err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	return writeResult(options, map[string]any{"schema": 1, "ok": true, "component": "browser-tools", "action": action, "mode": target.Mode, "target": target.Target, "ssh_port": target.SSHPort, "root": target.Root}, "browser-tools "+action+" requested")
}

func statusBrowserTools(options Options, ctx context.Context, target browserToolsTarget) int {
	output, err := browserToolsCommand(target, "curl -fsS http://127.0.0.1:8932/health")
	if err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	return renderRemote(options, output, fmt.Sprintf("browser-tools status: %s", strings.TrimSpace(string(output))))
}

func logsBrowserTools(options Options, ctx context.Context, target browserToolsTarget, args []string) int {
	if len(args) != 0 {
		return fail(options, ExitUsage, "browser-tools logs takes no positional arguments", nil)
	}
	output, err := browserToolsCommand(target, "tail -n 200 "+shellQuote(filepath.Join(target.Root, "logs", "browser-tools.log")))
	if err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	if options.JSON {
		return writeResult(options, map[string]any{"schema": 1, "ok": true, "component": "browser-tools", "action": "logs", "logs": redact(string(output))}, "")
	}
	fmt.Print(redact(string(output)))
	return ExitOK
}

func uninstallBrowserTools(options Options, ctx context.Context, target browserToolsTarget) int {
	if !options.NonInteractive {
		expected := "uninstall " + target.Root
		fmt.Fprintf(os.Stderr, "Permanently remove browser-tools from %s? Type %q to continue: ", target.Root, expected)
		answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil || strings.TrimSpace(answer) != expected {
			return fail(options, ExitFailure, "browser-tools uninstall cancelled", nil)
		}
	}
	command := "set -eu; if [ -f " + shellQuote(filepath.Join(target.Root, "run", "browser-tools.pid")) + " ]; then kill \"$(cat " + shellQuote(filepath.Join(target.Root, "run", "browser-tools.pid")) + ")\" 2>/dev/null || true; fi; rm -rf -- " + shellQuote(target.Root)
	if _, err := browserToolsCommand(target, command); err != nil {
		return fail(options, ExitFailure, err.Error(), nil)
	}
	return writeResult(options, map[string]any{"schema": 1, "ok": true, "component": "browser-tools", "action": "uninstall", "state": "removed", "root": target.Root}, "browser-tools uninstalled")
}
