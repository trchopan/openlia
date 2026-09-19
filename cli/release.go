package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

type releaseFile struct {
	Name string
	Mode int64
	Data []byte
}

func collectReleaseFiles(assets fs.FS) ([]releaseFile, error) {
	files := make([]releaseFile, 0)
	err := fs.WalkDir(assets, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if name == ".git" || strings.HasPrefix(name, ".git/") || strings.Contains(name, "/__pycache__/") || strings.HasSuffix(name, ".pyc") || strings.HasSuffix(name, ".pyo") {
			return nil
		}
		data, err := fs.ReadFile(assets, name)
		if err != nil {
			return err
		}
		mode := int64(0o600)
		if strings.HasSuffix(name, ".sh") || strings.HasSuffix(name, ".py") {
			mode = 0o700
		}
		files = append(files, releaseFile{Name: path.Clean(name), Mode: mode, Data: data})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Name < files[right].Name })
	return files, nil
}

func releaseArchive(assets fs.FS) ([]byte, string, error) {
	files, err := collectReleaseFiles(assets)
	if err != nil {
		return nil, "", fmt.Errorf("collect release assets: %w", err)
	}
	var buffer bytes.Buffer
	zipper := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipper)
	for _, file := range files {
		header := &tar.Header{
			Name:    file.Name,
			Mode:    file.Mode,
			Size:    int64(len(file.Data)),
			ModTime: time.Unix(0, 0).UTC(),
		}
		if err := writer.WriteHeader(header); err != nil {
			return nil, "", err
		}
		if _, err := writer.Write(file.Data); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	if err := zipper.Close(); err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(buffer.Bytes())
	return buffer.Bytes(), hex.EncodeToString(digest[:]), nil
}

func releaseSummary(assets fs.FS) (map[string]any, error) {
	files, err := collectReleaseFiles(assets)
	if err != nil {
		return nil, err
	}
	archive, digest, err := releaseArchive(assets)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"schema":         1,
		"version":        defaultVersion,
		"file_count":     len(files),
		"archive_bytes":  len(archive),
		"archive_sha256": digest,
		"hermes_tag":     "v2026.9.14",
		"locho_version":  "1.2.0-beta.1",
	}, nil
}
