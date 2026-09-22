package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const migrationProtocolSchema = 1

type SkillForkResult struct {
	OK        bool   `json:"ok"`
	Action    string `json:"action"`
	Skill     string `json:"skill"`
	Ownership string `json:"ownership"`
	PatchHash string `json:"patch_hash"`
	ForkHash  string `json:"fork_hash"`
	Backup    string `json:"backup,omitempty"`
}

type SkillMigrationContext struct {
	Schema           int    `json:"schema"`
	Skill            string `json:"skill"`
	Origin           string `json:"origin,omitempty"`
	Source           string `json:"source,omitempty"`
	OldCommit        string `json:"old_commit,omitempty"`
	NewCommit        string `json:"new_commit,omitempty"`
	OldBaseHash      string `json:"old_base_hash"`
	CurrentForkHash  string `json:"current_fork_hash"`
	CurrentPatchHash string `json:"current_patch_hash"`
	NewBaseHash      string `json:"new_base_hash"`
	NewVersion       string `json:"new_version"`
	NewSource        string `json:"new_source"`
	CreatedAt        string `json:"created_at"`
}

type SkillMigrationContextResult struct {
	OK      bool                  `json:"ok"`
	Action  string                `json:"action"`
	Skill   string                `json:"skill"`
	Context string                `json:"context"`
	Details SkillMigrationContext `json:"details"`
}

type SkillMigrationProposal struct {
	Schema           int      `json:"schema"`
	ID               string   `json:"id"`
	Skill            string   `json:"skill"`
	State            string   `json:"state"`
	Origin           string   `json:"origin,omitempty"`
	Source           string   `json:"source,omitempty"`
	OldCommit        string   `json:"old_commit,omitempty"`
	NewCommit        string   `json:"new_commit,omitempty"`
	OldBaseHash      string   `json:"old_base_hash"`
	CurrentForkHash  string   `json:"current_fork_hash"`
	OldPatchHash     string   `json:"old_patch_hash"`
	NewBaseHash      string   `json:"new_base_hash"`
	NewPatchHash     string   `json:"new_patch_hash"`
	ProposedForkHash string   `json:"proposed_fork_hash"`
	Conflicts        []string `json:"conflicts"`
	CreatedAt        string   `json:"created_at"`
	AppliedAt        string   `json:"applied_at,omitempty"`
	RejectedAt       string   `json:"rejected_at,omitempty"`
}

type SkillMigrationResult struct {
	OK        bool   `json:"ok"`
	Action    string `json:"action"`
	Proposal  string `json:"proposal"`
	Skill     string `json:"skill"`
	State     string `json:"state"`
	Backup    string `json:"backup,omitempty"`
	ForkHash  string `json:"fork_hash,omitempty"`
	PatchHash string `json:"patch_hash,omitempty"`
}

func ReadSkillMigrationProposal(config Config, proposalID string) (SkillMigrationProposal, error) {
	if err := config.ValidatePaths(); err != nil {
		return SkillMigrationProposal{}, err
	}
	if err := ValidateSafeComponent(proposalID, "proposal"); err != nil {
		return SkillMigrationProposal{}, err
	}
	path := filepath.Join(config.DataRoot, ".openlia", "proposals", proposalID, "proposal.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillMigrationProposal{}, err
	}
	var proposal SkillMigrationProposal
	if err := json.Unmarshal(data, &proposal); err != nil {
		return SkillMigrationProposal{}, err
	}
	if proposal.Schema != migrationProtocolSchema || proposal.ID != proposalID {
		return SkillMigrationProposal{}, fmt.Errorf("invalid migration proposal")
	}
	return proposal, nil
}

