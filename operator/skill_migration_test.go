package operator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const migrationNewCommit = "abcdef0123456789abcdef0123456789abcdef01"

func TestForkPreservesCustomizedSkillAcrossProfileUpdate(t *testing.T) {
	config, now := migrationTestConfig(t)
	profile := NewProfileOperator(config)
	if _, err := profile.Sync(); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(config.DataRoot, "skills", "example", "SKILL.md")
	if err := os.WriteFile(active, []byte("user fork\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ForkSkill(config, "example", now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Ownership != "forked" {
		t.Fatalf("ownership = %q, want forked", result.Ownership)
	}
	if err := os.WriteFile(filepath.Join(config.RepositoryRoot, "profile", "skills", "example", "SKILL.md"), []byte("upstream v2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	syncResult, err := profile.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if syncResult.Skills.Forked != 1 || syncResult.Skills.UpdatesAvailable != 1 {
		t.Fatalf("unexpected fork sync result: %+v", syncResult.Skills)
	}
	if _, err := os.Stat(filepath.Join(config.DataRoot, ".openlia", "migrations", "example", "context.json")); err != nil {
		t.Fatalf("migration context was not staged: %v", err)
	}
	contents, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "user fork\n" {
		t.Fatalf("active fork was overwritten: %q", contents)
	}
}

func TestProtectedMigrationSkillCannotBeForked(t *testing.T) {
	config, now := migrationTestConfig(t)
	systemSkill := filepath.Join(config.RepositoryRoot, "profile", "system-skills", protectedSkillName, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(systemSkill), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(systemSkill, []byte("system\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProfileOperator(config).Sync(); err != nil {
		t.Fatal(err)
	}
	if _, err := ForkSkill(config, protectedSkillName, now); err == nil {
		t.Fatal("protected migration skill was forked")
	}
	if _, err := os.Stat(filepath.Join(config.SystemSkillsRoot, protectedSkillName, "SKILL.md")); err != nil {
		t.Fatalf("protected skill was not synchronized: %v", err)
	}
	protectedPath := filepath.Join(config.SystemSkillsRoot, protectedSkillName, "SKILL.md")
	if err := os.WriteFile(protectedPath, []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProfileOperator(config).Sync(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(protectedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "system\n" {
		t.Fatalf("protected skill was not restored: %q", contents)
	}
}

func TestApplySkillMigrationUpdatesForkAndLineage(t *testing.T) {
	config, now := migrationTestConfig(t)
	profile := NewProfileOperator(config)
	if _, err := profile.Sync(); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(config.DataRoot, "skills", "example", "SKILL.md")
	if err := os.WriteFile(active, []byte("user fork\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ForkSkill(config, "example", now); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(config.RepositoryRoot, "profile", "skills", "example", "SKILL.md")
	if err := os.WriteFile(source, []byte("upstream v2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	contextResult, err := PrepareSkillMigration(config, "example", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	proposalRoot := filepath.Join(config.DataRoot, ".openlia", "proposals", "proposal-1")
	if err := os.MkdirAll(proposalRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	newBase := filepath.Join(contextResult.Context, "new-base")
	proposed := filepath.Join(proposalRoot, "proposed")
	if err := CopyDir(newBase, proposed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proposed, "SKILL.md"), []byte("upstream v2\nuser customization\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch, err := GenerateSkillPatch(newBase, proposed, "example")
	if err != nil {
		t.Fatal(err)
	}
	patchPath := filepath.Join(proposalRoot, "customization.patch")
	patchHash, err := WriteSkillPatch(patchPath, patch)
	if err != nil {
		t.Fatal(err)
	}
	proposedHash, err := DirectorySHA256(proposed)
	if err != nil {
		t.Fatal(err)
	}
	contextData, err := os.ReadFile(filepath.Join(contextResult.Context, "context.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contextDetails SkillMigrationContext
	if err := json.Unmarshal(contextData, &contextDetails); err != nil {
		t.Fatal(err)
	}
	if contextDetails.Origin != "" || contextDetails.Source != "" || contextDetails.OldCommit != "" || contextDetails.NewCommit != "" {
		t.Fatalf("bundled context contains external provenance: %+v", contextDetails)
	}
	proposal := SkillMigrationProposal{
		Schema:           migrationProtocolSchema,
		ID:               "proposal-1",
		Skill:            "example",
		State:            "pending",
		OldBaseHash:      contextDetails.OldBaseHash,
		CurrentForkHash:  contextDetails.CurrentForkHash,
		OldPatchHash:     contextDetails.CurrentPatchHash,
		NewBaseHash:      contextDetails.NewBaseHash,
		NewPatchHash:     patchHash,
		ProposedForkHash: "sha256:" + proposedHash,
		CreatedAt:        utcTimestamp(now),
	}
	proposalData, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proposalRoot, "proposal.json"), append(proposalData, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ApplySkillMigration(config, "proposal-1", now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "applied" {
		t.Fatalf("migration state = %q, want applied", result.State)
	}
	contents, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "upstream v2\nuser customization\n" {
		t.Fatalf("migrated skill = %q", contents)
	}
	metadata, err := readFullSkillMetadata(filepath.Join(config.MetaRoot, "managed", "skills", "example.json"), "example")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Ownership != "forked" || metadata.ForkSHA256 != result.ForkHash || metadata.PatchSHA256 != result.PatchHash {
		t.Fatalf("metadata was not updated: %+v", metadata)
	}
}

func TestExternalSkillMigrationPrepareAndApply(t *testing.T) {
	config, now, contextResult := prepareExternalMigrationFixture(t)
	if contextResult.Details.Origin != "external" || contextResult.Details.Source != "team" || contextResult.Details.OldCommit != testSkillCommit || contextResult.Details.NewCommit != migrationNewCommit {
		t.Fatalf("external context provenance = %+v", contextResult.Details)
	}
	proposal := writeMigrationProposal(t, config, contextResult, "external-success", nil)
	result, err := ApplySkillMigration(config, proposal.ID, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := readExternalSkillMetadata(filepath.Join(config.MetaRoot, "external-skills", "example.json"))
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "applied" || metadata.Commit != migrationNewCommit || metadata.ForkHash != result.ForkHash || metadata.PatchHash != result.PatchHash {
		t.Fatalf("external migration result=%+v metadata=%+v", result, metadata)
	}
	contents, err := os.ReadFile(filepath.Join(config.DataRoot, "skills", "example", "SKILL.md"))
	if err != nil || string(contents) != externalSkillText("upstream v2\nuser customization\n") {
		t.Fatalf("migrated external skill = %q, %v", contents, err)
	}
}

func TestExternalSkillMigrationRejectsStaleForkAndSource(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, config Config)
	}{
		{name: "fork", mutate: func(t *testing.T, config Config) {
			path := filepath.Join(config.DataRoot, "skills", "example", "SKILL.md")
			if err := os.WriteFile(path, []byte(externalSkillText("changed again\n")), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "source", mutate: func(t *testing.T, config Config) {
			if err := os.WriteFile(filepath.Join(config.SkillsCacheRoot, "team", "current"), []byte(testSkillCommit+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, now, contextResult := prepareExternalMigrationFixture(t)
			proposal := writeMigrationProposal(t, config, contextResult, "external-stale", nil)
			test.mutate(t, config)
			if _, err := ApplySkillMigration(config, proposal.ID, now.Add(2*time.Minute)); err == nil || !strings.Contains(err.Error(), "stale") {
				t.Fatalf("ApplySkillMigration() error = %v", err)
			}
			stored, err := ReadSkillMigrationProposal(config, proposal.ID)
			if err != nil || stored.State != "stale" {
				t.Fatalf("stale proposal = %+v, %v", stored, err)
			}
		})
	}
}

func TestExternalSkillMigrationRejectsConflicts(t *testing.T) {
	config, now, contextResult := prepareExternalMigrationFixture(t)
	proposal := writeMigrationProposal(t, config, contextResult, "external-conflict", []string{"SKILL.md"})
	if _, err := ApplySkillMigration(config, proposal.ID, now.Add(2*time.Minute)); err == nil || !strings.Contains(err.Error(), "unresolved conflicts") {
		t.Fatalf("ApplySkillMigration() error = %v", err)
	}
	stored, err := ReadSkillMigrationProposal(config, proposal.ID)
	if err != nil || stored.State != "pending" {
		t.Fatalf("conflicted proposal changed = %+v, %v", stored, err)
	}
}

func TestExternalMigrationDependencyAuditUsesProposalComposePath(t *testing.T) {
	config, fixture := externalSkillFixture(t, true)
	if err := os.MkdirAll(config.SkillsEnvRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	proposalSkill := filepath.Join(config.DataRoot, ".openlia", "proposals", "proposal-deps", ".validated-skill")
	if err := CopyDir(filepath.Join(fixture, "skills", "example"), proposalSkill); err != nil {
		t.Fatal(err)
	}
	runner := &recordedRunner{}
	manager := NewExternalSkillManager(config, &externalFixtureRunner{}, NewCompose(config, runner))
	item := ExternalSkillCatalogItem{Name: "example", Source: "team", Commit: testSkillCommit, Version: "1.2.3", Path: "skills/example"}
	manifestItem := SkillCollectionManifestItem{Name: "example", Path: "skills/example", Version: "1.2.3"}

	if _, err := manager.auditSkillTree(context.Background(), item, manifestItem, proposalSkill); err != nil {
		t.Fatal(err)
	}
	wantSource := "OPENLIA_SKILL_SOURCE=/opt/openlia/migration-source/proposal-deps/.validated-skill"
	if len(runner.calls) != 2 {
		t.Fatalf("dependency audit calls = %v", runner.calls)
	}
	for _, call := range runner.calls {
		if !strings.Contains(call, wantSource) || !strings.Contains(call, "skill-env-builder audit") {
			t.Fatalf("migration dependency audit used wrong Compose path: %s", call)
		}
	}
}

func prepareExternalMigrationFixture(t *testing.T) (Config, time.Time, SkillMigrationContextResult) {
	t.Helper()
	config, fixture := externalSkillFixture(t, false)
	publishFixture(t, config, fixture)
	for _, directory := range []string{config.DataRoot, config.MetaRoot, config.LochoRoot, config.BackupRoot} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	manager := NewExternalSkillManager(config, &externalFixtureRunner{}, NewCompose(config, &recordedRunner{}))
	manager.Now = func() time.Time { return now }
	if _, err := manager.Install(context.Background(), "team", "example", false); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(config.DataRoot, "skills", "example", "SKILL.md")
	if err := os.WriteFile(active, []byte(externalSkillText("base\nuser customization\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Fork(context.Background(), "example"); err != nil {
		t.Fatal(err)
	}
	newSnapshot := filepath.Join(config.SkillsCacheRoot, "team", migrationNewCommit)
	if err := CopyDir(fixture, newSnapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newSnapshot, "skills", "example", "SKILL.md"), []byte(externalSkillText("upstream v2\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := DirectorySHA256(newSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.SkillsCacheRoot, "team", migrationNewCommit+".sha256"), []byte("sha256:"+digest+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.SkillsCacheRoot, "team", "current"), []byte(migrationNewCommit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := PrepareSkillMigration(config, "example", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return config, now, result
}

func writeMigrationProposal(t *testing.T, config Config, contextResult SkillMigrationContextResult, id string, conflicts []string) SkillMigrationProposal {
	t.Helper()
	root := filepath.Join(config.DataRoot, ".openlia", "proposals", id)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	newBase := filepath.Join(contextResult.Context, "new-base")
	proposed := filepath.Join(root, "proposed")
	if err := CopyDir(newBase, proposed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proposed, "SKILL.md"), []byte(externalSkillText("upstream v2\nuser customization\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	patch, err := GenerateSkillPatch(newBase, proposed, "example")
	if err != nil {
		t.Fatal(err)
	}
	patchHash, err := WriteSkillPatch(filepath.Join(root, "customization.patch"), patch)
	if err != nil {
		t.Fatal(err)
	}
	proposedHash, err := DirectorySHA256(proposed)
	if err != nil {
		t.Fatal(err)
	}
	details := contextResult.Details
	proposal := SkillMigrationProposal{Schema: 1, ID: id, Skill: details.Skill, State: "pending", Origin: details.Origin, Source: details.Source, OldCommit: details.OldCommit, NewCommit: details.NewCommit, OldBaseHash: details.OldBaseHash, CurrentForkHash: details.CurrentForkHash, OldPatchHash: details.CurrentPatchHash, NewBaseHash: details.NewBaseHash, NewPatchHash: patchHash, ProposedForkHash: "sha256:" + proposedHash, Conflicts: conflicts, CreatedAt: details.CreatedAt}
	data, _ := json.Marshal(proposal)
	if err := os.WriteFile(filepath.Join(root, "proposal.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return proposal
}

func externalSkillText(body string) string {
	return "---\nname: example\ndescription: Example external skill\n---\n# Example\n" + body
}

func migrationTestConfig(t *testing.T) (Config, time.Time) {
	t.Helper()
	repo := t.TempDir()
	runtime := filepath.Join(t.TempDir(), "runtime")
	for path, contents := range map[string]string{
		filepath.Join(repo, "profile", "SOUL.md"):                       "soul\n",
		filepath.Join(repo, "profile", "AGENTS.md"):                     "agents\n",
		filepath.Join(repo, "profile", "config.yaml"):                   "config\n",
		filepath.Join(repo, "profile", "skills", "example", "SKILL.md"): "base\n",
		filepath.Join(repo, "release", "manifest.json"):                 `{"openlia":"test"}`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return testConfig(repo, runtime), time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
}
