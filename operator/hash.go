package operator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DirectorySHA256 returns the stable hash used by profile provenance records.
// Paths are sorted by slash-separated relative name and Python cache artifacts
// are omitted to preserve the existing skill provenance hash contract.
func DirectorySHA256(root string) (string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return "", fmt.Errorf("stat directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("directory does not exist: %s", root)
	}

	paths := make([]string, 0)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		parts := strings.Split(relative, "/")
		for _, part := range parts {
			if part == "__pycache__" {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if strings.HasSuffix(relative, ".pyc") {
			return nil
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk directory: %w", err)
	}
	sort.Strings(paths)

	digest := sha256.New()
	for _, relative := range paths {
		path := filepath.Join(root, filepath.FromSlash(relative))
		entryInfo, err := os.Lstat(path)
		if err != nil {
			return "", fmt.Errorf("stat %s: %w", relative, err)
		}
		if _, err := io.WriteString(digest, relative); err != nil {
			return "", err
		}
		if _, err := digest.Write([]byte{0}); err != nil {
			return "", err
		}
		switch {
		case entryInfo.Mode()&os.ModeSymlink != 0:
			if _, err := io.WriteString(digest, "symlink\x00"); err != nil {
				return "", err
			}
			link, err := os.Readlink(path)
			if err != nil {
				return "", fmt.Errorf("read symlink %s: %w", relative, err)
			}
			if _, err := io.WriteString(digest, link); err != nil {
				return "", err
			}
		case entryInfo.Mode().IsRegular():
			if _, err := io.WriteString(digest, "file\x00"); err != nil {
				return "", err
			}
			file, err := os.Open(path)
			if err != nil {
				return "", fmt.Errorf("open %s: %w", relative, err)
			}
			_, copyErr := io.Copy(digest, file)
			closeErr := file.Close()
			if copyErr != nil {
				return "", fmt.Errorf("read %s: %w", relative, copyErr)
			}
			if closeErr != nil {
				return "", fmt.Errorf("close %s: %w", relative, closeErr)
			}
		default:
			if _, err := io.WriteString(digest, "directory\x00"); err != nil {
				return "", err
			}
		}
		if _, err := digest.Write([]byte{0}); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// DirectoryHash is the concise compatibility name for callers that do not
// need to spell out the digest algorithm.
func DirectoryHash(root string) (string, error) {
	return DirectorySHA256(root)
}
