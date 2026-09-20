package operator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	distributionName = "openlia-personal-os"
	hashScope        = "skill-files-v1"
)

// ProfileOperator synchronizes distribution-owned profile files while leaving
// the workspace and locally customized skills untouched.
type ProfileOperator struct {
	Config Config
	Now    func() time.Time
}

func NewProfileOperator(config Config) *ProfileOperator {
	return &ProfileOperator{Config: config, Now: func() time.Time { return time.Now().UTC() }}
}

type ProfileSyncResult struct {
	OK        bool              `json:"ok"`
	Action    string            `json:"action"`
	Workspace string            `json:"workspace"`
	Skills    ProfileSkillStats `json:"skills"`
}

type ProfileSkillStats struct {
	Installed       int           `json:"installed"`
	Updated         int           `json:"updated"`
	Unchanged       int           `json:"unchanged"`
	Customized      int           `json:"customized"`
	Unmanaged       int           `json:"unmanaged"`
	CustomizedNames []string      `json:"customized_names"`
	UnmanagedNames  []string      `json:"unmanaged_names"`
	Results         []SkillResult `json:"results"`
}

type SkillStatusResult struct {
	Schema        int              `json:"schema"`
	OK            bool             `json:"ok"`
	Action        string           `json:"action"`
	Workspace     string           `json:"workspace"`
	SelectedSkill string           `json:"selected_skill"`
	Summary       SkillStatusStats `json:"summary"`
	Skills        SkillStatusItems `json:"skills"`
}

type SkillStatusStats struct {
	Managed         int `json:"managed"`
	Customized      int `json:"customized"`
	Unmanaged       int `json:"unmanaged"`
	New             int `json:"new"`
	UpdateAvailable int `json:"update_available"`
}

type SkillStatusItems struct {
	Results []SkillResult `json:"results"`
}

// SkillResult deliberately contains the union of the sync and status fields;
// both public commands expose the same object shape.
type SkillResult struct {
	Name             string `json:"name"`
	State            string `json:"state"`
	Action           string `json:"action"`
	ResultState      string `json:"result_state"`
	Reason           string `json:"reason"`
	Provenance       string `json:"provenance"`
	ManagedSource    string `json:"managed_source"`
	AvailableSource  string `json:"-"`
	InstalledVersion string `json:"installed_version"`
	AvailableVersion string `json:"available_version"`
	ManagedHash      string `json:"managed_hash"`
	CurrentHash      string `json:"current_hash"`
	AvailableHash    string `json:"available_hash"`
	InstalledAt      string `json:"installed_at"`
	UpdatedAt        string `json:"updated_at"`
	includeAvailable bool
}

func (result SkillResult) MarshalJSON() ([]byte, error) {
	type base struct {
		Name             string `json:"name"`
		State            string `json:"state"`
		Action           string `json:"action"`
		ResultState      string `json:"result_state"`
		Reason           string `json:"reason"`
		Provenance       string `json:"provenance"`
		ManagedSource    string `json:"managed_source"`
		InstalledVersion string `json:"installed_version"`
		AvailableVersion string `json:"available_version"`
		ManagedHash      string `json:"managed_hash"`
		CurrentHash      string `json:"current_hash"`
		AvailableHash    string `json:"available_hash"`
		InstalledAt      string `json:"installed_at"`
		UpdatedAt        string `json:"updated_at"`
	}
	values := base{Name: result.Name, State: result.State, Action: result.Action, ResultState: result.ResultState, Reason: result.Reason, Provenance: result.Provenance, ManagedSource: result.ManagedSource, InstalledVersion: result.InstalledVersion, AvailableVersion: result.AvailableVersion, ManagedHash: result.ManagedHash, CurrentHash: result.CurrentHash, AvailableHash: result.AvailableHash, InstalledAt: result.InstalledAt, UpdatedAt: result.UpdatedAt}
	if !result.includeAvailable {
		return json.Marshal(values)
	}
	return json.Marshal(struct {
		Name             string `json:"name"`
		State            string `json:"state"`
		Action           string `json:"action"`
		ResultState      string `json:"result_state"`
		Reason           string `json:"reason"`
		Provenance       string `json:"provenance"`
		ManagedSource    string `json:"managed_source"`
		AvailableSource  string `json:"available_source"`
		InstalledVersion string `json:"installed_version"`
		AvailableVersion string `json:"available_version"`
		ManagedHash      string `json:"managed_hash"`
		CurrentHash      string `json:"current_hash"`
		AvailableHash    string `json:"available_hash"`
		InstalledAt      string `json:"installed_at"`
		UpdatedAt        string `json:"updated_at"`
	}{Name: values.Name, State: values.State, Action: values.Action, ResultState: values.ResultState, Reason: values.Reason, Provenance: values.Provenance, ManagedSource: values.ManagedSource, AvailableSource: result.AvailableSource, InstalledVersion: values.InstalledVersion, AvailableVersion: values.AvailableVersion, ManagedHash: values.ManagedHash, CurrentHash: values.CurrentHash, AvailableHash: values.AvailableHash, InstalledAt: values.InstalledAt, UpdatedAt: values.UpdatedAt})
}

