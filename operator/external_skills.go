package operator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const externalSkillManifestName = "openlia-skills.json"

// SkillCollectionManifest is the dependency-free collection format. A source
// contains JSON shaped as {"schema":1,"skills":[{"name":"x","path":"skills/x","version":"1.0.0"}]}.
// Paths are source-relative directories, names are globally unique, and every
// skill directory must contain SKILL.md.
type SkillCollectionManifest struct {
	Schema int                           `json:"schema"`
	Skills []SkillCollectionManifestItem `json:"skills"`
}

type SkillCollectionManifestItem struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Version string   `json:"version"`
	Test    []string `json:"test,omitempty"`
}

type ExternalCommand struct {
	Name string
	Args []string
	Env  []string
	Dir  string
}

type ExternalCommandRunner interface {
	RunExternal(context.Context, ExternalCommand) (CommandResult, error)
}

type ExecExternalRunner struct{}

func (ExecExternalRunner) RunExternal(ctx context.Context, spec ExternalCommand) (CommandResult, error) {
	command := exec.CommandContext(ctx, spec.Name, spec.Args...)
	command.Dir = spec.Dir
	command.Env = spec.Env
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	result := CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return result, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.ExitCode = exitError.ExitCode()
	}
	detail := strings.TrimSpace(stderr.String())
	if len(detail) > 2000 {
		detail = detail[:2000]
	}
	if detail != "" {
		return result, fmt.Errorf("run %s: %w: %s", spec.Name, err, detail)
	}
	return result, fmt.Errorf("run %s: %w", spec.Name, err)
}

type ExternalSkillManager struct {
	Config  Config
	Runner  ExternalCommandRunner
	Compose Compose
	Now     func() time.Time
}

type SkillSourceResult struct {
	OK       bool   `json:"ok"`
	Action   string `json:"action"`
	Source   string `json:"source"`
	Commit   string `json:"commit"`
	Snapshot string `json:"snapshot,omitempty"`
	Skills   int    `json:"skills,omitempty"`
	Digest   string `json:"digest,omitempty"`
}

type ExternalSkillCatalogItem struct {
	Name               string `json:"name"`
	Source             string `json:"source"`
	Commit             string `json:"commit"`
	Version            string `json:"version"`
	Path               string `json:"path"`
	Installed          bool   `json:"installed"`
	UpdateAvailable    bool   `json:"update_available"`
	LocalModifications bool   `json:"local_modifications"`
	Ownership          string `json:"ownership,omitempty"`
}

type SkillAuditCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type SkillAuditReport struct {
	Schema          int                      `json:"schema"`
	OK              bool                     `json:"ok"`
	Action          string                   `json:"action"`
	Skill           ExternalSkillCatalogItem `json:"skill"`
	Checks          []SkillAuditCheck        `json:"checks"`
	SourceHash      string                   `json:"source_sha256"`
	LockHashes      map[string]string        `json:"lock_hashes"`
	EnvironmentHash string                   `json:"environment_sha256"`
}

type ExternalSkillMetadata struct {
	Schema       int    `json:"schema"`
	Name         string `json:"name"`
	Source       string `json:"source"`
	Commit       string `json:"commit"`
	Version      string `json:"version"`
	ContentHash  string `json:"content_sha256"`
	InstalledAt  string `json:"installed_at"`
	UpdatedAt    string `json:"updated_at"`
	Ownership    string `json:"ownership"`
	BaseSnapshot string `json:"base_snapshot"`
	PatchPath    string `json:"patch_path"`
	PatchHash    string `json:"patch_sha256"`
	ForkHash     string `json:"fork_sha256,omitempty"`
	ForkedAt     string `json:"forked_at,omitempty"`
	Environment  string `json:"environment_sha256"`
}

type ExternalSkillResult struct {
	OK        bool   `json:"ok"`
	Action    string `json:"action"`
	Name      string `json:"name"`
	Source    string `json:"source,omitempty"`
	Commit    string `json:"commit,omitempty"`
	Version   string `json:"version,omitempty"`
	Backup    string `json:"backup,omitempty"`
	Activated bool   `json:"activated"`
}

type ExternalSkillPlan struct {
	Schema           int                      `json:"schema"`
	OK               bool                     `json:"ok"`
	Action           string                   `json:"action"`
	Skill            ExternalSkillCatalogItem `json:"skill"`
	InstalledCommit  string                   `json:"installed_commit,omitempty"`
	InstalledVersion string                   `json:"installed_version,omitempty"`
	Audit            SkillAuditReport         `json:"audit"`
}

func skillEnabled(config Config, name string) bool {
	if !config.SkillsConfigured && len(config.EnabledSkills) == 0 {
		return true
	}
	for _, enabled := range config.EnabledSkills {
		if enabled == "*" || enabled == name {
			return true
		}
	}
	return false
}

func externalSkillPaths(config Config, name string) (string, string) {
	root := filepath.Join(config.DataRoot, "skills")
	return filepath.Join(root, name), filepath.Join(root, ".openlia-disabled", name)
}

func locateExternalSkillTree(config Config, name string) (string, bool, error) {
	active, disabled := externalSkillPaths(config, name)
	activeInfo, activeErr := os.Lstat(active)
	disabledInfo, disabledErr := os.Lstat(disabled)
	activeExists, disabledExists := activeErr == nil, disabledErr == nil
	if activeErr != nil && !errors.Is(activeErr, os.ErrNotExist) {
		return "", false, activeErr
	}
	if disabledErr != nil && !errors.Is(disabledErr, os.ErrNotExist) {
		return "", false, disabledErr
	}
	if activeExists && disabledExists {
		return "", false, fmt.Errorf("external skill exists in both active and disabled locations: %s", name)
	}
	if activeExists {
		if !activeInfo.IsDir() || activeInfo.Mode()&os.ModeSymlink != 0 {
			return "", false, fmt.Errorf("external skill tree is unsafe: %s", name)
		}
		return active, true, nil
	}
	if disabledExists {
		if !disabledInfo.IsDir() || disabledInfo.Mode()&os.ModeSymlink != 0 {
			return "", false, fmt.Errorf("external skill tree is unsafe: %s", name)
		}
		return disabled, false, nil
	}
	return "", false, os.ErrNotExist
}

