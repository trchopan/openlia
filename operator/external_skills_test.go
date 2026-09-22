package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const testSkillCommit = "0123456789abcdef0123456789abcdef01234567"

type externalRunnerCall struct {
	name string
	args []string
	env  []string
	dir  string
}

type externalFixtureRunner struct {
	fixture string
	calls   []externalRunnerCall
}

func (r *externalFixtureRunner) RunExternal(_ context.Context, command ExternalCommand) (CommandResult, error) {
	r.calls = append(r.calls, externalRunnerCall{name: command.Name, args: slices.Clone(command.Args), env: slices.Clone(command.Env), dir: command.Dir})
	if command.Name != "git" {
		return CommandResult{}, nil
	}
	if len(command.Args) > 0 && command.Args[0] == "ls-remote" {
		return CommandResult{Stdout: []byte(testSkillCommit + "\trefs/heads/main\n")}, nil
	}
	if len(command.Args) == 2 && command.Args[0] == "init" {
		return CommandResult{}, os.MkdirAll(filepath.Join(command.Args[1], ".git"), 0o700)
	}
	if len(command.Args) >= 4 && command.Args[0] == "-C" && command.Args[2] == "checkout" {
		return CommandResult{}, copyDirContents(r.fixture, command.Args[1])
	}
	if len(command.Args) >= 4 && command.Args[0] == "-C" && command.Args[2] == "rev-parse" {
		return CommandResult{Stdout: []byte(testSkillCommit + "\n")}, nil
	}
	return CommandResult{}, nil
}

