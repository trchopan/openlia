package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"openlia/operator"
)

func decryptBackupArchive(config Config, encryptedPath, directory string) (string, error) {
	if config.BackupIdentityFile == "" {
		return "", fmt.Errorf("operator backup identity is not configured; run `openlia backup keygen`")
	}
	if err := verifyCipherMetadata(encryptedPath); err != nil {
		return "", err
	}
	info, err := os.Stat(config.BackupIdentityFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("operator backup identity must be a regular file protected with mode 0600")
	}
	keyData, err := os.Open(config.BackupIdentityFile)
	if err != nil {
		return "", err
	}
	identities, err := age.ParseIdentities(keyData)
	_ = keyData.Close()
	if err != nil || len(identities) == 0 {
		return "", fmt.Errorf("operator backup identity file is invalid")
	}
	encrypted, err := os.Open(encryptedPath)
	if err != nil {
		return "", err
	}
	defer encrypted.Close()
	plain, err := age.Decrypt(encrypted, identities...)
	if err != nil {
		return "", fmt.Errorf("decrypt backup archive: %w", err)
	}
	outputPath := filepath.Join(directory, "openlia-restore.tar.gz")
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(output, plain)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(outputPath)
		if copyErr != nil {
			return "", fmt.Errorf("decrypt backup archive: %w", copyErr)
		}
		if syncErr != nil {
			return "", syncErr
		}
		return "", closeErr
	}
	if err := operator.ValidateBackupArchive(outputPath); err != nil {
		_ = os.Remove(outputPath)
		return "", fmt.Errorf("decrypted backup archive validation failed: %w", err)
	}
	return outputPath, nil
}

func verifyCipherMetadata(archive string) error {
	metadataPath := archive + ".json"
	data, err := os.ReadFile(metadataPath)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("encrypted backup metadata is missing")
	}
	if err != nil {
		return fmt.Errorf("read backup metadata: %w", err)
	}
	var metadata struct {
		SHA256    string `json:"sha256"`
		Encrypted bool   `json:"encrypted"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil || metadata.SHA256 == "" || !metadata.Encrypted {
		return fmt.Errorf("encrypted backup metadata is invalid")
	}
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if hex.EncodeToString(hash.Sum(nil)) != metadata.SHA256 {
		return fmt.Errorf("encrypted backup checksum does not match metadata")
	}
	return nil
}

func verifyRollbackMetadata(archive string) error {
	data, err := os.ReadFile(archive + ".json")
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("rollback metadata is missing")
	}
	if err != nil {
		return err
	}
	var metadata struct {
		SHA256 string `json:"sha256"`
	}
	if json.Unmarshal(data, &metadata) != nil || metadata.SHA256 == "" {
		return fmt.Errorf("backup metadata is invalid")
	}
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if hex.EncodeToString(hash.Sum(nil)) != metadata.SHA256 {
		return fmt.Errorf("backup archive checksum does not match metadata")
	}
	return nil
}

func encryptedBackupPath(path string) bool {
	return strings.HasSuffix(path, ".tar.gz.age")
}
