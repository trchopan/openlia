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

func TestWorkspaceMigrateMergeRequiresInteractiveWithoutAutoApprove(t *testing.T) {
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "config")
	_ = os.MkdirAll(configDir, 0o700)

	// In non-interactive mode without --auto-approve, merge should fail
	options := Options{JSON: true, NonInteractive: true}
	code := commandWorkspaceMigrateMerge(options, []string{"mig-nonexistent"})
	// If config not initialized, will return ExitPrereq or ExitUsage
	if code == ExitOK {
		t.Fatalf("merge in non-interactive mode should not succeed without auto-approve")
	}
}