func moveExternalSkillTree(source, destination, name string) error {
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("external skill destination already exists: %s", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := EnsureDir(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	if err := os.Rename(source, destination); err != nil {
		return fmt.Errorf("move external skill %s: %w", name, err)
	}
	return nil
}

func NewExternalSkillManager(config Config, runner ExternalCommandRunner, compose Compose) *ExternalSkillManager {
	if runner == nil {
		runner = ExecExternalRunner{}
	}
	if compose.Config.RuntimeRoot == "" {
		compose = NewCompose(config, nil)
	}
	return &ExternalSkillManager{Config: config, Runner: runner, Compose: compose, Now: func() time.Time { return time.Now().UTC() }}
}

func (m *ExternalSkillManager) CheckSource(ctx context.Context, id string) (SkillSourceResult, error) {
	source, err := m.source(id)
	if err != nil {
		return SkillSourceResult{}, err
	}
	commit, err := m.resolveCommit(ctx, source)
	if err != nil {
		return SkillSourceResult{}, err
	}
	return SkillSourceResult{OK: true, Action: "check", Source: id, Commit: commit}, nil
}

func (m *ExternalSkillManager) FetchSource(ctx context.Context, id string) (SkillSourceResult, error) {
	if err := m.Config.ValidatePaths(); err != nil {
		return SkillSourceResult{}, err
	}
	source, err := m.source(id)
	if err != nil {
		return SkillSourceResult{}, err
	}
	release, err := m.operationLock("source-" + id)
	if err != nil {
		return SkillSourceResult{}, err
	}
	defer release()
	commit, err := m.resolveCommit(ctx, source)
	if err != nil {
		return SkillSourceResult{}, err
	}
	destination := filepath.Join(m.Config.SkillsCacheRoot, source.ID, commit)
	if info, statErr := os.Stat(destination); statErr == nil && info.IsDir() {
		manifest, readErr := readCollectionManifest(destination, source.Manifest)
		if readErr != nil {
			return SkillSourceResult{}, readErr
		}
		if err := validateCollectionTree(destination, manifest); err != nil {
			return SkillSourceResult{}, err
		}
		digest, err := DirectorySHA256(destination)
		if err != nil {
			return SkillSourceResult{}, err
		}
		digest = "sha256:" + digest
		stored, readErr := os.ReadFile(filepath.Join(m.Config.SkillsCacheRoot, source.ID, commit+".sha256"))
		if readErr != nil || strings.TrimSpace(string(stored)) != digest {
			return SkillSourceResult{}, fmt.Errorf("cached skill source %s failed digest revalidation", id)
		}
		if err := AtomicWriteFile(filepath.Join(m.Config.SkillsCacheRoot, source.ID, "current"), []byte(commit+"\n"), 0o600); err != nil {
			return SkillSourceResult{}, err
		}
		return SkillSourceResult{OK: true, Action: "fetch", Source: id, Commit: commit, Snapshot: destination, Skills: len(manifest.Skills), Digest: digest}, nil
	}
	if err := EnsureDir(filepath.Join(m.Config.SkillsCacheRoot, source.ID), 0o755); err != nil {
		return SkillSourceResult{}, err
	}
	staging, err := os.MkdirTemp(filepath.Join(m.Config.SkillsCacheRoot, source.ID), ".fetch-*")
	if err != nil {
		return SkillSourceResult{}, err
	}
	defer os.RemoveAll(staging)
	env, cleanup, err := m.gitEnvironment()
	if err != nil {
		return SkillSourceResult{}, err
	}
	defer cleanup()
	commands := [][]string{{"init", staging}, {"-C", staging, "remote", "add", "origin", source.URL}, {"-C", staging, "fetch", "--depth=1", "origin", source.Ref}, {"-C", staging, "checkout", "--detach", commit}}
	for _, args := range commands {
		if _, err := m.Runner.RunExternal(ctx, ExternalCommand{Name: "git", Args: args, Env: env}); err != nil {
			return SkillSourceResult{}, fmt.Errorf("fetch skill source %s: %w", id, err)
		}
	}
	resolved, err := m.Runner.RunExternal(ctx, ExternalCommand{Name: "git", Args: []string{"-C", staging, "rev-parse", "HEAD^{commit}"}, Env: env})
	if err != nil || strings.TrimSpace(string(resolved.Stdout)) != commit {
		return SkillSourceResult{}, fmt.Errorf("skill source %s did not checkout the resolved commit", id)
	}
	index, err := m.Runner.RunExternal(ctx, ExternalCommand{Name: "git", Args: []string{"-C", staging, "ls-files", "--stage"}, Env: env})
	if err != nil {
		return SkillSourceResult{}, fmt.Errorf("inspect skill source %s: %w", id, err)
	}
	for _, line := range strings.Split(string(index.Stdout), "\n") {
		if strings.HasPrefix(line, "160000 ") {
			return SkillSourceResult{}, fmt.Errorf("skill source %s contains submodules", id)
		}
	}
	if _, err := os.Lstat(filepath.Join(staging, ".gitmodules")); err == nil {
		return SkillSourceResult{}, fmt.Errorf("skill source %s contains submodules", id)
	}
	if err := os.RemoveAll(filepath.Join(staging, ".git")); err != nil {
		return SkillSourceResult{}, err
	}
	manifest, err := readCollectionManifest(staging, source.Manifest)
	if err != nil {
		return SkillSourceResult{}, err
	}
	if err := validateCollectionTree(staging, manifest); err != nil {
		return SkillSourceResult{}, err
	}
	if err := normalizeSkill(staging); err != nil {
		return SkillSourceResult{}, err
	}
	if err := os.Rename(staging, destination); err != nil {
		return SkillSourceResult{}, fmt.Errorf("publish skill snapshot: %w", err)
	}
	digest, err := DirectorySHA256(destination)
	if err != nil {
		return SkillSourceResult{}, err
	}
	digest = "sha256:" + digest
	if err := AtomicWriteFile(filepath.Join(m.Config.SkillsCacheRoot, source.ID, commit+".sha256"), []byte(digest+"\n"), 0o600); err != nil {
		return SkillSourceResult{}, err
	}
	if err := AtomicWriteFile(filepath.Join(m.Config.SkillsCacheRoot, source.ID, "current"), []byte(commit+"\n"), 0o600); err != nil {
		return SkillSourceResult{}, err
	}
	return SkillSourceResult{OK: true, Action: "fetch", Source: id, Commit: commit, Snapshot: destination, Skills: len(manifest.Skills), Digest: digest}, nil
}

func (m *ExternalSkillManager) Catalog() ([]ExternalSkillCatalogItem, error) {
	if err := m.Config.ValidatePaths(); err != nil {
		return nil, err
	}
	var result []ExternalSkillCatalogItem
	for _, source := range m.Config.SkillSources {
		root := filepath.Join(m.Config.SkillsCacheRoot, source.ID)
		data, err := os.ReadFile(filepath.Join(root, "current"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		commit := strings.TrimSpace(string(data))
		if !isCommit(commit) {
			return nil, fmt.Errorf("invalid current snapshot for skill source %s", source.ID)
		}
		snapshot := filepath.Join(root, commit)
		if err := validateCachedSnapshot(root, commit, snapshot); err != nil {
			return nil, err
		}
		manifest, err := readCollectionManifest(snapshot, source.Manifest)
		if err != nil {
			return nil, err
		}
		for _, skill := range manifest.Skills {
			item := ExternalSkillCatalogItem{Name: skill.Name, Source: source.ID, Commit: commit, Version: skill.Version, Path: skill.Path}
			if metadata, metadataErr := readExternalSkillMetadata(filepath.Join(m.Config.MetaRoot, "external-skills", skill.Name+".json")); metadataErr == nil && metadata.Source == source.ID {
				item.Installed, item.UpdateAvailable, item.Ownership = true, metadata.Commit != commit, metadata.Ownership
				destination, _, locateErr := locateExternalSkillTree(m.Config, skill.Name)
				hash, hashErr := DirectorySHA256(destination)
				if locateErr != nil {
					hashErr = locateErr
				}
				item.LocalModifications = hashErr != nil || "sha256:"+hash != externalExpectedHash(metadata)
			}
			result = append(result, item)
		}
	}
	installedRoot := filepath.Join(m.Config.MetaRoot, "external-skills")
	installedEntries, err := os.ReadDir(installedRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, entry := range installedEntries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		metadata, readErr := readExternalSkillMetadata(filepath.Join(installedRoot, entry.Name()))
		if readErr != nil {
			return nil, readErr
		}
		found := false
		for _, item := range result {
			if item.Name == metadata.Name && item.Source == metadata.Source {
				found = true
				break
			}
		}
		if found {
			continue
		}
		path, _, locateErr := locateExternalSkillTree(m.Config, metadata.Name)
		currentHash := ""
		if locateErr == nil {
			if hash, hashErr := DirectorySHA256(path); hashErr == nil {
				currentHash = "sha256:" + hash
			}
		}
		result = append(result, ExternalSkillCatalogItem{Name: metadata.Name, Source: metadata.Source, Commit: metadata.Commit, Version: metadata.Version, Installed: true, LocalModifications: currentHash != externalExpectedHash(metadata), Ownership: metadata.Ownership})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		if result[i].Source != result[j].Source {
			return result[i].Source < result[j].Source
		}
		return result[i].Commit < result[j].Commit
	})
	return result, nil
}

func (m *ExternalSkillManager) Audit(ctx context.Context, sourceID, name string) (SkillAuditReport, error) {
	item, root, manifestItem, err := m.catalogSkill(sourceID, name)
	if err != nil {
		return SkillAuditReport{}, err
	}
	return m.auditSkillTree(ctx, item, manifestItem, filepath.Join(root, filepath.FromSlash(item.Path)))
}

func (m *ExternalSkillManager) Plan(ctx context.Context, sourceID, name string, update bool) (ExternalSkillPlan, error) {
	var installed ExternalSkillMetadata
	if update {
		var err error
		installed, err = readExternalSkillMetadata(filepath.Join(m.Config.MetaRoot, "external-skills", name+".json"))
		if err != nil {
			return ExternalSkillPlan{}, fmt.Errorf("external skill is not installed: %s", name)
		}
		if sourceID == "" {
			sourceID = installed.Source
		} else if sourceID != installed.Source {
			return ExternalSkillPlan{}, fmt.Errorf("external skill %s is installed from source %s, not %s", name, installed.Source, sourceID)
		}
	} else if sourceID == "" {
		return ExternalSkillPlan{}, fmt.Errorf("external skill installation requires a source")
	}
	if _, err := m.FetchSource(ctx, sourceID); err != nil {
		return ExternalSkillPlan{}, err
	}
	audit, err := m.Audit(ctx, sourceID, name)
	if err != nil {
		return ExternalSkillPlan{}, err
	}
	plan := ExternalSkillPlan{Schema: 1, OK: true, Action: "install", Skill: audit.Skill, Audit: audit}
	if update {
		plan.Action = "update"
		plan.InstalledCommit = installed.Commit
		plan.InstalledVersion = installed.Version
	}
	return plan, nil
}

func (m *ExternalSkillManager) auditSkillTree(ctx context.Context, item ExternalSkillCatalogItem, manifestItem SkillCollectionManifestItem, skillRoot string) (SkillAuditReport, error) {
	name := item.Name
	checks := []SkillAuditCheck{{Name: "tree", Status: "pass", Detail: "regular files only; SKILL.md frontmatter present"}}
	if err := validateRegularTree(skillRoot); err != nil {
		return SkillAuditReport{}, err
	}
	if err := validateSkillTree(skillRoot, name); err != nil {
		return SkillAuditReport{}, err
	}
	content, err := DirectorySHA256(skillRoot)
	if err != nil {
		return SkillAuditReport{}, err
	}
	lockHashes := make(map[string]string)
	actions := make([]string, 0, 2)
	if _, err := os.Stat(filepath.Join(skillRoot, "pyproject.toml")); err == nil {
		if info, lockErr := os.Stat(filepath.Join(skillRoot, "uv.lock")); lockErr != nil || !info.Mode().IsRegular() {
			return SkillAuditReport{}, fmt.Errorf("skill %s requires uv.lock with pyproject.toml", name)
		}
		lockHashes["uv.lock"], err = prefixedFileHash(filepath.Join(skillRoot, "uv.lock"))
		if err != nil {
			return SkillAuditReport{}, err
		}
		actions = append(actions, "python")
		checks = append(checks, SkillAuditCheck{Name: "python_dependencies", Status: "pass", Detail: "uv sync --locked --no-install-project"})
	}
	if _, err := os.Stat(filepath.Join(skillRoot, "package.json")); err == nil {
		if info, lockErr := os.Stat(filepath.Join(skillRoot, "bun.lock")); lockErr != nil || !info.Mode().IsRegular() {
			return SkillAuditReport{}, fmt.Errorf("skill %s requires bun.lock with package.json", name)
		}
		lockHashes["bun.lock"], err = prefixedFileHash(filepath.Join(skillRoot, "bun.lock"))
		if err != nil {
			return SkillAuditReport{}, err
		}
		actions = append(actions, "javascript")
		checks = append(checks, SkillAuditCheck{Name: "javascript_dependencies", Status: "pass", Detail: "bun install --frozen-lockfile --ignore-scripts"})
	}
	if len(checks) == 1 {
		checks = append(checks, SkillAuditCheck{Name: "dependencies", Status: "pass", Detail: "none declared"})
	}
	environmentHash := dependencyEnvironmentHash("sha256:"+content, lockHashes)
	if len(actions) > 0 {
		release, err := m.operationLock("env-" + environmentHash[7:])
		if err != nil {
			return SkillAuditReport{}, err
		}
		defer release()
		environmentRoot := filepath.Join(m.Config.SkillsEnvRoot, environmentHash[7:])
		stagingEnvironment, err := os.MkdirTemp(m.Config.SkillsEnvRoot, ".env-build-")
		if err != nil {
			return SkillAuditReport{}, err
		}
		defer os.RemoveAll(stagingEnvironment)
		if err := os.Chmod(stagingEnvironment, 0o755); err != nil {
			return SkillAuditReport{}, err
		}
		if os.Geteuid() == 0 {
			if err := os.Chown(stagingEnvironment, 10000, 10000); err != nil {
				return SkillAuditReport{}, err
			}
		}
		for _, dependency := range actions {
			containerSource := containerPath(m.Config.SkillsCacheRoot, "/opt/openlia/skill-cache", skillRoot)
			if containerSource == "" {
				containerSource = containerPath(filepath.Join(m.Config.DataRoot, ".openlia", "proposals"), "/opt/openlia/migration-source", skillRoot)
			}
			if containerSource == "" {
				return SkillAuditReport{}, fmt.Errorf("skill audit tree is outside configured roots")
			}
			containerEnvironment := containerPath(m.Config.SkillsEnvRoot, "/opt/openlia/skill-envs", stagingEnvironment)
			if _, err := m.Compose.Run(ctx, "run", "--rm", "--no-deps", "--user", m.builderUser(), "-e", "OPENLIA_SKILL_ACTION=audit", "-e", "OPENLIA_SKILL_DEPENDENCY="+dependency, "-e", "OPENLIA_SKILL_SOURCE="+containerSource, "-e", "OPENLIA_SKILL_ENV="+containerEnvironment, "skill-env-builder", "audit"); err != nil {
				return SkillAuditReport{}, fmt.Errorf("audit %s dependencies for %s: %w", dependency, name, err)
			}
		}
		if err := publishSkillEnvironment(stagingEnvironment, environmentRoot); err != nil {
			return SkillAuditReport{}, err
		}
	}
	auditItem := item
	auditItem.Installed, auditItem.UpdateAvailable, auditItem.LocalModifications, auditItem.Ownership = false, false, false, ""
	report := SkillAuditReport{Schema: 1, OK: true, Action: "audit", Skill: auditItem, Checks: checks, SourceHash: "sha256:" + content, LockHashes: lockHashes, EnvironmentHash: environmentHash}
	data, _ := json.Marshal(report)
	auditRoot := filepath.Join(m.Config.MetaRoot, "external-audits")
	if err := EnsureDir(auditRoot, 0o700); err != nil {
		return SkillAuditReport{}, err
	}
	if err := AtomicWriteFile(filepath.Join(auditRoot, item.Source+"-"+item.Commit+"-"+name+".json"), append(data, '\n'), 0o600); err != nil {
		return SkillAuditReport{}, err
	}
	return report, nil
}

func (m *ExternalSkillManager) Install(ctx context.Context, sourceID, name string, update bool, expectedCommits ...string) (ExternalSkillResult, error) {
	release, err := m.operationLock("skill-" + name)
	if err != nil {
		return ExternalSkillResult{}, err
	}
	defer release()
	if update {
		metadata, metadataErr := readExternalSkillMetadata(filepath.Join(m.Config.MetaRoot, "external-skills", name+".json"))
		if metadataErr != nil {
			return ExternalSkillResult{}, fmt.Errorf("external skill is not installed: %s", name)
		}
		if sourceID == "" {
			sourceID = metadata.Source
		} else if sourceID != metadata.Source {
			return ExternalSkillResult{}, fmt.Errorf("external skill %s is installed from source %s, not %s", name, metadata.Source, sourceID)
		}
		if _, fetchErr := m.FetchSource(ctx, sourceID); fetchErr != nil {
			return ExternalSkillResult{}, fetchErr
		}
	} else {
		if sourceID == "" {
			return ExternalSkillResult{}, fmt.Errorf("external skill installation requires a source")
		}
		if _, fetchErr := m.FetchSource(ctx, sourceID); fetchErr != nil {
			return ExternalSkillResult{}, fetchErr
		}
	}
	report, err := m.Audit(ctx, sourceID, name)
	if err != nil {
		return ExternalSkillResult{}, err
	}
	item := report.Skill
	if len(expectedCommits) > 0 && expectedCommits[0] != "" && item.Commit != expectedCommits[0] {
		return ExternalSkillResult{}, fmt.Errorf("skill source changed after approval planning; review the installation plan again")
	}
	sourceRoot := filepath.Join(m.Config.SkillsCacheRoot, item.Source, item.Commit, filepath.FromSlash(item.Path))
	activeDestination, disabledDestination := externalSkillPaths(m.Config, item.Name)
	desiredDestination := disabledDestination
	activated := skillEnabled(m.Config, item.Name)
	if activated {
		desiredDestination = activeDestination
	}
	destination := desiredDestination
	metadataPath := filepath.Join(m.Config.MetaRoot, "external-skills", item.Name+".json")
	if _, err := os.Stat(filepath.Join(m.Config.RepositoryRoot, "profile", "skills", item.Name)); err == nil || item.Name == protectedSkillName {
		return ExternalSkillResult{}, fmt.Errorf("external skill conflicts with bundled skill %s", item.Name)
	}
	old, oldErr := readExternalSkillMetadata(metadataPath)
	if !update && oldErr == nil {
		return ExternalSkillResult{}, fmt.Errorf("external skill is already installed: %s", item.Name)
	}
	if update && oldErr != nil {
		return ExternalSkillResult{}, fmt.Errorf("external skill is not installed: %s", item.Name)
	}
	if oldErr != nil {
		if _, _, locateErr := locateExternalSkillTree(m.Config, item.Name); locateErr == nil {
			return ExternalSkillResult{}, fmt.Errorf("external skill destination already exists: %s", item.Name)
		} else if !errors.Is(locateErr, os.ErrNotExist) {
			return ExternalSkillResult{}, locateErr
		}
	}
	if oldErr == nil {
		current, _, locateErr := locateExternalSkillTree(m.Config, item.Name)
		if locateErr != nil {
			return ExternalSkillResult{}, fmt.Errorf("locate external skill %s: %w", item.Name, locateErr)
		}
		hash, hashErr := DirectorySHA256(current)
		if hashErr != nil || "sha256:"+hash != externalExpectedHash(old) {
			return ExternalSkillResult{}, fmt.Errorf("external skill has local modifications: %s", item.Name)
		}
		if update && old.Ownership == "forked" && old.Commit != item.Commit {
			if _, err := PrepareSkillMigration(m.Config, item.Name, m.now()); err != nil {
				return ExternalSkillResult{}, err
			}
			return ExternalSkillResult{OK: true, Action: "update_available", Name: name, Source: item.Source, Commit: item.Commit, Version: item.Version, Activated: activated}, nil
		}
		if update && old.Commit == item.Commit {
			return ExternalSkillResult{OK: true, Action: "unchanged", Name: name, Source: item.Source, Commit: item.Commit, Version: item.Version, Activated: activated}, nil
		}
		destination = current
	}
	if err := m.prepareRuntime(); err != nil {
		return ExternalSkillResult{}, err
	}
	if err := EnsureDir(filepath.Dir(destination), 0o700); err != nil {
		return ExternalSkillResult{}, err
	}
	backup, err := CreateBackup(m.Config, "external-skill", m.now(), m.Compose)
	if err != nil {
		return ExternalSkillResult{}, err
	}
	action := externalAction(update)
	err = m.transactionalSkillMutation(destination, metadataPath, func() error {
		return m.withPausedHermes(ctx, func() error {
			if _, statErr := os.Lstat(destination); errors.Is(statErr, os.ErrNotExist) {
				if err := publishDirectory(sourceRoot, destination); err != nil {
					return err
				}
			} else if statErr != nil {
				return statErr
			} else if err := AtomicCopyDir(sourceRoot, destination); err != nil {
				return err
			}
			if err := normalizeSkill(destination); err != nil {
				return err
			}
			hash, err := DirectorySHA256(destination)
			if err != nil {
				return err
			}
			stamp := utcTimestamp(m.now())
			installed := stamp
			if oldErr == nil {
				installed = old.InstalledAt
			}
			baseRelative := "external-skills/" + item.Name + "/base"
			patchRelative := "external-skills/" + item.Name + "/customization.patch"
			if err := replaceDirectory(sourceRoot, filepath.Join(m.Config.MetaRoot, filepath.FromSlash(baseRelative))); err != nil {
				return err
			}
			patch, err := GenerateSkillPatch(filepath.Join(m.Config.MetaRoot, filepath.FromSlash(baseRelative)), destination, item.Name)
			if err != nil {
				return err
			}
			patchHash, err := WriteSkillPatch(filepath.Join(m.Config.MetaRoot, filepath.FromSlash(patchRelative)), patch)
			if err != nil {
				return err
			}
			metadata := ExternalSkillMetadata{Schema: 1, Name: item.Name, Source: item.Source, Commit: item.Commit, Version: item.Version, ContentHash: "sha256:" + hash, InstalledAt: installed, UpdatedAt: stamp, Ownership: "managed", BaseSnapshot: baseRelative, PatchPath: patchRelative, PatchHash: patchHash, Environment: report.EnvironmentHash}
			data, _ := json.Marshal(metadata)
			if err := AtomicWriteFile(metadataPath, append(data, '\n'), 0o600); err != nil {
				return err
			}
			if err := activateSkillEnvironment(m.Config.SkillsEnvRoot, item.Name, report.EnvironmentHash); err != nil {
				return err
			}
			return RecordChange(m.Config, action, "ok", backup.Archive, "external skill="+name+" source="+item.Source+" commit="+item.Commit, m.now())
		})
	})
	if err != nil {
		_ = RecordChange(m.Config, externalAction(update), "failed", backup.Archive, err.Error(), m.now())
		return ExternalSkillResult{}, err
	}
	if destination != desiredDestination {
		if err := moveExternalSkillTree(destination, desiredDestination, item.Name); err != nil {
			return ExternalSkillResult{}, err
		}
	}
	return ExternalSkillResult{OK: true, Action: action, Name: name, Source: item.Source, Commit: item.Commit, Version: item.Version, Backup: backup.Archive, Activated: activated}, nil
}

func (m *ExternalSkillManager) Uninstall(ctx context.Context, name string) (ExternalSkillResult, error) {
	if err := ValidateSafeComponent(name, "skill"); err != nil {
		return ExternalSkillResult{}, err
	}
	release, err := m.operationLock("skill-" + name)
	if err != nil {
		return ExternalSkillResult{}, err
	}
	defer release()
	metadataPath := filepath.Join(m.Config.MetaRoot, "external-skills", name+".json")
	metadata, err := readExternalSkillMetadata(metadataPath)
	if err != nil {
		return ExternalSkillResult{}, fmt.Errorf("external skill is not installed: %s", name)
	}
	destination, activated, err := locateExternalSkillTree(m.Config, name)
	if err != nil {
		return ExternalSkillResult{}, fmt.Errorf("locate external skill %s: %w", name, err)
	}
	hash, err := DirectorySHA256(destination)
	if err != nil || "sha256:"+hash != externalExpectedHash(metadata) {
		return ExternalSkillResult{}, fmt.Errorf("external skill has local modifications: %s", name)
	}
	backup, err := CreateBackup(m.Config, "external-skill", m.now(), m.Compose)
	if err != nil {
		return ExternalSkillResult{}, err
	}
	if err := m.transactionalSkillMutation(destination, metadataPath, func() error {
		return m.withPausedHermes(ctx, func() error {
			removed := destination + ".remove-" + fmt.Sprint(os.Getpid())
			if err := os.Rename(destination, removed); err != nil {
				return err
			}
			if err := os.Remove(metadataPath); err != nil {
				_ = os.Rename(removed, destination)
				return err
			}
			if err := os.RemoveAll(filepath.Join(m.Config.MetaRoot, "external-skills", name)); err != nil {
				return err
			}
			if err := os.Remove(filepath.Join(m.Config.SkillsEnvRoot, "by-skill", name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err := os.RemoveAll(removed); err != nil {
				return err
			}
			return RecordChange(m.Config, "uninstall", "ok", backup.Archive, "external skill="+name, m.now())
		})
	}); err != nil {
		_ = RecordChange(m.Config, "uninstall", "failed", backup.Archive, err.Error(), m.now())
		return ExternalSkillResult{}, err
	}
	return ExternalSkillResult{OK: true, Action: "uninstall", Name: name, Source: metadata.Source, Commit: metadata.Commit, Version: metadata.Version, Backup: backup.Archive, Activated: activated}, nil
}

func (m *ExternalSkillManager) stageForkUpdateContext(metadata ExternalSkillMetadata, newCommit, current, newBase string) error {
	root := filepath.Join(m.Config.DataRoot, ".openlia", "migrations", metadata.Name)
	if err := removeSafeDirectory(root); err != nil {
		return err
	}
	if err := EnsureDir(root, 0o700); err != nil {
		return err
	}
	oldBase := filepath.Join(m.Config.MetaRoot, filepath.FromSlash(metadata.BaseSnapshot))
	for source, name := range map[string]string{oldBase: "old-base", current: "current-fork", newBase: "new-base"} {
		if err := CopyDir(source, filepath.Join(root, name)); err != nil {
			return err
		}
	}
	patch, err := GenerateSkillPatch(oldBase, current, metadata.Name)
	if err != nil {
		return err
	}
	if _, err := WriteSkillPatch(filepath.Join(root, "customization.patch"), patch); err != nil {
		return err
	}
	oldHash, err := DirectorySHA256(oldBase)
	if err != nil {
		return err
	}
	currentHash, err := DirectorySHA256(current)
	if err != nil {
		return err
	}
	newHash, err := DirectorySHA256(newBase)
	if err != nil {
		return err
	}
	patchHash, err := SkillPatchHash(patch)
	if err != nil {
		return err
	}
	context := SkillMigrationContext{Schema: migrationProtocolSchema, Skill: metadata.Name, Origin: "external", Source: metadata.Source, OldCommit: metadata.Commit, NewCommit: newCommit, OldBaseHash: "sha256:" + oldHash, CurrentForkHash: "sha256:" + currentHash, CurrentPatchHash: patchHash, NewBaseHash: "sha256:" + newHash, NewVersion: metadata.Version, NewSource: metadata.Source, CreatedAt: utcTimestamp(m.now())}
	data, _ := json.Marshal(context)
	return AtomicWriteFile(filepath.Join(root, "context.json"), append(data, '\n'), 0o600)
}

func (m *ExternalSkillManager) Test(ctx context.Context, sourceID, name string) (ExternalSkillResult, error) {
	report, err := m.Audit(ctx, sourceID, name)
	if err != nil {
		return ExternalSkillResult{}, err
	}
	item, root, manifestItem, err := m.catalogSkill(sourceID, name)
	if err != nil {
		return ExternalSkillResult{}, err
	}
	if len(manifestItem.Test) == 0 {
		return ExternalSkillResult{}, fmt.Errorf("skill %s does not declare a test command", name)
	}
	command, err := json.Marshal(manifestItem.Test)
	if err != nil {
		return ExternalSkillResult{}, err
	}
	containerSource := containerPath(m.Config.SkillsCacheRoot, "/opt/openlia/skill-cache", filepath.Join(root, filepath.FromSlash(item.Path)))
	containerEnvironment := containerPath(m.Config.SkillsEnvRoot, "/opt/openlia/skill-envs", filepath.Join(m.Config.SkillsEnvRoot, strings.TrimPrefix(report.EnvironmentHash, "sha256:")))
	_, err = m.Compose.Run(ctx, "run", "--rm", "--no-deps", "--user", m.builderUser(), "-e", "OPENLIA_SKILL_ACTION=test", "-e", "OPENLIA_SKILL_SOURCE="+containerSource, "-e", "OPENLIA_SKILL_ENV="+containerEnvironment, "-e", "OPENLIA_SKILL_TEST_JSON="+string(command), "skill-test-runner", "test")
	if err != nil {
		return ExternalSkillResult{}, fmt.Errorf("test skill %s: %w", name, err)
	}
	return ExternalSkillResult{OK: true, Action: "test", Name: name, Source: item.Source, Commit: item.Commit, Version: item.Version}, nil
}

func (m *ExternalSkillManager) Fork(ctx context.Context, name string) (ExternalSkillResult, error) {
	if metadata, err := readExternalSkillMetadata(filepath.Join(m.Config.MetaRoot, "external-skills", name+".json")); err == nil && metadata.Ownership == "forked" {
		return m.updateForkMetadata(ctx, name, true)
	}
	return m.updateForkMetadata(ctx, name, false)
}

func (m *ExternalSkillManager) RefreshFork(ctx context.Context, name string) (ExternalSkillResult, error) {
	return m.updateForkMetadata(ctx, name, true)
}

func (m *ExternalSkillManager) updateForkMetadata(_ context.Context, name string, refresh bool) (ExternalSkillResult, error) {
	release, err := m.operationLock("skill-" + name)
	if err != nil {
		return ExternalSkillResult{}, err
	}
	defer release()
	metadataPath := filepath.Join(m.Config.MetaRoot, "external-skills", name+".json")
	metadata, err := readExternalSkillMetadata(metadataPath)
	if err != nil {
		return ExternalSkillResult{}, fmt.Errorf("external skill is not installed: %s", name)
	}
	if refresh && metadata.Ownership != "forked" {
		return ExternalSkillResult{}, fmt.Errorf("external skill is not forked: %s", name)
	}
	if !refresh && metadata.Ownership == "forked" {
		return ExternalSkillResult{}, fmt.Errorf("external skill is already forked: %s", name)
	}
	destination, activated, err := locateExternalSkillTree(m.Config, name)
	if err != nil {
		return ExternalSkillResult{}, fmt.Errorf("locate external skill %s: %w", name, err)
	}
	base := filepath.Join(m.Config.MetaRoot, filepath.FromSlash(metadata.BaseSnapshot))
	action := "fork"
	if refresh {
		action = "fork-refresh"
	}
	backup, err := CreateBackup(m.Config, "external-skill-"+action, m.now(), m.Compose)
	if err != nil {
		return ExternalSkillResult{}, err
	}
	err = m.transactionalSkillMutation(destination, metadataPath, func() error {
		patch, err := GenerateSkillPatch(base, destination, name)
		if err != nil {
			return err
		}
		patchHash, err := WriteSkillPatch(filepath.Join(m.Config.MetaRoot, filepath.FromSlash(metadata.PatchPath)), patch)
		if err != nil {
			return err
		}
		hash, err := DirectorySHA256(destination)
		if err != nil {
			return err
		}
		metadata.Ownership, metadata.PatchHash, metadata.ForkHash = "forked", patchHash, "sha256:"+hash
		if metadata.ForkedAt == "" {
			metadata.ForkedAt = utcTimestamp(m.now())
		}
		metadata.UpdatedAt = utcTimestamp(m.now())
		data, _ := json.Marshal(metadata)
		if err := AtomicWriteFile(metadataPath, append(data, '\n'), 0o600); err != nil {
			return err
		}
		return RecordChange(m.Config, action, "ok", backup.Archive, "external skill="+name, m.now())
	})
	if err != nil {
		return ExternalSkillResult{}, err
	}
	return ExternalSkillResult{OK: true, Action: action, Name: name, Source: metadata.Source, Commit: metadata.Commit, Version: metadata.Version, Backup: backup.Archive, Activated: activated}, nil
}

func (m *ExternalSkillManager) Reset(ctx context.Context, name string) (ExternalSkillResult, error) {
	release, err := m.operationLock("skill-" + name)
	if err != nil {
		return ExternalSkillResult{}, err
	}
	defer release()
	metadataPath := filepath.Join(m.Config.MetaRoot, "external-skills", name+".json")
	metadata, err := readExternalSkillMetadata(metadataPath)
	if err != nil {
		return ExternalSkillResult{}, fmt.Errorf("external skill is not installed: %s", name)
	}
	destination, activated, err := locateExternalSkillTree(m.Config, name)
	if err != nil {
		return ExternalSkillResult{}, fmt.Errorf("locate external skill %s: %w", name, err)
	}
	base := filepath.Join(m.Config.MetaRoot, filepath.FromSlash(metadata.BaseSnapshot))
	backup, err := CreateBackup(m.Config, "external-skill-reset", m.now(), m.Compose)
	if err != nil {
		return ExternalSkillResult{}, err
	}
	err = m.transactionalSkillMutation(destination, metadataPath, func() error {
		return m.withPausedHermes(ctx, func() error {
			if err := replaceDirectory(base, destination); err != nil {
				return err
			}
			hash, err := DirectorySHA256(destination)
			if err != nil {
				return err
			}
			patch, err := GenerateSkillPatch(base, destination, name)
			if err != nil {
				return err
			}
			metadata.PatchHash, err = WriteSkillPatch(filepath.Join(m.Config.MetaRoot, filepath.FromSlash(metadata.PatchPath)), patch)
			if err != nil {
				return err
			}
			metadata.Ownership, metadata.ForkHash, metadata.ForkedAt, metadata.ContentHash = "managed", "", "", "sha256:"+hash
			metadata.UpdatedAt = utcTimestamp(m.now())
			data, _ := json.Marshal(metadata)
			if err := AtomicWriteFile(metadataPath, append(data, '\n'), 0o600); err != nil {
				return err
			}
			return RecordChange(m.Config, "reset", "ok", backup.Archive, "external skill="+name, m.now())
		})
	})
	if err != nil {
		_ = RecordChange(m.Config, "reset", "failed", backup.Archive, err.Error(), m.now())
		return ExternalSkillResult{}, err
	}
	return ExternalSkillResult{OK: true, Action: "reset", Name: name, Source: metadata.Source, Commit: metadata.Commit, Version: metadata.Version, Backup: backup.Archive, Activated: activated}, nil
}

func (m *ExternalSkillManager) source(id string) (SkillSourceConfig, error) {
	if err := m.Config.ValidatePaths(); err != nil {
		return SkillSourceConfig{}, err
	}
	for _, source := range m.Config.SkillSources {
		if source.ID == id {
			if source.Manifest == "" {
				source.Manifest = externalSkillManifestName
			}
			return source, nil
		}
	}
	return SkillSourceConfig{}, fmt.Errorf("unknown skill source %q", id)
}

func (m *ExternalSkillManager) resolveCommit(ctx context.Context, source SkillSourceConfig) (string, error) {
	env, cleanup, err := m.gitEnvironment()
	if err != nil {
		return "", err
	}
	defer cleanup()
	result, err := m.Runner.RunExternal(ctx, ExternalCommand{Name: "git", Args: []string{"ls-remote", "--exit-code", source.URL, source.Ref}, Env: env})
	if err != nil {
		token, _ := readSecretValue(m.Config.SecretFile, "OPENLIA_SKILLS_GIT_TOKEN")
		hint := "check the repository URL and branch"
		if token == "" {
			hint = "if the repository is private, add OPENLIA_SKILLS_GIT_TOKEN to [secrets].source and run openlia auth rotate"
		}
		return "", fmt.Errorf("check skill source %s: %w; %s", source.ID, err, hint)
	}
	fields := strings.Fields(string(result.Stdout))
	if len(fields) < 2 || !isCommit(fields[0]) {
		return "", fmt.Errorf("skill source %s did not resolve to an immutable commit", source.ID)
	}
	for index := 2; index < len(fields); index += 2 {
		if fields[index] != fields[0] {
			return "", fmt.Errorf("skill source %s ref is ambiguous", source.ID)
		}
	}
	return strings.ToLower(fields[0]), nil
}

func (m *ExternalSkillManager) gitEnvironment() ([]string, func(), error) {
	token, err := readSecretValue(m.Config.SecretFile, "OPENLIA_SKILLS_GIT_TOKEN")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, func() {}, err
	}
	if err := EnsureDir(m.Config.RuntimeRoot, 0o700); err != nil {
		return nil, func() {}, err
	}
	directory, err := os.MkdirTemp(m.Config.RuntimeRoot, ".skill-askpass-*")
	if err != nil {
		return nil, func() {}, err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		os.RemoveAll(directory)
		return nil, func() {}, err
	}
	tokenFile := filepath.Join(directory, "token")
	if err := os.WriteFile(tokenFile, []byte(token), 0o600); err != nil {
		os.RemoveAll(directory)
		return nil, func() {}, err
	}
	helper := filepath.Join(directory, "askpass")
	contents := []byte("#!/bin/sh\ncase \"$1\" in *Username*) printf '%s\\n' x-access-token ;; *) cat \"$OPENLIA_SKILLS_TOKEN_FILE\" ;; esac\n")
	if err := os.WriteFile(helper, contents, 0o700); err != nil {
		os.RemoveAll(directory)
		return nil, func() {}, err
	}
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + directory, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=" + helper, "OPENLIA_SKILLS_TOKEN_FILE=" + tokenFile}
	return env, func() { _ = os.RemoveAll(directory) }, nil
}

func readSecretValue(path, wanted string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", os.ErrNotExist
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("protected secret file must be a regular mode-0600 file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read protected secret file: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
		if ok && key == wanted {
			return value, nil
		}
	}
	return "", nil
}

func readCollectionManifest(root, relative string) (SkillCollectionManifest, error) {
	if relative == "" {
		relative = externalSkillManifestName
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return SkillCollectionManifest{}, fmt.Errorf("read skill collection manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest SkillCollectionManifest
	if err := decoder.Decode(&manifest); err != nil || manifest.Schema != 1 || len(manifest.Skills) == 0 {
		return SkillCollectionManifest{}, fmt.Errorf("invalid skill collection manifest")
	}
	seen := make(map[string]bool)
	for _, item := range manifest.Skills {
		if ValidateSafeComponent(item.Name, "skill") != nil || item.Version == "" || seen[item.Name] || !safeRelativePath(item.Path) || len(item.Test) > 0 && item.Test[0] == "" {
			return SkillCollectionManifest{}, fmt.Errorf("invalid skill collection manifest item %q", item.Name)
		}
		seen[item.Name] = true
	}
	sort.Slice(manifest.Skills, func(i, j int) bool { return manifest.Skills[i].Name < manifest.Skills[j].Name })
	return manifest, nil
}

func validateCollectionTree(root string, manifest SkillCollectionManifest) error {
	if err := validateRegularTree(root); err != nil {
		return err
	}
	for _, item := range manifest.Skills {
		if err := validateSkillTree(filepath.Join(root, filepath.FromSlash(item.Path)), item.Name); err != nil {
			return err
		}
	}
	return nil
}

func validateRegularTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
			if part == ".git" {
				return fmt.Errorf("skill source contains forbidden .git metadata")
			}
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("skill source contains symlink: %s", relative)
		}
		if !entry.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("skill source contains special file: %s", relative)
		}
		if info.Mode().IsRegular() {
			file, openErr := os.Open(path)
			if openErr != nil {
				return openErr
			}
			data := make([]byte, len("version https://git-lfs.github.com/spec/v1\n"))
			read, readErr := file.Read(data)
			closeErr := file.Close()
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return readErr
			}
			if closeErr != nil {
				return closeErr
			}
			data = data[:read]
			if bytes.HasPrefix(data, []byte("version https://git-lfs.github.com/spec/v1\n")) {
				return fmt.Errorf("skill source contains Git LFS pointer: %s", relative)
			}
		}
		return nil
	})
}

