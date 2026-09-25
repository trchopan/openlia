package operator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func prepareInstructionFixture(t *testing.T) Config {
	t.Helper()
	repo := t.TempDir()
	runtime := filepath.Join(t.TempDir(), "runtime")
	config := testConfig(repo, runtime)
	files := map[string]string{
		filepath.Join(repo, "profile", "AGENTS.md"):              "new profile agents\n",
		filepath.Join(repo, "profile", "SOUL.md"):                "new soul\n",
		filepath.Join(repo, "workspace-template", "AGENTS.md"):   "new workspace agents\n",
		filepath.Join(repo, "release", "manifest.json"):          `{"openlia":"test-v2"}`,
		filepath.Join(config.DataRoot, "AGENTS.md"):              "old profile agents\n",
		filepath.Join(config.DataRoot, "SOUL.md"):                "old soul\n",
		filepath.Join(config.DataRoot, "workspace", "AGENTS.md"): "old workspace agents\n",
	}
	for path, data := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{config.MetaRoot, config.LochoRoot, config.BackupRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return config
}

func TestInstructionSyncMigratesManagedProfileAndPreservesWorkspace(t *testing.T) {
	config := prepareInstructionFixture(t)
	managed := filepath.Join(config.MetaRoot, "managed")
	if err := os.MkdirAll(managed, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"AGENTS.md.sha256": []byte(strings.TrimPrefix(instructionHash([]byte("old profile agents\n")), "sha256:") + "\n"),
		"SOUL.md.sha256":   []byte(instructionHash(normalizeOutputLanguageBlock([]byte("old soul\n"))) + "\n"),
	} {
		if err := os.WriteFile(filepath.Join(managed, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := SyncInstructions(config, time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !result.AttentionRequired {
		t.Fatalf("workspace update was not reported: %+v", result)
	}
	profile, err := os.ReadFile(filepath.Join(config.DataRoot, "AGENTS.md"))
	if err != nil || string(profile) != "new profile agents\n" {
		t.Fatalf("managed profile was not updated: %q, %v", profile, err)
	}
	workspace, err := os.ReadFile(filepath.Join(config.DataRoot, "workspace", "AGENTS.md"))
	if err != nil || string(workspace) != "old workspace agents\n" {
		t.Fatalf("workspace instruction was replaced: %q, %v", workspace, err)
	}
	status, err := InstructionStatus(config, "workspace/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Instructions) != 1 || status.Instructions[0].State != "update_available" {
		t.Fatalf("unexpected workspace status: %+v", status)
	}
}

func TestInstructionSyncPreservesCustomizedProfile(t *testing.T) {
	config := prepareInstructionFixture(t)
	result, err := SyncInstructions(config, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !result.AttentionRequired {
		t.Fatalf("customized profile updates were not reported: %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(config.DataRoot, "AGENTS.md"))
	if err != nil || string(data) != "old profile agents\n" {
		t.Fatalf("customized profile was replaced: %q, %v", data, err)
	}
}

func TestInstructionKeepAcknowledgesUpstreamWithoutReplacingLocal(t *testing.T) {
	config := prepareInstructionFixture(t)
	if _, err := SyncInstructions(config, time.Now()); err != nil {
		t.Fatal(err)
	}
	status, err := InstructionStatus(config, "profile/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	item := status.Instructions[0]
	result, err := MutateInstruction(config, item.Name, "keep", InstructionMutationRequest{Schema: 1, ExpectedCurrentHash: item.CurrentHash, ExpectedAvailableHash: item.AvailableHash}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "customized" || result.Backup == "" {
		t.Fatalf("unexpected keep result: %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(config.DataRoot, "AGENTS.md"))
	if err != nil || string(data) != "old profile agents\n" {
		t.Fatalf("keep changed local content: %q, %v", data, err)
	}
	status, err = InstructionStatus(config, "profile/AGENTS.md")
	if err != nil || status.AttentionRequired {
		t.Fatalf("keep did not acknowledge update: %+v, %v", status, err)
	}
}

func TestMergeInstructionLines(t *testing.T) {
	merged, err := mergeInstructionLines([]byte("one\ntwo\nthree\n"), []byte("ONE\ntwo\nthree\n"), []byte("one\ntwo\nTHREE\n"))
	if err != nil || string(merged) != "ONE\ntwo\nTHREE\n" {
		t.Fatalf("independent edits did not merge: %q, %v", merged, err)
	}
	if _, err := mergeInstructionLines([]byte("one\n"), []byte("local\n"), []byte("upstream\n")); err == nil {
		t.Fatal("overlapping edits merged without review")
	}
}

func TestSimpleInstructionDiffKeepsSharedContext(t *testing.T) {
	diff := simpleInstructionDiff("base", []byte("one\ntwo\nthree\n"), "upstream", []byte("one\nTWO\nthree\n"))
	for _, expected := range []string{" one\n", "-two\n", "+TWO\n", " three\n"} {
		if !strings.Contains(diff, expected) {
			t.Fatalf("diff missing %q:\n%s", expected, diff)
		}
	}
}