func ForkSkill(config Config, name string, now time.Time) (SkillForkResult, error) {
	if err := config.ValidatePaths(); err != nil {
		return SkillForkResult{}, err
	}
	if err := ValidateSafeComponent(name, "skill"); err != nil {
		return SkillForkResult{}, err
	}
	if name == protectedSkillName {
		return SkillForkResult{}, fmt.Errorf("protected system skill cannot be forked")
	}
	metadataPath := filepath.Join(config.MetaRoot, "managed", "skills", name+".json")
	metadata, err := readFullSkillMetadata(metadataPath, name)
	if err != nil {
		manager := NewExternalSkillManager(config, nil, NewCompose(config, nil))
		result, externalErr := manager.Fork(context.Background(), name)
		if externalErr != nil {
			return SkillForkResult{}, externalErr
		}
		externalMetadata, readErr := readExternalSkillMetadata(filepath.Join(config.MetaRoot, "external-skills", name+".json"))
		if readErr != nil {
			return SkillForkResult{}, readErr
		}
		return SkillForkResult{OK: true, Action: result.Action, Skill: name, Ownership: externalMetadata.Ownership, PatchHash: externalMetadata.PatchHash, ForkHash: externalMetadata.ForkHash}, nil
	}
	if metadata.Ownership == "forked" {
		return SkillForkResult{}, fmt.Errorf("skill is already forked: %s", name)
	}
	source := filepath.Join(config.RepositoryRoot, "profile", "skills", name)
	destination := filepath.Join(config.DataRoot, "skills", name)
	basePath := filepath.Join(config.MetaRoot, filepath.FromSlash(metadata.BaseSnapshot))
	if _, err := os.Stat(source); err != nil {
		return SkillForkResult{}, fmt.Errorf("skill is not bundled: %s", name)
	}
	baseHash, err := DirectorySHA256(basePath)
	if err != nil || "sha256:"+baseHash != metadata.ContentSHA256 {
		return SkillForkResult{}, fmt.Errorf("skill base snapshot is missing or invalid")
	}
	if info, err := os.Lstat(destination); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return SkillForkResult{}, fmt.Errorf("installed skill is missing or unsafe: %s", name)
	}
	currentHash, err := DirectorySHA256(destination)
	if err != nil {
		return SkillForkResult{}, err
	}
	resume, err := pauseHermesForSkillMutation(config)
	if err != nil {
		return SkillForkResult{}, err
	}
	defer resume()
	patch, err := GenerateSkillPatch(basePath, destination, name)
	if err != nil {
		return SkillForkResult{}, err
	}
	backup, err := CreateBackup(config, "skill-fork", now)
	if err != nil {
		return SkillForkResult{}, err
	}
	patchPath := filepath.Join(config.MetaRoot, filepath.FromSlash(metadata.PatchPath))
	patchHash, err := WriteSkillPatch(patchPath, patch)
	if err != nil {
		return SkillForkResult{}, err
	}
	metadata.Ownership = "forked"
	metadata.ForkSHA256 = "sha256:" + currentHash
	metadata.PatchSHA256 = patchHash
	metadata.ForkedAt = utcTimestamp(now)
	metadata.UpdatedAt = utcTimestamp(now)
	if err := writeSkillMetadata(metadataPath, metadata); err != nil {
		return SkillForkResult{}, err
	}
	if err := RecordChange(config, "skill-fork", "ok", backup.Archive, "skill="+name, now); err != nil {
		return SkillForkResult{}, err
	}
	return SkillForkResult{OK: true, Action: "fork", Skill: name, Ownership: metadata.Ownership, PatchHash: patchHash, ForkHash: metadata.ForkSHA256, Backup: backup.Archive}, nil
}