type SkillMetadata struct {
	Schema              int    `json:"schema"`
	Skill               string `json:"skill"`
	Distribution        string `json:"distribution"`
	DistributionVersion string `json:"distribution_version"`
	SourceID            string `json:"source_id"`
	ArtifactPath        string `json:"artifact_path"`
	HashScope           string `json:"hash_scope"`
	ContentSHA256       string `json:"content_sha256"`
	InstalledAt         string `json:"installed_at"`
	UpdatedAt           string `json:"updated_at"`
}

type metadataInfo struct {
	State       string
	Hash        string
	Version     string
	Source      string
	InstalledAt string
	UpdatedAt   string
	Reason      string
}

var contentHashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func (p *ProfileOperator) Sync() (ProfileSyncResult, error) {
	if err := p.Config.ValidatePaths(); err != nil {
		return ProfileSyncResult{}, err
	}
	if err := EnsureDir(p.Config.DataRoot, 0o700); err != nil {
		return ProfileSyncResult{}, err
	}
	if err := EnsureDir(filepath.Join(p.Config.DataRoot, "skills"), 0o700); err != nil {
		return ProfileSyncResult{}, err
	}
	if err := EnsureDir(filepath.Join(p.Config.DataRoot, "scripts"), 0o700); err != nil {
		return ProfileSyncResult{}, err
	}
	managedRoot := filepath.Join(p.Config.MetaRoot, "managed")
	if err := EnsureDir(filepath.Join(managedRoot, "skills"), 0o700); err != nil {
		return ProfileSyncResult{}, err
	}

	distributionVersion, distributionSource := p.distributionInfo()
	for _, item := range []struct {
		source string
		dest   string
		marker string
		mode   fs.FileMode
	}{
		{filepath.Join(p.Config.RepositoryRoot, "profile", "SOUL.md"), filepath.Join(p.Config.DataRoot, "SOUL.md"), filepath.Join(managedRoot, "SOUL.md.sha256"), 0o600},
		{filepath.Join(p.Config.RepositoryRoot, "profile", "AGENTS.md"), filepath.Join(p.Config.DataRoot, "AGENTS.md"), filepath.Join(managedRoot, "AGENTS.md.sha256"), 0o600},
		{filepath.Join(p.Config.RepositoryRoot, "profile", "config.yaml"), filepath.Join(p.Config.DataRoot, "config.yaml"), filepath.Join(managedRoot, "config.yaml.sha256"), 0o600},
		{filepath.Join(p.Config.RepositoryRoot, "profile", "cron", "scripts", "openlia-workspace-git-sync.sh"), filepath.Join(p.Config.DataRoot, "scripts", "openlia-workspace-git-sync.sh"), filepath.Join(managedRoot, "openlia-workspace-git-sync.sh.sha256"), 0o700},
	} {
		if err := p.syncFile(item.source, item.dest, item.marker, item.mode); err != nil {
			return ProfileSyncResult{}, err
		}
	}

	result := ProfileSyncResult{OK: true, Action: "profile-sync", Workspace: "preserved", Skills: ProfileSkillStats{
		CustomizedNames: []string{}, UnmanagedNames: []string{}, Results: []SkillResult{},
	}}
	sourceRoot := filepath.Join(p.Config.RepositoryRoot, "profile", "skills")
	entries, err := os.ReadDir(sourceRoot)
	if err != nil {
		return ProfileSyncResult{}, fmt.Errorf("read bundled skills: %w", err)
	}
	enabled := p.enabledSkills()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := ValidateSafeComponent(entry.Name(), "skill"); err != nil {
			return ProfileSyncResult{}, err
		}
		name := entry.Name()
		destination := filepath.Join(p.Config.DataRoot, "skills", name)
		disabledDestination := filepath.Join(p.Config.DataRoot, "skills", ".openlia-disabled", name)
		if !enabled[name] && !enabled["*"] {
			if info, statErr := os.Lstat(destination); statErr == nil && ((info.IsDir() && info.Mode()&os.ModeSymlink == 0) || info.Mode()&os.ModeSymlink != 0) {
				if info.Mode()&os.ModeSymlink != 0 {
					if target, targetErr := os.Stat(destination); targetErr != nil || !target.IsDir() {
						continue
					}
				}
				if err := EnsureDir(filepath.Dir(disabledDestination), 0o700); err != nil {
					return ProfileSyncResult{}, err
				}
				if err := os.Rename(destination, disabledDestination); err != nil {
					return ProfileSyncResult{}, fmt.Errorf("disable skill %s: %w", name, err)
				}
			}
			continue
		}
		if _, statErr := os.Lstat(destination); errors.Is(statErr, os.ErrNotExist) {
			if _, disabledErr := os.Lstat(disabledDestination); disabledErr == nil {
				if err := os.Rename(disabledDestination, destination); err != nil {
					return ProfileSyncResult{}, fmt.Errorf("enable skill %s: %w", name, err)
				}
			}
		}
		skillResult, err := p.syncSkill(filepath.Join(sourceRoot, name), destination, filepath.Join(managedRoot, "skills", name+".json"), name, distributionVersion, distributionSource)
		if err != nil {
			return ProfileSyncResult{}, err
		}
		result.Skills.Results = append(result.Skills.Results, skillResult)
		p.countSyncResult(&result.Skills, skillResult)
	}

	runtimeSkills := filepath.Join(p.Config.DataRoot, "skills")
	entries, err = os.ReadDir(runtimeSkills)
	if err != nil {
		return ProfileSyncResult{}, fmt.Errorf("read runtime skills: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == ".openlia-disabled" || (!entry.IsDir() && entry.Type()&os.ModeSymlink == 0) {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(sourceRoot, entry.Name())); statErr == nil {
			continue
		}
		name := entry.Name()
		if err := ValidateSafeComponent(name, "skill"); err != nil {
			continue
		}
		skillResult, err := p.removedSkill(filepath.Join(runtimeSkills, name), name, filepath.Join(managedRoot, "skills", name+".json"), distributionVersion, distributionSource)
		if err != nil {
			return ProfileSyncResult{}, err
		}
		result.Skills.Results = append(result.Skills.Results, skillResult)
		p.countSyncResult(&result.Skills, skillResult)
	}
	detail := fmt.Sprintf("profile assets synchronized without workspace replacement; updated=%d; customized=%d; unmanaged=%d", result.Skills.Updated, result.Skills.Customized, result.Skills.Unmanaged)
	if err := RecordChange(p.Config, "profile-sync", "ok", "", detail, p.now()); err != nil {
		return ProfileSyncResult{}, err
	}
	return result, nil
}

