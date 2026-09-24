package operator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProfileSyncPreservesCustomizedSkillsAndTracksMetadata(t *testing.T) {
	repo := t.TempDir()
	runtime := filepath.Join(t.TempDir(), "runtime")
	skillSource := filepath.Join(repo, "profile", "skills", "example")
	if err := os.MkdirAll(skillSource, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, contents := range map[string]string{
		filepath.Join(repo, "profile", "SOUL.md"):                       "soul\n",
		filepath.Join(repo, "profile", "AGENTS.md"):                     "agents\n",
		filepath.Join(repo, "profile", "config.yaml"):                   "config\n",
		filepath.Join(repo, "profile", "skills", "example", "SKILL.md"): "skill\n",
		filepath.Join(repo, "release", "manifest.json"):                 `{"openlia":"test"}`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	config := testConfig(repo, runtime)
	p := NewProfileOperator(config)
	p.Now = func() time.Time { return time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC) }
	first, err := p.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if first.Skills.Installed != 1 || first.Skills.Results[0].Action != "installed" {
		t.Fatalf("unexpected initial sync: %+v", first)
	}
	second, err := p.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if second.Skills.Unchanged != 1 || second.Skills.Results[0].Provenance != "valid" {
		t.Fatalf("unexpected unchanged sync: %+v", second)
	}
	destination := filepath.Join(config.DataRoot, "skills", "example", "SKILL.md")
	if err := os.WriteFile(destination, []byte("customized\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	customized, err := p.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if customized.Skills.Customized != 1 || customized.Skills.Results[0].Reason != "local_modifications_detected" {
		t.Fatalf("customization was not blocked: %+v", customized)
	}
	if err := os.Remove(filepath.Join(config.MetaRoot, "managed", "skills", "example.json")); err != nil {
		t.Fatal(err)
	}
	unmanaged, err := p.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if unmanaged.Skills.Unmanaged != 1 || unmanaged.Skills.Results[0].Reason != "metadata_missing" {
		t.Fatalf("missing provenance was not reported: %+v", unmanaged)
	}
}

func TestProfileSyncRendersFallbackProviders(t *testing.T) {
	repo := t.TempDir()
	runtime := filepath.Join(t.TempDir(), "runtime")
	configPath := filepath.Join(repo, "profile", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "profile", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	configTemplate := "model:\n  provider: copilot\n\n" + fallbackBlockStart + "\nfallback_providers: []\n" + fallbackBlockEnd + "\n"
	if err := os.WriteFile(configPath, []byte(configTemplate), 0o644); err != nil {
		t.Fatal(err)
	}
	config := testConfig(repo, runtime)
	config.FallbackProviders = []FallbackProviderConfig{
		{Provider: "custom", Model: "gateway-model", BaseURL: "http://locho-laptop:11434/v1", KeyEnv: "OPENAI_GATEWAY_API_KEY"},
		{Provider: "openai-api", Model: "official-model"},
	}
	if _, err := NewProfileOperator(config).Sync(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(config.DataRoot, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "base_url: \"http://locho-laptop:11434/v1\"\n    key_env: \"OPENAI_GATEWAY_API_KEY\"\n  - provider: \"openai-api\"\n    model: \"official-model\""
	if !strings.Contains(string(data), want) {
		t.Fatalf("fallback providers were not rendered:\n%s", data)
	}
}

func TestProfileSyncUpdatesManagedOutputLanguage(t *testing.T) {
	repo := t.TempDir()
	runtime := filepath.Join(t.TempDir(), "runtime")
	for path, contents := range map[string]string{
		filepath.Join(repo, "profile", "SOUL.md"):       "# Soul\n\nProfile guidance.\n\n" + outputLanguageBlockStart + "\nUse `en` as the default language.\n" + outputLanguageBlockEnd + "\n",
		filepath.Join(repo, "profile", "AGENTS.md"):     "agents\n",
		filepath.Join(repo, "profile", "config.yaml"):   "config\n",
		filepath.Join(repo, "release", "manifest.json"): `{"openlia":"test"}`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	config := testConfig(repo, runtime)
	if err := os.MkdirAll(filepath.Join(repo, "profile", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	config.OutputLanguage = "en"
	if _, err := NewProfileOperator(config).Sync(); err != nil {
		t.Fatal(err)
	}
	soulPath := filepath.Join(config.DataRoot, "SOUL.md")
	data, err := os.ReadFile(soulPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Use `en` as the default language") {
		t.Fatalf("default output language was not rendered:\n%s", data)
	}

	config.OutputLanguage = "vi"
	if _, err := NewProfileOperator(config).Sync(); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(soulPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Use `vi` as the default language") || strings.Contains(string(data), "Use `en` as the default language") {
		t.Fatalf("updated output language was not rendered:\n%s", data)
	}

	if err := os.WriteFile(soulPath, append([]byte("custom guidance\n"), data...), 0o600); err != nil {
		t.Fatal(err)
	}
	config.OutputLanguage = "en"
	if _, err := NewProfileOperator(config).Sync(); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(soulPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "custom guidance") || !strings.Contains(string(data), "Use `en` as the default language") {
		t.Fatalf("custom guidance or updated language was lost:\n%s", data)
	}
}
