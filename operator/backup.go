package operator

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type BackupResult struct {
	OK      bool   `json:"ok"`
	Archive string `json:"archive"`
	Secrets string `json:"secrets,omitempty"`
	Action  string `json:"action,omitempty"`
	State   string `json:"state,omitempty"`
}

type backupManifest struct {
	Schema       int    `json:"schema"`
	Reason       string `json:"reason"`
	CreatedAt    string `json:"created_at"`
	SecretValues string `json:"secret_values"`
	Capabilities string `json:"capabilities"`
}

type backupMetadata struct {
	Schema    int    `json:"schema"`
	Archive   string `json:"archive"`
	SHA256    string `json:"sha256"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"created_at"`
	Secrets   string `json:"secrets"`
}

func CreateBackup(config Config, reason string, now time.Time, composers ...Compose) (BackupResult, error) {
	var compose *Compose
	if len(composers) > 0 {
		compose = &composers[0]
	}
	return createBackup(context.Background(), config, reason, now, compose)
}

func createBackup(ctx context.Context, config Config, reason string, now time.Time, compose *Compose) (result BackupResult, err error) {
	if err := config.ValidatePaths(); err != nil {
		return BackupResult{}, err
	}
	if err := ValidateSafeComponent(reason, "backup-reason"); err != nil {
		return BackupResult{}, err
	}
	if info, err := os.Lstat(config.DataRoot); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return BackupResult{}, fmt.Errorf("Hermes data directory is missing")
	}
	if err := EnsureDir(config.BackupRoot, 0o700); err != nil {
		return BackupResult{}, err
	}
	if err := EnsureDir(config.MetaRoot, 0o700); err != nil {
		return BackupResult{}, err
	}
	if err := EnsureDir(config.LochoRoot, 0o700); err != nil {
		return BackupResult{}, err
	}
	for _, item := range []struct {
		path  string
		label string
	}{
		{config.MetaRoot, "metadata directory"},
		{config.LochoRoot, "Locho directory"},
	} {
		info, statErr := os.Lstat(item.path)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return BackupResult{}, fmt.Errorf("%s is not a directory", item.label)
		}
	}
	wasRunning := false
	if compose != nil && compose.ServiceRunning(ctx, "hermes") {
		if _, err := compose.Run(ctx, "stop", "hermes"); err != nil {
			return BackupResult{}, fmt.Errorf("cannot stop Hermes for a consistent backup")
		}
		wasRunning = true
	}
	defer func() {
		if !wasRunning {
			return
		}
		if _, restartErr := compose.Run(ctx, "up", "-d", "--no-deps", "hermes"); restartErr != nil && err == nil {
			err = fmt.Errorf("backup completed but Hermes could not be restarted")
		}
	}()
	stamp := now.UTC().Format("20060102T150405Z")
	temporary, err := os.CreateTemp(config.BackupRoot, ".archive-*.tar.gz")
	if err != nil {
		return BackupResult{}, err
	}
	temporaryName := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryName)
		}
	}()
	compressor := gzip.NewWriter(temporary)
	archive := tar.NewWriter(compressor)
	manifest := backupManifest{Schema: 1, Reason: reason, CreatedAt: utcTimestamp(now), SecretValues: "excluded", Capabilities: "excluded"}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		_ = temporary.Close()
		return BackupResult{}, err
	}
	for _, root := range []string{"hermes", "meta", "locho"} {
		path := filepath.Join(config.RuntimeRoot, root)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := appendArchiveTree(archive, config.RuntimeRoot, path); err != nil {
			_ = archive.Close()
			_ = compressor.Close()
			_ = temporary.Close()
			return BackupResult{}, err
		}
	}
	if err := writeTarFile(archive, "manifest.json", manifestData, 0o600); err != nil {
		_ = archive.Close()
		_ = compressor.Close()
		_ = temporary.Close()
		return BackupResult{}, err
	}
	if err := archive.Close(); err != nil {
		_ = compressor.Close()
		_ = temporary.Close()
		return BackupResult{}, err
	}
	if err := compressor.Close(); err != nil {
		_ = temporary.Close()
		return BackupResult{}, err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return BackupResult{}, err
	}
	if err := temporary.Close(); err != nil {
		return BackupResult{}, err
	}
	if err := validateRestoreArchive(temporaryName); err != nil {
		return BackupResult{}, fmt.Errorf("backup archive validation failed")
	}
	digest, err := fileSHA256(temporaryName)
	if err != nil {
		return BackupResult{}, err
	}
	destination := filepath.Join(config.BackupRoot, fmt.Sprintf("openlia-%s-%d.tar.gz", stamp, os.Getpid()))
	for suffix := 1; ; suffix++ {
		if _, statErr := os.Lstat(destination); errors.Is(statErr, os.ErrNotExist) {
			break
		} else if statErr != nil {
			return BackupResult{}, statErr
		}
		destination = filepath.Join(config.BackupRoot, fmt.Sprintf("openlia-%s-%d-%d.tar.gz", stamp, os.Getpid(), suffix))
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return BackupResult{}, err
	}
	removeTemporary = false
	if err := os.Chmod(destination, 0o600); err != nil {
		return BackupResult{}, err
	}
	metadata := backupMetadata{Schema: 1, Archive: destination, SHA256: digest, Reason: reason, CreatedAt: utcTimestamp(now), Secrets: "excluded"}
	metadataData, err := json.Marshal(metadata)
	if err != nil {
		return BackupResult{}, err
	}
	if err := AtomicWriteFile(destination+".json", append(metadataData, '\n'), 0o600); err != nil {
		return BackupResult{}, err
	}
	return BackupResult{OK: true, Archive: destination, Secrets: "excluded"}, nil
}

