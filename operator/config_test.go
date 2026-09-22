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
		APIHost:           "127.0.0.1",
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
		"OPENLIA_REPO_ROOT":          repo,
		"OPENLIA_RUNTIME_ROOT":       runtime,
		"OPENLIA_PROJECT_NAME":       "example",
		"OPENLIA_PROVIDER":           "copilot",
		"OPENLIA_FALLBACK_PROVIDERS": `[{"provider":"custom","model":"gateway-model","base_url":"https://gateway.example.test/v1","key_env":"OPENAI_GATEWAY_API_KEY"},{"provider":"openai-api","model":"official-model"}]`,
		"OPENLIA_LOCAL_MODE":         "true",
		"OPENLIA_ENABLED_SKILLS":     "daily-briefing,workspace-git",
		"OPENLIA_SKILLS_CONFIGURED":  "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !config.LocalMode || !config.SkillsConfigured || len(config.EnabledSkills) != 2 || config.NetworkName != "example-private" || config.Provider != "copilot" || len(config.FallbackProviders) != 2 || config.FallbackProviders[0].BaseURL != "https://gateway.example.test/v1" || config.FallbackProviders[1].Model != "official-model" {
		t.Fatalf("unexpected typed config: %+v", config)
	}
}
