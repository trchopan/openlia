package operator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type localWorkspaceGitRunner struct {
	workspace   string
	cronCalls   int
	remoteCalls int
}

func (runner *localWorkspaceGitRunner) Run(ctx context.Context, name string, args ...string) (CommandResult, error) {
	if name != "docker" {
		return CommandResult{ExitCode: 1}, fmt.Errorf("unexpected command %s", name)
	}
	for index, arg := range args {
		if arg == "config" && index+1 < len(args) && args[index+1] == "--services" {
			return CommandResult{Stdout: []byte("hermes\n")}, nil
		}
	}
	for index, arg := range args {
		if arg == "ps" {
			return CommandResult{Stdout: []byte("hermes\n")}, nil
		}
		if arg != "exec" {
			continue
		}
		for commandIndex := index + 1; commandIndex < len(args); commandIndex++ {
			switch args[commandIndex] {
			case "git":
				gitArgs := args[commandIndex+1:]
				if len(gitArgs) > 0 && (gitArgs[0] == "fetch" || gitArgs[0] == "ls-remote" || gitArgs[0] == "push") {
					runner.remoteCalls++
				}
				command := exec.CommandContext(ctx, "git", append([]string{"-C", runner.workspace}, gitArgs...)...)
				stdout, stderr := strings.Builder{}, strings.Builder{}
				command.Stdout = &stdout
				command.Stderr = &stderr
				err := command.Run()
				result := CommandResult{Stdout: []byte(stdout.String()), Stderr: []byte(stderr.String())}
				if err != nil {
					var exitError *exec.ExitError
					if errors.As(err, &exitError) {
						result.ExitCode = exitError.ExitCode()
					} else {
						result.ExitCode = 1
					}
				}
				return result, err
			case "test":
				return CommandResult{}, nil
			case "cron":
				runner.cronCalls++
				return CommandResult{}, nil
			}
		}
	}
	return CommandResult{ExitCode: 1}, fmt.Errorf("unexpected docker arguments: %v", args)
}

