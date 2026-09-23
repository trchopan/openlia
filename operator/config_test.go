package operator

import (
	"path/filepath"
	"strings"
	"testing"
)

func testConfig(repo, runtime string) Config {
	return Config{
		RepositoryRoot:    repo,
		RuntimeRoot:       runtime,
		InstallRoot:       filepath.Dir(runtime),
		ProjectName:       "test-project",
		NetworkName:       "test-project-private",
		ComposeFile:       filepath.Join(repo, "docker", "compose.yaml"),
		ComposeProjectDir: filepath.Join(repo, "docker"),
		GeneratedCompose:  filepath.Join(repo, "docker", "compose.generated.yaml"),
		DataRoot:          filepath.Join(runtime, "hermes"),
		SystemSkillsRoot:  filepath.Join(runtime, "system-skills"),
		LochoRoot:         filepath.Join(runtime, "locho"),
		SecretDir:         filepath.Join(runtime, "secrets"),
		SecretFile:        filepath.Join(runtime, "secrets", "hermes.env"),
		BackupRoot:        filepath.Join(runtime, "backups"),
		MetaRoot:          filepath.Join(runtime, "meta"),
		StateFile:         filepath.Join(runtime, "meta", "stack-state"),
		SkillsCacheRoot:   filepath.Join(runtime, "skill-cache"),
		SkillsEnvRoot:     filepath.Join(runtime, "skill-envs"),
		APIHost:           "127.0.0.1",
		WorkspaceUIHost:   "",
		WorkspaceUIPort:   8089,
		OpenWebUIHost:     "",
		OpenWebUIPort:     8090,
		OpenWebUIDataRoot: filepath.Join(runtime, "open-webui"),
	}
}

func TestConfigValidatePaths(t *testing.T) {
	repo := t.TempDir()
	runtimeParent := t.TempDir()
	config := testConfig(repo, filepath.Join(runtimeParent, "runtime"))
	if err := config.ValidatePaths(); err != nil {
		t.Fatalf("valid paths rejected: %v", err)
	}

	tests := []struct {
		name  string
		edit  func(*Config)
		match string
	}{
		{"relative runtime", func(c *Config) { c.RuntimeRoot = "runtime" }, "runtime-root must be absolute"},
		{"unsafe runtime component", func(c *Config) { c.RuntimeRoot = filepath.Join(runtimeParent, "runtime") + "/../other" }, "unsafe path component"},
		{"repository runtime", func(c *Config) { c.RuntimeRoot = filepath.Join(repo, "runtime") }, "must not be inside the repository"},
		{"broad runtime", func(c *Config) { c.RuntimeRoot = "/tmp" }, "runtime-root is too broad"},
		{"unsafe project", func(c *Config) { c.ProjectName = "../project" }, "invalid project-name"},
		{"local compose", func(c *Config) { c.ComposeFile = filepath.Join(repo, "local", "compose.yaml") }, "compose files must not be under local"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := config
			test.edit(&candidate)
			if err := candidate.ValidatePaths(); err == nil || !strings.Contains(err.Error(), test.match) {
				t.Fatalf("ValidatePaths() error = %v, want %q", err, test.match)
			}
		})
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	repo := t.TempDir()
	runtime := filepath.Join(t.TempDir(), "runtime")
	config, err := LoadConfigFromEnv(map[string]string{
		"OPENLIA_REPO_ROOT":                  repo,
		"OPENLIA_RUNTIME_ROOT":               runtime,
		"OPENLIA_PROJECT_NAME":               "example",
		"OPENLIA_PROVIDER":                   "copilot",
		"OPENLIA_FALLBACK_PROVIDERS":         `[{"provider":"custom","model":"gateway-model","base_url":"https://gateway.example.test/v1","key_env":"OPENAI_GATEWAY_API_KEY"},{"provider":"openai-api","model":"official-model"}]`,
		"OPENLIA_LOCAL_MODE":                 "true",
		"OPENLIA_ENABLED_SKILLS":             "daily-briefing,workspace-git",
		"OPENLIA_SKILLS_CONFIGURED":          "true",
		"OPENLIA_WORKSPACE_UI_HOST":          "0.0.0.0",
		"OPENLIA_WORKSPACE_UI_PORT":          "8090",
		"OPENLIA_WORKSPACE_UI_PUBLIC_ORIGIN": "https://workspace.example.test",
		"OPENLIA_WORKSPACE_UI_AUTH_REQUIRED": "true",
		"OPENLIA_OPEN_WEBUI_HOST":            "127.0.0.1",
		"OPENLIA_OPEN_WEBUI_PORT":            "8090",
		"OPENLIA_OPEN_WEBUI_IMAGE":           "ghcr.io/open-webui/open-webui:main",
		"OPENLIA_OPEN_WEBUI_AUTH":            "false",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !config.LocalMode || !config.SkillsConfigured || config.WorkspaceUIHost != "0.0.0.0" || config.WorkspaceUIPort != 8090 || config.WorkspaceUIPublicOrigin != "https://workspace.example.test" || config.OpenWebUIHost != "127.0.0.1" || config.OpenWebUIPort != 8090 || config.OpenWebUIImage != "ghcr.io/open-webui/open-webui:main" || config.OpenWebUIAuth != false || len(config.EnabledSkills) != 2 || config.NetworkName != "example-private" || config.Provider != "copilot" || len(config.FallbackProviders) != 2 || config.FallbackProviders[0].BaseURL != "https://gateway.example.test/v1" || config.FallbackProviders[1].Model != "official-model" {
		t.Fatalf("unexpected typed config: %+v", config)
	}
}

func TestWorkspaceUIHostValidation(t *testing.T) {
	for _, host := range []string{"localhost", "192.168.1.10", "127.0.0.2"} {
		config := testConfig(t.TempDir(), filepath.Join(t.TempDir(), "runtime"))
		config.WorkspaceUIHost = host
		if err := config.ValidatePaths(); err == nil {
			t.Fatalf("workspace UI host %q was accepted", host)
		}
	}
	for _, port := range []int{0, 65536} {
		config := testConfig(t.TempDir(), filepath.Join(t.TempDir(), "runtime"))
		config.WorkspaceUIHost = "127.0.0.1"
		config.WorkspaceUIPort = port
		if err := config.ValidatePaths(); err == nil {
			t.Fatalf("workspace UI port %d was accepted", port)
		}
	}
}

func TestOpenWebUIHostValidation(t *testing.T) {
	for _, host := range []string{"localhost", "192.168.1.10", "127.0.0.2"} {
		config := testConfig(t.TempDir(), filepath.Join(t.TempDir(), "runtime"))
		config.OpenWebUIHost = host
		if err := config.ValidatePaths(); err == nil {
			t.Fatalf("open-webui host %q was accepted", host)
		}
	}
	for _, port := range []int{0, 65536} {
		config := testConfig(t.TempDir(), filepath.Join(t.TempDir(), "runtime"))
		config.OpenWebUIHost = "127.0.0.1"
		config.OpenWebUIPort = port
		if err := config.ValidatePaths(); err == nil {
			t.Fatalf("open-webui port %d was accepted", port)
		}
	}
}