func TestSkillSourceConfigJSONAndValidation(t *testing.T) {
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	config, err := LoadConfigFromEnv(map[string]string{
		"OPENLIA_REPO_ROOT":     t.TempDir(),
		"OPENLIA_RUNTIME_ROOT":  runtimeRoot,
		"OPENLIA_SKILL_SOURCES": `[{"id":"team","url":"https://github.com/acme/skills.git","ref":"main","manifest":"catalog.json"}]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(config.SkillSources) != 1 || config.SkillSources[0].Manifest != "catalog.json" || config.SkillsCacheRoot != filepath.Join(runtimeRoot, "skill-cache") || config.SkillsEnvRoot != filepath.Join(runtimeRoot, "skill-envs") {
		t.Fatalf("unexpected skill source config: %+v", config)
	}
	if err := config.ValidatePaths(); err != nil {
		t.Fatal(err)
	}

	badURLs := []string{"http://github.com/acme/skills", "https://token@github.com/acme/skills", "https://gitlab.com/acme/skills", "https://github.com/acme/skills/extra"}
	for _, value := range badURLs {
		candidate := config
		candidate.SkillSources = []SkillSourceConfig{{ID: "team", URL: value, Ref: "main"}}
		if err := candidate.ValidatePaths(); err == nil {
			t.Errorf("unsafe URL accepted: %s", value)
		}
	}
	candidate := config
	candidate.SkillsCacheRoot = filepath.Join(t.TempDir(), "outside")
	if err := candidate.ValidatePaths(); err == nil {
		t.Fatal("cache root outside runtime accepted")
	}
}

func TestSkillSourceConfigAcceptsCanonicalAndRejectsConflictingAliases(t *testing.T) {
	base := map[string]string{"OPENLIA_REPO_ROOT": t.TempDir(), "OPENLIA_RUNTIME_ROOT": filepath.Join(t.TempDir(), "runtime")}
	base["OPENLIA_SKILL_SOURCES"] = `[{"name":"team","repository":"https://github.com/acme/skills.git","branch":"main"}]`
	config, err := LoadConfigFromEnv(base)
	if err != nil || len(config.SkillSources) != 1 || config.SkillSources[0].ID != "team" {
		t.Fatalf("canonical source: %+v, %v", config.SkillSources, err)
	}
	base["OPENLIA_SKILL_SOURCES"] = `[{"name":"team","id":"other","repository":"https://github.com/acme/skills.git","branch":"main"}]`
	if _, err := LoadConfigFromEnv(base); err == nil {
		t.Fatal("conflicting source aliases accepted")
	}
}

func TestExternalSkillFetchUsesAskpassAndPublishesImmutableSnapshot(t *testing.T) {
	config, fixture := externalSkillFixture(t, false)
	if err := os.MkdirAll(config.SecretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	token := "github_pat_secret-value"
	if err := os.WriteFile(config.SecretFile, []byte("OPENLIA_SKILLS_GIT_TOKEN="+token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &externalFixtureRunner{fixture: fixture}
	manager := NewExternalSkillManager(config, runner, Compose{})
	result, err := manager.FetchSource(context.Background(), "team")
	if err != nil {
		t.Fatal(err)
	}
	if result.Commit != testSkillCommit || result.Skills != 1 {
		t.Fatalf("unexpected fetch result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(result.Snapshot, ".git")); !os.IsNotExist(err) {
		t.Fatal("published snapshot retained .git")
	}
	current, err := os.ReadFile(filepath.Join(config.SkillsCacheRoot, "team", "current"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(current)) != testSkillCommit {
		t.Fatalf("current snapshot = %q", current)
	}
	items, err := manager.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "example" || items[0].Source != "team" {
		t.Fatalf("unexpected catalog: %+v", items)
	}
	for _, call := range runner.calls {
		joined := strings.Join(append([]string{call.name}, call.args...), " ")
		if strings.Contains(joined, token) {
			t.Fatalf("token exposed in command arguments: %s", joined)
		}
		if call.name == "git" && !containsEnvPrefix(call.env, "GIT_ASKPASS=") {
			t.Fatalf("git command omitted askpass: %+v", call)
		}
	}
}

func TestExternalSkillFetchRejectsUnsafeTrees(t *testing.T) {
	config, fixture := externalSkillFixture(t, false)
	if err := os.Symlink("SKILL.md", filepath.Join(fixture, "skills", "example", "link")); err != nil {
		t.Fatal(err)
	}
	manager := NewExternalSkillManager(config, &externalFixtureRunner{fixture: fixture}, Compose{})
	if _, err := manager.FetchSource(context.Background(), "team"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("FetchSource() error = %v", err)
	}
}

func TestExternalSkillAuditUsesFrozenIsolatedDependencyCommands(t *testing.T) {
	config, fixture := externalSkillFixture(t, true)
	if err := os.MkdirAll(config.SkillsEnvRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	publishFixture(t, config, fixture)
	runner := &externalFixtureRunner{}
	composeRunner := &recordedRunner{}
	manager := NewExternalSkillManager(config, runner, NewCompose(config, composeRunner))
	report, err := manager.Audit(context.Background(), "team", "example")
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || len(report.Checks) != 3 || report.Checks[0].Name != "tree" || report.Checks[1].Name != "python_dependencies" || report.Checks[2].Name != "javascript_dependencies" {
		t.Fatalf("audit report is not deterministic: %+v", report)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("dependencies executed on host: %+v", runner.calls)
	}
	if len(composeRunner.calls) != 2 {
		t.Fatalf("compose dependency calls = %+v", composeRunner.calls)
	}
	for _, call := range composeRunner.calls {
		if !strings.Contains(call, "run --rm --no-deps") || !strings.Contains(call, "skill-env-builder audit") {
			t.Fatalf("unexpected builder contract: %s", call)
		}
	}
	if !contentHashPattern.MatchString(report.SourceHash) || !contentHashPattern.MatchString(report.EnvironmentHash) || len(report.LockHashes) != 2 {
		t.Fatalf("audit hashes missing: %+v", report)
	}
}

func TestExternalSkillPlanFetchesAuditsAndReturnsResolvedCommit(t *testing.T) {
	config, fixture := externalSkillFixture(t, false)
	runner := &externalFixtureRunner{fixture: fixture}
	manager := NewExternalSkillManager(config, runner, NewCompose(config, &recordedRunner{}))

	plan, err := manager.Plan(context.Background(), "team", "example", false)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.OK || plan.Action != "install" || plan.Skill.Commit != testSkillCommit || !plan.Audit.OK || plan.Audit.Skill.Commit != testSkillCommit {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if len(runner.calls) == 0 || !slices.ContainsFunc(runner.calls, func(call externalRunnerCall) bool {
		return call.name == "git" && len(call.args) > 0 && call.args[0] == "ls-remote"
	}) {
		t.Fatalf("plan did not fetch its source: %+v", runner.calls)
	}
	auditPath := filepath.Join(config.MetaRoot, "external-audits", "team-"+testSkillCommit+"-example.json")
	if _, err := os.Stat(auditPath); err != nil {
		t.Fatalf("plan did not persist its audit: %v", err)
	}
}

func TestExternalSkillInstallRejectsExpectedCommitMismatch(t *testing.T) {
	config, fixture := externalSkillFixture(t, false)
	publishFixture(t, config, fixture)
	manager := NewExternalSkillManager(config, &externalFixtureRunner{}, NewCompose(config, &recordedRunner{}))

	otherCommit := "abcdef0123456789abcdef0123456789abcdef01"
	if _, err := manager.Install(context.Background(), "team", "example", false, otherCommit); err == nil || !strings.Contains(err.Error(), "changed after approval planning") {
		t.Fatalf("Install() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(config.MetaRoot, "external-skills", "example.json")); !os.IsNotExist(err) {
		t.Fatalf("commit mismatch installed metadata: %v", err)
	}
}

func TestExternalSkillCatalogRejectsSnapshotDigestTampering(t *testing.T) {
	config, fixture := externalSkillFixture(t, false)
	publishFixture(t, config, fixture)
	path := filepath.Join(config.SkillsCacheRoot, "team", testSkillCommit, "skills", "example", "SKILL.md")
	if err := os.WriteFile(path, []byte(externalSkillText("tampered\n")), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := NewExternalSkillManager(config, &externalFixtureRunner{}, Compose{}).Catalog()
	if err == nil || !strings.Contains(err.Error(), "digest revalidation") {
		t.Fatalf("Catalog() error = %v", err)
	}
}

func TestExternalSkillInstallUpdateAndUninstall(t *testing.T) {
	config, fixture := externalSkillFixture(t, false)
	publishFixture(t, config, fixture)
	for _, directory := range []string{config.DataRoot, config.MetaRoot, config.LochoRoot, config.BackupRoot} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	runner := &externalFixtureRunner{}
	composeRunner := &recordedRunner{}
	manager := NewExternalSkillManager(config, runner, NewCompose(config, composeRunner))
	manager.Now = func() time.Time { return time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC) }
	installed, err := manager.Install(context.Background(), "team", "example", false)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Action != "install" || installed.Backup == "" {
		t.Fatalf("unexpected install: %+v", installed)
	}
	metadataPath := filepath.Join(config.MetaRoot, "external-skills", "example.json")
	metadata, err := readExternalSkillMetadata(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Source != "team" || metadata.Commit != testSkillCommit {
		t.Fatalf("source metadata missing: %+v", metadata)
	}
	if _, err := manager.Install(context.Background(), "team", "example", true); err != nil {
		t.Fatal(err)
	}
	uninstalled, err := manager.Uninstall(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if uninstalled.Action != "uninstall" {
		t.Fatalf("unexpected uninstall: %+v", uninstalled)
	}
	if _, err := os.Stat(filepath.Join(config.DataRoot, "skills", "example")); !os.IsNotExist(err) {
		t.Fatal("skill remains after uninstall")
	}
}

func TestExternalSkillInstallRejectsModifiedAndBundledSkills(t *testing.T) {
	config, fixture := externalSkillFixture(t, false)
	publishFixture(t, config, fixture)
	for _, directory := range []string{config.DataRoot, config.MetaRoot, config.LochoRoot, config.BackupRoot} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	manager := NewExternalSkillManager(config, &externalFixtureRunner{}, NewCompose(config, &recordedRunner{}))
	if _, err := manager.Install(context.Background(), "team", "example", false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.DataRoot, "skills", "example", "changed"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Install(context.Background(), "team", "example", true); err == nil || !strings.Contains(err.Error(), "local modifications") {
		t.Fatalf("modified update error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(config.RepositoryRoot, "profile", "skills", "example"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(config.MetaRoot, "external-skills", "example.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Install(context.Background(), "team", "example", false); err == nil || !strings.Contains(err.Error(), "bundled") {
		t.Fatalf("bundled collision error = %v", err)
	}
}

func TestExternalSkillForkIsIdempotentAndRefreshesDirtyFork(t *testing.T) {
	config, fixture := externalSkillFixture(t, false)
	publishFixture(t, config, fixture)
	prepareProfileFixture(t, config)
	manager := NewExternalSkillManager(config, &externalFixtureRunner{}, NewCompose(config, &recordedRunner{}))
	if _, err := manager.Install(context.Background(), "team", "example", false); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(config.DataRoot, "skills", "example")
	custom := filepath.Join(destination, "custom.txt")
	if err := os.WriteFile(custom, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := manager.Fork(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if first.Action != "fork" {
		t.Fatalf("first Fork() action = %q", first.Action)
	}
	if err := os.WriteFile(custom, []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := manager.Fork(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if second.Action != "fork-refresh" {
		t.Fatalf("second Fork() action = %q", second.Action)
	}
	metadata, err := readExternalSkillMetadata(filepath.Join(config.MetaRoot, "external-skills", "example.json"))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := DirectorySHA256(destination)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ForkHash != "sha256:"+hash {
		t.Fatalf("fork hash = %q, want sha256:%s", metadata.ForkHash, hash)
	}
}

func TestInstalledExternalSkillsForSourceUsesMetadataWithoutCurrentCache(t *testing.T) {
	config, fixture := externalSkillFixture(t, false)
	publishFixture(t, config, fixture)
	prepareProfileFixture(t, config)
	manager := NewExternalSkillManager(config, &externalFixtureRunner{}, NewCompose(config, &recordedRunner{}))
	if _, err := manager.Install(context.Background(), "team", "example", false); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(config.SkillsCacheRoot, "team")); err != nil {
		t.Fatal(err)
	}

	installed, err := installedExternalSkillsForSource(config, "team")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(installed, []string{"example"}) {
		t.Fatalf("installed skills = %v", installed)
	}
}

func TestSourceQualifiedMutationsRejectMismatchedInstalledSource(t *testing.T) {
	config, fixture := externalSkillFixture(t, false)
	publishFixture(t, config, fixture)
	prepareProfileFixture(t, config)
	manager := NewExternalSkillManager(config, &externalFixtureRunner{}, NewCompose(config, &recordedRunner{}))
	if _, err := manager.Install(context.Background(), "team", "example", false); err != nil {
		t.Fatal(err)
	}

	for _, action := range []string{"update", "uninstall", "reset", "fork-refresh"} {
		t.Run(action, func(t *testing.T) {
			var output, errorOutput bytes.Buffer
			args := []string{action, "other/example"}
			if action != "fork-refresh" {
				args = append(args, "--approve")
			}
			code := runExternalSkills(context.Background(), config, args, &output, &errorOutput, true)
			if code != ExitUsage || !strings.Contains(output.String(), "installed from source team, not other") {
				t.Fatalf("code=%d output=%q stderr=%q", code, output.String(), errorOutput.String())
			}
		})
	}
}

func TestSkillSourceUsedDispatchUsesInstalledProvenance(t *testing.T) {
	config, fixture := externalSkillFixture(t, false)
	publishFixture(t, config, fixture)
	prepareProfileFixture(t, config)
	manager := NewExternalSkillManager(config, &externalFixtureRunner{}, NewCompose(config, &recordedRunner{}))
	if _, err := manager.Install(context.Background(), "team", "example", false); err != nil {
		t.Fatal(err)
	}

	var output, errorOutput bytes.Buffer
	code := runSkillSources(context.Background(), config, []string{"used", "team"}, &output, &errorOutput, true)
	if code != ExitOK || !strings.Contains(output.String(), `"example"`) {
		t.Fatalf("code=%d output=%q stderr=%q", code, output.String(), errorOutput.String())
	}
}

func TestExternalSkillLifecycleFollowsEnabledConfiguration(t *testing.T) {
	config, fixture := externalSkillFixture(t, false)
	publishFixture(t, config, fixture)
	prepareProfileFixture(t, config)
	config.SkillsConfigured = true
	config.EnabledSkills = []string{}
	manager := NewExternalSkillManager(config, &externalFixtureRunner{}, NewCompose(config, &recordedRunner{}))
	installed, err := manager.Install(context.Background(), "team", "example", false)
	if err != nil {
		t.Fatal(err)
	}
	active, disabled := externalSkillPaths(config, "example")
	if installed.Activated {
		t.Fatalf("disabled install reported activated: %+v", installed)
	}
	if _, err := os.Stat(disabled); err != nil {
		t.Fatalf("disabled skill was not installed: %v", err)
	}
	if _, err := os.Stat(active); !os.IsNotExist(err) {
		t.Fatalf("disabled skill exists at active path: %v", err)
	}
	originalHash, err := DirectorySHA256(disabled)
	if err != nil {
		t.Fatal(err)
	}

	config.EnabledSkills = []string{"example"}
	profile := NewProfileOperator(config)
	enabledResult, err := profile.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if result := findSkillResult(enabledResult.Skills.Results, "example"); result.Action != "enabled" || result.ResultState != "managed" {
		t.Fatalf("unexpected enabled result: %+v", result)
	}
	activeHash, err := DirectorySHA256(active)
	if err != nil || activeHash != originalHash {
		t.Fatalf("profile sync changed external content: hash=%s err=%v", activeHash, err)
	}
	status, err := profile.Status("example")
	if err != nil {
		t.Fatal(err)
	}
	if status.Skills.Results[0].State != "managed" || status.Skills.Results[0].Action != "unchanged" {
		t.Fatalf("unexpected enabled status: %+v", status.Skills.Results[0])
	}

	config.EnabledSkills = nil
	profile = NewProfileOperator(config)
	disabledResult, err := profile.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if result := findSkillResult(disabledResult.Skills.Results, "example"); result.Action != "disabled" || result.ResultState != "disabled" {
		t.Fatalf("unexpected disabled result: %+v", result)
	}
	status, err = profile.Status("")
	if err != nil {
		t.Fatal(err)
	}
	if result := findSkillResult(status.Skills.Results, "example"); result.State != "disabled" || result.Action != "disabled" {
		t.Fatalf("unexpected disabled status: %+v", result)
	}
	disabledHash, err := DirectorySHA256(disabled)
	if err != nil || disabledHash != originalHash {
		t.Fatalf("disable changed external content: hash=%s err=%v", disabledHash, err)
	}
}

func TestExternalSkillDisabledMutationsAndLifecycleCollision(t *testing.T) {
	config, fixture := externalSkillFixture(t, false)
	publishFixture(t, config, fixture)
	prepareProfileFixture(t, config)
	config.SkillsConfigured = true
	manager := NewExternalSkillManager(config, &externalFixtureRunner{}, NewCompose(config, &recordedRunner{}))
	if _, err := manager.Install(context.Background(), "team", "example", false); err != nil {
		t.Fatal(err)
	}
	active, disabled := externalSkillPaths(config, "example")
	if err := os.WriteFile(filepath.Join(disabled, "custom"), []byte("custom\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	forked, err := manager.Fork(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if forked.Activated {
		t.Fatalf("disabled fork reported activated: %+v", forked)
	}
	reset, err := manager.Reset(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if reset.Activated {
		t.Fatalf("disabled reset reported activated: %+v", reset)
	}
	if _, err := os.Stat(filepath.Join(disabled, "custom")); !os.IsNotExist(err) {
		t.Fatalf("reset did not restore disabled tree: %v", err)
	}
	if err := CopyDir(disabled, active); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProfileOperator(config).Sync(); err == nil || !strings.Contains(err.Error(), "both active and disabled") {
		t.Fatalf("lifecycle collision error = %v", err)
	}
	if err := os.RemoveAll(active); err != nil {
		t.Fatal(err)
	}
	uninstalled, err := manager.Uninstall(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if uninstalled.Activated {
		t.Fatalf("disabled uninstall reported activated: %+v", uninstalled)
	}
	if _, err := os.Stat(disabled); !os.IsNotExist(err) {
		t.Fatalf("disabled tree remains after uninstall: %v", err)
	}
}

func TestExternalSkillTransactionRollsBackTreeAndMetadata(t *testing.T) {
	config, _ := externalSkillFixture(t, false)
	manager := NewExternalSkillManager(config, &externalFixtureRunner{}, NewCompose(config, &recordedRunner{}))
	if err := manager.prepareRuntime(); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(config.DataRoot, "skills", "example")
	metadataPath := filepath.Join(config.MetaRoot, "external-skills", "example.json")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "old"), []byte("tree"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataPath, []byte("old metadata\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantTree, _ := DirectorySHA256(destination)
	err := manager.transactionalSkillMutation(destination, metadataPath, func() error {
		if err := os.RemoveAll(destination); err != nil {
			return err
		}
		if err := os.MkdirAll(destination, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(destination, "new"), []byte("tree"), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(metadataPath, []byte("new metadata\n"), 0o600); err != nil {
			return err
		}
		return errors.New("injected failure")
	})
	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("transaction error = %v", err)
	}
	gotTree, _ := DirectorySHA256(destination)
	metadata, _ := os.ReadFile(metadataPath)
	if gotTree != wantTree || string(metadata) != "old metadata\n" {
		t.Fatalf("rollback failed: tree=%s metadata=%q", gotTree, metadata)
	}
}

func TestExternalSkillCommandRequiresApprovalAndParsesSourceName(t *testing.T) {
	config, _ := externalSkillFixture(t, false)
	var output, errorOutput bytes.Buffer
	code := runExternalSkills(context.Background(), config, []string{"install", "team/example"}, &output, &errorOutput, true)
	if code != ExitUsage || !strings.Contains(output.String(), "requires --approve") {
		t.Fatalf("code=%d output=%q stderr=%q", code, output.String(), errorOutput.String())
	}
	output.Reset()
	code = runExternalSkills(context.Background(), config, []string{"install", "example", "--source", "other", "--approve"}, &output, &errorOutput, true)
	if code != ExitFailure || !strings.Contains(output.String(), "unknown skill source") {
		t.Fatalf("source parsing code=%d output=%q", code, output.String())
	}
}

func externalSkillFixture(t *testing.T, dependencies bool) (Config, string) {
	t.Helper()
	config := testConfig(t.TempDir(), filepath.Join(t.TempDir(), "runtime"))
	config.SkillSources = []SkillSourceConfig{{ID: "team", URL: "https://github.com/acme/skills.git", Ref: "main"}}
	fixture := t.TempDir()
	skillRoot := filepath.Join(fixture, "skills", "example")
	if err := os.MkdirAll(skillRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := SkillCollectionManifest{Schema: 1, Skills: []SkillCollectionManifestItem{{Name: "example", Path: "skills/example", Version: "1.2.3", Test: []string{"go", "test", "./..."}}}}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(fixture, externalSkillManifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("---\nname: example\ndescription: Example external skill\n---\n# Example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if dependencies {
		for name, contents := range map[string]string{"pyproject.toml": "[project]\nname='example'\n", "uv.lock": "version = 1\n", "package.json": "{}\n", "bun.lock": "lockfileVersion = 1\n"} {
			if err := os.WriteFile(filepath.Join(skillRoot, name), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return config, fixture
}

func publishFixture(t *testing.T, config Config, fixture string) {
	t.Helper()
	destination := filepath.Join(config.SkillsCacheRoot, "team", testSkillCommit)
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CopyDir(fixture, destination); err != nil {
		t.Fatal(err)
	}
	digest, err := DirectorySHA256(destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.SkillsCacheRoot, "team", testSkillCommit+".sha256"), []byte("sha256:"+digest+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.SkillsCacheRoot, "team", "current"), []byte(testSkillCommit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func prepareProfileFixture(t *testing.T, config Config) {
	t.Helper()
	for _, directory := range []string{
		filepath.Join(config.RepositoryRoot, "profile", "skills"),
		config.DataRoot,
		config.MetaRoot,
		config.LochoRoot,
		config.BackupRoot,
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func findSkillResult(results []SkillResult, name string) SkillResult {
	for _, result := range results {
		if result.Name == name {
			return result
		}
	}
	return SkillResult{}
}

func containsEnvPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