func validateSkillTree(root, name string) error {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("skill %s path is not a directory", name)
	}
	data, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil {
		return fmt.Errorf("skill %s requires SKILL.md", name)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 4 || strings.TrimSpace(lines[0]) != "---" {
		return fmt.Errorf("skill %s has invalid frontmatter", name)
	}
	end, declaredName, description := -1, "", ""
	for index := 1; index < len(lines); index++ {
		line := strings.TrimSpace(lines[index])
		if line == "---" {
			end = index
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "name":
			declaredName = strings.Trim(strings.TrimSpace(value), "\"'")
		case "description":
			description = strings.TrimSpace(value)
		}
	}
	if end < 0 || declaredName != name || description == "" {
		return fmt.Errorf("skill %s frontmatter requires matching name and description", name)
	}
	if _, err := os.Stat(filepath.Join(root, "pyproject.toml")); err == nil {
		if lock, lockErr := os.Stat(filepath.Join(root, "uv.lock")); lockErr != nil || !lock.Mode().IsRegular() {
			return fmt.Errorf("skill %s requires uv.lock with pyproject.toml", name)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "package.json")); err == nil {
		if lock, lockErr := os.Stat(filepath.Join(root, "bun.lock")); lockErr != nil || !lock.Mode().IsRegular() {
			return fmt.Errorf("skill %s requires bun.lock with package.json", name)
		}
	}
	return nil
}

