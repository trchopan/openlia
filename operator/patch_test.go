package operator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillPatchReplaysCustomizedTree(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base")
	custom := filepath.Join(root, "custom")
	replayed := filepath.Join(root, "replayed")
	for path, contents := range map[string]string{
		filepath.Join(base, "SKILL.md"):   "base\n",
		filepath.Join(base, "remove.md"):  "remove\n",
		filepath.Join(custom, "SKILL.md"): "custom\n",
		filepath.Join(custom, "add.md"):   "add\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	patch, err := GenerateSkillPatch(base, custom, "example")
	if err != nil {
		t.Fatal(err)
	}
	if len(patch.Operations) != 3 {
		t.Fatalf("patch operations = %d, want 3", len(patch.Operations))
	}
	if err := ApplySkillPatch(base, replayed, patch); err != nil {
		t.Fatal(err)
	}
	baseHash, err := DirectorySHA256(custom)
	if err != nil {
		t.Fatal(err)
	}
	replayedHash, err := DirectorySHA256(replayed)
	if err != nil {
		t.Fatal(err)
	}
	if baseHash != replayedHash {
		t.Fatalf("replayed hash = %s, custom hash = %s", replayedHash, baseHash)
	}
	patchHash, err := SkillPatchHash(patch)
	if err != nil || patchHash == "" {
		t.Fatalf("SkillPatchHash() = %q, %v", patchHash, err)
	}
}

func TestSkillPatchRejectsUnsafePath(t *testing.T) {
	patch := SkillPatch{
		Schema:   1,
		Format:   skillPatchFormat,
		Skill:    "example",
		BaseHash: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Operations: []SkillPatchOperation{{
			Path:    "../escape",
			Action:  "add",
			NewHash: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			Content: "unsafe",
		}},
	}
	if _, err := SkillPatchHash(patch); err == nil {
		t.Fatal("unsafe patch path was accepted")
	}
}
