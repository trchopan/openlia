package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	temporary := t.TempDir()
	path := filepath.Join(temporary, "config.toml")
	t.Setenv("OPENLIA_CONFIG", path)
	want := defaultConfig()
	want.Target = "operator@example.test"
	want.Model = "test-model"
	want.Timezone = "Asia/Tokyo"
	want.SecretSource = filepath.Join(temporary, "hermes.env")
	want.EnabledSkills = []string{"daily-briefing", "deep-research"}
	want.WorkspaceGit = WorkspaceGitConfig{
		Enabled:     true,
		Provider:    "github",
		Remote:      "https://github.com/example/private-vault.git",
		Branch:      "main",
		Schedule:    "every 5m",
		AuthorName:  "OpenLia Agent",
		AuthorEmail: "openlia@example.test",
	}
	if err := saveConfig(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.Target != want.Target || got.Model != want.Model || got.Timezone != want.Timezone || got.SecretSource != want.SecretSource || len(got.EnabledSkills) != 2 || got.WorkspaceGit != want.WorkspaceGit {
		t.Fatalf("round trip mismatch: got %#v want %#v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode is %o, want 600", info.Mode().Perm())
	}
}

func TestConfigDefaultsTimezoneForLegacyConfig(t *testing.T) {
	got, err := parseConfig("[openlia]\nschema = 1\n")
	if err != nil {
		t.Fatal(err)
	}
	if got.Timezone != defaultTimezone {
		t.Fatalf("timezone = %q, want %q", got.Timezone, defaultTimezone)
	}
}

func TestLocalConfigRoundTrip(t *testing.T) {
	temporary := t.TempDir()
	path := filepath.Join(temporary, "config.toml")
	t.Setenv("OPENLIA_CONFIG", path)
	want := defaultConfig()
	want.Mode = "local"
	want.Target = ""
	want.InstallRoot = filepath.Join(temporary, "runtime")
	if err := saveConfig(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != "local" || got.Target != "" || got.InstallRoot != want.InstallRoot {
		t.Fatalf("local config mismatch: got %#v want %#v", got, want)
	}
}

func TestLegacyRemoteRootConfigIsReadable(t *testing.T) {
	got, err := parseConfig("[openlia]\nschema = 1\nremote_root = \"/srv/openlia-test\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != "ssh" || got.InstallRoot != "/srv/openlia-test" {
		t.Fatalf("legacy config mismatch: got %#v", got)
	}
}

func TestConfigRejectsInvalidTimezone(t *testing.T) {
	for _, timezone := range []string{"", "Local", "../UTC", "Mars/Olympus", "Asia/Ho Chi Minh"} {
		config := defaultConfig()
		config.Timezone = timezone
		if err := validateConfig(config); err == nil {
			t.Fatalf("timezone %q was accepted", timezone)
		}
	}

	config := defaultConfig()
	config.Timezone = "America/New_York"
	if err := validateConfig(config); err != nil {
		t.Fatalf("valid timezone was rejected: %v", err)
	}
}

func TestMissingConfigReturnsDefaultAndSentinel(t *testing.T) {
	t.Setenv("OPENLIA_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	got, err := loadConfig()
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want not-exist", err)
	}
	if got.Version != defaultVersion || len(got.EnabledSkills) != len(defaultSkills) {
		t.Fatalf("unexpected default config: %#v", got)
	}
}

func TestConfigRejectsUnsafeTargetAndRoot(t *testing.T) {
	config := defaultConfig()
	config.Target = "user@host;rm -rf /"
	if err := validateConfig(config); err == nil {
		t.Fatal("unsafe target was accepted")
	}
	config = defaultConfig()
	config.InstallRoot = "/srv"
	if err := validateConfig(config); err == nil {
		t.Fatal("broad installation root was accepted")
	}
}

func TestLocalConfigRejectsTarget(t *testing.T) {
	config := defaultConfig()
	config.Mode = "local"
	config.Target = "operator@example.test"
	if err := validateConfig(config); err == nil {
		t.Fatal("local config accepted an SSH target")
	}
}

func TestConfigRejectsRelativeSecretSource(t *testing.T) {
	config := defaultConfig()
	config.SecretSource = "hermes.env"
	if err := validateConfig(config); err == nil {
		t.Fatal("relative secret source was accepted")
	}
}

func TestWorkspaceGitRejectsUnsafeRemote(t *testing.T) {
	for _, remote := range []string{
		"https://github.com/example/private-vault.git?token=secret",
		"https://github.com/example/private-vault/extra.git",
		"http://github.com/example/private-vault.git",
		"git@github.com:example/private-vault.git",
	} {
		config := defaultConfig()
		config.WorkspaceGit.Enabled = true
		config.WorkspaceGit.Remote = remote
		if err := validateConfig(config); err == nil {
			t.Fatalf("workspace Git remote %q was accepted", remote)
		}
	}
}

func TestWorkspaceGitAcceptsConfiguredRemote(t *testing.T) {
	config := defaultConfig()
	config.WorkspaceGit.Enabled = true
	config.WorkspaceGit.Remote = "https://github.com/example/private-vault.git"
	if err := validateConfig(config); err != nil {
		t.Fatalf("valid workspace Git config was rejected: %v", err)
	}
}

func TestProtectedSourcePathRequiresMode600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hermes.env")
	if err := os.WriteFile(path, []byte("OPENAI_API_KEY=test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateProtectedSourcePath(path, "secret source"); err != nil {
		t.Fatalf("valid protected source was rejected: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateProtectedSourcePath(path, "secret source"); err == nil {
		t.Fatal("insecure secret source was accepted")
	}
}
