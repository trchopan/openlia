package operator

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// Run is the standalone Go operator entrypoint. The root CLI is intentionally
// left unchanged while this command is evaluated as a replacement boundary.
func Run(args []string, input io.Reader, output, errorOutput io.Writer) int {
	return RunContext(context.Background(), args, input, output, errorOutput)
}

// RunContext is the cancellable entrypoint used by the host CLI.
func RunContext(ctx context.Context, args []string, input io.Reader, output, errorOutput io.Writer) int {
	jsonOutput, args := removeFlag(args, "--json")
	if len(args) == 0 || contains(args, "--help") || contains(args, "-h") {
		writeUsage(output)
		return ExitOK
	}
	config, err := LoadConfig()
	if err != nil {
		return commandError(output, errorOutput, jsonOutput, ExitUsage, err)
	}
	command := args[0]
	args = args[1:]
	now := time.Now().UTC()

	switch command {
	case "bootstrap":
		checkOnly, args := removeFlag(args, "--check-only")
		if len(args) != 0 {
			return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("bootstrap accepts only --check-only"))
		}
		if _, err := ValidateRuntime(ctx, config, nil); err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitPrereq, err)
		}
		result, err := BootstrapContext(ctx, config, checkOnly, now, NewCompose(config, nil))
		if err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		if jsonOutput {
			_ = WriteJSON(output, result)
		} else if !result.OK {
			fmt.Fprintln(output, "openlia bootstrap: runtime root is missing")
		} else {
			fmt.Fprintln(output, "openlia bootstrap: runtime prepared; no services started")
		}
		if !result.OK {
			return ExitFailure
		}
		return ExitOK
	case "profile":
		if len(args) > 1 || (len(args) == 1 && args[0] != "sync") {
			return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("profile accepts sync"))
		}
		result, err := NewProfileOperator(config).Sync()
		if err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		return emit(output, result, jsonOutput, "openlia profile: distribution assets synchronized; workspace preserved")
	case "skill-status":
		selected, remaining, err := stringFlag(args, "--skill")
		if err != nil || len(remaining) != 0 {
			return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("skill-status accepts --skill NAME"))
		}
		result, err := NewProfileOperator(config).Status(selected)
		if err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		return emit(output, result, jsonOutput, "openlia skill-status: read-only skill provenance inspection")
	case "skill-fork":
		if len(args) != 1 {
			return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("skill-fork requires NAME"))
		}
		result, err := ForkSkill(config, args[0], now)
		if err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		return emit(output, result, jsonOutput, "openlia skill-fork: skill forked; active files preserved")
	case "skill-migration":
		return runSkillMigration(config, args, output, errorOutput, jsonOutput, now)
	case "backup":
		return runBackup(ctx, config, args, input, output, errorOutput, jsonOutput, now)
	case "attachments":
		return runAttachments(ctx, config, args, output, errorOutput, jsonOutput, now)
	case "auth":
		return runAuth(ctx, config, args, output, errorOutput, jsonOutput, now)
	case "deploy":
		return runDeploy(ctx, config, args, output, errorOutput, jsonOutput, now)
	case "healthcheck":
		return runHealthcheck(ctx, config, args, output, errorOutput, jsonOutput)
	case "workspace-git":
		return runWorkspaceGit(ctx, config, args, output, errorOutput, jsonOutput)
	case "uninstall":
		if len(args) != 0 {
			return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("uninstall accepts no arguments"))
		}
		result, err := Uninstall(ctx, config, NewCompose(config, nil))
		if err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		return emit(output, result, jsonOutput, "openlia uninstall: installation removed; Docker images preserved")
	default:
		return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("unknown command %q", command))
	}
}

func runBackup(ctx context.Context, config Config, args []string, _ io.Reader, output, errorOutput io.Writer, jsonOutput bool, now time.Time) int {
	action := "create"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action, args = args[0], args[1:]
	}
	if action == "create" {
		reason := "manual"
		remaining, value, err := consumeOptionalValue(args, "--reason")
		if err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitUsage, err)
		}
		if value != "" {
			reason = value
		}
		if len(remaining) != 0 {
			return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("backup create accepts --reason NAME"))
		}
		compose := NewCompose(config, nil)
		result, err := createBackup(ctx, config, reason, now, &compose)
		if err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		if err := RecordChange(config, "backup", "ok", result.Archive, "reason="+reason, now); err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		return emit(output, result, jsonOutput, "openlia backup: created "+result.Archive)
	}
	if action != "restore" {
		return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("backup requires create or restore"))
	}
	archive, remaining, err := stringFlag(args, "--archive")
	if err != nil || len(remaining) != 0 {
		return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("backup restore accepts --archive PATH"))
	}
	compose := NewCompose(config, nil)
	result, err := restoreBackup(ctx, config, archive, now, &compose)
	if err != nil {
		return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
	}
	return emit(output, result, jsonOutput, "openlia backup: restored "+result.Archive+"; stack remains stopped")
}

