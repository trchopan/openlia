package cli

import (
	"path/filepath"
	"testing"
	"testing/fstest"
)

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
		{"map", "laptop", "ollama", "--wrong", "openai-endpoint"},
	} {
		code := commandAttachments(Options{}, args)
		if code != ExitUsage {
			t.Fatalf("expected ExitUsage for args %v, got %d", args, code)
		}
	}
}
