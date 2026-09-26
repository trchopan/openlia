package operator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const instructionMetadataSchema = 1

type InstructionMetadata struct {
	Schema            int    `json:"schema"`
	Name              string `json:"name"`
	Ownership         string `json:"ownership"`
	Distribution      string `json:"distribution_version"`
	SourceID          string `json:"source_id"`
	BaselineHash      string `json:"baseline_hash"`
	CurrentHash       string `json:"current_hash"`
	AvailableHash     string `json:"available_hash,omitempty"`
	BaselineSnapshot  string `json:"baseline_snapshot"`
	AvailableSnapshot string `json:"available_snapshot,omitempty"`
	InstalledAt       string `json:"installed_at"`
	UpdatedAt         string `json:"updated_at"`
}

type InstructionResult struct {
	Name            string `json:"name"`
	State           string `json:"state"`
	Ownership       string `json:"ownership"`
	Action          string `json:"action"`
	CurrentHash     string `json:"current_hash"`
	BaselineHash    string `json:"baseline_hash"`
	AvailableHash   string `json:"available_hash"`
	UpdateAvailable bool   `json:"update_available"`
	Customized      bool   `json:"customized"`
	Distribution    string `json:"distribution_version,omitempty"`
	SourceID        string `json:"source_id,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

type InstructionStatusResult struct {
	Schema            int                 `json:"schema"`
	OK                bool                `json:"ok"`
	Action            string              `json:"action"`
	AttentionRequired bool                `json:"attention_required"`
	Instructions      []InstructionResult `json:"instructions"`
}

type InstructionDiffResult struct {
	Schema   int    `json:"schema"`
	OK       bool   `json:"ok"`
	Action   string `json:"action"`
	Name     string `json:"name"`
	State    string `json:"state"`
	Local    string `json:"local_diff"`
	Upstream string `json:"upstream_diff"`
	ThreeWay string `json:"three_way_diff"`
}

type InstructionMutationResult struct {
	Schema     int    `json:"schema"`
	OK         bool   `json:"ok"`
	Action     string `json:"action"`
	Name       string `json:"name"`
	State      string `json:"state"`
	ResultHash string `json:"result_hash"`
	Backup     string `json:"backup"`
	Conflicts  int    `json:"conflicts"`
}

type InstructionMutationRequest struct {
	Schema                int    `json:"schema"`
	ExpectedCurrentHash   string `json:"expected_current_hash"`
	ExpectedAvailableHash string `json:"expected_available_hash"`
}

type instructionSpec struct {
	Name       string
	Source     string
	Target     string
	LegacyHash string
	Workspace  bool
	Soul       bool
}

func instructionSpecs(config Config) []instructionSpec {
	managed := filepath.Join(config.MetaRoot, "managed")
	return []instructionSpec{
		{Name: "profile/AGENTS.md", Source: filepath.Join(config.RepositoryRoot, "profile", "AGENTS.md"), Target: filepath.Join(config.DataRoot, "AGENTS.md"), LegacyHash: filepath.Join(managed, "AGENTS.md.sha256")},
		{Name: "profile/SOUL.md", Source: filepath.Join(config.RepositoryRoot, "profile", "SOUL.md"), Target: filepath.Join(config.DataRoot, "SOUL.md"), LegacyHash: filepath.Join(managed, "SOUL.md.sha256"), Soul: true},
		{Name: "workspace/AGENTS.md", Source: filepath.Join(config.RepositoryRoot, "workspace-template", "AGENTS.md"), Target: filepath.Join(config.DataRoot, "workspace", "AGENTS.md"), Workspace: true},
	}
}

func instructionKey(name string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(name)
}

func instructionPaths(config Config, name string) (metadata, baseline, available string) {
	root := filepath.Join(config.MetaRoot, "managed", "instructions", instructionKey(name))
	return filepath.Join(root, "metadata.json"), filepath.Join(root, "baseline.md"), filepath.Join(root, "available.md")
}

func instructionSpecByName(config Config, name string) (instructionSpec, error) {
	for _, spec := range instructionSpecs(config) {
		if spec.Name == name {
			return spec, nil
		}
	}
	return instructionSpec{}, fmt.Errorf("unknown instruction %q; use profile/AGENTS.md, profile/SOUL.md, or workspace/AGENTS.md", name)
}

func canonicalInstruction(spec instructionSpec, data []byte) []byte {
	if spec.Soul {
		if !strings.Contains(string(data), outputLanguageBlockStart) {
			withBlock, err := renderOutputLanguageBlock(data, "en")
			if err == nil {
				data = withBlock
			}
		}
		return normalizeOutputLanguageBlock(data)
	}
	return data
}

func instructionHash(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func readInstructionFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("instruction path is not a regular file: %s", path)
	}
	return os.ReadFile(path)
}

func readInstructionMetadata(path, name string) (InstructionMetadata, error) {
	data, err := readInstructionFile(path)
	if err != nil {
		return InstructionMetadata{}, err
	}
	var metadata InstructionMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return InstructionMetadata{}, err
	}
	if metadata.Schema != instructionMetadataSchema || metadata.Name != name || metadata.BaselineHash == "" || metadata.BaselineSnapshot == "" {
		return InstructionMetadata{}, fmt.Errorf("invalid instruction metadata: %s", name)
	}
	return metadata, nil
}

func writeInstructionMetadata(path string, metadata InstructionMetadata) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return AtomicWriteFile(path, append(data, '\n'), 0o600)
}

func distributionIdentity(config Config) (string, string) {
	version, source := "unknown", "embedded"
	if data, err := os.ReadFile(filepath.Join(config.RepositoryRoot, "release", "manifest.json")); err == nil {
		var manifest struct {
			OpenLIA string `json:"openlia"`
		}
		if json.Unmarshal(data, &manifest) == nil && manifest.OpenLIA != "" {
			version = manifest.OpenLIA
		}
	}
	if data, err := os.ReadFile(filepath.Join(config.RepositoryRoot, "release.sha256")); err == nil && strings.TrimSpace(string(data)) != "" {
		source = "sha256:" + strings.TrimSpace(string(data))
	}
	return version, source
}

// SyncInstructions updates unmodified profile instructions and stages all
// customized or workspace-template changes for explicit review.
func SyncInstructions(config Config, now time.Time) (InstructionStatusResult, error) {
	version, sourceID := distributionIdentity(config)
	results := make([]InstructionResult, 0, 3)
	for _, spec := range instructionSpecs(config) {
		if _, err := os.Lstat(spec.Source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return InstructionStatusResult{}, err
		}
		result, err := syncInstruction(config, spec, version, sourceID, now)
		if err != nil {
			return InstructionStatusResult{}, err
		}
		results = append(results, result)
	}
	return instructionStatusEnvelope("sync", results), nil
}

func syncInstruction(config Config, spec instructionSpec, version, sourceID string, now time.Time) (InstructionResult, error) {
	sourceData, err := readInstructionFile(spec.Source)
	if err != nil {
		return InstructionResult{}, err
	}
	currentData, err := readInstructionFile(spec.Target)
	if errors.Is(err, os.ErrNotExist) {
		currentData = sourceData
		if spec.Soul {
			currentData, err = renderOutputLanguageBlock(currentData, config.OutputLanguage)
			if err != nil {
				return InstructionResult{}, err
			}
		}
		if err := AtomicWriteFile(spec.Target, currentData, 0o600); err != nil {
			return InstructionResult{}, err
		}
		if err := ensureRuntimeOwner(spec.Target, config.RuntimeUID, config.RuntimeGID, 0o600); err != nil {
			return InstructionResult{}, err
		}
	} else if err != nil {
		return InstructionResult{}, err
	}
	sourceCanonical := canonicalInstruction(spec, sourceData)
	currentCanonical := canonicalInstruction(spec, currentData)
	metadataPath, baselinePath, availablePath := instructionPaths(config, spec.Name)
	if err := EnsureDir(filepath.Dir(metadataPath), 0o700); err != nil {
		return InstructionResult{}, err
	}
	metadata, metadataErr := readInstructionMetadata(metadataPath, spec.Name)
	stamp := utcTimestamp(now)
	if errors.Is(metadataErr, os.ErrNotExist) {
		ownership := "customized"
		managed := false
		if !spec.Workspace {
			if markerData, markerErr := os.ReadFile(spec.LegacyHash); markerErr == nil {
				marker := strings.TrimSpace(string(markerData))
				managed = marker == instructionHash(currentCanonical) || marker == strings.TrimPrefix(instructionHash(currentCanonical), "sha256:")
			} else {
				managed = instructionHash(currentCanonical) == instructionHash(sourceCanonical)
			}
		}
		if managed {
			ownership = "managed"
		}
		if err := AtomicWriteFile(baselinePath, currentCanonical, 0o600); err != nil {
			return InstructionResult{}, err
		}
		metadata = InstructionMetadata{Schema: 1, Name: spec.Name, Ownership: ownership, Distribution: version, SourceID: sourceID, BaselineHash: instructionHash(currentCanonical), CurrentHash: instructionHash(currentCanonical), BaselineSnapshot: filepath.ToSlash(strings.TrimPrefix(baselinePath, config.MetaRoot+string(filepath.Separator))), InstalledAt: stamp, UpdatedAt: stamp}
	} else if metadataErr != nil {
		return InstructionResult{}, metadataErr
	}
	baselineData, err := readInstructionFile(baselinePath)
	if err != nil {
		return InstructionResult{}, fmt.Errorf("read instruction baseline %s: %w", spec.Name, err)
	}
	baselineHash := instructionHash(baselineData)
	if baselineHash != metadata.BaselineHash {
		return InstructionResult{}, fmt.Errorf("instruction baseline hash mismatch: %s", spec.Name)
	}
	currentHash, sourceHash := instructionHash(currentCanonical), instructionHash(sourceCanonical)
	localChanged, upstreamChanged := currentHash != baselineHash, sourceHash != baselineHash
	action := "unchanged"
	if !spec.Workspace && !localChanged && upstreamChanged && metadata.Ownership == "managed" {
		writeData := sourceCanonical
		if spec.Soul {
			writeData, err = renderOutputLanguageBlock(writeData, config.OutputLanguage)
			if err != nil {
				return InstructionResult{}, err
			}
		}
		if err := AtomicWriteFile(spec.Target, writeData, 0o600); err != nil {
			return InstructionResult{}, err
		}
		if err := ensureRuntimeOwner(spec.Target, config.RuntimeUID, config.RuntimeGID, 0o600); err != nil {
			return InstructionResult{}, err
		}
		if err := AtomicWriteFile(baselinePath, sourceCanonical, 0o600); err != nil {
			return InstructionResult{}, err
		}
		_ = os.Remove(availablePath)
		currentCanonical, currentHash, baselineHash = sourceCanonical, sourceHash, sourceHash
		localChanged, upstreamChanged, action = false, false, "updated"
		metadata.AvailableHash, metadata.AvailableSnapshot = "", ""
	} else if upstreamChanged {
		if err := AtomicWriteFile(availablePath, sourceCanonical, 0o600); err != nil {
			return InstructionResult{}, err
		}
		metadata.AvailableHash = sourceHash
		metadata.AvailableSnapshot = filepath.ToSlash(strings.TrimPrefix(availablePath, config.MetaRoot+string(filepath.Separator)))
		action = "review_required"
	} else {
		_ = os.Remove(availablePath)
		metadata.AvailableHash, metadata.AvailableSnapshot = "", ""
	}
	if spec.Soul {
		rendered, renderErr := renderOutputLanguageBlock(currentCanonical, config.OutputLanguage)
		if renderErr != nil {
			return InstructionResult{}, renderErr
		}
		if string(rendered) != string(currentData) {
			if err := AtomicWriteFile(spec.Target, rendered, 0o600); err != nil {
				return InstructionResult{}, err
			}
			if err := ensureRuntimeOwner(spec.Target, config.RuntimeUID, config.RuntimeGID, 0o600); err != nil {
				return InstructionResult{}, err
			}
		}
	}
	if err := ensureRuntimeOwner(spec.Target, config.RuntimeUID, config.RuntimeGID, 0o600); err != nil {
		return InstructionResult{}, err
	}
	if localChanged {
		metadata.Ownership = "customized"
	}
	metadata.Distribution, metadata.SourceID, metadata.BaselineHash, metadata.CurrentHash, metadata.UpdatedAt = version, sourceID, baselineHash, currentHash, stamp
	if err := writeInstructionMetadata(metadataPath, metadata); err != nil {
		return InstructionResult{}, err
	}
	if err := ensureRuntimeOwner(spec.Target, config.RuntimeUID, config.RuntimeGID, 0o600); err != nil {
		return InstructionResult{}, err
	}
	return instructionResult(spec, metadata, currentHash, baselineHash, sourceHash, localChanged, upstreamChanged, action), nil
}

func instructionResult(spec instructionSpec, metadata InstructionMetadata, currentHash, baselineHash, sourceHash string, localChanged, upstreamChanged bool, action string) InstructionResult {
	state := "current"
	if localChanged && upstreamChanged {
		state = "merge_required"
	} else if upstreamChanged {
		state = "update_available"
	} else if localChanged {
		state = "customized"
	}
	return InstructionResult{Name: spec.Name, State: state, Ownership: metadata.Ownership, Action: action, CurrentHash: currentHash, BaselineHash: baselineHash, AvailableHash: sourceHash, UpdateAvailable: upstreamChanged, Customized: localChanged || metadata.Ownership == "customized", Distribution: metadata.Distribution, SourceID: metadata.SourceID}
}

func instructionStatusEnvelope(action string, results []InstructionResult) InstructionStatusResult {
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	attention := false
	for _, result := range results {
		attention = attention || result.UpdateAvailable
	}
	return InstructionStatusResult{Schema: 1, OK: true, Action: action, AttentionRequired: attention, Instructions: results}
}

func InstructionStatus(config Config, selected string) (InstructionStatusResult, error) {
	results := make([]InstructionResult, 0, 3)
	for _, spec := range instructionSpecs(config) {
		if selected != "" && selected != spec.Name {
			continue
		}
		result, err := inspectInstruction(config, spec)
		if err != nil {
			return InstructionStatusResult{}, err
		}
		results = append(results, result)
	}
	if selected != "" && len(results) == 0 {
		return InstructionStatusResult{}, fmt.Errorf("unknown instruction %q", selected)
	}
	return instructionStatusEnvelope("status", results), nil
}

func inspectInstruction(config Config, spec instructionSpec) (InstructionResult, error) {
	current, err := readInstructionFile(spec.Target)
	if err != nil {
		return InstructionResult{}, err
	}
	source, err := readInstructionFile(spec.Source)
	if err != nil {
		return InstructionResult{}, err
	}
	metadataPath, baselinePath, _ := instructionPaths(config, spec.Name)
	metadata, err := readInstructionMetadata(metadataPath, spec.Name)
	if err != nil {
		return InstructionResult{}, err
	}
	baseline, err := readInstructionFile(baselinePath)
	if err != nil {
		return InstructionResult{}, err
	}
	currentHash := instructionHash(canonicalInstruction(spec, current))
	baselineHash := instructionHash(baseline)
	sourceHash := instructionHash(canonicalInstruction(spec, source))
	if baselineHash != metadata.BaselineHash {
		return InstructionResult{}, fmt.Errorf("instruction baseline hash mismatch: %s", spec.Name)
	}
	return instructionResult(spec, metadata, currentHash, baselineHash, sourceHash, currentHash != baselineHash, sourceHash != baselineHash, "none"), nil
}

func InstructionDiff(config Config, name string) (InstructionDiffResult, error) {
	spec, err := instructionSpecByName(config, name)
	if err != nil {
		return InstructionDiffResult{}, err
	}
	status, err := inspectInstruction(config, spec)
	if err != nil {
		return InstructionDiffResult{}, err
	}
	_, baselinePath, availablePath := instructionPaths(config, spec.Name)
	baseline, err := readInstructionFile(baselinePath)
	if err != nil {
		return InstructionDiffResult{}, err
	}
	current, err := readInstructionFile(spec.Target)
	if err != nil {
		return InstructionDiffResult{}, err
	}
	current = canonicalInstruction(spec, current)
	available, err := readInstructionFile(availablePath)
	if errors.Is(err, os.ErrNotExist) {
		available, err = readInstructionFile(spec.Source)
		available = canonicalInstruction(spec, available)
	}
	if err != nil {
		return InstructionDiffResult{}, err
	}
	local := simpleInstructionDiff("baseline", baseline, "local", current)
	upstream := simpleInstructionDiff("baseline", baseline, "upstream", available)
	return InstructionDiffResult{Schema: 1, OK: true, Action: "diff", Name: name, State: status.State, Local: local, Upstream: upstream, ThreeWay: local + upstream}, nil
}

func simpleInstructionDiff(fromName string, from []byte, toName string, to []byte) string {
	if string(from) == string(to) {
		return ""
	}
	fromLines := strings.Split(strings.TrimSuffix(string(from), "\n"), "\n")
	toLines := strings.Split(strings.TrimSuffix(string(to), "\n"), "\n")
	lcs := make([][]int, len(fromLines)+1)
	for index := range lcs {
		lcs[index] = make([]int, len(toLines)+1)
	}
	for left := len(fromLines) - 1; left >= 0; left-- {
		for right := len(toLines) - 1; right >= 0; right-- {
			if fromLines[left] == toLines[right] {
				lcs[left][right] = lcs[left+1][right+1] + 1
			} else if lcs[left+1][right] >= lcs[left][right+1] {
				lcs[left][right] = lcs[left+1][right]
			} else {
				lcs[left][right] = lcs[left][right+1]
			}
		}
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "--- %s\n+++ %s\n", fromName, toName)
	left, right := 0, 0
	for left < len(fromLines) || right < len(toLines) {
		switch {
		case left < len(fromLines) && right < len(toLines) && fromLines[left] == toLines[right]:
			fmt.Fprintf(&builder, " %s\n", fromLines[left])
			left++
			right++
		case right >= len(toLines) || left < len(fromLines) && lcs[left+1][right] >= lcs[left][right+1]:
			fmt.Fprintf(&builder, "-%s\n", fromLines[left])
			left++
		default:
			fmt.Fprintf(&builder, "+%s\n", toLines[right])
			right++
		}
	}
	return builder.String()
}

func MutateInstruction(config Config, name, action string, request InstructionMutationRequest, now time.Time) (InstructionMutationResult, error) {
	if request.Schema != 1 {
		return InstructionMutationResult{}, fmt.Errorf("invalid instruction mutation request")
	}
	spec, err := instructionSpecByName(config, name)
	if err != nil {
		return InstructionMutationResult{}, err
	}
	status, err := inspectInstruction(config, spec)
	if err != nil {
		return InstructionMutationResult{}, err
	}
	if request.ExpectedCurrentHash != status.CurrentHash || request.ExpectedAvailableHash != status.AvailableHash {
		return InstructionMutationResult{}, fmt.Errorf("instruction changed since review: %s", name)
	}
	metadataPath, baselinePath, availablePath := instructionPaths(config, name)
	metadata, err := readInstructionMetadata(metadataPath, name)
	if err != nil {
		return InstructionMutationResult{}, err
	}
	available, err := readInstructionFile(availablePath)
	if errors.Is(err, os.ErrNotExist) {
		available, err = readInstructionFile(spec.Source)
		available = canonicalInstruction(spec, available)
	}
	if err != nil {
		return InstructionMutationResult{}, err
	}
	current, err := readInstructionFile(spec.Target)
	if err != nil {
		return InstructionMutationResult{}, err
	}
	current = canonicalInstruction(spec, current)
	resultData := current
	ownership := "customized"
	switch action {
	case "keep":
	case "reset":
		resultData, ownership = available, "managed"
		if spec.Workspace {
			ownership = "customized"
		}
	case "merge":
		baseline, readErr := readInstructionFile(baselinePath)
		if readErr != nil {
			return InstructionMutationResult{}, readErr
		}
		resultData, err = mergeInstructionLines(baseline, current, available)
		if err != nil {
			return InstructionMutationResult{Schema: 1, OK: false, Action: action, Name: name, State: "conflict", Conflicts: 1}, err
		}
	default:
		return InstructionMutationResult{}, fmt.Errorf("unsupported instruction action %q", action)
	}
	targetRelative, targetErr := filepath.Rel(config.RuntimeRoot, spec.Target)
	metadataRelative, metadataErr := filepath.Rel(config.RuntimeRoot, metadataPath)
	baselineRelative, baselineErr := filepath.Rel(config.RuntimeRoot, baselinePath)
	if targetErr != nil || metadataErr != nil || baselineErr != nil {
		return InstructionMutationResult{}, fmt.Errorf("instruction paths are outside the runtime")
	}
	backup, err := CreateRollback(config, "instructions-"+action, now, filepath.ToSlash(targetRelative), filepath.ToSlash(metadataRelative), filepath.ToSlash(baselineRelative))
	if err != nil {
		return InstructionMutationResult{}, err
	}
	writeData := resultData
	if spec.Soul {
		writeData, err = renderOutputLanguageBlock(writeData, config.OutputLanguage)
		if err != nil {
			return InstructionMutationResult{}, err
		}
	}
	if action != "keep" {
		if err := AtomicWriteFile(spec.Target, writeData, 0o600); err != nil {
			return InstructionMutationResult{}, err
		}
		if err := ensureRuntimeOwner(spec.Target, config.RuntimeUID, config.RuntimeGID, 0o600); err != nil {
			return InstructionMutationResult{}, err
		}
	}
	if err := AtomicWriteFile(baselinePath, available, 0o600); err != nil {
		return InstructionMutationResult{}, err
	}
	_ = os.Remove(availablePath)
	metadata.Ownership = ownership
	metadata.BaselineHash = instructionHash(available)
	metadata.CurrentHash = instructionHash(resultData)
	metadata.AvailableHash, metadata.AvailableSnapshot = "", ""
	metadata.UpdatedAt = utcTimestamp(now)
	if err := writeInstructionMetadata(metadataPath, metadata); err != nil {
		return InstructionMutationResult{}, err
	}
	state := "customized"
	if metadata.CurrentHash == metadata.BaselineHash {
		state = "current"
	}
	return InstructionMutationResult{Schema: 1, OK: true, Action: action, Name: name, State: state, ResultHash: metadata.CurrentHash, Backup: backup.Archive}, nil
}

// mergeInstructionLines conservatively merges line changes. Independent full
// sections merge automatically; overlapping edits require explicit review.
func mergeInstructionLines(base, local, upstream []byte) ([]byte, error) {
	if string(local) == string(base) {
		return upstream, nil
	}
	if string(upstream) == string(base) || string(local) == string(upstream) {
		return local, nil
	}
	baseLines := strings.Split(string(base), "\n")
	localLines := strings.Split(string(local), "\n")
	upstreamLines := strings.Split(string(upstream), "\n")
	if len(baseLines) == len(localLines) && len(baseLines) == len(upstreamLines) {
		merged := make([]string, len(baseLines))
		for index := range baseLines {
			switch {
			case localLines[index] == baseLines[index]:
				merged[index] = upstreamLines[index]
			case upstreamLines[index] == baseLines[index], localLines[index] == upstreamLines[index]:
				merged[index] = localLines[index]
			default:
				return nil, fmt.Errorf("instruction merge has overlapping changes")
			}
		}
		return []byte(strings.Join(merged, "\n")), nil
	}
	return nil, fmt.Errorf("instruction merge requires manual review for structural changes")
}
