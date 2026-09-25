package operator

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

type WorkspaceGitOptions struct {
	Action      string
	Remote      string
	Branch      string
	Schedule    string
	AuthorName  string
	AuthorEmail string
}

type WorkspaceGitResult struct {
	OK            bool   `json:"ok"`
	Action        string `json:"action,omitempty"`
	Workspace     string `json:"workspace,omitempty"`
	Branch        string `json:"branch,omitempty"`
	Remote        string `json:"remote,omitempty"`
	Schedule      string `json:"schedule,omitempty"`
	AutomaticPull bool   `json:"automatic_pull,omitempty"`
	InitialPush   bool   `json:"initial_push,omitempty"`
	Status        string `json:"status,omitempty"`
}

const workspacePath = "/opt/data/workspace"

func WorkspaceGit(ctx context.Context, compose Compose, options WorkspaceGitOptions) (WorkspaceGitResult, error) {
	if options.Action == "" {
		options.Action = "status"
	}
	if options.Action != "status" && options.Action != "setup" && options.Action != "ensure" {
		return WorkspaceGitResult{}, fmt.Errorf("unknown workspace Git action %s", options.Action)
	}
	if options.Action != "status" {
		if options.Schedule == "" {
			options.Schedule = "every 5m"
		}
		if options.AuthorName == "" {
			options.AuthorName = "OpenLia Agent"
		}
		if options.AuthorEmail == "" {
			options.AuthorEmail = "openlia@localhost"
		}
		if options.Branch == "" {
			return WorkspaceGitResult{}, fmt.Errorf("workspace Git setup requires a branch")
		}
		if options.Remote != "" {
			if err := validateGitHubRemote(options.Remote); err != nil {
				return WorkspaceGitResult{}, err
			}
		}
		if err := validateGitBranch(options.Branch); err != nil {
			return WorkspaceGitResult{}, err
		}
		if err := validateWorkspaceGitMetadata(options); err != nil {
			return WorkspaceGitResult{}, err
		}
	}
	runGit := func(args ...string) (CommandResult, error) {
		return compose.Run(ctx, append([]string{"exec", "-T", "-u", "10000", "-w", workspacePath, "hermes", "git"}, args...)...)
	}
	runHermes := func(args ...string) (CommandResult, error) {
		return compose.Run(ctx, append([]string{"exec", "-T", "-u", "10000", "hermes", "hermes"}, args...)...)
	}
	if !compose.ServiceRunning(ctx, "hermes") {
		return WorkspaceGitResult{}, fmt.Errorf("Hermes must be running for workspace Git operations")
	}
	if _, err := compose.Run(ctx, "exec", "-T", "-u", "10000", "hermes", "test", "-x", "/opt/data/scripts/openlia-workspace-git-sync.sh"); err != nil {
		return WorkspaceGitResult{}, fmt.Errorf("workspace Git sync script is missing from the Hermes profile")
	}

	if options.Action == "status" {
		branch, err := runGit("branch", "--show-current")
		if err != nil {
			return WorkspaceGitResult{}, fmt.Errorf("workspace is not a Git repository")
		}
		remote, _ := runGit("remote", "get-url", "origin")
		status, err := runGit("status", "--short", "--branch")
		if err != nil {
			return WorkspaceGitResult{}, fmt.Errorf("could not read workspace Git status")
		}
		return WorkspaceGitResult{OK: true, Workspace: workspacePath, Branch: strings.TrimSpace(string(branch.Stdout)), Remote: strings.TrimSpace(string(remote.Stdout)), Status: strings.TrimSpace(string(status.Stdout))}, nil
	}

	initialized := false
	if _, err := runGit("rev-parse", "--git-dir"); err != nil {
		if _, err := runGit("init", "-b", options.Branch); err != nil {
			return WorkspaceGitResult{}, fmt.Errorf("workspace Git initialization failed")
		}
		initialized = true
	}

	currentBranch := ""
	if result, err := runGit("branch", "--show-current"); err == nil {
		currentBranch = strings.TrimSpace(string(result.Stdout))
	}
	headCommit := ""
	if result, err := runGit("rev-parse", "--verify", "HEAD"); err == nil {
		headCommit = strings.TrimSpace(string(result.Stdout))
	}
	if options.Action == "ensure" && !initialized && headCommit != "" {
		if currentBranch != options.Branch {
			return WorkspaceGitResult{}, fmt.Errorf("workspace Git is on branch %s, expected %s; refusing to switch it", currentBranch, options.Branch)
		}
		if options.Remote != "" {
			existingRemote := gitRemote(runGit)
			if existingRemote != options.Remote {
				return WorkspaceGitResult{}, fmt.Errorf("workspace Git origin differs from the configured remote")
			}
			if err := configureWorkspaceCron(runHermes, options.Schedule); err != nil {
				return WorkspaceGitResult{}, err
			}
		}
		if err := configureWorkspaceRemoteState(runGit, options.Remote != ""); err != nil {
			return WorkspaceGitResult{}, err
		}
		return WorkspaceGitResult{OK: true, Action: options.Action, Remote: options.Remote, Branch: options.Branch, Schedule: options.Schedule, AutomaticPull: options.Remote != ""}, nil
	}

	if headCommit == "" {
		if currentBranch != options.Branch {
			if _, err := runGit("branch", "-M", options.Branch); err != nil {
				return WorkspaceGitResult{}, fmt.Errorf("workspace Git branch initialization failed")
			}
		}
	} else if currentBranch != options.Branch {
		return WorkspaceGitResult{}, fmt.Errorf("workspace Git is on branch %s, expected %s; refusing to switch it", currentBranch, options.Branch)
	}
	existingRemote := gitRemote(runGit)
	if options.Remote != "" && existingRemote != "" && existingRemote != options.Remote {
		return WorkspaceGitResult{}, fmt.Errorf("workspace Git origin differs from the configured remote; refusing to replace it")
	}
	if options.Remote != "" && existingRemote == "" {
		if _, err := runGit("remote", "add", "origin", options.Remote); err != nil {
			return WorkspaceGitResult{}, fmt.Errorf("workspace Git remote setup failed")
		}
	}
	if _, err := runGit("config", "--local", "user.name", options.AuthorName); err != nil {
		return WorkspaceGitResult{}, fmt.Errorf("workspace Git author configuration failed")
	}
	if _, err := runGit("config", "--local", "user.email", options.AuthorEmail); err != nil {
		return WorkspaceGitResult{}, fmt.Errorf("workspace Git author configuration failed")
	}
	if _, err := runGit("config", "--local", "openlia.workspace-branch", options.Branch); err != nil {
		return WorkspaceGitResult{}, fmt.Errorf("workspace Git configuration failed")
	}
	if err := configureWorkspaceRemoteState(runGit, false); err != nil {
		return WorkspaceGitResult{}, err
	}

	if headCommit == "" {
		if _, err := runGit("add", "--all", "--", "."); err != nil {
			return WorkspaceGitResult{}, fmt.Errorf("workspace Git staging failed")
		}
		if err := stagedPathsAreSafe(runGit); err != nil {
			unstageProtectedWorkspacePaths(runGit, headCommit != "")
			return WorkspaceGitResult{}, err
		}
		if _, err := runGit("commit", "--allow-empty", "-m", "chore: initialize OpenLia workspace"); err != nil {
			return WorkspaceGitResult{}, fmt.Errorf("workspace Git initial commit failed")
		}
	} else if options.Action == "setup" && options.Remote != "" {
		if _, err := runGit("add", "--all", "--", "."); err != nil {
			return WorkspaceGitResult{}, fmt.Errorf("workspace Git staging failed")
		}
		if err := commitStagedChanges(runGit, "backup: synchronize workspace"); err != nil {
			return WorkspaceGitResult{}, err
		}
	}

	if options.Remote == "" {
		return WorkspaceGitResult{OK: true, Action: options.Action, Workspace: workspacePath, Branch: options.Branch}, nil
	}

	remoteBranchExists, err := remoteBranchExists(runGit, options.Branch)
	if err != nil {
		return WorkspaceGitResult{}, err
	}

	if remoteBranchExists {
		if _, err := runGit("fetch", "--quiet", "origin", options.Branch); err != nil {
			return WorkspaceGitResult{}, fmt.Errorf("workspace Git remote fetch failed")
		}
		headIsAncestor := isGitAncestor(runGit, "HEAD", "origin/"+options.Branch)
		remoteIsAncestor := false
		if !headIsAncestor {
			remoteIsAncestor = isGitAncestor(runGit, "origin/"+options.Branch, "HEAD")
		}
		if !headIsAncestor && !remoteIsAncestor {
			if _, err := runGit("merge", "--no-commit", "--no-ff", "--allow-unrelated-histories", "origin/"+options.Branch); err != nil {
				_, _ = runGit("merge", "--abort")
				return WorkspaceGitResult{}, fmt.Errorf("workspace Git histories conflict; automatic pull is disabled until the conflict is resolved")
			}
			if err := stagedPathsAreSafe(runGit); err != nil {
				_, _ = runGit("merge", "--abort")
				return WorkspaceGitResult{}, fmt.Errorf("workspace Git merge contained a credential or runtime path; automatic pull is disabled")
			}
			if _, err := runGit("commit", "-m", "chore: reconcile workspace with remote backup"); err != nil {
				return WorkspaceGitResult{}, fmt.Errorf("workspace Git merge commit failed")
			}
		} else if headIsAncestor {
			if _, err := runGit("merge", "--ff-only", "origin/"+options.Branch); err != nil {
				return WorkspaceGitResult{}, fmt.Errorf("workspace Git fast-forward failed")
			}
		}
	}
	if _, err := runGit("push", "--set-upstream", "origin", options.Branch); err != nil {
		return WorkspaceGitResult{}, fmt.Errorf("workspace Git initial push failed")
	}
	if err := configureWorkspaceCron(runHermes, options.Schedule); err != nil {
		return WorkspaceGitResult{}, err
	}
	if err := configureWorkspaceRemoteState(runGit, true); err != nil {
		return WorkspaceGitResult{}, err
	}
	return WorkspaceGitResult{OK: true, Action: options.Action, Remote: options.Remote, Branch: options.Branch, Schedule: options.Schedule, AutomaticPull: true, InitialPush: true}, nil
}