func PrepareSkillMigration(config Config, name string, now time.Time) (SkillMigrationContextResult, error) {
	if err := config.ValidatePaths(); err != nil {
		return SkillMigrationContextResult{}, err
	}
	if err := ValidateSafeComponent(name, "skill"); err != nil {
		return SkillMigrationContextResult{}, err
	}
	metadataPath := filepath.Join(config.MetaRoot, "managed", "skills", name+".json")
	metadata, err := readFullSkillMetadata(metadataPath, name)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return SkillMigrationContextResult{}, err
		}
		return prepareExternalSkillMigration(config, name, now)
	}
	if metadata.Ownership != "forked" {
		return SkillMigrationContextResult{}, fmt.Errorf("skill is not forked: %s", name)
	}
	basePath := filepath.Join(config.MetaRoot, filepath.FromSlash(metadata.BaseSnapshot))
	destination := filepath.Join(config.DataRoot, "skills", name)
	newBase := filepath.Join(config.RepositoryRoot, "profile", "skills", name)
	if _, err := os.Stat(newBase); err != nil {
		return SkillMigrationContextResult{}, fmt.Errorf("skill is no longer bundled: %s", name)
	}
	currentForkHash, err := DirectorySHA256(destination)
	if err != nil {
		return SkillMigrationContextResult{}, err
	}
	oldBaseHash, err := DirectorySHA256(basePath)
	if err != nil {
		return SkillMigrationContextResult{}, err
	}
	newBaseHash, err := DirectorySHA256(newBase)
	if err != nil {
		return SkillMigrationContextResult{}, err
	}
	patch, err := GenerateSkillPatch(basePath, destination, name)
	if err != nil {
		return SkillMigrationContextResult{}, err
	}
	patchHash, err := SkillPatchHash(patch)
	if err != nil {
		return SkillMigrationContextResult{}, err
	}
	contextRoot := filepath.Join(config.DataRoot, ".openlia", "migrations", name)
	if err := removeSafeDirectory(contextRoot); err != nil {
		return SkillMigrationContextResult{}, err
	}
	if err := EnsureDir(filepath.Dir(contextRoot), 0o700); err != nil {
		return SkillMigrationContextResult{}, err
	}
	if err := CopyDir(basePath, filepath.Join(contextRoot, "old-base")); err != nil {
		return SkillMigrationContextResult{}, err
	}
	if err := CopyDir(destination, filepath.Join(contextRoot, "current-fork")); err != nil {
		return SkillMigrationContextResult{}, err
	}
	if err := CopyDir(newBase, filepath.Join(contextRoot, "new-base")); err != nil {
		return SkillMigrationContextResult{}, err
	}
	if _, err := WriteSkillPatch(filepath.Join(contextRoot, "customization.patch"), patch); err != nil {
		return SkillMigrationContextResult{}, err
	}
	version, source := (&ProfileOperator{Config: config}).distributionInfo()
	details := SkillMigrationContext{Schema: migrationProtocolSchema, Skill: name, OldBaseHash: "sha256:" + oldBaseHash, CurrentForkHash: "sha256:" + currentForkHash, CurrentPatchHash: patchHash, NewBaseHash: "sha256:" + newBaseHash, NewVersion: version, NewSource: source, CreatedAt: utcTimestamp(now)}
	data, err := json.Marshal(details)
	if err != nil {
		return SkillMigrationContextResult{}, err
	}
	if err := AtomicWriteFile(filepath.Join(contextRoot, "context.json"), append(data, '\n'), 0o600); err != nil {
		return SkillMigrationContextResult{}, err
	}
	return SkillMigrationContextResult{OK: true, Action: "migration-prepare", Skill: name, Context: contextRoot, Details: details}, nil
}

func prepareExternalSkillMigration(config Config, name string, now time.Time) (SkillMigrationContextResult, error) {
	metadata, err := readExternalSkillMetadata(filepath.Join(config.MetaRoot, "external-skills", name+".json"))
	if err != nil {
		return SkillMigrationContextResult{}, err
	}
	if metadata.Ownership != "forked" {
		return SkillMigrationContextResult{}, fmt.Errorf("skill is not forked: %s", name)
	}
	destination, _, err := locateExternalSkillTree(config, name)
	if err != nil {
		return SkillMigrationContextResult{}, fmt.Errorf("locate external skill %s: %w", name, err)
	}
	currentHash, err := DirectorySHA256(destination)
	if err != nil {
		return SkillMigrationContextResult{}, err
	}
	if "sha256:"+currentHash != metadata.ForkHash {
		return SkillMigrationContextResult{}, fmt.Errorf("external fork has changed since it was refreshed: %s", name)
	}
	oldBase := filepath.Join(config.MetaRoot, filepath.FromSlash(metadata.BaseSnapshot))
	oldBaseHash, err := DirectorySHA256(oldBase)
	if err != nil || "sha256:"+oldBaseHash != metadata.ContentHash {
		return SkillMigrationContextResult{}, fmt.Errorf("external skill base snapshot is missing or invalid")
	}
	patch, err := GenerateSkillPatch(oldBase, destination, name)
	if err != nil {
		return SkillMigrationContextResult{}, err
	}
	patchHash, err := SkillPatchHash(patch)
	if err != nil {
		return SkillMigrationContextResult{}, err
	}
	if patchHash != metadata.PatchHash {
		return SkillMigrationContextResult{}, fmt.Errorf("external fork patch metadata is stale: %s", name)
	}
	manager := NewExternalSkillManager(config, nil, NewCompose(config, nil))
	item, _, _, newBase, err := externalMigrationTarget(manager, metadata.Source, name, "")
	if err != nil {
		return SkillMigrationContextResult{}, err
	}
	newBaseHash, err := DirectorySHA256(newBase)
	if err != nil {
		return SkillMigrationContextResult{}, err
	}
	contextRoot := filepath.Join(config.DataRoot, ".openlia", "migrations", name)
	if err := removeSafeDirectory(contextRoot); err != nil {
		return SkillMigrationContextResult{}, err
	}
	if err := EnsureDir(filepath.Dir(contextRoot), 0o700); err != nil {
		return SkillMigrationContextResult{}, err
	}
	for source, target := range map[string]string{oldBase: "old-base", destination: "current-fork", newBase: "new-base"} {
		if err := CopyDir(source, filepath.Join(contextRoot, target)); err != nil {
			return SkillMigrationContextResult{}, err
		}
	}
	if _, err := WriteSkillPatch(filepath.Join(contextRoot, "customization.patch"), patch); err != nil {
		return SkillMigrationContextResult{}, err
	}
	details := SkillMigrationContext{Schema: migrationProtocolSchema, Skill: name, Origin: "external", Source: metadata.Source, OldCommit: metadata.Commit, NewCommit: item.Commit, OldBaseHash: metadata.ContentHash, CurrentForkHash: metadata.ForkHash, CurrentPatchHash: patchHash, NewBaseHash: "sha256:" + newBaseHash, NewVersion: item.Version, NewSource: metadata.Source, CreatedAt: utcTimestamp(now)}
	data, err := json.Marshal(details)
	if err != nil {
		return SkillMigrationContextResult{}, err
	}
	if err := AtomicWriteFile(filepath.Join(contextRoot, "context.json"), append(data, '\n'), 0o600); err != nil {
		return SkillMigrationContextResult{}, err
	}
	return SkillMigrationContextResult{OK: true, Action: "migration-prepare", Skill: name, Context: contextRoot, Details: details}, nil
}