func (p *ProfileOperator) Status(selected string) (SkillStatusResult, error) {
	if err := p.Config.ValidatePaths(); err != nil {
		return SkillStatusResult{}, err
	}
	if selected != "" {
		if err := ValidateSafeComponent(selected, "skill"); err != nil {
			return SkillStatusResult{}, err
		}
	}
	if info, err := os.Stat(filepath.Join(p.Config.DataRoot, "skills")); err != nil || !info.IsDir() {
		return SkillStatusResult{}, fmt.Errorf("runtime skills directory is missing")
	}
	if info, err := os.Stat(filepath.Join(p.Config.MetaRoot, "managed", "skills")); err != nil || !info.IsDir() {
		return SkillStatusResult{}, fmt.Errorf("skill provenance directory is missing")
	}
	distributionVersion, distributionSource := p.distributionInfo()
	result := SkillStatusResult{Schema: 1, OK: true, Action: "profile-status", Workspace: "preserved", SelectedSkill: selected, Skills: SkillStatusItems{Results: []SkillResult{}}}
	if selected != "" {
		source := filepath.Join(p.Config.RepositoryRoot, "profile", "skills", selected)
		if info, err := os.Stat(source); err != nil || !info.IsDir() {
			source = ""
		}
		skillResult, err := p.statusSkill(selected, source, distributionVersion, distributionSource)
		if err != nil {
			return SkillStatusResult{}, err
		}
		skillResult.includeAvailable = true
		result.Skills.Results = append(result.Skills.Results, skillResult)
		p.countStatusResult(&result.Summary, skillResult)
		return result, nil
	}

	sourceRoot := filepath.Join(p.Config.RepositoryRoot, "profile", "skills")
	entries, err := os.ReadDir(sourceRoot)
	if err != nil {
		return SkillStatusResult{}, fmt.Errorf("read bundled skills: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := ValidateSafeComponent(entry.Name(), "skill"); err != nil {
			return SkillStatusResult{}, err
		}
		skillResult, err := p.statusSkill(entry.Name(), filepath.Join(sourceRoot, entry.Name()), distributionVersion, distributionSource)
		if err != nil {
			return SkillStatusResult{}, err
		}
		skillResult.includeAvailable = true
		result.Skills.Results = append(result.Skills.Results, skillResult)
		p.countStatusResult(&result.Summary, skillResult)
	}
	runtimeEntries, err := os.ReadDir(filepath.Join(p.Config.DataRoot, "skills"))
	if err != nil {
		return SkillStatusResult{}, fmt.Errorf("read runtime skills: %w", err)
	}
	for _, entry := range runtimeEntries {
		if entry.Name() == ".openlia-disabled" || (!entry.IsDir() && entry.Type()&os.ModeSymlink == 0) {
			continue
		}
		if _, err := os.Stat(filepath.Join(sourceRoot, entry.Name())); err == nil {
			continue
		}
		if err := ValidateSafeComponent(entry.Name(), "skill"); err != nil {
			continue
		}
		skillResult, err := p.statusSkill(entry.Name(), "", distributionVersion, distributionSource)
		if err != nil {
			return SkillStatusResult{}, err
		}
		skillResult.includeAvailable = true
		result.Skills.Results = append(result.Skills.Results, skillResult)
		p.countStatusResult(&result.Summary, skillResult)
	}
	return result, nil
}