func safeRelativePath(path string) bool {
	return path != "" && path != "." && !strings.HasPrefix(path, "/") && filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) == path && !strings.HasPrefix(path, "../") && !strings.Contains(path, "/../")
}

func isCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (m *ExternalSkillManager) catalogSkill(sourceID, name string) (ExternalSkillCatalogItem, string, SkillCollectionManifestItem, error) {
	if err := ValidateSafeComponent(name, "skill"); err != nil {
		return ExternalSkillCatalogItem{}, "", SkillCollectionManifestItem{}, err
	}
	items, err := m.Catalog()
	if err != nil {
		return ExternalSkillCatalogItem{}, "", SkillCollectionManifestItem{}, err
	}
	var matches []ExternalSkillCatalogItem
	for _, item := range items {
		if item.Name == name && (sourceID == "" || item.Source == sourceID) {
			matches = append(matches, item)
		}
	}
	if len(matches) == 0 {
		return ExternalSkillCatalogItem{}, "", SkillCollectionManifestItem{}, fmt.Errorf("external skill not found: %s", name)
	}
	if len(matches) > 1 {
		return ExternalSkillCatalogItem{}, "", SkillCollectionManifestItem{}, fmt.Errorf("external skill %s is ambiguous; specify source", name)
	}
	item := matches[len(matches)-1]
	root := filepath.Join(m.Config.SkillsCacheRoot, item.Source, item.Commit)
	source, _ := m.source(item.Source)
	manifest, err := readCollectionManifest(root, source.Manifest)
	if err != nil {
		return ExternalSkillCatalogItem{}, "", SkillCollectionManifestItem{}, err
	}
	for _, candidate := range manifest.Skills {
		if candidate.Name == name {
			return item, root, candidate, nil
		}
	}
	return ExternalSkillCatalogItem{}, "", SkillCollectionManifestItem{}, fmt.Errorf("external skill not found: %s", name)
}