func ApplySkillMigration(config Config, proposalID string, now time.Time) (SkillMigrationResult, error) {
	if err := config.ValidatePaths(); err != nil {
		return SkillMigrationResult{}, err
	}
	if err := ValidateSafeComponent(proposalID, "proposal"); err != nil {
		return SkillMigrationResult{}, err
	}
	proposalRoot := filepath.Join(config.DataRoot, ".openlia", "proposals", proposalID)
	proposalPath := filepath.Join(proposalRoot, "proposal.json")
	proposalData, err := os.ReadFile(proposalPath)
	if err != nil {
		return SkillMigrationResult{}, fmt.Errorf("read migration proposal: %w", err)
	}
	var proposal SkillMigrationProposal
	if err := json.Unmarshal(proposalData, &proposal); err != nil {
		return SkillMigrationResult{}, fmt.Errorf("parse migration proposal: %w", err)
	}
	if proposal.Schema != migrationProtocolSchema || proposal.ID != proposalID || proposal.State != "pending" {
		return SkillMigrationResult{}, fmt.Errorf("migration proposal is not pending")
	}
	if len(proposal.Conflicts) != 0 {
		return SkillMigrationResult{}, fmt.Errorf("migration proposal still has unresolved conflicts")
	}
	if err := ValidateSafeComponent(proposal.Skill, "skill"); err != nil {
		return SkillMigrationResult{}, err
	}
	if proposal.Origin == "external" {
		return applyExternalSkillMigration(config, proposalRoot, proposalPath, proposalData, proposal, now)
	}
	if proposal.Origin != "" {
		return SkillMigrationResult{}, fmt.Errorf("unsupported migration proposal origin %q", proposal.Origin)
	}
	resume, err := pauseHermesForSkillMutation(config)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	defer resume()
	metadataPath := filepath.Join(config.MetaRoot, "managed", "skills", proposal.Skill+".json")
	metadata, err := readFullSkillMetadata(metadataPath, proposal.Skill)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	if metadata.Ownership != "forked" {
		return SkillMigrationResult{}, fmt.Errorf("skill is not forked: %s", proposal.Skill)
	}
	if proposal.OldBaseHash != metadata.ContentSHA256 {
		return markProposalStale(proposalPath, proposal, now, "fork base changed")
	}
	destination := filepath.Join(config.DataRoot, "skills", proposal.Skill)
	currentHash, err := DirectorySHA256(destination)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	if "sha256:"+currentHash != proposal.CurrentForkHash {
		return markProposalStale(proposalPath, proposal, now, "current fork changed")
	}
	source := filepath.Join(config.RepositoryRoot, "profile", "skills", proposal.Skill)
	newBaseHash, err := DirectorySHA256(source)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	if "sha256:"+newBaseHash != proposal.NewBaseHash {
		return markProposalStale(proposalPath, proposal, now, "upstream base changed")
	}
	patchPath := filepath.Join(proposalRoot, "customization.patch")
	patch, err := ReadSkillPatch(patchPath)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	if patch.Skill != proposal.Skill || patch.BaseHash != proposal.NewBaseHash {
		return SkillMigrationResult{}, fmt.Errorf("migration proposal patch does not match the proposal")
	}
	patchHash, err := SkillPatchHash(patch)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	if patchHash != proposal.NewPatchHash {
		return SkillMigrationResult{}, fmt.Errorf("migration proposal patch hash mismatch")
	}
	proposedTree := filepath.Join(proposalRoot, ".validated-skill")
	if err := removeSafeDirectory(proposedTree); err != nil {
		return SkillMigrationResult{}, err
	}
	if err := ApplySkillPatch(source, proposedTree, patch); err != nil {
		return SkillMigrationResult{}, fmt.Errorf("validate migration patch: %w", err)
	}
	proposedHash, err := DirectorySHA256(proposedTree)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	if "sha256:"+proposedHash != proposal.ProposedForkHash {
		return SkillMigrationResult{}, fmt.Errorf("migration proposal fork hash mismatch")
	}
	backup, err := CreateBackup(config, "skill-migration", now)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	oldBase := filepath.Join(config.MetaRoot, filepath.FromSlash(metadata.BaseSnapshot))
	oldPatch := filepath.Join(config.MetaRoot, filepath.FromSlash(metadata.PatchPath))
	oldMetadata, err := os.ReadFile(metadataPath)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	oldPatchData, err := os.ReadFile(oldPatch)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	txRoot, err := os.MkdirTemp(filepath.Join(config.MetaRoot, "managed"), ".migration-tx-")
	if err != nil {
		return SkillMigrationResult{}, err
	}
	defer os.RemoveAll(txRoot)
	oldFork := filepath.Join(txRoot, "old-fork")
	oldBaseCopy := filepath.Join(txRoot, "old-base")
	if err := CopyDir(destination, oldFork); err != nil {
		return SkillMigrationResult{}, err
	}
	if err := CopyDir(oldBase, oldBaseCopy); err != nil {
		return SkillMigrationResult{}, err
	}
	if err := replaceDirectory(proposedTree, destination); err != nil {
		return rollbackSkillMigration(destination, oldFork, oldBase, oldBaseCopy, oldPatch, oldPatchData, metadataPath, oldMetadata, proposalPath, proposalData, err)
	}
	if err := replaceDirectory(source, oldBase); err != nil {
		return rollbackSkillMigration(destination, oldFork, oldBase, oldBaseCopy, oldPatch, oldPatchData, metadataPath, oldMetadata, proposalPath, proposalData, err)
	}
	if err := AtomicWriteFile(oldPatch, mustPatchBytes(patch), 0o600); err != nil {
		return rollbackSkillMigration(destination, oldFork, oldBase, oldBaseCopy, oldPatch, oldPatchData, metadataPath, oldMetadata, proposalPath, proposalData, err)
	}
	metadata.ContentSHA256 = proposal.NewBaseHash
	metadata.DistributionVersion, metadata.SourceID = (&ProfileOperator{Config: config}).distributionInfo()
	metadata.PatchSHA256 = patchHash
	metadata.ForkSHA256 = proposal.ProposedForkHash
	metadata.UpdatedAt = utcTimestamp(now)
	metadataData, err := json.Marshal(metadata)
	if err != nil {
		return rollbackSkillMigration(destination, oldFork, oldBase, oldBaseCopy, oldPatch, oldPatchData, metadataPath, oldMetadata, proposalPath, proposalData, err)
	}
	if err := AtomicWriteFile(metadataPath, append(metadataData, '\n'), 0o600); err != nil {
		return rollbackSkillMigration(destination, oldFork, oldBase, oldBaseCopy, oldPatch, oldPatchData, metadataPath, oldMetadata, proposalPath, proposalData, err)
	}
	proposal.State = "applied"
	proposal.AppliedAt = utcTimestamp(now)
	updatedProposal, err := json.Marshal(proposal)
	if err != nil {
		return rollbackSkillMigration(destination, oldFork, oldBase, oldBaseCopy, oldPatch, oldPatchData, metadataPath, oldMetadata, proposalPath, proposalData, err)
	}
	if err := AtomicWriteFile(proposalPath, append(updatedProposal, '\n'), 0o600); err != nil {
		return rollbackSkillMigration(destination, oldFork, oldBase, oldBaseCopy, oldPatch, oldPatchData, metadataPath, oldMetadata, proposalPath, proposalData, err)
	}
	if err := RecordChange(config, "skill-migration", "ok", backup.Archive, "skill="+proposal.Skill+" proposal="+proposalID, now); err != nil {
		return rollbackSkillMigration(destination, oldFork, oldBase, oldBaseCopy, oldPatch, oldPatchData, metadataPath, oldMetadata, proposalPath, proposalData, err)
	}
	return SkillMigrationResult{OK: true, Action: "skill-migration", Proposal: proposalID, Skill: proposal.Skill, State: proposal.State, Backup: backup.Archive, ForkHash: proposal.ProposedForkHash, PatchHash: patchHash}, nil
}