func runAttachments(ctx context.Context, config Config, args []string, output, errorOutput io.Writer, jsonOutput bool, now time.Time) int {
	action := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action, args = args[0], args[1:]
	}
	switch action {
	case "list":
		if len(args) != 0 {
			return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("attachments list accepts no arguments"))
		}
		result, err := ListAttachments(config)
		if err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		return emit(output, result, jsonOutput, "openlia attachments: runtime capabilities redacted")
	case "generate":
		if len(args) != 0 {
			return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("attachments generate accepts no arguments"))
		}
		if err := GenerateAttachmentsContext(ctx, config, NewCompose(config, nil)); err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		return emit(output, map[string]any{"ok": true, "action": "attachments-generate", "capabilities": "redacted"}, jsonOutput, "openlia attachments: Compose sidecars regenerated")
	case "rotate":
		if len(args) == 0 {
			return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("attachments rotate requires HOST --source PATH"))
		}
		host := args[0]
		previousState, _ := ReadState(config)
		source, remaining, err := stringFlag(args[1:], "--source")
		if err != nil || len(remaining) != 0 {
			return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("attachments rotate requires HOST --source PATH"))
		}
		if err := RotateAttachmentContext(ctx, config, host, source, now, NewCompose(config, nil)); err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		restart := "deferred-until-deploy"
		if previousState == stateRunning {
			restart = "sidecar-only"
		}
		return emit(output, map[string]any{"ok": true, "action": "locho-rotate", "host": host, "capabilities": "redacted", "restart": restart}, jsonOutput, "openlia attachments: host="+host+" rotated; capabilities were not displayed")
	default:
		return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("unknown attachments action %s", action))
	}
}

func runAuth(ctx context.Context, config Config, args []string, output, errorOutput io.Writer, jsonOutput bool, now time.Time) int {
	action := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action, args = args[0], args[1:]
	}
	if action == "list" {
		if len(args) != 0 {
			return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("auth list accepts no arguments"))
		}
		result, err := ListAuth(config)
		if err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		return emit(output, result, jsonOutput, "openlia auth: secret values redacted")
	}
	if action != "rotate" {
		return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("auth requires list or rotate"))
	}
	source, remaining, err := stringFlag(args, "--source")
	if err != nil || len(remaining) != 0 {
		return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("auth rotate requires --source PATH"))
	}
	compose := NewCompose(config, nil)
	if err := rotateAuthWithCompose(ctx, config, compose, source, now); err != nil {
		return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
	}
	return emit(output, map[string]any{"ok": true, "action": "auth-rotate", "secret_values": "redacted", "restart": "hermes_only"}, jsonOutput, "openlia auth: credentials rotated; values were not displayed")
}

func runDeploy(ctx context.Context, config Config, args []string, output, errorOutput io.Writer, jsonOutput bool, now time.Time) int {
	action := "deploy"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action, args = args[0], args[1:]
	}
	component := "all"
	forceStart := false
	for len(args) > 0 {
		switch args[0] {
		case "--start":
			forceStart = true
			args = args[1:]
		case "--component":
			if len(args) < 2 {
				return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("--component requires a value"))
			}
			component, args = args[1], args[2:]
		default:
			return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("unknown deploy option %s", args[0]))
		}
	}
	if action == "profile" {
		backup, err := CreateBackup(config, "profile-update", now, NewCompose(config, nil))
		if err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		result, err := NewProfileOperator(config).Sync()
		if err != nil {
			_ = RecordChange(config, "profile", "failed", backup.Archive, "profile synchronization failed", now)
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		if err := refreshProtectedSkillRuntime(ctx, config, NewCompose(config, nil)); err != nil {
			_ = RecordChange(config, "profile", "failed", backup.Archive, "protected skill reload failed", now)
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		if err := RecordChange(config, "profile", "ok", backup.Archive, "profile assets synchronized", now); err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		human := fmt.Sprintf("openlia deploy: profile assets synchronized; workspace preserved; updated=%d; forked=%d; updates_available=%d; customized=%d; unmanaged=%d", result.Skills.Updated, result.Skills.Forked, result.Skills.UpdatesAvailable, result.Skills.Customized, result.Skills.Unmanaged)
		return emit(output, map[string]any{"ok": true, "action": "profile", "backup": backup.Archive, "workspace": "preserved", "profile_sync": result}, jsonOutput, human)
	}
	result, err := Deploy(ctx, config, NewCompose(config, nil), DeployOptions{Action: action, Component: component, ForceStart: forceStart}, now)
	if err != nil {
		return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
	}
	return emit(output, result, jsonOutput, fmt.Sprintf("openlia deploy: action=%s state=%s", result.Action, result.State))
}

func refreshProtectedSkillRuntime(ctx context.Context, config Config, compose Compose) error {
	state, err := ReadState(config)
	if err != nil {
		return err
	}
	if state != stateRunning {
		return nil
	}
	if _, err := compose.Run(ctx, "up", "-d", "--no-deps", "--force-recreate", "hermes"); err != nil {
		return fmt.Errorf("reload Hermes protected skills: %w", err)
	}
	return nil
}

func runHealthcheck(ctx context.Context, config Config, args []string, output, errorOutput io.Writer, jsonOutput bool) int {
	allowStopped, args := removeFlag(args, "--allow-stopped")
	providerCheck, args := removeFlag(args, "--provider-check")
	if len(args) != 0 {
		return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("unknown healthcheck option %s", args[0]))
	}
	result, err := Healthcheck(ctx, config, NewCompose(config, nil), allowStopped, providerCheck)
	if err != nil {
		return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
	}
	if jsonOutput {
		_ = WriteJSON(output, result)
	} else {
		fmt.Fprintf(output, "openlia health: state=%s result=%s\n", result.State, map[bool]string{true: "ok", false: "failed"}[result.OK])
	}
	if !result.OK {
		return ExitFailure
	}
	return ExitOK
}

