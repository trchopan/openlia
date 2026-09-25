package operator

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWorkspaceGitSyncHelper(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	_, sourceFile, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(sourceFile), "..", "profile", "cron", "scripts", "openlia-workspace-git-sync.sh")
	testRoot := t.TempDir()
	remote := filepath.Join(testRoot, "remote.git")
	publisher := filepath.Join(testRoot, "publisher")
	workspace := filepath.Join(testRoot, "workspace")
	runGit(t, "init", "--bare", remote)
	runGit(t, "clone", remote, publisher)
	runGit(t, "-C", publisher, "switch", "-c", "main")
	runGit(t, "-C", publisher, "config", "user.name", "Test")
	runGit(t, "-C", publisher, "config", "user.email", "test@example.test")
	writeFile(t, filepath.Join(publisher, "record.md"), "initial\n")
	runGit(t, "-C", publisher, "add", "--all", "--", ".")
	runGit(t, "-C", publisher, "commit", "-m", "initial")
	runGit(t, "-C", publisher, "push", "--set-upstream", "origin", "main")
	runGit(t, "clone", "--branch", "main", remote, workspace)
	runGit(t, "-C", workspace, "config", "user.name", "Test")
	runGit(t, "-C", workspace, "config", "user.email", "test@example.test")
	runGit(t, "-C", workspace, "config", "openlia.workspace-branch", "main")

	writeFile(t, filepath.Join(workspace, "local.md"), "local\n")
	dirtyOutput := runSync(t, script, workspace)
	if !bytes.Contains(dirtyOutput, []byte("skipped_dirty")) {
		t.Fatalf("dirty sync output = %s", dirtyOutput)
	}
	os.Remove(filepath.Join(workspace, "local.md"))
	runGit(t, "-C", workspace, "config", "openlia.workspace-remote-enabled", "false")
	disabledOutput := runSync(t, script, workspace)
	if !bytes.Contains(disabledOutput, []byte(`"status":"disabled"`)) {
		t.Fatalf("disabled sync output = %s", disabledOutput)
	}
	runGit(t, "-C", workspace, "config", "openlia.workspace-remote-enabled", "true")

	writeFile(t, filepath.Join(publisher, "remote.md"), "remote\n")
	runGit(t, "-C", publisher, "add", "--all", "--", ".")
	runGit(t, "-C", publisher, "commit", "-m", "remote")
	runGit(t, "-C", publisher, "push", "origin", "main")
	pullOutput := runSync(t, script, workspace)
	if !bytes.Contains(pullOutput, []byte("fast_forwarded")) {
		t.Fatalf("pull output = %s", pullOutput)
	}

	writeFile(t, filepath.Join(publisher, "AGENTS.md"), "remote instruction\n")
	runGit(t, "-C", publisher, "add", "--all", "--", ".")
	runGit(t, "-C", publisher, "commit", "-m", "instructions")
	runGit(t, "-C", publisher, "push", "origin", "main")
	command := exec.Command("bash", script)
	command.Env = append(os.Environ(), "OPENLIA_WORKSPACE_GIT_ROOT="+workspace)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("protected sync succeeded: %s", output)
	}
	if _, err := os.Stat(filepath.Join(workspace, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("protected file was pulled, stat error = %v", err)
	}
}

func runSync(t *testing.T, script, workspace string) []byte {
	t.Helper()
	command := exec.Command("bash", script)
	command.Env = append(os.Environ(), "OPENLIA_WORKSPACE_GIT_ROOT="+workspace)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("sync failed: %v\n%s", err, output)
	}
	return output
}

func runGit(t *testing.T, args ...string) []byte {
	t.Helper()
	command := exec.Command("git", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return output
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