func applyExternalSkillMigration(config Config, proposalRoot, proposalPath string, oldProposalData []byte, proposal SkillMigrationProposal, now time.Time) (SkillMigrationResult, error) {
	if ValidateSafeComponent(proposal.Source, "source") != nil || !isCommit(proposal.OldCommit) || !isCommit(proposal.NewCommit) {
		return SkillMigrationResult{}, fmt.Errorf("invalid external migration provenance")
	}
	metadataPath := filepath.Join(config.MetaRoot, "external-skills", proposal.Skill+".json")
	metadata, err := readExternalSkillMetadata(metadataPath)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	if metadata.Ownership != "forked" {
		return SkillMigrationResult{}, fmt.Errorf("skill is not forked: %s", proposal.Skill)
	}
	if metadata.Source != proposal.Source || metadata.Commit != proposal.OldCommit {
		return markProposalStale(proposalPath, proposal, now, "external source or old commit changed")
	}
	if metadata.ContentHash != proposal.OldBaseHash || metadata.PatchHash != proposal.OldPatchHash {
		return markProposalStale(proposalPath, proposal, now, "fork base or patch changed")
	}
	oldBase := filepath.Join(config.MetaRoot, filepath.FromSlash(metadata.BaseSnapshot))
	oldBaseHash, err := DirectorySHA256(oldBase)
	if err != nil || "sha256:"+oldBaseHash != proposal.OldBaseHash {
		return markProposalStale(proposalPath, proposal, now, "fork base snapshot changed")
	}
	destination, _, err := locateExternalSkillTree(config, proposal.Skill)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	currentHash, err := DirectorySHA256(destination)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	if "sha256:"+currentHash != metadata.ForkHash || metadata.ForkHash != proposal.CurrentForkHash {
		return markProposalStale(proposalPath, proposal, now, "current fork changed")
	}
	manager := NewExternalSkillManager(config, nil, NewCompose(config, nil))
	item, _, manifestItem, newBase, err := externalMigrationTarget(manager, proposal.Source, proposal.Skill, proposal.NewCommit)
	if err != nil {
		return markProposalStale(proposalPath, proposal, now, "external source snapshot changed")
	}
	newBaseHash, err := DirectorySHA256(newBase)
	if err != nil || "sha256:"+newBaseHash != proposal.NewBaseHash {
		return markProposalStale(proposalPath, proposal, now, "upstream base changed")
	}
	patch, err := ReadSkillPatch(filepath.Join(proposalRoot, "customization.patch"))
	if err != nil {
		return SkillMigrationResult{}, err
	}
	if patch.Skill != proposal.Skill || patch.BaseHash != proposal.NewBaseHash {
		return SkillMigrationResult{}, fmt.Errorf("migration proposal patch does not match the proposal")
	}
	patchHash, err := SkillPatchHash(patch)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	if patchHash != proposal.NewPatchHash {
		return SkillMigrationResult{}, fmt.Errorf("migration proposal patch hash mismatch")
	}
	proposedTree := filepath.Join(proposalRoot, ".validated-skill")
	if err := removeSafeDirectory(proposedTree); err != nil {
		return SkillMigrationResult{}, err
	}
	if err := ApplySkillPatch(newBase, proposedTree, patch); err != nil {
		return SkillMigrationResult{}, fmt.Errorf("validate migration patch: %w", err)
	}
	proposedHash, err := DirectorySHA256(proposedTree)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	if "sha256:"+proposedHash != proposal.ProposedForkHash {
		return SkillMigrationResult{}, fmt.Errorf("migration proposal fork hash mismatch")
	}
	audit, err := manager.auditSkillTree(context.Background(), item, manifestItem, proposedTree)
	if err != nil {
		return SkillMigrationResult{}, fmt.Errorf("audit migrated external skill: %w", err)
	}
	backup, err := CreateBackup(config, "skill-migration", now, manager.Compose)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	manager.Now = func() time.Time { return now }
	err = manager.transactionalSkillMutation(destination, metadataPath, func() error {
		return manager.withPausedHermes(context.Background(), func() error {
			if err := replaceDirectory(proposedTree, destination); err != nil {
				return err
			}
			if err := replaceDirectory(newBase, oldBase); err != nil {
				return err
			}
			if err := AtomicWriteFile(filepath.Join(config.MetaRoot, filepath.FromSlash(metadata.PatchPath)), mustPatchBytes(patch), 0o600); err != nil {
				return err
			}
			metadata.Commit, metadata.Version, metadata.ContentHash = item.Commit, item.Version, proposal.NewBaseHash
			metadata.PatchHash, metadata.ForkHash, metadata.UpdatedAt, metadata.Environment = patchHash, proposal.ProposedForkHash, utcTimestamp(now), audit.EnvironmentHash
			metadataData, err := json.Marshal(metadata)
			if err != nil {
				return err
			}
			if err := AtomicWriteFile(metadataPath, append(metadataData, '\n'), 0o600); err != nil {
				return err
			}
			if err := activateSkillEnvironment(config.SkillsEnvRoot, proposal.Skill, audit.EnvironmentHash); err != nil {
				return err
			}
			proposal.State, proposal.AppliedAt = "applied", utcTimestamp(now)
			proposalData, err := json.Marshal(proposal)
			if err != nil {
				return err
			}
			if err := AtomicWriteFile(proposalPath, append(proposalData, '\n'), 0o600); err != nil {
				return err
			}
			return RecordChange(config, "skill-migration", "ok", backup.Archive, "external skill="+proposal.Skill+" source="+proposal.Source+" commit="+proposal.NewCommit+" proposal="+proposal.ID, now)
		})
	})
	if err != nil {
		_ = AtomicWriteFile(proposalPath, oldProposalData, 0o600)
		return SkillMigrationResult{}, err
	}
	return SkillMigrationResult{OK: true, Action: "skill-migration", Proposal: proposal.ID, Skill: proposal.Skill, State: proposal.State, Backup: backup.Archive, ForkHash: proposal.ProposedForkHash, PatchHash: patchHash}, nil
}

