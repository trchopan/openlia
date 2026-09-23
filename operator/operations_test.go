package operator

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
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

type workspaceUIRunner struct {
	calls []string
}

func (r *workspaceUIRunner) Run(_ context.Context, name string, args ...string) (CommandResult, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, call)
	if strings.Contains(call, "ps --services --filter status=running") {
		return CommandResult{Stdout: []byte("workspace-ui\n")}, nil
	}
	if strings.Contains(call, "port workspace-ui 8089") {
		return CommandResult{Stdout: []byte("127.0.0.1:8089\n")}, nil
	}
	return CommandResult{}, nil
}

type openWebUIRunner struct {
	calls []string
}

func (r *openWebUIRunner) Run(_ context.Context, name string, args ...string) (CommandResult, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, call)
	if strings.Contains(call, "ps --services --filter status=running") {
		return CommandResult{Stdout: []byte("open-webui\n")}, nil
	}
	if strings.Contains(call, "port open-webui 8080") {
		return CommandResult{Stdout: []byte("127.0.0.1:8090\n")}, nil
	}
	return CommandResult{}, nil
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

func TestDeployWorkspaceUIComponentTargetsOnlyWorkspaceUI(t *testing.T) {
	repo := t.TempDir()
	runtime := filepath.Join(t.TempDir(), "runtime")
	config := testConfig(repo, runtime)
	config.WorkspaceUIHost = "127.0.0.1"
	if err := os.MkdirAll(filepath.Dir(config.ComposeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ComposeFile, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.SecretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.SecretFile, []byte("# test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.MetaRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteState(config, stateNeverStarted); err != nil {
		t.Fatal(err)
	}
	runner := &workspaceUIRunner{}
	if _, err := Deploy(context.Background(), config, NewCompose(config, runner), DeployOptions{Action: "start", Component: "workspace-ui", HealthAttempts: 1}, time.Now()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "up -d --no-deps --force-recreate workspace-ui") {
		t.Fatalf("workspace-ui target was not started directly: %s", joined)
	}
	if strings.Contains(joined, "hermes") || strings.Contains(joined, "locho") {
		t.Fatalf("targeted workspace-ui deploy touched another service: %s", joined)
	}
}

func TestDeployOpenWebUIComponentTargetsOnlyOpenWebUI(t *testing.T) {
	repo := t.TempDir()
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	config := testConfig(repo, runtimeRoot)
	config.OpenWebUIHost = "127.0.0.1"
	config.OpenWebUIPort = 8090
	if err := os.MkdirAll(filepath.Join(repo, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ComposeFile, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.SecretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.SecretFile, []byte("# test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.MetaRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteState(config, stateNeverStarted); err != nil {
		t.Fatal(err)
	}
	runner := &openWebUIRunner{}
	if _, err := Deploy(context.Background(), config, NewCompose(config, runner), DeployOptions{Action: "start", Component: "open-webui", HealthAttempts: 1}, time.Now()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "up -d --no-deps --force-recreate open-webui") {
		t.Fatalf("open-webui target was not started directly: %s", joined)
	}
	if strings.Contains(joined, "hermes") || strings.Contains(joined, "locho") || strings.Contains(joined, "workspace-ui") {
		t.Fatalf("targeted open-webui deploy touched another service: %s", joined)
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
	for _, expected := range []string{"locho-laptop:", "openlia-tools:", "build:", "docker/locho.Dockerfile", "docker/tools.Dockerfile", "command: [\"bun\", \"/opt/openlia/tools/server.js\"]", "cap_drop: [ALL]", "no-new-privileges:true", "OPENLIA_BROWSER_MCP_URL: \"http://locho-laptop:8931\"", "OPENLIA_TOOLS_URL: \"http://openlia-tools:8787\""} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated Compose missing %q:\n%s", expected, text)
		}
	}
}

func TestGeneratedAttachmentsContainWorkspaceUIWhenEnabled(t *testing.T) {
	for _, test := range []struct {
		name      string
		host      string
		port      int
		published string
	}{
		{name: "loopback", host: "127.0.0.1", port: 8089, published: "127.0.0.1:8089:8089"},
		{name: "all interfaces", host: "0.0.0.0", port: 8090, published: "0.0.0.0:8090:8090"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := t.TempDir()
			runtimeRoot := filepath.Join(t.TempDir(), "runtime")
			config := testConfig(repo, runtimeRoot)
			config.WorkspaceUIHost = test.host
			config.WorkspaceUIPort = test.port
			config.WorkspaceUIAuthRequired = test.host == "0.0.0.0"
			config.WorkspaceUIPasswordHashFile = filepath.Join(runtimeRoot, "secrets", "workspace-ui-password.hash")
			if err := os.MkdirAll(filepath.Join(repo, "docker"), 0o755); err != nil {
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
			for _, expected := range []string{"workspace-ui:", "docker/workspace-ui.Dockerfile", test.published, fmt.Sprintf("OPENLIA_WORKSPACE_UI_PORT: \"%d\"", test.port), "target: /workspace", "user: \"10000:10000\"", "cap_drop: [ALL]"} {
				if !strings.Contains(text, expected) {
					t.Fatalf("generated Compose missing %q:\n%s", expected, text)
				}
			}
			if test.host == "0.0.0.0" {
				for _, expected := range []string{"OPENLIA_WORKSPACE_UI_AUTH_REQUIRED: \"true\"", "target: /run/openlia-secrets/workspace-ui-password.hash", "read_only: true"} {
					if !strings.Contains(text, expected) {
						t.Fatalf("generated public Workspace UI Compose missing %q:\n%s", expected, text)
					}
				}
			} else if strings.Contains(text, "OPENLIA_WORKSPACE_UI_AUTH_REQUIRED") {
				t.Fatal("loopback Workspace UI unexpectedly enabled authentication")
			}
		})
	}
}

func TestGeneratedAttachmentsContainOpenWebUI(t *testing.T) {
	repo := t.TempDir()
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	config := testConfig(repo, runtimeRoot)
	config.OpenWebUIHost = "127.0.0.1"
	config.OpenWebUIPort = 8090
	config.OpenWebUIImage = "ghcr.io/open-webui/open-webui:main"
	config.OpenWebUIAuth = true
	if err := os.MkdirAll(filepath.Join(repo, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.SecretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.SecretFile, []byte("COPILOT_GITHUB_TOKEN=gho_testtoken\n"), 0o600); err != nil {
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
	for _, expected := range []string{
		"open-webui:",
		"ghcr.io/open-webui/open-webui:main",
		"127.0.0.1:8090:8080",
		"target: /app/backend/data",
		"OPENAI_API_BASE_URL: \"http://hermes:8642/v1\"",
		"ENABLE_OLLAMA_API: \"False\"",
		"WEBUI_NAME: \"OpenLia\"",
		"WEBUI_AUTH: \"True\"",
		"API_SERVER_ENABLED: \"true\"",
		"API_SERVER_HOST: \"0.0.0.0\"",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated Compose missing %q:\n%s", expected, text)
		}
	}

	// Verify secrets were provisioned
	openWebUIEnvPath := filepath.Join(config.SecretDir, "open-webui.env")
	envData, err := os.ReadFile(openWebUIEnvPath)
	if err != nil {
		t.Fatalf("open-webui.env missing: %v", err)
	}
	envText := string(envData)
	if !strings.Contains(envText, "OPENAI_API_KEY=sk-openlia-") || !strings.Contains(envText, "WEBUI_SECRET_KEY=") {
		t.Fatalf("open-webui.env missing expected keys:\n%s", envText)
	}

	apiKey, err := readSecretValue(openWebUIEnvPath, "OPENAI_API_KEY")
	if err != nil || apiKey == "" {
		t.Fatalf("failed to read OPENAI_API_KEY: %v", err)
	}
	serverKey, err := readSecretValue(config.SecretFile, "API_SERVER_KEY")
	if err != nil || serverKey == "" {
		t.Fatalf("failed to read API_SERVER_KEY: %v", err)
	}
	if apiKey != serverKey {
		t.Fatalf("key mismatch: open-webui=%q hermes=%q", apiKey, serverKey)
	}

	// Ensure secret token NEVER leaks into generated compose
	if strings.Contains(text, apiKey) {
		t.Fatalf("API key %q leaked into generated Compose file:\n%s", apiKey, text)
	}
}

func TestGeneratedAttachmentsDisableHermesBrowserToolsetForPlaywrightRole(t *testing.T) {
	repo := t.TempDir()
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	config := testConfig(repo, runtimeRoot)
	config.ServiceRoles = map[string]string{"laptop.playwright": "playwright-browser"}
	if err := os.MkdirAll(filepath.Join(repo, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}
	attachment := filepath.Join(config.LochoRoot, "laptop", "attachments.toml")
	if err := os.MkdirAll(filepath.Dir(attachment), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attachment, []byte("host_id = \"laptop\"\nlisten_host = \"127.0.0.1\"\n[[services]]\ncapability = \"playwright:tcp:capability\"\nlisten_port = 8931\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.DataRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(config.DataRoot, "config.yaml")
	if err := os.WriteFile(configPath, []byte("browser:\n  backend: \"off\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := GenerateAttachments(config); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "disabled_toolsets:\n    - browser") || !strings.Contains(string(data), "url: \"http://locho-laptop:8931/sse\"") || !strings.Contains(string(data), "transport: \"sse\"") {
		t.Fatalf("browser toolset was not disabled:\n%s", data)
	}
	config.ServiceRoles["laptop.playwright"] = "unassigned"
	if err := GenerateAttachments(config); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), managedBrowserPolicyStart) || strings.Contains(string(data), "disabled_toolsets:\n    - browser") {
		t.Fatalf("managed browser policy was not removed:\n%s", data)
	}
}

func TestBrowserPolicyRefusesUserAgentMapping(t *testing.T) {
	data := []byte("agent:\n  disabled_toolsets: [terminal]\n")
	if _, _, err := renderBrowserPolicy(data, true, "http://locho-laptop:8931"); err == nil {
		t.Fatal("browser policy overwrote a user-owned agent mapping")
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

func TestLochoServiceRegistryAndRoleMapping(t *testing.T) {
	repo := t.TempDir()
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	config := testConfig(repo, runtimeRoot)
	if err := os.MkdirAll(filepath.Join(repo, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}
	config.ServiceRoles = map[string]string{
		"laptop.playwright": "playwright-browser",
		"laptop.ollama":     "openai-gateway",
	}
	attachmentDir := filepath.Join(config.LochoRoot, "laptop")
	if err := os.MkdirAll(attachmentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	attachmentContent := `host_id = "laptop"
listen_host = "0.0.0.0"

[[services]]
capability = "playwright:tcp:supersecrettoken1"
listen_port = 8931

[[services]]
capability = "ollama:http:supersecrettoken2"
listen_port = 11434

[[services]]
capability = "genai:http:supersecrettoken3"
listen_port = 8765

[[services]]
capability = "ssh:tcp:supersecrettoken4"
listen_port = 2222
`
	if err := os.WriteFile(filepath.Join(attachmentDir, "attachments.toml"), []byte(attachmentContent), 0o600); err != nil {
		t.Fatal(err)
	}

	listResult, err := ListAttachments(config)
	if err != nil {
		t.Fatalf("ListAttachments failed: %v", err)
	}
	if len(listResult.Hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(listResult.Hosts))
	}
	host := listResult.Hosts[0]
	if len(host.Services) != 4 {
		t.Fatalf("expected 4 services, got %d", len(host.Services))
	}

	roleMap := make(map[string]string)
	endpointMap := make(map[string]string)
	for _, svc := range host.Services {
		roleMap[svc.Name] = svc.Role
		endpointMap[svc.Name] = svc.Endpoint
	}
	if roleMap["playwright"] != "playwright-browser" {
		t.Errorf("playwright role = %q, want 'playwright-browser'", roleMap["playwright"])
	}
	if roleMap["ollama"] != "openai-gateway" {
		t.Errorf("ollama role = %q, want 'openai-gateway'", roleMap["ollama"])
	}
	if roleMap["genai"] != "unassigned" {
		t.Errorf("genai role = %q, want 'unassigned'", roleMap["genai"])
	}
	if roleMap["ssh"] != "unassigned" {
		t.Errorf("ssh role = %q, want 'unassigned'", roleMap["ssh"])
	}

	if endpointMap["playwright"] != "http://locho-laptop:8931" {
		t.Errorf("playwright endpoint = %q, want 'http://locho-laptop:8931'", endpointMap["playwright"])
	}
	if endpointMap["ollama"] != "http://locho-laptop:11434" {
		t.Errorf("ollama endpoint = %q, want 'http://locho-laptop:11434'", endpointMap["ollama"])
	}
	if endpointMap["ssh"] != "locho-laptop:2222" {
		t.Errorf("ssh endpoint = %q, want 'locho-laptop:2222'", endpointMap["ssh"])
	}

	if err := GenerateAttachments(config); err != nil {
		t.Fatalf("GenerateAttachments failed: %v", err)
	}

	// Verify services.json in DataRoot
	dataRegistryPath := filepath.Join(config.DataRoot, "services.json")
	dataRegistryBytes, err := os.ReadFile(dataRegistryPath)
	if err != nil {
		t.Fatalf("failed to read data services.json: %v", err)
	}
	var registry ServiceRegistry
	if err := json.Unmarshal(dataRegistryBytes, &registry); err != nil {
		t.Fatalf("failed to unmarshal services.json: %v", err)
	}
	if len(registry.Services) != 4 {
		t.Fatalf("services.json contains %d services, want 4", len(registry.Services))
	}

	// Ensure secret tokens NEVER leak into registry or compose file
	for _, forbidden := range []string{"supersecrettoken1", "supersecrettoken2", "supersecrettoken3", "supersecrettoken4"} {
		if strings.Contains(string(dataRegistryBytes), forbidden) {
			t.Fatalf("services.json leaked forbidden token %q", forbidden)
		}
	}

	// Verify generated Compose
	composeBytes, err := os.ReadFile(config.GeneratedCompose)
	if err != nil {
		t.Fatalf("failed to read generated compose: %v", err)
	}
	composeText := string(composeBytes)
	for _, forbidden := range []string{"supersecrettoken1", "supersecrettoken2", "supersecrettoken3", "supersecrettoken4"} {
		if strings.Contains(composeText, forbidden) {
			t.Fatalf("compose leaked forbidden token %q", forbidden)
		}
	}

	for _, expected := range []string{
		"OPENLIA_BROWSER_MCP_URL: \"http://locho-laptop:8931\"",
		"OPENLIA_SERVICE_LAPTOP_PLAYWRIGHT_URL: \"http://locho-laptop:8931\"",
		"OPENLIA_SERVICE_LAPTOP_OLLAMA_URL: \"http://locho-laptop:11434\"",
	} {
		if !strings.Contains(composeText, expected) {
			t.Errorf("generated Compose missing %q:\n%s", expected, composeText)
		}
	}
	if strings.Contains(composeText, "OPENAI_BASE_URL") {
		t.Fatalf("generated Compose must not configure OPENAI_BASE_URL:\n%s", composeText)
	}
}