func TestWorkspaceGitMaintainsLocalHistoryWithoutRemote(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("workspace instructions\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &localWorkspaceGitRunner{workspace: workspace}
	compose := NewCompose(Config{ProjectName: "test", ComposeProjectDir: t.TempDir(), ComposeFile: filepath.Join(t.TempDir(), "compose.yaml")}, runner)
	options := WorkspaceGitOptions{
		Action:      "setup",
		Branch:      "main",
		Schedule:    "every 5m",
		AuthorName:  "OpenLia Agent",
		AuthorEmail: "openlia@localhost",
	}

	result, err := WorkspaceGit(context.Background(), compose, options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.AutomaticPull || result.InitialPush || result.Remote != "" {
		t.Fatalf("unexpected local setup result: %+v", result)
	}
	if got := runWorkspaceGitCommand(t, workspace, "log", "-1", "--format=%s"); got != "chore: initialize OpenLia workspace" {
		t.Fatalf("initial commit subject = %q", got)
	}
	if got := runWorkspaceGitCommand(t, workspace, "remote"); got != "" {
		t.Fatalf("local workspace has unexpected remote %q", got)
	}
	if got := runWorkspaceGitCommand(t, workspace, "config", "--bool", "--get", "openlia.workspace-remote-enabled"); got != "false" {
		t.Fatalf("local workspace remote state = %q", got)
	}
	if runner.cronCalls != 0 || runner.remoteCalls != 0 {
		t.Fatalf("local setup used remote synchronization: cron=%d remote=%d", runner.cronCalls, runner.remoteCalls)
	}

	if err := os.WriteFile(filepath.Join(workspace, "note.md"), []byte("local change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	options.Action = "ensure"
	if _, err := WorkspaceGit(context.Background(), compose, options); err != nil {
		t.Fatal(err)
	}
	if got := runWorkspaceGitCommand(t, workspace, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("ensure created an unexpected commit count: %s", got)
	}
	if got := runWorkspaceGitCommand(t, workspace, "status", "--short"); got != "?? note.md" {
		t.Fatalf("ensure did not preserve the uncommitted change: %q", got)
	}
}

func TestWorkspaceGitEnsureInitializesEmptyLocalRepository(t *testing.T) {
	workspace := t.TempDir()
	if output, err := exec.Command("git", "-C", workspace, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("initialize empty workspace: %v: %s", err, output)
	}
	runner := &localWorkspaceGitRunner{workspace: workspace}
	compose := NewCompose(Config{ProjectName: "test", ComposeProjectDir: t.TempDir(), ComposeFile: filepath.Join(t.TempDir(), "compose.yaml")}, runner)
	result, err := WorkspaceGit(context.Background(), compose, WorkspaceGitOptions{
		Action:      "ensure",
		Branch:      "main",
		AuthorName:  "OpenLia Agent",
		AuthorEmail: "openlia@localhost",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.AutomaticPull {
		t.Fatalf("unexpected ensure result: %+v", result)
	}
	if got := runWorkspaceGitCommand(t, workspace, "log", "-1", "--format=%s"); got != "chore: initialize OpenLia workspace" {
		t.Fatalf("initial commit subject = %q", got)
	}
	if runner.cronCalls != 0 || runner.remoteCalls != 0 {
		t.Fatalf("local ensure used remote synchronization: cron=%d remote=%d", runner.cronCalls, runner.remoteCalls)
	}
}

func TestWorkspaceGitSetupUnstagesProtectedPathsOnFailure(t *testing.T) {
	workspace := t.TempDir()
	if output, err := exec.Command("git", "-C", workspace, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("initialize workspace: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(workspace, "safe.md"), []byte("staged before setup\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", workspace, "add", "safe.md").CombinedOutput(); err != nil {
		t.Fatalf("stage safe path: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".env"), []byte("TOKEN=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &localWorkspaceGitRunner{workspace: workspace}
	compose := NewCompose(Config{ProjectName: "test", ComposeProjectDir: t.TempDir(), ComposeFile: filepath.Join(t.TempDir(), "compose.yaml")}, runner)
	_, err := WorkspaceGit(context.Background(), compose, WorkspaceGitOptions{
		Action:      "setup",
		Branch:      "main",
		AuthorName:  "OpenLia Agent",
		AuthorEmail: "openlia@localhost",
	})
	if err == nil || !strings.Contains(err.Error(), "refused a credential or runtime path") {
		t.Fatalf("setup error = %v", err)
	}
	if got := runWorkspaceGitCommand(t, workspace, "diff", "--cached", "--name-only"); got != "safe.md" {
		t.Fatalf("staged paths after rejection = %q", got)
	}
	if got := runWorkspaceGitCommand(t, workspace, "status", "--short"); got != "A  safe.md\n?? .env" {
		t.Fatalf("protected file was not preserved as untracked: %q", got)
	}
}

func TestWorkspaceGitSetupRetainsRemoteSynchronization(t *testing.T) {
	workspace := t.TempDir()
	remote := filepath.Join(t.TempDir(), "workspace.git")
	if output, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("initialize bare remote: %v: %s", err, output)
	}
	if output, err := exec.Command("git", "-C", workspace, "init").CombinedOutput(); err != nil {
		t.Fatalf("initialize workspace: %v: %s", err, output)
	}
	remoteURL := "https://github.com/example/private-vault.git"
	if output, err := exec.Command("git", "-C", workspace, "config", "url.file://"+remote+".insteadOf", remoteURL).CombinedOutput(); err != nil {
		t.Fatalf("configure test URL rewrite: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(workspace, "record.md"), []byte("workspace record\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := &localWorkspaceGitRunner{workspace: workspace}
	compose := NewCompose(Config{ProjectName: "test", ComposeProjectDir: t.TempDir(), ComposeFile: filepath.Join(t.TempDir(), "compose.yaml")}, runner)
	result, err := WorkspaceGit(context.Background(), compose, WorkspaceGitOptions{
		Action:      "setup",
		Remote:      remoteURL,
		Branch:      "main",
		Schedule:    "every 5m",
		AuthorName:  "OpenLia Agent",
		AuthorEmail: "openlia@localhost",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.AutomaticPull || !result.InitialPush || result.Remote != remoteURL {
		t.Fatalf("unexpected remote setup result: %+v", result)
	}
	if got := runWorkspaceGitCommand(t, workspace, "config", "--get", "remote.origin.url"); got != remoteURL {
		t.Fatalf("origin = %q", got)
	}
	localHead := runWorkspaceGitCommand(t, workspace, "rev-parse", "HEAD")
	remoteHeadOutput, err := exec.Command("git", "--git-dir", remote, "rev-parse", "refs/heads/main").CombinedOutput()
	if err != nil {
		t.Fatalf("read remote main: %v: %s", err, remoteHeadOutput)
	}
	if remoteHead := strings.TrimSpace(string(remoteHeadOutput)); remoteHead != localHead {
		t.Fatalf("remote HEAD = %q, local HEAD = %q", remoteHead, localHead)
	}
	if runner.cronCalls == 0 || runner.remoteCalls == 0 {
		t.Fatalf("remote setup did not synchronize: cron=%d remote=%d", runner.cronCalls, runner.remoteCalls)
	}

	_, err = WorkspaceGit(context.Background(), compose, WorkspaceGitOptions{
		Action:      "ensure",
		Branch:      "main",
		AuthorName:  "OpenLia Agent",
		AuthorEmail: "openlia@localhost",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := runWorkspaceGitCommand(t, workspace, "config", "--get", "remote.origin.url"); got != remoteURL {
		t.Fatalf("user-visible origin was changed: %q", got)
	}
	if got := runWorkspaceGitCommand(t, workspace, "config", "--bool", "--get", "openlia.workspace-remote-enabled"); got != "false" {
		t.Fatalf("disabled remote state = %q", got)
	}
}

func runWorkspaceGitCommand(t *testing.T, workspace string, args ...string) string {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", workspace}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