func externalMigrationTarget(manager *ExternalSkillManager, sourceID, name, expectedCommit string) (ExternalSkillCatalogItem, string, SkillCollectionManifestItem, string, error) {
	source, err := manager.source(sourceID)
	if err != nil {
		return ExternalSkillCatalogItem{}, "", SkillCollectionManifestItem{}, "", err
	}
	currentData, err := os.ReadFile(filepath.Join(manager.Config.SkillsCacheRoot, sourceID, "current"))
	if err != nil {
		return ExternalSkillCatalogItem{}, "", SkillCollectionManifestItem{}, "", err
	}
	commit := strings.TrimSpace(string(currentData))
	if !isCommit(commit) || expectedCommit != "" && commit != expectedCommit {
		return ExternalSkillCatalogItem{}, "", SkillCollectionManifestItem{}, "", fmt.Errorf("external source current commit changed")
	}
	root := filepath.Join(manager.Config.SkillsCacheRoot, sourceID, commit)
	digest, err := DirectorySHA256(root)
	if err != nil {
		return ExternalSkillCatalogItem{}, "", SkillCollectionManifestItem{}, "", err
	}
	stored, err := os.ReadFile(filepath.Join(manager.Config.SkillsCacheRoot, sourceID, commit+".sha256"))
	if err != nil || strings.TrimSpace(string(stored)) != "sha256:"+digest {
		return ExternalSkillCatalogItem{}, "", SkillCollectionManifestItem{}, "", fmt.Errorf("cached skill source %s failed digest revalidation", sourceID)
	}
	manifest, err := readCollectionManifest(root, source.Manifest)
	if err != nil {
		return ExternalSkillCatalogItem{}, "", SkillCollectionManifestItem{}, "", err
	}
	if err := validateCollectionTree(root, manifest); err != nil {
		return ExternalSkillCatalogItem{}, "", SkillCollectionManifestItem{}, "", err
	}
	for _, candidate := range manifest.Skills {
		if candidate.Name == name {
			item := ExternalSkillCatalogItem{Name: name, Source: sourceID, Commit: commit, Version: candidate.Version, Path: candidate.Path}
			return item, root, candidate, filepath.Join(root, filepath.FromSlash(candidate.Path)), nil
		}
	}
	return ExternalSkillCatalogItem{}, "", SkillCollectionManifestItem{}, "", fmt.Errorf("external skill not found: %s", name)
}

