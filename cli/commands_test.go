package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestDeploymentResultRunningGate(t *testing.T) {
	if !deploymentResultIsRunning([]byte(`{"state":"running"}`)) {
		t.Fatal("running deployment did not enable workspace Git reconciliation")
	}
	for _, raw := range [][]byte{[]byte(`{"state":"stopped"}`), []byte(`{"state":"never-started"}`), []byte(`not-json`)} {
		if deploymentResultIsRunning(raw) {
			t.Fatalf("non-running deployment enabled workspace Git reconciliation: %s", raw)
		}
	}
}

func TestDeploymentResultStateRejectsMissingOrInvalidState(t *testing.T) {
	for _, raw := range [][]byte{[]byte(`{}`), []byte(`not-json`)} {
		if state, ok := deploymentResultState(raw); ok || state != "" {
			t.Fatalf("deployment state %q, %t for %s", state, ok, raw)
		}
	}
}

func TestSkillSourceAddAndRemoveDispatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("OPENLIA_CONFIG", path)
	if err := saveConfig(defaultConfig()); err != nil {
		t.Fatal(err)
	}
	if code := commandSkillSources(Options{}, []string{"add", "team", "--repository", "https://github.com/example/skills", "--branch", "release/v2"}); code != ExitOK {
		t.Fatalf("add exit code = %d", code)
	}
	config, err := loadConfig()
	if err != nil || len(config.SkillSources) != 1 || config.SkillSources[0].Name != "team" {
		t.Fatalf("source was not persisted: %#v, %v", config.SkillSources, err)
	}
	if code := commandSkillSources(Options{}, []string{"remove", "team"}); code != ExitOK {
		t.Fatalf("remove exit code = %d", code)
	}
	config, err = loadConfig()
	if err != nil || len(config.SkillSources) != 0 {
		t.Fatalf("source was not removed: %#v, %v", config.SkillSources, err)
	}
}

func TestSkillParsersAcceptExternalIdentifiers(t *testing.T) {
	if !validExternalSkillIdentifier("team/research") || !validSkillIdentifier("team/research") || !validSkillIdentifier("daily-briefing") {
		t.Fatal("valid skill identifier was rejected")
	}
	for _, value := range []string{"team", "team/research/extra", "../research", "team/.hidden", "team/re search"} {
		if validExternalSkillIdentifier(value) {
			t.Fatalf("invalid external identifier %q was accepted", value)
		}
	}
}

func TestSkillMutationsRejectNonInteractiveApproval(t *testing.T) {
	t.Setenv("OPENLIA_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	for _, args := range [][]string{{"install", "team/research"}, {"update", "team/research"}, {"uninstall", "research"}, {"reset", "research"}} {
		if code := commandSkills(Options{NonInteractive: true}, args, fstest.MapFS{}); code != ExitUsage {
			t.Fatalf("skills %v exit code = %d, want %d", args, code, ExitUsage)
		}
	}
}

func TestAuthSetupPersistsPromptedSourceBeforeDeployment(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("OPENLIA_CONFIG", configPath)
	config := defaultConfig()
	config.Mode = "local"
	config.Target = ""
	config.InstallRoot = filepath.Join(t.TempDir(), "missing-deployment")
	if err := saveConfig(config); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "hermes.env")
	if err := os.WriteFile(source, []byte("OPENAI_API_KEY=test-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString(source + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	previousStdin := os.Stdin
	os.Stdin = input
	t.Cleanup(func() {
		os.Stdin = previousStdin
		_ = input.Close()
	})

	if code := commandAuth(Options{}, []string{"setup"}); code != ExitFailure {
		t.Fatalf("auth setup exit code = %d, want deployment failure", code)
	}
	stored, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if stored.SecretSource != source {
		t.Fatalf("persisted secret source = %q, want %q", stored.SecretSource, source)
	}
}

func TestRunDispatchesSkillSources(t *testing.T) {
	t.Setenv("OPENLIA_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	if code := Run([]string{"skill-sources", "list"}, fstest.MapFS{}); code != ExitPrereq {
		t.Fatalf("skill-sources dispatch exit code = %d, want %d", code, ExitPrereq)
	}
}

func TestRunDispatchesBrowser(t *testing.T) {
	t.Setenv("OPENLIA_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	if code := Run([]string{"browser", "status"}, fstest.MapFS{}); code != ExitPrereq {
		t.Fatalf("browser dispatch exit code = %d, want %d", code, ExitPrereq)
	}
}

func TestSkillSourceAddDoesNotPersistEnvironmentToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("OPENLIA_CONFIG", path)
	t.Setenv("OPENLIA_SKILLS_GIT_TOKEN", "github_pat_super_secret")
	if err := saveConfig(defaultConfig()); err != nil {
		t.Fatal(err)
	}
	if code := commandSkillSources(Options{}, []string{"add", "team", "--repository", "https://github.com/example/skills"}); code != ExitOK {
		t.Fatalf("add exit code = %d", code)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "github_pat_super_secret") || strings.Contains(string(data), "OPENLIA_SKILLS_GIT_TOKEN") {
		t.Fatalf("token leaked to config: %s", data)
	}
}

