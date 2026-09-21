package operator

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type recordedRunner struct {
	calls []string
}

func (r *recordedRunner) Run(_ context.Context, name string, args ...string) (CommandResult, error) {
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	if name == "docker" && len(args) >= 2 && args[0] == "info" {
		return CommandResult{Stdout: []byte("linux\n")}, nil
	}
	if name == "docker" && len(args) >= 2 && args[0] == "network" && args[1] == "inspect" {
		return CommandResult{ExitCode: 1}, fmt.Errorf("network is absent")
	}
	return CommandResult{}, nil
}

func TestValidateRuntimeDoesNotRequireTargetArchiveTools(t *testing.T) {
	repo := t.TempDir()
	config := testConfig(repo, filepath.Join(t.TempDir(), "runtime"))
	runner := &recordedRunner{}
	if _, err := ValidateRuntime(context.Background(), config, runner); err != nil {
		t.Fatal(err)
	}
	pythonCommand := "py" + "thon3 "
	tarCommand := "t" + "ar "
	for _, call := range runner.calls {
		if strings.HasPrefix(call, pythonCommand) || strings.HasPrefix(call, tarCommand) {
			t.Fatalf("target archive prerequisite was invoked: %s", call)
		}
	}
}

func TestProtectedSkillRefreshRecreatesOnlyRunningHermes(t *testing.T) {
	config := testConfig(t.TempDir(), filepath.Join(t.TempDir(), "runtime"))
	if err := os.MkdirAll(config.MetaRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteState(config, stateRunning); err != nil {
		t.Fatal(err)
	}
	runner := &recordedRunner{}
	if err := refreshProtectedSkillRuntime(context.Background(), config, NewCompose(config, runner)); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 || !strings.Contains(runner.calls[0], "up -d --no-deps --force-recreate hermes") {
		t.Fatalf("unexpected protected skill refresh calls: %v", runner.calls)
	}
}

func TestBackupExcludesSecretsAndAttachmentCapabilities(t *testing.T) {
	config := testConfig(t.TempDir(), filepath.Join(t.TempDir(), "runtime"))
	for _, directory := range []string{config.DataRoot, config.LochoRoot, config.MetaRoot, config.BackupRoot, config.SecretDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(config.DataRoot, "keep.txt"):                        "keep",
		filepath.Join(config.DataRoot, "auth.json"):                       "secret",
		filepath.Join(config.DataRoot, "session.env"):                     "secret",
		filepath.Join(config.LochoRoot, "laptop", "attachments.toml"):     "capability",
		filepath.Join(config.LochoRoot, "laptop", "runtime.txt"):          "runtime",
		filepath.Join(config.RuntimeRoot, "secrets", "hermes.env"):        "secret",
		filepath.Join(config.RuntimeRoot, "hermes", "logs", "hermes.log"): "log",
	}
	for path, contents := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := CreateBackup(config, "test", time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	names := archiveNames(t, result.Archive)
	if !names["hermes/keep.txt"] || !names["locho/laptop/runtime.txt"] {
		t.Fatalf("ordinary runtime files missing from archive: %v", names)
	}
	for _, forbidden := range []string{"hermes/auth.json", "hermes/session.env", "locho/laptop/attachments.toml", "secrets/hermes.env", "hermes/logs/hermes.log"} {
		if names[forbidden] {
			t.Fatalf("sensitive archive member present: %s", forbidden)
		}
	}
}

func TestRestoreRejectsUnsafeArchiveBeforeChangingState(t *testing.T) {
	config := testConfig(t.TempDir(), filepath.Join(t.TempDir(), "runtime"))
	for _, directory := range []string{config.DataRoot, config.MetaRoot, config.BackupRoot, config.LochoRoot} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	current := filepath.Join(config.DataRoot, "keep.txt")
	if err := os.WriteFile(current, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(config.BackupRoot, "openlia-unsafe.tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "../escape", Mode: 0o600, Size: 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = tarWriter.Write([]byte("x"))
	_ = tarWriter.Close()
	_ = gzipWriter.Close()
	_ = file.Close()
	if _, err := RestoreBackup(config, archive, time.Now()); err == nil {
		t.Fatal("unsafe archive was accepted")
	}
	contents, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "current" {
		t.Fatalf("current data changed after rejected archive: %q", contents)
	}
}

func TestRestoreCreatesPreflightBackupWithoutOverwritingSelectedArchive(t *testing.T) {
	config := testConfig(t.TempDir(), filepath.Join(t.TempDir(), "runtime"))
	for _, directory := range []string{config.DataRoot, config.MetaRoot, config.BackupRoot, config.LochoRoot} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(config.DataRoot, "value.txt")
	if err := os.WriteFile(path, []byte("archived"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive, err := CreateBackup(config, "restore-test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreBackup(config, archive.Archive, time.Now()); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "archived" {
		t.Fatalf("restored contents = %q, want archived", contents)
	}
}

func TestGeneratedAttachmentsContainLochoBuildAndHardening(t *testing.T) {
	repo := t.TempDir()
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	config := testConfig(repo, runtimeRoot)
	if err := os.MkdirAll(filepath.Join(repo, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}
	attachment := filepath.Join(config.LochoRoot, "laptop", "attachments.toml")
	if err := os.MkdirAll(filepath.Dir(attachment), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attachment, []byte("host_id = \"laptop\"\nlisten_host = \"127.0.0.1\"\n[[services]]\ncapability = \"genai:http:capability\"\nlisten_port = 8088\n[[services]]\ncapability = \"playwright:tcp:capability\"\nlisten_port = 8931\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := GenerateAttachments(config); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(config.GeneratedCompose)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{"locho-laptop:", "openlia-tools:", "build:", "docker/locho.Dockerfile", "docker/tools.Dockerfile", "cap_drop: [ALL]", "no-new-privileges:true", "OPENLIA_BROWSER_MCP_URL: \"http://locho-laptop:8931\"", "OPENLIA_TOOLS_URL: \"http://openlia-tools:8787\""} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated Compose missing %q:\n%s", expected, text)
		}
	}
}

func TestWorkspaceGitValidationAndProtectedPaths(t *testing.T) {
	for _, remote := range []string{
		"https://github.com/example/private-vault.git",
		"https://github.com/example/private-vault",
	} {
		if err := validateGitHubRemote(remote); err != nil {
			t.Fatalf("valid remote rejected: %v", err)
		}
	}
	for _, remote := range []string{
		"http://github.com/example/private-vault.git",
		"https://evil.example/example/private-vault.git",
		"https://github.com/example/private-vault.git?token=leak",
		"https://github.com/example/private-vault/extra.git",
	} {
		if err := validateGitHubRemote(remote); err == nil {
			t.Fatalf("unsafe remote accepted: %s", remote)
		}
	}
	for _, path := range []string{".env", "nested/auth.json", "logs/hermes.log", "cache/token", "private.key", "notes.md"} {
		want := path != "notes.md"
		if protectedWorkspacePath(path) != want {
			t.Fatalf("protectedWorkspacePath(%q) = %t, want %t", path, protectedWorkspacePath(path), want)
		}
	}
}

func TestUninstallAcceptsCanonicalizedTemporaryRootSymlink(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(t.TempDir(), "openlia")
	config := testConfig(repo, filepath.Join(root, "runtime"))
	config.InstallRoot = root
	config.ProjectName = "test-project"
	config.NetworkName = "test-project-private"
	if err := os.MkdirAll(filepath.Join(root, "releases", "0.1.0"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "releases", "0.1.0"), filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.MetaRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(config.ComposeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ComposeFile, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteRuntimeMetadata(config, time.Now()); err != nil {
		t.Fatal(err)
	}
	result, err := Uninstall(context.Background(), config, NewCompose(config, &recordedRunner{}))
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "removed" {
		t.Fatalf("uninstall state = %q, want removed", result.State)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("installation root still exists, stat error = %v", err)
	}
}

func archiveNames(t *testing.T, path string) map[string]bool {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	decompressor, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(decompressor)
	result := map[string]bool{}
	for {
		header, err := reader.Next()
		if err != nil {
			break
		}
		result[strings.TrimSuffix(header.Name, "/")] = true
	}
	_ = decompressor.Close()
	_ = file.Close()
	return result
}

func TestPruneBackups(t *testing.T) {
	config := testConfig(t.TempDir(), filepath.Join(t.TempDir(), "runtime"))
	if err := os.MkdirAll(config.BackupRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	baseTime := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	// Create 7 backup archives with .json pairs spaced 1 minute apart
	for i := 1; i <= 7; i++ {
		archivePath := filepath.Join(config.BackupRoot, fmt.Sprintf("openlia-20260921T10000%dZ-%d.tar.gz", i, i))
		if err := os.WriteFile(archivePath, []byte(fmt.Sprintf("archive-%d", i)), 0o600); err != nil {
			t.Fatal(err)
		}
		jsonPath := archivePath + ".json"
		if err := os.WriteFile(jsonPath, []byte(fmt.Sprintf("meta-%d", i)), 0o600); err != nil {
			t.Fatal(err)
		}
		modTime := baseTime.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(archivePath, modTime, modTime); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(jsonPath, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}

	// Create 4 compose-generated files
	for i := 1; i <= 4; i++ {
		composePath := filepath.Join(config.BackupRoot, fmt.Sprintf("compose-generated-20260921T10000%dZ-%d", i, i))
		if err := os.WriteFile(composePath, []byte(fmt.Sprintf("compose-%d", i)), 0o600); err != nil {
			t.Fatal(err)
		}
		modTime := baseTime.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(composePath, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}

	// Prune keeping 3
	removed, err := PruneBackups(config, 3)
	if err != nil {
		t.Fatalf("PruneBackups failed: %v", err)
	}

	// 4 archives (1, 2, 3, 4) and 1 compose file (1) should be removed = 5 total
	if len(removed) != 5 {
		t.Fatalf("expected 5 removed files, got %d: %v", len(removed), removed)
	}

	// Verify archives 1..4 are gone, 5..7 remain
	for i := 1; i <= 4; i++ {
		archivePath := filepath.Join(config.BackupRoot, fmt.Sprintf("openlia-20260921T10000%dZ-%d.tar.gz", i, i))
		if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
			t.Fatalf("expected archive %d to be deleted, but it exists", i)
		}
		if _, err := os.Stat(archivePath + ".json"); !os.IsNotExist(err) {
			t.Fatalf("expected metadata %d to be deleted, but it exists", i)
		}
	}
	for i := 5; i <= 7; i++ {
		archivePath := filepath.Join(config.BackupRoot, fmt.Sprintf("openlia-20260921T10000%dZ-%d.tar.gz", i, i))
		if _, err := os.Stat(archivePath); err != nil {
			t.Fatalf("expected archive %d to exist, but stat error: %v", i, err)
		}
		if _, err := os.Stat(archivePath + ".json"); err != nil {
			t.Fatalf("expected metadata %d to exist, but stat error: %v", i, err)
		}
	}

	// Verify compose-generated-1 is gone, 2..4 remain
	compose1 := filepath.Join(config.BackupRoot, "compose-generated-20260921T100001Z-1")
	if _, err := os.Stat(compose1); !os.IsNotExist(err) {
		t.Fatal("expected compose-generated-1 to be deleted")
	}
	for i := 2; i <= 4; i++ {
		composePath := filepath.Join(config.BackupRoot, fmt.Sprintf("compose-generated-20260921T10000%dZ-%d", i, i))
		if _, err := os.Stat(composePath); err != nil {
			t.Fatalf("expected compose file %d to exist: %v", i, err)
		}
	}
}
