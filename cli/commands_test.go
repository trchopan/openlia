package cli

import (
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