func RestoreBackup(config Config, archivePath string, now time.Time, composers ...Compose) (BackupResult, error) {
	var compose *Compose
	if len(composers) > 0 {
		compose = &composers[0]
	}
	return restoreBackup(context.Background(), config, archivePath, now, compose)
}

func restoreBackup(ctx context.Context, config Config, archivePath string, now time.Time, compose *Compose) (BackupResult, error) {
	if err := config.ValidatePaths(); err != nil {
		return BackupResult{}, err
	}
	if archivePath == "" {
		entries, err := os.ReadDir(config.BackupRoot)
		if err != nil {
			return BackupResult{}, err
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), "openlia-") && strings.HasSuffix(entry.Name(), ".tar.gz") {
				archivePath = filepath.Join(config.BackupRoot, entry.Name())
			}
		}
	}
	if archivePath == "" {
		return BackupResult{}, fmt.Errorf("restore archive must be inside the backup directory")
	}
	if err := ValidateAbsolutePath(archivePath, "backup-archive"); err != nil {
		return BackupResult{}, err
	}
	if !within(archivePath, config.BackupRoot) {
		return BackupResult{}, fmt.Errorf("restore archive must be inside the backup directory")
	}
	if info, err := os.Stat(archivePath); err != nil || !info.Mode().IsRegular() {
		return BackupResult{}, fmt.Errorf("restore archive does not exist")
	}
	if info, err := os.Lstat(archivePath); err != nil || info.Mode()&os.ModeSymlink != 0 {
		return BackupResult{}, fmt.Errorf("restore archive must be a regular file")
	}
	if err := validateRestoreArchive(archivePath); err != nil {
		return BackupResult{}, err
	}
	if err := EnsureDir(config.DataRoot, 0o700); err != nil {
		return BackupResult{}, err
	}
	if info, err := os.Lstat(config.DataRoot); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return BackupResult{}, fmt.Errorf("Hermes data directory is missing")
	}
	if err := EnsureDir(config.MetaRoot, 0o700); err != nil {
		return BackupResult{}, err
	}
	preRestore, err := createBackup(ctx, config, "restore-preflight", now, compose)
	if err != nil {
		return BackupResult{}, err
	}
	if compose != nil {
		if _, statErr := os.Stat(config.ComposeFile); statErr == nil {
			_, _ = compose.Run(ctx, "stop", "hermes")
		}
	}
	if err := WriteState(config, stateStopped); err != nil {
		return BackupResult{}, err
	}
	parent := filepath.Dir(config.DataRoot)
	staging, err := os.MkdirTemp(parent, ".restore-*")
	if err != nil {
		return BackupResult{}, err
	}
	defer os.RemoveAll(staging)
	if err := extractRestoreArchive(archivePath, staging); err != nil {
		_ = RecordChange(config, "restore", "failed", preRestore.Archive, "restore extraction failed", now)
		return BackupResult{}, err
	}
	hermesInfo, hermesErr := os.Stat(filepath.Join(staging, "hermes"))
	if hermesErr != nil || !hermesInfo.IsDir() {
		_ = RecordChange(config, "restore", "failed", preRestore.Archive, "restore extraction failed", now)
		return BackupResult{}, fmt.Errorf("restore extraction failed; current state was preserved")
	}
	oldData := filepath.Join(config.BackupRoot, fmt.Sprintf("pre-restore-hermes-%s-%d", now.UTC().Format("20060102T150405Z"), os.Getpid()))
	for suffix := 1; ; suffix++ {
		if _, statErr := os.Lstat(oldData); errors.Is(statErr, os.ErrNotExist) {
			break
		} else if statErr != nil {
			return BackupResult{}, statErr
		}
		oldData = filepath.Join(config.BackupRoot, fmt.Sprintf("pre-restore-hermes-%s-%d-%d", now.UTC().Format("20060102T150405Z"), os.Getpid(), suffix))
	}
	if err := os.Rename(config.DataRoot, oldData); err != nil {
		_ = RecordChange(config, "restore", "failed", preRestore.Archive, "current data move failed", now)
		return BackupResult{}, fmt.Errorf("current data move failed; restore was not applied")
	}
	if err := os.Rename(filepath.Join(staging, "hermes"), config.DataRoot); err != nil {
		_ = os.Rename(oldData, config.DataRoot)
		_ = RecordChange(config, "restore", "failed", preRestore.Archive, "restore replacement failed", now)
		return BackupResult{}, fmt.Errorf("restore replacement failed; current state was restored")
	}
	if err := EnsureDir(config.MetaRoot, 0o700); err != nil {
		return BackupResult{}, err
	}
	if err := RecordChange(config, "restore", "ok", preRestore.Archive, "archive="+archivePath+" old_data="+oldData, now); err != nil {
		return BackupResult{}, err
	}
	return BackupResult{OK: true, Action: "restore", Archive: archivePath, State: stateStopped, Secrets: "supplied_out_of_band"}, nil
}