func TestInitRejectsSecretSourceOverride(t *testing.T) {
	if got := commandInit(Options{}, []string{
		"--target", "operator@example.test",
		"--secret-source", "/tmp/hermes.env",
	}, fstest.MapFS{}); got != ExitUsage {
		t.Fatalf("init with --secret-source exit code = %d, want %d", got, ExitUsage)
	}
}

func TestAuthRejectsSourceOverride(t *testing.T) {
	if got := commandAuth(Options{}, []string{"rotate", "--source", "/tmp/hermes.env"}); got != ExitUsage {
		t.Fatalf("auth rotate with --source exit code = %d, want %d", got, ExitUsage)
	}
}

func TestWorkspaceUIPasswordRejectsNonInteractiveSetup(t *testing.T) {
	if got := commandWorkspaceUI(Options{NonInteractive: true}, []string{"password"}); got != ExitUsage {
		t.Fatalf("non-interactive workspace-ui password exit code = %d, want %d", got, ExitUsage)
	}
	if got := commandWorkspaceUI(Options{}, []string{"password", "secret"}); got != ExitUsage {
		t.Fatalf("workspace-ui password argument exit code = %d, want %d", got, ExitUsage)
	}
}

func TestUninstallRequiresExplicitTargetRootAndProject(t *testing.T) {
	if got := commandUninstall(Options{}, nil); got != ExitUsage {
		t.Fatalf("uninstall without deployment arguments exit code = %d, want %d", got, ExitUsage)
	}
}

func TestSkillMigrationApplyRequiresInteractiveApproval(t *testing.T) {
	if got := commandSkillMigration(Options{NonInteractive: true}, []string{"apply", "proposal-1"}); got != ExitUsage {
		t.Fatalf("non-interactive migration apply exit code = %d, want %d", got, ExitUsage)
	}
}

func TestAttachmentsMapRejectsInvalidRole(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("OPENLIA_CONFIG", filepath.Join(temporary, "config.toml"))
	if err := saveConfig(defaultConfig()); err != nil {
		t.Fatal(err)
	}

	for _, invalid := range []string{"generic", "image-generator", "unassigned", "custom", ""} {
		code := commandAttachments(Options{}, []string{"map", "laptop", "ollama", "--role", invalid})
		if code != ExitUsage {
			t.Fatalf("expected ExitUsage for invalid role %q, got %d", invalid, code)
		}
	}
}
func TestAttachmentsMapRejectsMissingArgs(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("OPENLIA_CONFIG", filepath.Join(temporary, "config.toml"))
	if err := saveConfig(defaultConfig()); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"map"},
		{"map", "laptop"},
		{"map", "laptop", "ollama"},
		{"map", "laptop", "ollama", "--wrong", "openai-gateway"},
	} {
		code := commandAttachments(Options{}, args)
		if code != ExitUsage {
			t.Fatalf("expected ExitUsage for args %v, got %d", args, code)
		}
	}
}

func TestUpdateOpenWebUIRejectsUnconfigured(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("OPENLIA_CONFIG", filepath.Join(temporary, "config.toml"))
	if err := saveConfig(defaultConfig()); err != nil {
		t.Fatal(err)
	}
	if got := commandUpdate(Options{}, []string{"open-webui"}, fstest.MapFS{}); got != ExitUsage {
		t.Fatalf("update open-webui exit code = %d, want %d", got, ExitUsage)
	}
}