func (p *ProfileOperator) syncFile(source, destination, marker string, mode fs.FileMode) error {
	info, err := os.Stat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	sourceHash, err := fileSHA256(source)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); errors.Is(err, os.ErrNotExist) {
		if err := AtomicCopyFile(source, destination, mode); err != nil {
			return err
		}
		return writeMarker(marker, sourceHash)
	}
	destinationHash, err := fileSHA256(destination)
	if err != nil {
		return err
	}
	previousHash := ""
	if contents, readErr := os.ReadFile(marker); readErr == nil {
		previousHash = strings.TrimSpace(string(contents))
	}
	if (previousHash == "" && destinationHash == sourceHash) || (previousHash != "" && destinationHash == previousHash) {
		if destinationHash != sourceHash {
			if err := AtomicCopyFile(source, destination, mode); err != nil {
				return err
			}
		}
		if err := writeMarker(marker, sourceHash); err != nil {
			return err
		}
	}
	return os.Chmod(destination, mode)
}

func (p *ProfileOperator) syncSkill(source, destination, metadataPath, name, version, sourceID string) (SkillResult, error) {
	sourceHash, err := DirectorySHA256(source)
	if err != nil {
		return SkillResult{}, err
	}
	sourceHash = "sha256:" + sourceHash
	metadata, err := readSkillMetadata(metadataPath, name)
	if err != nil {
		return SkillResult{}, err
	}
	if _, err := os.Lstat(destination); errors.Is(err, os.ErrNotExist) {
		if err := CopyDir(source, destination); err != nil {
			return SkillResult{}, err
		}
		writtenMetadata := p.newMetadata(name, sourceHash, version, sourceID)
		if err := writeSkillMetadata(metadataPath, writtenMetadata); err != nil {
			return SkillResult{}, err
		}
		writtenInfo := metadataInfoFromMetadata(writtenMetadata)
		return skillResult(name, "new", "installed", "managed", "installed_from_distribution", "created", writtenInfo, writtenInfo.Version, sourceHash, sourceHash), nil
	}
	info, statErr := os.Lstat(destination)
	if statErr != nil {
		return SkillResult{}, statErr
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		state, provenance, reason := "unmanaged", metadata.State, metadata.Reason
		if metadata.State == "valid" {
			state, provenance, reason = "customized", "valid", "runtime_skill_shape_changed"
		}
		return p.blockedSkillResult(name, state, provenance, reason, metadata), nil
	}
	currentHash, err := DirectorySHA256(destination)
	if err != nil {
		return SkillResult{}, err
	}
	currentHash = "sha256:" + currentHash
	if metadata.State == "valid" && currentHash == metadata.Hash {
		if currentHash == sourceHash {
			if err := normalizeSkill(destination); err != nil {
				return SkillResult{}, err
			}
			return skillResult(name, "managed", "unchanged", "managed", "managed_artifact_unchanged", "valid", metadata, metadata.Version, currentHash, sourceHash), nil
		}
		if err := AtomicCopyDir(source, destination); err != nil {
			return SkillResult{}, err
		}
		newMetadata := p.newMetadata(name, sourceHash, version, sourceID)
		newMetadata.InstalledAt = metadata.InstalledAt
		if err := writeSkillMetadata(metadataPath, newMetadata); err != nil {
			return SkillResult{}, err
		}
		if err := normalizeSkill(destination); err != nil {
			return SkillResult{}, err
		}
		newInfo := metadataInfoFromMetadata(newMetadata)
		return skillResult(name, "managed", "updated", "managed", "managed_artifact_updated", "valid", newInfo, newInfo.Version, sourceHash, sourceHash), nil
	}
	if metadata.State == "valid" {
		return p.blockedSkillResultWithCurrent(name, "customized", "valid", "local_modifications_detected", metadata, currentHash), nil
	}
	return p.blockedSkillResultWithCurrent(name, "unmanaged", metadata.State, metadata.Reason, metadata, currentHash), nil
}

