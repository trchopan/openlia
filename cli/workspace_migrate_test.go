package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceMigrateRequiresSubcommandOrPath(t *testing.T) {
	options := Options{JSON: true}
	code := commandWorkspaceMigrate(options, []string{})
	if code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
}

func TestWorkspaceMigrateUploadRejectsMissingFolder(t *testing.T) {
	options := Options{JSON: true}
	code := commandWorkspaceMigrateUpload(options, []string{})
	if code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}

	code = commandWorkspaceMigrateUpload(options, []string{"/non/existent/path/for/sure"})
	if code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
}

func TestWorkspaceMigrateMergeRejectsAutoApproveAndRequiresInteractive(t *testing.T) {
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "config")
	_ = os.MkdirAll(configDir, 0o700)

	// --auto-approve should be rejected as an unknown flag
	options := Options{JSON: true}
	code := commandWorkspaceMigrateMerge(options, []string{"--auto-approve", "mig-nonexistent"})
	if code != ExitUsage {
		t.Fatalf("merge with --auto-approve exit code = %d, want %d", code, ExitUsage)
	}

	// In non-interactive mode, merge should fail
	options = Options{JSON: true, NonInteractive: true}
	code = commandWorkspaceMigrateMerge(options, []string{"mig-nonexistent"})
	if code == ExitOK {
		t.Fatalf("merge in non-interactive mode should not succeed")
	}
}