func pauseHermesForSkillMutation(config Config) (func(), error) {
	compose := NewCompose(config, nil)
	ctx := context.Background()
	if !compose.ServiceRunning(ctx, "hermes") {
		return func() {}, nil
	}
	if _, err := compose.Run(ctx, "stop", "hermes"); err != nil {
		return nil, fmt.Errorf("stop Hermes for skill mutation: %w", err)
	}
	if err := WriteState(config, stateStopped); err != nil {
		_, _ = compose.Run(ctx, "up", "-d", "--no-deps", "hermes")
		return nil, err
	}
	return func() {
		if _, err := compose.Run(ctx, "up", "-d", "--no-deps", "hermes"); err == nil {
			_ = WriteState(config, stateRunning)
		}
	}, nil
}

func RejectSkillMigration(config Config, proposalID string, now time.Time) (SkillMigrationResult, error) {
	if err := config.ValidatePaths(); err != nil {
		return SkillMigrationResult{}, err
	}
	if err := ValidateSafeComponent(proposalID, "proposal"); err != nil {
		return SkillMigrationResult{}, err
	}
	path := filepath.Join(config.DataRoot, ".openlia", "proposals", proposalID, "proposal.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	var proposal SkillMigrationProposal
	if err := json.Unmarshal(data, &proposal); err != nil {
		return SkillMigrationResult{}, err
	}
	if proposal.Schema != migrationProtocolSchema || proposal.ID != proposalID || proposal.State != "pending" {
		return SkillMigrationResult{}, fmt.Errorf("migration proposal is not pending")
	}
	proposal.State = "rejected"
	proposal.RejectedAt = utcTimestamp(now)
	updated, err := json.Marshal(proposal)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	if err := AtomicWriteFile(path, append(updated, '\n'), 0o600); err != nil {
		return SkillMigrationResult{}, err
	}
	return SkillMigrationResult{OK: true, Action: "skill-migration-reject", Proposal: proposalID, Skill: proposal.Skill, State: proposal.State}, nil
}

func readFullSkillMetadata(path, expectedSkill string) (SkillMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillMetadata{}, err
	}
	var metadata SkillMetadata
	if err := json.Unmarshal(data, &metadata); err != nil || !validSkillMetadata(metadata, expectedSkill) {
		return SkillMetadata{}, fmt.Errorf("invalid skill provenance for %s", expectedSkill)
	}
	return metadata, nil
}