func readExternalSkillMetadata(path string) (ExternalSkillMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ExternalSkillMetadata{}, err
	}
	var metadata ExternalSkillMetadata
	if json.Unmarshal(data, &metadata) != nil || metadata.Schema != 1 || ValidateSafeComponent(metadata.Name, "skill") != nil || ValidateSafeComponent(metadata.Source, "source") != nil || !isCommit(metadata.Commit) || !contentHashPattern.MatchString(metadata.ContentHash) || !contentHashPattern.MatchString(metadata.Environment) || (metadata.Ownership != "managed" && metadata.Ownership != "forked") || metadata.BaseSnapshot != "external-skills/"+metadata.Name+"/base" || metadata.PatchPath != "external-skills/"+metadata.Name+"/customization.patch" || !contentHashPattern.MatchString(metadata.PatchHash) || metadata.Ownership == "forked" && (!contentHashPattern.MatchString(metadata.ForkHash) || metadata.ForkedAt == "") {
		return ExternalSkillMetadata{}, fmt.Errorf("invalid external skill metadata")
	}
	return metadata, nil
}

func installedExternalSkillsForSource(config Config, source string) ([]string, error) {
	root := filepath.Join(config.MetaRoot, "external-skills")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		metadata, readErr := readExternalSkillMetadata(filepath.Join(root, entry.Name()))
		if readErr != nil {
			return nil, readErr
		}
		if metadata.Source == source {
			result = append(result, metadata.Name)
		}
	}
	sort.Strings(result)
	return result, nil
}