func validateGitHubRemote(remote string) error {
	parsed, err := url.Parse(remote)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("workspace Git remote must be an HTTPS GitHub repository URL")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || !safeGitHubSegment(parts[0]) || !safeGitHubSegment(parts[1]) {
		return fmt.Errorf("workspace Git remote must use https://github.com/OWNER/REPOSITORY[.git]")
	}
	return nil
}

func safeGitHubSegment(value string) bool {
	if strings.HasSuffix(value, ".git") {
		value = strings.TrimSuffix(value, ".git")
	}
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("._-", character)) {
			return false
		}
	}
	return true
}

func validateGitBranch(branch string) error {
	if branch == "" || strings.HasPrefix(branch, ".") || strings.HasPrefix(branch, "-") || strings.Contains(branch, "..") || strings.ContainsAny(branch, " ~^:?*[\\\"\r\n") {
		return fmt.Errorf("workspace Git branch is invalid")
	}
	for _, character := range branch {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("._/-", character)) {
			return fmt.Errorf("workspace Git branch is invalid")
		}
	}
	return nil
}

func validateWorkspaceGitMetadata(options WorkspaceGitOptions) error {
	if options.Schedule == "" || len(options.Schedule) > 120 || strings.ContainsAny(options.Schedule, "\r\n") {
		return fmt.Errorf("workspace Git schedule is invalid")
	}
	if options.AuthorName == "" || len(options.AuthorName) > 200 || strings.ContainsAny(options.AuthorName, "\r\n") {
		return fmt.Errorf("workspace Git author name is invalid")
	}
	if options.AuthorEmail == "" || len(options.AuthorEmail) > 254 || !strings.Contains(options.AuthorEmail, "@") || strings.ContainsAny(options.AuthorEmail, "\r\n") {
		return fmt.Errorf("workspace Git author email is invalid")
	}
	return nil
}