func (p *ProfileOperator) removedSkill(destination, name, metadataPath, version, sourceID string) (SkillResult, error) {
	metadata, err := readSkillMetadata(metadataPath, name)
	if err != nil {
		return SkillResult{}, err
	}
	currentHash := ""
	if info, statErr := os.Lstat(destination); statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		hash, hashErr := DirectorySHA256(destination)
		if hashErr != nil {
			return SkillResult{}, hashErr
		}
		currentHash = "sha256:" + hash
	}
	return SkillResult{
		Name: name, State: "unmanaged", Action: "blocked", ResultState: "unmanaged", Reason: "distribution_removed",
		Provenance: metadata.State, ManagedSource: metadata.Source, InstalledVersion: metadata.Version,
		ManagedHash: metadata.Hash, CurrentHash: currentHash, InstalledAt: metadata.InstalledAt, UpdatedAt: metadata.UpdatedAt,
	}, nil
}

func (p *ProfileOperator) statusSkill(name, source, version, sourceID string) (SkillResult, error) {
	destination := filepath.Join(p.Config.DataRoot, "skills", name)
	metadataPath := filepath.Join(p.Config.MetaRoot, "managed", "skills", name+".json")
	available := source != ""
	if !available {
		if _, err := os.Lstat(destination); errors.Is(err, os.ErrNotExist) {
			return SkillResult{}, fmt.Errorf("skill is not bundled or installed: %s", name)
		}
	}
	metadata, err := readSkillMetadata(metadataPath, name)
	if err != nil {
		return SkillResult{}, err
	}
	availableHash := ""
	if available {
		hash, hashErr := DirectorySHA256(source)
		if hashErr != nil {
			return SkillResult{}, hashErr
		}
		availableHash = "sha256:" + hash
	}
	currentHash := ""
	if info, statErr := os.Lstat(destination); statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		hash, hashErr := DirectorySHA256(destination)
		if hashErr != nil {
			return SkillResult{}, hashErr
		}
		currentHash = "sha256:" + hash
	}
	if _, err := os.Lstat(destination); errors.Is(err, os.ErrNotExist) {
		return SkillResult{Name: name, State: "new", Action: "not_installed", ResultState: "new", Reason: "skill_not_installed", Provenance: "unavailable", ManagedSource: metadata.Source, AvailableSource: sourceID, InstalledVersion: metadata.Version, AvailableVersion: version, ManagedHash: metadata.Hash, CurrentHash: currentHash, AvailableHash: availableHash, InstalledAt: metadata.InstalledAt, UpdatedAt: metadata.UpdatedAt}, nil
	}
	if !available {
		return SkillResult{Name: name, State: "unmanaged", Action: "blocked", ResultState: "unmanaged", Reason: "distribution_removed", Provenance: metadata.State, ManagedSource: metadata.Source, InstalledVersion: metadata.Version, AvailableVersion: version, ManagedHash: metadata.Hash, CurrentHash: currentHash, AvailableHash: availableHash, InstalledAt: metadata.InstalledAt, UpdatedAt: metadata.UpdatedAt}, nil
	}
	if metadata.State == "valid" && currentHash == metadata.Hash {
		action, reason := "unchanged", "managed_artifact_unchanged"
		if currentHash != availableHash {
			action, reason = "update_available", "distribution_changed"
		}
		return SkillResult{Name: name, State: "managed", Action: action, ResultState: "managed", Reason: reason, Provenance: "valid", ManagedSource: metadata.Source, AvailableSource: sourceID, InstalledVersion: metadata.Version, AvailableVersion: version, ManagedHash: metadata.Hash, CurrentHash: currentHash, AvailableHash: availableHash, InstalledAt: metadata.InstalledAt, UpdatedAt: metadata.UpdatedAt}, nil
	}
	if metadata.State == "valid" {
		return SkillResult{Name: name, State: "customized", Action: "blocked", ResultState: "customized", Reason: "local_modifications_detected", Provenance: "valid", ManagedSource: metadata.Source, AvailableSource: sourceID, InstalledVersion: metadata.Version, AvailableVersion: version, ManagedHash: metadata.Hash, CurrentHash: currentHash, AvailableHash: availableHash, InstalledAt: metadata.InstalledAt, UpdatedAt: metadata.UpdatedAt}, nil
	}
	return SkillResult{Name: name, State: "unmanaged", Action: "blocked", ResultState: "unmanaged", Reason: metadata.Reason, Provenance: metadata.State, ManagedSource: metadata.Source, AvailableSource: sourceID, InstalledVersion: metadata.Version, AvailableVersion: version, ManagedHash: metadata.Hash, CurrentHash: currentHash, AvailableHash: availableHash, InstalledAt: metadata.InstalledAt, UpdatedAt: metadata.UpdatedAt}, nil
}

