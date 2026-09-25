package operator

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFilesystemOperatorWorkflow(t *testing.T) {
	repo := t.TempDir()
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	config := testConfig(repo, runtimeRoot)
	for path, contents := range map[string]string{
		filepath.Join(repo, "profile", "SOUL.md"):                                          "soul\n",
		filepath.Join(repo, "profile", "AGENTS.md"):                                        "agents\n",
		filepath.Join(repo, "profile", "config.yaml"):                                      "secrets: {}\n",
		filepath.Join(repo, "profile", "skills", "example", "SKILL.md"):                    "skill\n",
		filepath.Join(repo, "profile", "cron", "scripts", "openlia-workspace-git-sync.sh"): "#!/usr/bin/env bash\n",
		filepath.Join(repo, "release", "manifest.json"):                                    `{"openlia":"test"}`,
		filepath.Join(repo, "workspace-template", "inbox", ".gitkeep"):                     "",
		filepath.Join(repo, "docker", ".gitkeep"):                                          "",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	if result, err := Bootstrap(config, false, now); err != nil || !result.OK {
		t.Fatalf("Bootstrap() = %+v, %v", result, err)
	}
	if err := GenerateAttachments(config); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProfileOperator(config).Sync(); err != nil {
		t.Fatal(err)
	}
	scriptInfo, err := os.Stat(filepath.Join(config.DataRoot, "scripts", "openlia-workspace-git-sync.sh"))
	if err != nil {
		t.Fatalf("workspace Git sync script was not installed: %v", err)
	}
	if scriptInfo.Mode().Perm() != 0o755 {
		t.Fatalf("workspace Git sync script mode = %v, want 0755", scriptInfo.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(config.DataRoot, "workspace", "inbox")); err != nil {
		t.Fatalf("workspace template was not initialized: %v", err)
	}

	metadataSentinel := filepath.Join(config.MetaRoot, "managed", "restore-sentinel")
	if err := os.WriteFile(metadataSentinel, []byte("archived metadata\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup, err := CreateBackup(config, "test", now)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(config.DataRoot, "workspace", "inbox", "sentinel.md")
	if err := os.WriteFile(sentinel, []byte("preserve me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataSentinel, []byte("current metadata\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreBackup(config, backup.Archive, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("restore retained post-backup file, stat error = %v", err)
	}
	if state, err := ReadState(config); err != nil || state != StateStopped {
		t.Fatalf("restored state = %q, %v", state, err)
	}
	metadata, err := os.ReadFile(metadataSentinel)
	if err != nil {
		t.Fatal(err)
	}
	if string(metadata) != "archived metadata\n" {
		t.Fatalf("metadata was not restored: %q", metadata)
	}
}