func removeSafeDirectory(path string) error {
	if path == "" || filepath.Clean(path) == string(filepath.Separator) {
		return fmt.Errorf("refusing to remove unsafe directory")
	}
	if info, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	} else if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to remove symlink directory")
	}
	return os.RemoveAll(path)
}

func mustPatchBytes(patch SkillPatch) []byte {
	data, _ := json.Marshal(patch)
	return append(data, '\n')
}

func markProposalStale(path string, proposal SkillMigrationProposal, now time.Time, reason string) (SkillMigrationResult, error) {
	proposal.State = "stale"
	proposal.RejectedAt = utcTimestamp(now)
	data, err := json.Marshal(proposal)
	if err != nil {
		return SkillMigrationResult{}, err
	}
	if err := AtomicWriteFile(path, append(data, '\n'), 0o600); err != nil {
		return SkillMigrationResult{}, err
	}
	return SkillMigrationResult{}, fmt.Errorf("migration proposal is stale: %s", reason)
}

func rollbackSkillMigration(destination, oldFork, base, oldBase, patch string, oldPatch []byte, metadataPath string, oldMetadata []byte, proposalPath string, oldProposal []byte, cause error) (SkillMigrationResult, error) {
	_ = replaceDirectory(oldFork, destination)
	_ = replaceDirectory(oldBase, base)
	_ = AtomicWriteFile(patch, oldPatch, 0o600)
	_ = AtomicWriteFile(metadataPath, oldMetadata, 0o600)
	_ = AtomicWriteFile(proposalPath, oldProposal, 0o600)
	return SkillMigrationResult{}, cause
}
