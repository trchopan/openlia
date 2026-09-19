package cli

import (
	"strings"
	"testing"
)

func TestOperationCommandIncludesTimezone(t *testing.T) {
	config := defaultConfig()
	config.Target = "operator@example.test"
	command := (Remote{Config: config}).operationCommand("ops/deploy.sh")
	if !strings.Contains(command, "HERMES_TIMEZONE='Asia/Ho_Chi_Minh'") {
		t.Fatalf("operation command does not include configured timezone: %s", command)
	}
}

func TestUninstallCommandUsesCurrentReleaseAndInstallRoot(t *testing.T) {
	config := defaultConfig()
	config.Target = "operator@example.test"
	config.RemoteRoot = "/home/operator/openlia_dev"
	config.Project = "openlia_dev"
	command := (Remote{Config: config}).operationCommandForRoot(config.RemoteRoot+"/current", "ops/uninstall.sh", "--json")
	if !strings.Contains(command, "OPENLIA_INSTALL_ROOT='/home/operator/openlia_dev'") {
		t.Fatalf("uninstall command does not include install root: %s", command)
	}
	if !strings.Contains(command, "/home/operator/openlia_dev/current/ops/uninstall.sh") {
		t.Fatalf("uninstall command does not use current release: %s", command)
	}
}

func TestLegacyUninstallFallbackIsRootAware(t *testing.T) {
	config := defaultConfig()
	config.Target = "operator@example.test"
	config.RemoteRoot = "/home/operator/openlia_dev"
	config.Project = "openlia_dev"
	command := (Remote{Config: config}).legacyUninstallCommand(config.RemoteRoot + "/current")
	for _, expected := range []string{
		"docker compose",
		"down --remove-orphans",
		"docker ps -aq",
		"rm -rf -- '/home/operator/openlia_dev'",
		"images\":\"preserved",
	} {
		if !strings.Contains(command, expected) {
			t.Fatalf("legacy uninstall fallback missing %q: %s", expected, command)
		}
	}
}