func (p *ProfileOperator) newMetadata(name, hash, version, source string) SkillMetadata {
	stamp := utcTimestamp(p.now())
	return SkillMetadata{Schema: 1, Skill: name, Distribution: distributionName, DistributionVersion: version, SourceID: source, ArtifactPath: "profile/skills/" + name, HashScope: hashScope, ContentSHA256: hash, InstalledAt: stamp, UpdatedAt: stamp}
}

func (p *ProfileOperator) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now().UTC()
}

func (p *ProfileOperator) blockedSkillResult(name, state, provenance, reason string, metadata metadataInfo) SkillResult {
	return p.blockedSkillResultWithCurrent(name, state, provenance, reason, metadata, "")
}

func (p *ProfileOperator) blockedSkillResultWithCurrent(name, state, provenance, reason string, metadata metadataInfo, current string) SkillResult {
	return SkillResult{Name: name, State: state, Action: "blocked", ResultState: state, Reason: reason, Provenance: provenance, ManagedSource: metadata.Source, InstalledVersion: metadata.Version, ManagedHash: metadata.Hash, CurrentHash: current, InstalledAt: metadata.InstalledAt, UpdatedAt: metadata.UpdatedAt}
}

func skillResult(name, state, action, resultState, reason, provenance string, managed metadataInfo, availableVersion, currentHash, availableHash string) SkillResult {
	return SkillResult{Name: name, State: state, Action: action, ResultState: resultState, Reason: reason, Provenance: provenance, ManagedSource: managed.Source, InstalledVersion: managed.Version, AvailableVersion: availableVersion, ManagedHash: managed.Hash, CurrentHash: currentHash, AvailableHash: availableHash, InstalledAt: managed.InstalledAt, UpdatedAt: managed.UpdatedAt}
}

