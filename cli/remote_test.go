package cli

import (
	"context"
	"os"
	"path/filepath"
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

func TestOperationCommandIncludesOutputLanguage(t *testing.T) {
	config := defaultConfig()
	config.Target = "operator@example.test"
	config.OutputLanguage = "vi"
	command := (Remote{Config: config}).operationCommand("ops/deploy.sh")
	if !strings.Contains(command, "OPENLIA_OUTPUT_LANGUAGE='vi'") {
		t.Fatalf("operation command does not include configured output language: %s", command)
	}
	if !strings.Contains(strings.Join(operationEnvironment(config, "/tmp/release"), "\n"), "OPENLIA_OUTPUT_LANGUAGE=vi") {
		t.Fatal("local environment does not transport output language")
	}
}

func TestOperationCommandPrefersTargetOperatorWithLegacyFallback(t *testing.T) {
	config := defaultConfig()
	config.Target = "operator@example.test"
	command := (Remote{Config: config}).operationCommand("ops/deploy.sh", "deploy", "--json")
	for _, expected := range []string{
		"operator/linux-amd64/openlia-operator",
		"operator/linux-arm64/openlia-operator",
		"uname -m",
		"ops/deploy.sh",
	} {
		if !strings.Contains(command, expected) {
			t.Fatalf("operator command missing %q: %s", expected, command)
		}
	}
}

func TestOperatorArgumentsMapAllOperationScripts(t *testing.T) {
	for _, script := range []string{
		"ops/bootstrap.sh",
		"ops/profile.sh",
		"ops/skill-status.sh",
		"skill-fork",
		"skill-migration",
		"ops/backup.sh",
		"ops/attachments.sh",
		"ops/auth.sh",
		"ops/deploy.sh",
		"ops/healthcheck.sh",
		"ops/workspace-git.sh",
		"ops/uninstall.sh",
		"skill-sources",
		"skills",
		"workspace-migrate",
		"instructions",
	} {
		if _, ok := operatorArguments(script, []string{"--json"}); !ok {
			t.Fatalf("script %s was not mapped to the Go operator", script)
		}
	}
}

func TestSkillSourcesTransportAsJSON(t *testing.T) {
	config := defaultConfig()
	config.Target = "operator@example.test"
	config.SkillSources = []SkillSourceConfig{{Name: "team", Repository: "https://github.com/example/skills", Branch: "release/v2"}}
	want := `[{"name":"team","repository":"https://github.com/example/skills","branch":"release/v2"}]`
	command := (Remote{Config: config}).operationCommand("skills", "list", "--json")
	if !strings.Contains(command, "OPENLIA_SKILL_SOURCES="+shellQuote(want)) {
		t.Fatalf("remote command does not transport skill sources JSON: %s", command)
	}
	if !strings.Contains(strings.Join(operationEnvironment(config, "/tmp/release"), "\n"), "OPENLIA_SKILL_SOURCES="+want) {
		t.Fatal("local environment does not transport skill sources JSON")
	}
}

func TestOperatorOnlyOperationsHaveNoLegacyFallback(t *testing.T) {
	config := defaultConfig()
	config.Target = "operator@example.test"
	for _, operation := range []string{"skill-sources", "skills", "workspace-git", "instructions"} {
		command := (Remote{Config: config}).operationCommand(operation, "list", "--json")
		if strings.Contains(command, "ops/skills") || !strings.Contains(command, "Go operator is required") {
			t.Fatalf("%s operation has unsafe fallback: %s", operation, command)
		}
	}
}

func TestRemoteExtractsOperatorErrorJSON(t *testing.T) {
	bin := t.TempDir()
	ssh := filepath.Join(bin, "ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\nprintf '%s\\n' '{\"schema\":1,\"ok\":false,\"error\":\"remote skill failure\"}'\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	config := defaultConfig()
	config.Target = "operator@example.test"
	_, err := (Remote{Config: config}).ssh(context.Background(), "ignored", nil)
	if err == nil || err.Error() != "remote command failed: remote skill failure" {
		t.Fatalf("Remote.ssh() error = %v", err)
	}
}

func TestLocalExtractsOperatorErrorJSON(t *testing.T) {
	config := defaultConfig()
	config.Mode = "local"
	config.Target = ""
	config.InstallRoot = filepath.Join(t.TempDir(), "openlia")
	_, err := (Local{Config: config}).operator(context.Background(), filepath.Join(config.InstallRoot, "release"), nil, "unknown", "--json")
	if err == nil || !strings.Contains(err.Error(), "local operator failed: unknown command") {
		t.Fatalf("Local.operator() error = %v", err)
	}
}

func TestWorkspaceGitArgumentsDoNotContainSecrets(t *testing.T) {
	config := defaultConfig()
	config.WorkspaceGit.Enabled = true
	config.WorkspaceGit.Remote = "https://github.com/example/private-vault.git"
	args := workspaceGitArguments("setup", config.WorkspaceGit)
	for _, arg := range args {
		if strings.Contains(arg, "TOKEN") || strings.Contains(arg, "ghp_") || strings.Contains(arg, "github_pat_") {
			t.Fatalf("workspace Git operation argument contains a credential: %q", arg)
		}
	}
}

func TestUninstallCommandUsesCurrentReleaseAndInstallRoot(t *testing.T) {
	config := defaultConfig()
	config.Target = "operator@example.test"
	config.InstallRoot = "/home/operator/openlia_dev"
	config.Project = "openlia_dev"
	command := (Remote{Config: config}).operationCommandForRoot(config.InstallRoot+"/current", "ops/uninstall.sh", "--json")
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
	config.InstallRoot = "/home/operator/openlia_dev"
	config.Project = "openlia_dev"
	command := (Remote{Config: config}).legacyUninstallCommand(config.InstallRoot + "/current")
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