func appendArchiveTree(archive *tar.Writer, root, path string) error {
	return filepath.WalkDir(path, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if excludedBackupPath(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			return archive.WriteHeader(&tar.Header{Name: relative + "/", Mode: int64(info.Mode().Perm()), Typeflag: tar.TypeDir})
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		input, err := os.Open(current)
		if err != nil {
			return err
		}
		if err := archive.WriteHeader(&tar.Header{Name: relative, Mode: int64(info.Mode().Perm()), Size: info.Size(), Typeflag: tar.TypeReg}); err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(archive, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func writeTarFile(archive *tar.Writer, name string, data []byte, mode os.FileMode) error {
	if err := archive.WriteHeader(&tar.Header{Name: name, Mode: int64(mode), Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := archive.Write(data)
	return err
}

func excludedBackupPath(path string) bool {
	parts := strings.Split(path, "/")
	if len(parts) == 3 && parts[0] == "locho" && parts[len(parts)-1] == "attachments.toml" {
		return true
	}
	for _, part := range parts {
		switch part {
		case "secrets", "pairing", "mcp-tokens", "browser-profile", "logs":
			return true
		}
	}
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".env") || strings.HasSuffix(base, ".sock") || strings.HasSuffix(base, ".secret") || strings.Contains(base, ".secret") || base == "auth.json"
}

func safeArchiveMember(name string) bool {
	return !strings.HasPrefix(name, "/") && !strings.ContainsAny(name, " \t\r\n") && name != ".." && !strings.HasPrefix(name, "../") && !strings.Contains(name, "/../") && filepath.ToSlash(filepath.Clean(filepath.FromSlash(name))) == name
}

func validateRestoreArchive(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	decompressor, err := gzip.NewReader(file)
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("restore archive is not a readable tar archive")
	}
	reader := tar.NewReader(decompressor)
	for {
		header, readErr := reader.Next()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = decompressor.Close()
			_ = file.Close()
			return fmt.Errorf("restore archive is not a readable tar archive")
		}
		name := strings.TrimSuffix(header.Name, "/")
		if name == "" || name == "manifest.json" {
			continue
		}
		if !safeArchiveMember(name) || !(name == "hermes" || name == "meta" || name == "locho" || strings.HasPrefix(name, "hermes/") || strings.HasPrefix(name, "meta/") || strings.HasPrefix(name, "locho/")) {
			_ = decompressor.Close()
			_ = file.Close()
			return fmt.Errorf("restore archive contains an unsupported or unsafe member")
		}
		if header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg || header.Size < 0 {
			_ = decompressor.Close()
			_ = file.Close()
			return fmt.Errorf("restore archive contains an unsupported or unsafe member")
		}
	}
	if err := decompressor.Close(); err != nil {
		_ = file.Close()
		return fmt.Errorf("restore archive is not a readable tar archive")
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("restore archive is not a readable tar archive")
	}
	return nil
}

func extractRestoreArchive(path, destination string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	decompressor, err := gzip.NewReader(file)
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("restore archive is not a readable tar archive")
	}
	reader := tar.NewReader(decompressor)
	defer decompressor.Close()
	defer file.Close()
	for {
		header, readErr := reader.Next()
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("restore archive is not a readable tar archive")
		}
		name := strings.TrimSuffix(header.Name, "/")
		if name == "" || name == "manifest.json" {
			continue
		}
		if !safeArchiveMember(name) || !(name == "hermes" || name == "meta" || name == "locho" || strings.HasPrefix(name, "hermes/") || strings.HasPrefix(name, "meta/") || strings.HasPrefix(name, "locho/")) || (header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg) || header.Size < 0 {
			return fmt.Errorf("restore archive contains an unsupported or unsafe member")
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		written, copyErr := io.CopyN(output, reader, header.Size)
		closeErr := output.Close()
		if copyErr != nil || closeErr != nil || written != header.Size {
			return fmt.Errorf("restore extraction failed")
		}
	}
}