func metadataInfoFromMetadata(metadata SkillMetadata) metadataInfo {
	return metadataInfo{State: "valid", Hash: metadata.ContentSHA256, Version: metadata.DistributionVersion, Source: metadata.SourceID, InstalledAt: metadata.InstalledAt, UpdatedAt: metadata.UpdatedAt}
}

func (p *ProfileOperator) countSyncResult(stats *ProfileSkillStats, result SkillResult) {
	switch result.Action {
	case "installed":
		stats.Installed++
	case "updated":
		stats.Updated++
	case "unchanged":
		stats.Unchanged++
	case "blocked":
		if result.State == "customized" {
			stats.Customized++
			stats.CustomizedNames = append(stats.CustomizedNames, result.Name)
		} else {
			stats.Unmanaged++
			stats.UnmanagedNames = append(stats.UnmanagedNames, result.Name)
		}
	}
}

func (p *ProfileOperator) countStatusResult(stats *SkillStatusStats, result SkillResult) {
	switch result.State {
	case "managed":
		stats.Managed++
		if result.Action == "update_available" {
			stats.UpdateAvailable++
		}
	case "customized":
		stats.Customized++
	case "unmanaged":
		stats.Unmanaged++
	case "new":
		stats.New++
	}
}

func (p *ProfileOperator) enabledSkills() map[string]bool {
	result := make(map[string]bool)
	if !p.Config.SkillsConfigured && len(p.Config.EnabledSkills) == 0 {
		result["*"] = true
		return result
	}
	for _, skill := range p.Config.EnabledSkills {
		result[skill] = true
	}
	return result
}

func (p *ProfileOperator) distributionInfo() (string, string) {
	version := "unknown"
	data, err := os.ReadFile(filepath.Join(p.Config.RepositoryRoot, "release", "manifest.json"))
	if err == nil {
		var manifest struct {
			OpenLia string `json:"openlia"`
		}
		if json.Unmarshal(data, &manifest) == nil && manifest.OpenLia != "" {
			version = manifest.OpenLia
		}
	}
	source := "local-checkout"
	if data, err := os.ReadFile(filepath.Join(p.Config.RepositoryRoot, "release.sha256")); err == nil {
		if fields := strings.Fields(string(data)); len(fields) > 0 && fields[0] != "" {
			source = "release:" + fields[0]
		}
	}
	return version, source
}

func readSkillMetadata(path, expectedSkill string) (metadataInfo, error) {
	legacyMarker := strings.TrimSuffix(path, ".json") + ".sha256"
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		reason := "metadata_missing"
		if _, markerErr := os.Stat(legacyMarker); markerErr == nil {
			reason = "legacy_provenance"
		}
		return metadataInfo{State: "unmanaged", Reason: reason}, nil
	}
	if err != nil {
		return metadataInfo{}, err
	}
	var metadata SkillMetadata
	if json.Unmarshal(data, &metadata) != nil || !validSkillMetadata(metadata, expectedSkill) {
		return metadataInfo{State: "invalid", Reason: "invalid_provenance"}, nil
	}
	return metadataInfo{State: "valid", Hash: metadata.ContentSHA256, Version: metadata.DistributionVersion, Source: metadata.SourceID, InstalledAt: metadata.InstalledAt, UpdatedAt: metadata.UpdatedAt}, nil
}

func validSkillMetadata(metadata SkillMetadata, expectedSkill string) bool {
	return metadata.Schema == 1 && metadata.Skill == expectedSkill && metadata.Distribution == distributionName && metadata.DistributionVersion != "" && metadata.SourceID != "" && metadata.ArtifactPath == "profile/skills/"+expectedSkill && metadata.HashScope == hashScope && contentHashPattern.MatchString(metadata.ContentSHA256) && metadata.InstalledAt != "" && metadata.UpdatedAt != ""
}

func writeSkillMetadata(path string, metadata SkillMetadata) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return AtomicWriteFile(path, append(data, '\n'), 0o600)
}

func writeMarker(path, hash string) error {
	return AtomicWriteFile(path, []byte(hash+"\n"), 0o600)
}

func normalizeSkill(path string) error {
	return filepath.WalkDir(path, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			return os.Chmod(current, 0o755)
		}
		return os.Chmod(current, 0o644)
	})
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