func gitRemote(runGit func(...string) (CommandResult, error)) string {
	result, err := runGit("remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(result.Stdout))
}

func remoteBranchExists(runGit func(...string) (CommandResult, error), branch string) (bool, error) {
	result, err := runGit("ls-remote", "--heads", "origin", branch)
	if err != nil && result.ExitCode != 2 {
		return false, fmt.Errorf("could not read the configured GitHub remote; check the PAT and repository permissions")
	}
	return strings.TrimSpace(string(result.Stdout)) != "", nil
}

func isGitAncestor(runGit func(...string) (CommandResult, error), older, newer string) bool {
	_, err := runGit("merge-base", "--is-ancestor", older, newer)
	return err == nil
}

func commitStagedChanges(runGit func(...string) (CommandResult, error), message string) error {
	result, err := runGit("diff", "--cached", "--quiet")
	if err != nil && result.ExitCode != 1 {
		return fmt.Errorf("could not inspect staged workspace changes")
	}
	if result.ExitCode == 0 && err == nil {
		return nil
	}
	if err := stagedPathsAreSafe(runGit); err != nil {
		unstageProtectedWorkspacePaths(runGit, true)
		return err
	}
	if _, err := runGit("commit", "-m", message); err != nil {
		return fmt.Errorf("workspace Git commit failed")
	}
	return nil
}

func configureWorkspaceRemoteState(runGit func(...string) (CommandResult, error), enabled bool) error {
	if _, err := runGit("config", "--local", "openlia.workspace-remote-enabled", fmt.Sprintf("%t", enabled)); err != nil {
		return fmt.Errorf("workspace Git configuration failed")
	}
	return nil
}

func unstageProtectedWorkspacePaths(runGit func(...string) (CommandResult, error), hasHead bool) {
	result, err := runGit("diff", "--cached", "--name-only")
	if err != nil {
		return
	}
	for _, path := range strings.Split(strings.TrimSpace(string(result.Stdout)), "\n") {
		if path == "" || !protectedWorkspacePath(path) {
			continue
		}
		if hasHead {
			_, _ = runGit("reset", "HEAD", "--", path)
		} else {
			_, _ = runGit("rm", "--cached", "--ignore-unmatch", "--", path)
		}
	}
}

func stagedPathsAreSafe(runGit func(...string) (CommandResult, error)) error {
	result, err := runGit("diff", "--cached", "--name-only")
	if err != nil {
		return fmt.Errorf("could not inspect staged workspace paths")
	}
	for _, path := range strings.Split(strings.TrimSuffix(string(result.Stdout), "\n"), "\n") {
		if path != "" && protectedWorkspacePath(path) {
			return fmt.Errorf("workspace Git refused a credential or runtime path: %s", path)
		}
	}
	return nil
}

func protectedWorkspacePath(path string) bool {
	return path == ".env" || strings.HasPrefix(path, ".env.") || strings.HasSuffix(path, ".env") || strings.Contains(path, ".secret") || strings.HasSuffix(path, ".pem") || strings.HasSuffix(path, ".key") || strings.HasSuffix(path, ".p12") || strings.HasSuffix(path, ".pfx") || path == "auth.json" || strings.HasSuffix(path, "/auth.json") || strings.HasPrefix(path, "sessions/") || strings.HasPrefix(path, "logs/") || strings.HasPrefix(path, "cache/") || strings.HasPrefix(path, "browser-profile/") || strings.HasPrefix(path, "mcp-tokens/") || strings.HasPrefix(path, "pairing/")
}

func configureWorkspaceCron(runHermes func(...string) (CommandResult, error), schedule string) error {
	if _, err := runHermes("cron", "edit", "openlia-workspace-git", "--schedule", schedule); err == nil {
		return nil
	}
	if _, err := runHermes("cron", "create", schedule, "--no-agent", "--script", "openlia-workspace-git-sync.sh", "--workdir", workspacePath, "--deliver", "local", "--name", "openlia-workspace-git"); err != nil {
		return fmt.Errorf("workspace Git cron configuration failed")
	}
	return nil
}
