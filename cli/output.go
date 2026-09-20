package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
)

const (
	ExitOK       = 0
	ExitFailure  = 1
	ExitUsage    = 2
	ExitPrereq   = 3
	ExitInternal = 4
)

type Options struct {
	JSON           bool
	NonInteractive bool
	Follow         bool
	ProviderCheck  bool
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`\b(?:sk|ghp|gho|ghu|github_pat)_[A-Za-z0-9_\-]+`),
	regexp.MustCompile(`(?i)([A-Z][A-Z0-9_]*(?:KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL)[A-Z0-9_]*\s*=\s*)[^\s,;]+`),
	regexp.MustCompile(`(?i)(capabilit(?:y|ies)["']?\s*[:=]\s*["']?)[^\s,"']+`),
}

func redact(value string) string {
	for _, pattern := range secretPatterns {
		value = pattern.ReplaceAllString(value, `${1}[REDACTED]`)
	}
	return value
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func writeResult(options Options, value any, human string) int {
	if options.JSON {
		if err := writeJSON(value); err != nil {
			fmt.Fprintf(os.Stderr, "openlia: %s\n", redact(err.Error()))
			return ExitInternal
		}
		return ExitOK
	}
	if human != "" {
		fmt.Fprintln(os.Stdout, redact(human))
	}
	return ExitOK
}

func fail(options Options, code int, message string, fields map[string]any) int {
	message = redact(message)
	if options.JSON {
		payload := map[string]any{"schema": 1, "ok": false, "error": message}
		for key, value := range fields {
			payload[key] = value
		}
		if err := writeJSON(payload); err != nil {
			fmt.Fprintf(os.Stderr, "openlia: %s\n", redact(err.Error()))
		}
	} else {
		fmt.Fprintf(os.Stderr, "openlia: %s\n", message)
	}
	return code
}

func usage(out io.Writer) {
	fmt.Fprintln(out, `openlia - a thin Hermes Agent operations control plane

Usage:
  openlia init [--local|--target user@host] [--root /path] [--project NAME]
               [--timezone Asia/Ho_Chi_Minh] [--model MODEL] [--provider PROVIDER]
               [--external-network NAME] [--api] [--api-host 127.0.0.1]
               [--workspace-git-remote https://github.com/OWNER/REPO.git]
  openlia status [--json]
  openlia doctor [--json]
  openlia deploy | start | stop | restart
  openlia uninstall [--local|--target user@host] --project NAME --root /path
  openlia logs [--follow]
  openlia update [openlia|hermes|locho]
  openlia skills list|show|status|fork|migrate|enable|disable|test
  openlia auth list|setup|rotate
  openlia attachments list
  openlia attachments rotate <host> --source PATH
  openlia backup create|restore
  openlia workspace git setup|status

Global flags: --json, --non-interactive, --version, --help

Secrets are accepted only through protected files or Hermes' configured secret source.`)
}