func (m *ExternalSkillManager) prepareRuntime() error {
	for _, path := range []string{m.Config.DataRoot, filepath.Join(m.Config.DataRoot, "skills"), m.Config.MetaRoot, filepath.Join(m.Config.MetaRoot, "external-skills"), filepath.Join(m.Config.MetaRoot, "locks"), m.Config.BackupRoot, m.Config.LochoRoot} {
		if err := EnsureDir(path, 0o700); err != nil {
			return err
		}
	}
	for _, path := range []string{m.Config.SkillsCacheRoot, m.Config.SkillsEnvRoot} {
		if err := EnsureDir(path, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (m *ExternalSkillManager) operationLock(name string) (func(), error) {
	if err := ValidateSafeComponent(name, "operation lock"); err != nil {
		return nil, err
	}
	root := filepath.Join(m.Config.MetaRoot, "locks")
	if err := EnsureDir(root, 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(root, name+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("operation already in progress: %s", name)
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
}

func (m *ExternalSkillManager) transactionalSkillMutation(destination, metadataPath string, operation func() error) error {
	root, err := os.MkdirTemp(m.Config.MetaRoot, ".external-skill-tx-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	treeBackup, metadataBackup := filepath.Join(root, "tree"), filepath.Join(root, "metadata")
	name := strings.TrimSuffix(filepath.Base(metadataPath), ".json")
	assets := filepath.Join(m.Config.MetaRoot, "external-skills", strings.TrimSuffix(filepath.Base(metadataPath), ".json"))
	assetsBackup := filepath.Join(root, "assets")
	environmentLink := filepath.Join(m.Config.SkillsEnvRoot, "by-skill", name)
	environmentTarget, environmentLinkExists, err := readSkillEnvironmentLink(environmentLink)
	if err != nil {
		return err
	}
	treeExists, metadataExists, assetsExist := false, false, false
	if info, statErr := os.Lstat(destination); statErr == nil && info.IsDir() {
		treeExists = true
		if err := CopyDir(destination, treeBackup); err != nil {
			return err
		}
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if data, readErr := os.ReadFile(metadataPath); readErr == nil {
		metadataExists = true
		if err := os.WriteFile(metadataBackup, data, 0o600); err != nil {
			return err
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if info, statErr := os.Lstat(assets); statErr == nil && info.IsDir() {
		assetsExist = true
		if err := CopyDir(assets, assetsBackup); err != nil {
			return err
		}
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err := operation(); err != nil {
		_ = restoreSkillEnvironmentLink(environmentLink, environmentLinkExists, environmentTarget)
		if treeExists {
			_ = replaceDirectory(treeBackup, destination)
		} else {
			_ = os.RemoveAll(destination)
		}
		if metadataExists {
			if data, readErr := os.ReadFile(metadataBackup); readErr == nil {
				_ = AtomicWriteFile(metadataPath, data, 0o600)
			}
		} else {
			_ = os.Remove(metadataPath)
		}
		if assetsExist {
			_ = replaceDirectory(assetsBackup, assets)
		} else {
			_ = os.RemoveAll(assets)
		}
		return err
	}
	return nil
}

func readSkillEnvironmentLink(path string) (string, bool, error) {
	target, err := os.Readlink(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return target, true, nil
}

func restoreSkillEnvironmentLink(path string, exists bool, target string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !exists {
		return nil
	}
	if err := EnsureDir(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.Symlink(target, path)
}

func prefixedFileHash(path string) (string, error) {
	value, err := fileSHA256(path)
	if err != nil {
		return "", err
	}
	return "sha256:" + value, nil
}

func dependencyEnvironmentHash(content string, locks map[string]string) string {
	keys := make([]string, 0, len(locks))
	for key := range locks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	digest := sha256.New()
	_, _ = io.WriteString(digest, content+"\n")
	for _, key := range keys {
		_, _ = io.WriteString(digest, key+"="+locks[key]+"\n")
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

func activateSkillEnvironment(root, name, hash string) error {
	if ValidateSafeComponent(name, "skill") != nil || !contentHashPattern.MatchString(hash) {
		return fmt.Errorf("invalid skill environment identity")
	}
	links := filepath.Join(root, "by-skill")
	if err := EnsureDir(links, 0o755); err != nil {
		return err
	}
	target := filepath.Join("..", strings.TrimPrefix(hash, "sha256:"))
	temporary := filepath.Join(links, "."+name+"-"+fmt.Sprint(os.Getpid()))
	_ = os.Remove(temporary)
	if err := os.Symlink(target, temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, filepath.Join(links, name)); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func publishSkillEnvironment(staging, destination string) error {
	backup := destination + ".old-" + fmt.Sprint(os.Getpid())
	if _, err := os.Lstat(destination); err == nil {
		if err := os.Rename(destination, backup); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(staging, destination); err != nil {
		if _, statErr := os.Lstat(backup); statErr == nil {
			_ = os.Rename(backup, destination)
		}
		return err
	}
	_ = os.RemoveAll(backup)
	return nil
}

func validateCachedSnapshot(sourceRoot, commit, snapshot string) error {
	digest, err := DirectorySHA256(snapshot)
	if err != nil {
		return err
	}
	stored, err := os.ReadFile(filepath.Join(sourceRoot, commit+".sha256"))
	if err != nil || strings.TrimSpace(string(stored)) != "sha256:"+digest {
		return fmt.Errorf("cached skill source failed digest revalidation")
	}
	return nil
}

func (m *ExternalSkillManager) builderUser() string {
	if m.Config.LocalMode && os.Geteuid() != 0 {
		return fmt.Sprintf("%d:%d", os.Geteuid(), os.Getegid())
	}
	return "10000:10000"
}

func containerPath(hostRoot, containerRoot, path string) string {
	relative, err := filepath.Rel(hostRoot, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(filepath.Join(containerRoot, relative))
}

func externalAction(update bool) string {
	if update {
		return "update"
	}
	return "install"
}

func externalExpectedHash(metadata ExternalSkillMetadata) string {
	if metadata.Ownership == "forked" {
		return metadata.ForkHash
	}
	return metadata.ContentHash
}

func (m *ExternalSkillManager) withPausedHermes(ctx context.Context, operation func() error) (err error) {
	state, err := ReadState(m.Config)
	if err != nil {
		return err
	}
	if state == stateRunning {
		if _, err := m.Compose.Run(ctx, "stop", "hermes"); err != nil {
			return fmt.Errorf("pause Hermes for skill activation: %w", err)
		}
		defer func() {
			if _, restartErr := m.Compose.Run(ctx, "up", "-d", "--no-deps", "--force-recreate", "hermes"); restartErr != nil && err == nil {
				err = fmt.Errorf("skill changed but Hermes restart failed: %w", restartErr)
			}
		}()
	}
	return operation()
}

func (m *ExternalSkillManager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now().UTC()
}

func publishDirectory(source, destination string) error {
	parent := filepath.Dir(destination)
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(destination)+"-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := copyDirContents(source, staging); err != nil {
		return err
	}
	if err := os.Rename(staging, destination); err != nil {
		return fmt.Errorf("activate skill directory: %w", err)
	}
	return nil
}

func mergeEnvironment(base, overrides []string) []string {
	values := make(map[string]string, len(base)+len(overrides))
	order := make([]string, 0, len(base)+len(overrides))
	for _, item := range append(append([]string{}, base...), overrides...) {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = item
	}
	result := make([]string, 0, len(values))
	for _, key := range order {
		result = append(result, values[key])
	}
	return result
}
