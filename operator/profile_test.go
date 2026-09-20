package operator

import (
	"os"
	"path/filepath"
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
