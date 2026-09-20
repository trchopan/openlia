package operator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const skillPatchFormat = "openlia-filepatch-v1"

// SkillPatch is the internal, deterministic representation of the user's
// customization relative to an upstream skill base.
type SkillPatch struct {
	Schema     int                   `json:"schema"`
	Format     string                `json:"format"`
	Skill      string                `json:"skill"`
	BaseHash   string                `json:"base_hash"`
	Operations []SkillPatchOperation `json:"operations"`
}

type SkillPatchOperation struct {
	Path     string `json:"path"`
	Action   string `json:"action"`
	BaseHash string `json:"base_hash,omitempty"`
	NewHash  string `json:"new_hash,omitempty"`
	Content  string `json:"content,omitempty"`
}

func GenerateSkillPatch(base, current, skill string) (SkillPatch, error) {
	baseHash, err := DirectorySHA256(base)
	if err != nil {
		return SkillPatch{}, err
	}
	currentHash, err := DirectorySHA256(current)
	if err != nil {
		return SkillPatch{}, err
	}
	baseFiles, err := readSkillFiles(base)
	if err != nil {
		return SkillPatch{}, err
	}
	currentFiles, err := readSkillFiles(current)
	if err != nil {
		return SkillPatch{}, err
	}

	paths := make([]string, 0, len(baseFiles)+len(currentFiles))
	seen := make(map[string]bool)
	for path := range baseFiles {
		seen[path] = true
		paths = append(paths, path)
	}
	for path := range currentFiles {
		if !seen[path] {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	operations := make([]SkillPatchOperation, 0)
	for _, path := range paths {
		baseContent, inBase := baseFiles[path]
		currentContent, inCurrent := currentFiles[path]
		switch {
		case inBase && !inCurrent:
			operations = append(operations, SkillPatchOperation{Path: path, Action: "delete", BaseHash: bytesHash(baseContent)})
		case !inBase && inCurrent:
			operations = append(operations, SkillPatchOperation{Path: path, Action: "add", NewHash: bytesHash(currentContent), Content: string(currentContent)})
		case inBase && !bytesEqual(baseContent, currentContent):
			operations = append(operations, SkillPatchOperation{Path: path, Action: "modify", BaseHash: bytesHash(baseContent), NewHash: bytesHash(currentContent), Content: string(currentContent)})
		}
	}
	patch := SkillPatch{Schema: 1, Format: skillPatchFormat, Skill: skill, BaseHash: "sha256:" + baseHash, Operations: operations}
	if currentHash == baseHash {
		patch.Operations = []SkillPatchOperation{}
	} else if len(patch.Operations) == 0 {
		return SkillPatch{}, fmt.Errorf("skill change cannot be represented by a file patch")
	}
	return patch, nil
}

func ApplySkillPatch(base, destination string, patch SkillPatch) error {
	if err := validateSkillPatch(patch); err != nil {
		return err
	}
	baseHash, err := DirectorySHA256(base)
	if err != nil {
		return err
	}
	if "sha256:"+baseHash != patch.BaseHash {
		return fmt.Errorf("skill patch base hash mismatch")
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("patch destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := CopyDir(base, destination); err != nil {
		return err
	}
	for _, operation := range patch.Operations {
		path := filepath.Join(destination, filepath.FromSlash(operation.Path))
		switch operation.Action {
		case "add":
			if bytesHash([]byte(operation.Content)) != operation.NewHash {
				return fmt.Errorf("patch add content hash mismatch: %s", operation.Path)
			}
			if _, err := os.Lstat(path); err == nil {
				return fmt.Errorf("patch add target already exists: %s", operation.Path)
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err := writePatchedFile(path, []byte(operation.Content)); err != nil {
				return err
			}
		case "modify":
			if bytesHash([]byte(operation.Content)) != operation.NewHash {
				return fmt.Errorf("patch modify content hash mismatch: %s", operation.Path)
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read patch target %s: %w", operation.Path, err)
			}
			if bytesHash(contents) != operation.BaseHash {
				return fmt.Errorf("patch target hash mismatch: %s", operation.Path)
			}
			if err := writePatchedFile(path, []byte(operation.Content)); err != nil {
				return err
			}
		case "delete":
			contents, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read patch target %s: %w", operation.Path, err)
			}
			if bytesHash(contents) != operation.BaseHash {
				return fmt.Errorf("patch target hash mismatch: %s", operation.Path)
			}
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("delete patch target %s: %w", operation.Path, err)
			}
		default:
			return fmt.Errorf("unsupported skill patch action %q", operation.Action)
		}
	}
	return normalizeSkill(destination)
}

func replaceDirectory(source, destination string) error {
	if _, err := os.Lstat(destination); errors.Is(err, os.ErrNotExist) {
		return CopyDir(source, destination)
	} else if err != nil {
		return err
	}
	return AtomicCopyDir(source, destination)
}

func ReadSkillPatch(path string) (SkillPatch, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillPatch{}, err
	}
	var patch SkillPatch
	if err := json.Unmarshal(data, &patch); err != nil {
		return SkillPatch{}, fmt.Errorf("parse skill patch: %w", err)
	}
	if err := validateSkillPatch(patch); err != nil {
		return SkillPatch{}, err
	}
	return patch, nil
}

func WriteSkillPatch(path string, patch SkillPatch) (string, error) {
	if err := validateSkillPatch(patch); err != nil {
		return "", err
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if err := EnsureDir(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := AtomicWriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return bytesHash(data), nil
}

func SkillPatchHash(patch SkillPatch) (string, error) {
	if err := validateSkillPatch(patch); err != nil {
		return "", err
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return "", err
	}
	return bytesHash(append(data, '\n')), nil
}

func validateSkillPatch(patch SkillPatch) error {
	if patch.Schema != 1 || patch.Format != skillPatchFormat {
		return fmt.Errorf("invalid skill patch format")
	}
	if err := ValidateSafeComponent(patch.Skill, "skill"); err != nil {
		return err
	}
	if !contentHashPattern.MatchString(patch.BaseHash) {
		return fmt.Errorf("invalid skill patch base hash")
	}
	previous := ""
	for _, operation := range patch.Operations {
		if err := validatePatchPath(operation.Path); err != nil {
			return err
		}
		if operation.Path <= previous {
			return fmt.Errorf("skill patch operations are not sorted or contain duplicates")
		}
		previous = operation.Path
		switch operation.Action {
		case "add":
			if operation.BaseHash != "" || !contentHashPattern.MatchString(operation.NewHash) || !utf8.ValidString(operation.Content) || bytesHash([]byte(operation.Content)) != operation.NewHash {
				return fmt.Errorf("invalid skill patch add operation: %s", operation.Path)
			}
		case "modify":
			if !contentHashPattern.MatchString(operation.BaseHash) || !contentHashPattern.MatchString(operation.NewHash) || !utf8.ValidString(operation.Content) || bytesHash([]byte(operation.Content)) != operation.NewHash {
				return fmt.Errorf("invalid skill patch modify operation: %s", operation.Path)
			}
		case "delete":
			if !contentHashPattern.MatchString(operation.BaseHash) || operation.NewHash != "" || operation.Content != "" {
				return fmt.Errorf("invalid skill patch delete operation: %s", operation.Path)
			}
		default:
			return fmt.Errorf("invalid skill patch action: %s", operation.Action)
		}
	}
	return nil
}

func readSkillFiles(root string) (map[string][]byte, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("stat skill directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("skill directory does not exist: %s", root)
	}
	files := make(map[string][]byte)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." || ignoredSkillArtifact(relative) {
			if entry.IsDir() && relative != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("skill contains unsupported entry: %s", filepath.ToSlash(relative))
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = contents
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read skill files: %w", err)
	}
	return files, nil
}

func writePatchedFile(path string, contents []byte) error {
	if !utf8.Valid(contents) {
		return fmt.Errorf("skill patch contains non-UTF-8 content")
	}
	if err := EnsureDir(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, contents, 0o644)
}

func validatePatchPath(path string) error {
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\\\r\n\t") {
		return fmt.Errorf("invalid skill patch path")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean != path || clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return fmt.Errorf("invalid skill patch path: %s", path)
	}
	return nil
}

func ignoredSkillArtifact(relative string) bool {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for _, part := range parts {
		if part == "__pycache__" {
			return true
		}
	}
	return strings.HasSuffix(relative, ".pyc") || strings.HasSuffix(relative, ".pyo")
}

func bytesHash(contents []byte) string {
	digest := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