func runWorkspaceGit(ctx context.Context, config Config, args []string, output, errorOutput io.Writer, jsonOutput bool) int {
	action := "setup"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action, args = args[0], args[1:]
	}
	options := WorkspaceGitOptions{Action: action, Branch: "main", Schedule: "every 5m", AuthorName: "OpenLia Agent", AuthorEmail: "openlia@localhost"}
	for len(args) > 0 {
		name := args[0]
		if len(args) < 2 {
			return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("%s requires a value", name))
		}
		value := args[1]
		args = args[2:]
		switch name {
		case "--remote":
			options.Remote = value
		case "--branch":
			options.Branch = value
		case "--schedule":
			options.Schedule = value
		case "--author-name":
			options.AuthorName = value
		case "--author-email":
			options.AuthorEmail = value
		default:
			return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("unknown workspace-git option %s", name))
		}
	}
	result, err := WorkspaceGit(ctx, NewCompose(config, nil), options)
	if err != nil {
		return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
	}
	return emit(output, result, jsonOutput, "openlia workspace git: operation completed")
}

func emit(output io.Writer, value any, jsonOutput bool, human string) int {
	if jsonOutput {
		if err := WriteJSON(output, value); err != nil {
			return ExitInternal
		}
		return ExitOK
	}
	if human != "" {
		fmt.Fprintln(output, human)
	}
	return ExitOK
}

func commandError(output, errorOutput io.Writer, jsonOutput bool, code int, err error) int {
	if jsonOutput {
		_ = WriteJSON(output, map[string]any{"schema": 1, "ok": false, "error": formatError(err).Error()})
	} else {
		fmt.Fprintf(errorOutput, "openlia: %s\n", formatError(err))
	}
	return code
}

func removeFlag(args []string, wanted string) (bool, []string) {
	found := false
	result := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == wanted {
			found = true
		} else {
			result = append(result, arg)
		}
	}
	return found, result
}

func stringFlag(args []string, name string) (string, []string, error) {
	value := ""
	remaining := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		if args[index] == name {
			if index+1 >= len(args) || args[index+1] == "" {
				return "", nil, fmt.Errorf("%s requires a value", name)
			}
			value = args[index+1]
			index++
		} else {
			remaining = append(remaining, args[index])
		}
	}
	return value, remaining, nil
}

func consumeOptionalValue(args []string, name string) ([]string, string, error) {
	value, remaining, err := stringFlag(args, name)
	return remaining, value, err
}

func contains(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}

func writeUsage(writer io.Writer) {
	fmt.Fprintln(writer, `openlia operator

Usage:
  openlia-operator <subcommand> [--json]

Subcommands:
  bootstrap profile skill-status skill-fork skill-migration backup attachments auth deploy
  healthcheck workspace-git uninstall`)
}

func runSkillMigration(config Config, args []string, output, errorOutput io.Writer, jsonOutput bool, now time.Time) int {
	if len(args) == 0 {
		return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("skill-migration requires prepare, show, apply, or reject"))
	}
	action := args[0]
	if len(args) != 2 {
		return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("skill-migration %s requires one name or proposal ID", action))
	}
	switch action {
	case "prepare":
		result, err := PrepareSkillMigration(config, args[1], now)
		if err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		return emit(output, result, jsonOutput, "openlia skill-migration: context staged for Hermes")
	case "show":
		proposal, err := ReadSkillMigrationProposal(config, args[1])
		if err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		return emit(output, proposal, jsonOutput, "openlia skill-migration: proposal loaded")
	case "apply":
		result, err := ApplySkillMigration(config, args[1], now)
		if err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		return emit(output, result, jsonOutput, "openlia skill-migration: proposal applied")
	case "reject":
		result, err := RejectSkillMigration(config, args[1], now)
		if err != nil {
			return commandError(output, errorOutput, jsonOutput, ExitFailure, err)
		}
		return emit(output, result, jsonOutput, "openlia skill-migration: proposal rejected")
	default:
		return commandError(output, errorOutput, jsonOutput, ExitUsage, fmt.Errorf("unknown skill-migration action %s", action))
	}
}
