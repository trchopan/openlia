package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	temporary := t.TempDir()
	path := filepath.Join(temporary, "config.toml")
	t.Setenv("OPENLIA_CONFIG", path)
	want := defaultConfig()
	want.Target = "operator@example.test"
	want.WorkspaceUIHost = "127.0.0.1"
	want.WorkspaceUIPort = 8089
	want.WorkspaceUIPublicOrigin = "https://workspace.example.test"
	want.OpenWebUIHost = "127.0.0.1"
	want.OpenWebUIPort = 8090
	want.OpenWebUIImage = "ghcr.io/open-webui/open-webui:v0.5.20"
	want.OpenWebUIAuth = false
	want.Model = "test-model"
	want.OutputLanguage = "vi"
	want.FallbackProviders = []FallbackProviderConfig{
		{Provider: "custom", Model: "gateway-model", BaseURL: "https://gateway.example.test/v1", KeyEnv: "OPENAI_GATEWAY_API_KEY"},
		{Provider: "openai-api", Model: "official-model"},
	}
	want.Timezone = "Asia/Tokyo"
	want.SecretSource = filepath.Join(temporary, "hermes.env")
	want.SkillSources = []SkillSourceConfig{
		{Name: "official", Repository: "https://github.com/openlia/skills.git", Branch: "main"},
		{Name: "team", Repository: "https://github.com/example/team-skills", Branch: "release/v2"},
	}
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
	want.BrowserTools = BrowserToolsConfig{
		Configured:         true,
		Mode:               "ssh",
		Target:             "browser@example.test",
		SSHPort:            2222,
		Root:               "/home/browser/services/browser-tools",
		ExtensionTokenFile: "/home/browser/services/playwright-server-token.txt",
	}
	if err := saveConfig(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.Target != want.Target || got.Model != want.Model || got.OutputLanguage != want.OutputLanguage || got.WorkspaceUIHost != want.WorkspaceUIHost || got.WorkspaceUIPort != want.WorkspaceUIPort || got.WorkspaceUIPublicOrigin != want.WorkspaceUIPublicOrigin || got.OpenWebUIHost != want.OpenWebUIHost || got.OpenWebUIPort != want.OpenWebUIPort || got.OpenWebUIImage != want.OpenWebUIImage || got.OpenWebUIAuth != want.OpenWebUIAuth || len(got.FallbackProviders) != 2 || got.FallbackProviders[0] != want.FallbackProviders[0] || got.FallbackProviders[1] != want.FallbackProviders[1] || got.Timezone != want.Timezone || got.SecretSource != want.SecretSource || len(got.EnabledSkills) != 2 || got.WorkspaceGit != want.WorkspaceGit || got.BrowserTools != want.BrowserTools || len(got.SkillSources) != 2 || got.SkillSources[0] != want.SkillSources[0] || got.SkillSources[1] != want.SkillSources[1] {
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

func TestBrowserToolsConfigValidation(t *testing.T) {
	valid := defaultConfig()
	valid.BrowserTools = BrowserToolsConfig{Configured: true, Mode: "local", SSHPort: 22, Root: "/Users/test/services/browser-tools", ExtensionTokenFile: "/Users/test/services/playwright-server-token.txt"}
	if err := validateConfig(valid); err != nil {
		t.Fatalf("valid local browser-tools config rejected: %v", err)
	}
	valid.BrowserTools = BrowserToolsConfig{Configured: true, Mode: "ssh", Target: "user@example.test", SSHPort: 2222, Root: "/home/user/services/browser-tools", ExtensionTokenFile: "/home/user/services/playwright-server-token.txt"}
	if err := validateConfig(valid); err != nil {
		t.Fatalf("valid ssh browser-tools config rejected: %v", err)
	}
	for _, config := range []BrowserToolsConfig{
		{Configured: true, Mode: "remote", Root: "/Users/test/services/browser-tools", ExtensionTokenFile: "/Users/test/services/playwright-server-token.txt"},
		{Configured: true, Mode: "local", Target: "user@example.test", Root: "/Users/test/services/browser-tools", ExtensionTokenFile: "/Users/test/services/playwright-server-token.txt"},
		{Configured: true, Mode: "ssh", Root: "/home/user/services/browser-tools", ExtensionTokenFile: "/home/user/services/playwright-server-token.txt"},
		{Configured: true, Mode: "ssh", Target: "user@example.test", SSHPort: 0, Root: "/home/user/services/browser-tools", ExtensionTokenFile: "/home/user/services/playwright-server-token.txt"},
		{Configured: true, Mode: "ssh", Target: "example.test", Root: "/home/user/services/browser-tools", ExtensionTokenFile: "/home/user/services/playwright-server-token.txt"},
		{Configured: true, Mode: "local", Root: "~/services/browser-tools", ExtensionTokenFile: "/Users/test/services/playwright-server-token.txt"},
	} {
		candidate := defaultConfig()
		candidate.BrowserTools = config
		if err := validateConfig(candidate); err == nil {
			t.Fatalf("invalid browser-tools config accepted: %#v", config)
		}
	}
}

func TestBrowserToolsConfigAbsentFromLegacyConfig(t *testing.T) {
	config, err := parseConfig("[openlia]\nschema = 1\n")
	if err != nil {
		t.Fatal(err)
	}
	if config.BrowserTools.Configured {
		t.Fatal("legacy config unexpectedly configured browser-tools")
	}
	if strings.Contains(renderConfig(config), "[browser-tools]") {
		t.Fatal("legacy config unexpectedly rendered browser-tools")
	}
}

func TestSkillSourcesRejectUnsafeConfiguration(t *testing.T) {
	for _, repository := range []string{"http://github.com/example/skills", "https://user:token@github.com/example/skills", "https://github.com/example/skills?token=secret", "https://github.com/example/skills#main", "https://gitlab.com/example/skills", "https://github.com/example/skills/extra"} {
		config := defaultConfig()
		config.SkillSources = []SkillSourceConfig{{Name: "team", Repository: repository, Branch: "main"}}
		if err := validateConfig(config); err == nil {
			t.Fatalf("skill source repository %q was accepted", repository)
		}
	}
	for _, branch := range []string{"", "../main", "feature//one", "main.lock", "main@{1}", "main/"} {
		config := defaultConfig()
		config.SkillSources = []SkillSourceConfig{{Name: "team", Repository: "https://github.com/example/skills", Branch: branch}}
		if err := validateConfig(config); err == nil {
			t.Fatalf("skill source branch %q was accepted", branch)
		}
	}
	config := defaultConfig()
	config.SkillSources = []SkillSourceConfig{{Name: "team", Repository: "https://github.com/example/one", Branch: "main"}, {Name: "team", Repository: "https://github.com/example/two", Branch: "main"}}
	if err := validateConfig(config); err == nil {
		t.Fatal("duplicate skill source names were accepted")
	}
}

func TestSkillSourceConfigNeverContainsToken(t *testing.T) {
	config := defaultConfig()
	config.SkillSources = []SkillSourceConfig{{Name: "team", Repository: "https://github.com/example/skills", Branch: "main"}}
	config.SecretSource = "/tmp/hermes.env"
	rendered := renderConfig(config)
	if strings.Contains(rendered, "OPENLIA_SKILLS_GIT_TOKEN") || strings.Contains(rendered, "github_pat_") {
		t.Fatalf("token material leaked into config: %s", rendered)
	}
}

func TestConfigRejectsInvalidFallbackProvider(t *testing.T) {
	for _, endpoint := range []string{
		"gateway.example.test/v1",
		"https://user:pass@gateway.example.test/v1",
		"https://gateway.example.test/v1?token=secret",
		"https://gateway.example.test/v1#fragment",
	} {
		config := defaultConfig()
		config.FallbackProviders = []FallbackProviderConfig{{Provider: "custom", Model: "gateway", BaseURL: endpoint}}
		if err := validateConfig(config); err == nil {
			t.Fatalf("gateway URL %q was accepted", endpoint)
		}
	}
	config := defaultConfig()
	config.FallbackProviders = []FallbackProviderConfig{{Provider: "custom", Model: "gateway"}}
	if err := validateConfig(config); err == nil {
		t.Fatal("custom fallback without base URL was accepted")
	}
}

func TestConfigAcceptsFallbackProviders(t *testing.T) {
	config := defaultConfig()
	config.FallbackProviders = []FallbackProviderConfig{
		{Provider: "custom", Model: "gateway", BaseURL: "http://gateway.example.test:8080/v1"},
		{Provider: "openai-api", Model: "official"},
	}
	if err := validateConfig(config); err != nil {
		t.Fatalf("valid gateway URL was rejected: %v", err)
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
	if got.OutputLanguage != defaultOutputLanguage {
		t.Fatalf("output language = %q, want %q", got.OutputLanguage, defaultOutputLanguage)
	}
	if got.WorkspaceUIHost != "" || got.WorkspaceUIPort != defaultWorkspaceUIPort {
		t.Fatalf("workspace UI = %q:%d, want disabled with default port", got.WorkspaceUIHost, got.WorkspaceUIPort)
	}
}

func TestConfigRejectsInvalidOutputLanguage(t *testing.T) {
	for _, language := range []string{"", "e", "en_US", "en space", "../vi", "en--US", "en-"} {
		config := defaultConfig()
		config.OutputLanguage = language
		if err := validateConfig(config); err == nil {
			t.Fatalf("output language %q was accepted", language)
		}
	}
	for _, language := range []string{"en", "vi", "pt-BR", "zh-Hans-CN"} {
		config := defaultConfig()
		config.OutputLanguage = language
		if err := validateConfig(config); err != nil {
			t.Fatalf("valid output language %q was rejected: %v", language, err)
		}
	}
}

func TestConfigRejectsLegacyWorkspaceUI(t *testing.T) {
	for _, data := range []string{
		"[openlia]\nschema = 1\nworkspace-ui = true\n",
		"[openlia]\nschema = 1\nworkspace-ui-host = \"127.0.0.1:8089\"\n",
	} {
		if _, err := parseConfig(data); err == nil {
			t.Fatalf("legacy Workspace UI setting was accepted: %s", data)
		}
	}
}

func TestConfigRejectsInvalidWorkspaceUIHost(t *testing.T) {
	for _, host := range []string{"localhost", "192.168.1.10", "127.0.0.2"} {
		config := defaultConfig()
		config.WorkspaceUIHost = host
		if err := validateConfig(config); err == nil {
			t.Fatalf("workspace UI host %q was accepted", host)
		}
	}
	for _, port := range []int{0, 65536} {
		config := defaultConfig()
		config.WorkspaceUIHost = "127.0.0.1"
		config.WorkspaceUIPort = port
		if err := validateConfig(config); err == nil {
			t.Fatalf("workspace UI port %d was accepted", port)
		}
	}
}

func TestConfigRejectsInvalidWorkspaceUIPublicOrigin(t *testing.T) {
	for _, origin := range []string{
		"workspace.example.test",
		"ftp://workspace.example.test",
		"https://user:pass@workspace.example.test",
		"https://workspace.example.test/path",
		"https://workspace.example.test?token=secret",
	} {
		config := defaultConfig()
		config.WorkspaceUIPublicOrigin = origin
		if err := validateConfig(config); err == nil {
			t.Fatalf("workspace UI public origin %q was accepted", origin)
		}
	}
}

func TestPublicWorkspaceUIRequiresSupportedPasswordHash(t *testing.T) {
	config := defaultConfig()
	config.WorkspaceUIHost = "0.0.0.0"
	if err := validateConfig(config); err == nil {
		t.Fatal("public workspace UI without a password hash was accepted")
	}
	hash, err := hashWorkspaceUIPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	config.WorkspaceUIPasswordHash = hash
	if err := validateConfig(config); err != nil {
		t.Fatalf("generated password hash was rejected: %v", err)
	}
	parsed, err := parseConfig(renderConfig(config))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.WorkspaceUIPasswordHash != hash {
		t.Fatalf("password hash did not round trip")
	}
	for _, invalid := range []string{"plain", "$argon2id$v=19$m=1024,t=1,p=1$YWJj$YWJj"} {
		config.WorkspaceUIPasswordHash = invalid
		if err := validateConfig(config); err == nil {
			t.Fatalf("invalid password hash %q was accepted", invalid)
		}
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

func TestConfigServicesRoundTrip(t *testing.T) {
	temporary := t.TempDir()
	sourcePath := filepath.Join(temporary, "locho-attachments.toml")
	if err := os.WriteFile(sourcePath, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(temporary, "config.toml")
	t.Setenv("OPENLIA_CONFIG", path)
	want := defaultConfig()
	want.Services = []ServiceHostConfig{
		{
			Name:   "m1pro",
			Source: sourcePath,
			Roles: map[string]string{
				"browser-tools": RoleBrowserTools,
				"genai":         RoleOpenAIGateway,
			},
		},
	}
	if err := saveConfig(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Services) != 1 {
		t.Fatalf("expected 1 service host, got %d", len(got.Services))
	}
	host := got.Services[0]
	if host.Name != "m1pro" || host.Source != sourcePath || len(host.Roles) != 2 || host.Roles["browser-tools"] != RoleBrowserTools || host.Roles["genai"] != RoleOpenAIGateway {
		t.Fatalf("services round-trip mismatch: got %#v want %#v", host, want.Services[0])
	}
}

func TestConfigServicesMultiHostRoundTrip(t *testing.T) {
	temporary := t.TempDir()
	source1 := filepath.Join(temporary, "host1.toml")
	source2 := filepath.Join(temporary, "host2.toml")
	_ = os.WriteFile(source1, []byte("h1"), 0o600)
	_ = os.WriteFile(source2, []byte("h2"), 0o600)
	path := filepath.Join(temporary, "config.toml")
	t.Setenv("OPENLIA_CONFIG", path)
	want := defaultConfig()
	want.Services = []ServiceHostConfig{
		{
			Name:   "m1pro",
			Source: source1,
			Roles: map[string]string{
				"browser-tools": RoleBrowserTools,
			},
		},
		{
			Name:   "gpu-server",
			Source: source2,
			Roles: map[string]string{
				"vllm": RoleOpenAIGateway,
			},
		},
	}
	if err := saveConfig(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Services) != 2 {
		t.Fatalf("expected 2 service hosts, got %d", len(got.Services))
	}
	if got.Services[0].Name != "m1pro" || got.Services[1].Name != "gpu-server" {
		t.Fatalf("unexpected hosts: got %#v", got.Services)
	}
}

func TestConfigServicesRejectsInvalidRole(t *testing.T) {
	for _, invalid := range []string{"generic", "image-generator", "unassigned", "custom", "openai-endpoint", ""} {
		config := defaultConfig()
		config.Services = []ServiceHostConfig{
			{
				Name: "laptop",
				Roles: map[string]string{
					"svc": invalid,
				},
			},
		}
		if err := validateConfig(config); err == nil {
			t.Fatalf("invalid service role %q was accepted", invalid)
		}
	}
}

func TestConfigServicesRejectsDottedKey(t *testing.T) {
	data := `
[openlia]
schema = 1
version = "0.1.0"
mode = "local"
root = ".openlia"
project = "openlia"
model = "gpt-5.6-luna"
timezone = "UTC"
provider = "openai-api"

[[services]]
name = "m1pro"
"m1pro.genai" = "openai-gateway"
`
	_, err := parseConfig(data)
	if err == nil {
		t.Fatal("expected error on dotted service key, got nil")
	}
}

func TestConfigServicesRejectsDuplicateHost(t *testing.T) {
	config := defaultConfig()
	config.Services = []ServiceHostConfig{
		{Name: "m1pro"},
		{Name: "m1pro"},
	}
	if err := validateConfig(config); err == nil {
		t.Fatal("expected error on duplicate host name, got nil")
	}
}

func TestOpenWebUIValidation(t *testing.T) {
	config := defaultConfig()
	config.OpenWebUIHost = "invalid-host"
	if err := validateConfig(config); err == nil {
		t.Fatal("expected error on invalid open-webui.host, got nil")
	}

	config = defaultConfig()
	config.OpenWebUIHost = "127.0.0.1"
	config.OpenWebUIPort = 0
	if err := validateConfig(config); err == nil {
		t.Fatal("expected error on invalid open-webui.port, got nil")
	}

	config = defaultConfig()
	config.OpenWebUIHost = "127.0.0.1"
	config.OpenWebUIPort = 8090
	if err := validateConfig(config); err != nil {
		t.Fatalf("valid open-webui rejected: %v", err)
	}
}

func TestOpenWebUIRender(t *testing.T) {
	config := defaultConfig()
	config.OpenWebUIHost = "127.0.0.1"
	config.OpenWebUIPort = 8090
	config.OpenWebUIAuth = true
	rendered := renderConfig(config)
	if !strings.Contains(rendered, "[open-webui]\nhost = \"127.0.0.1\"\nport = 8090\n") {
		t.Fatalf("rendered config missing [open-webui]:\n%s", rendered)
	}

	config.OpenWebUIAuth = false
	rendered = renderConfig(config)
	if !strings.Contains(rendered, "auth = false\n") {
		t.Fatalf("rendered config missing auth = false:\n%s", rendered)
	}
}
