package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
)

func TestDecryptBackupArchiveUsesOperatorIdentityAndVerifiesCiphertext(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	identityPath := filepath.Join(directory, "backup-identity.txt")
	if err := os.WriteFile(identityPath, []byte(identity.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plain := validDurableArchive(t)
	encryptedPath := filepath.Join(directory, "openlia-test.tar.gz.age")
	output, err := os.Create(encryptedPath)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := age.Encrypt(output, identity.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mustReadFile(t, encryptedPath))
	metadata, _ := json.Marshal(map[string]any{"encrypted": true, "sha256": hex.EncodeToString(digest[:])})
	if err := os.WriteFile(encryptedPath+".json", append(metadata, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	config := defaultConfig()
	config.BackupIdentityFile = identityPath
	config.BackupRecipient = identity.Recipient().String()
	decrypted, err := decryptBackupArchive(config, encryptedPath, directory)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(decrypted)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("decrypted archive differs from source")
	}
	if err := os.WriteFile(encryptedPath, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := decryptBackupArchive(config, encryptedPath, t.TempDir()); err == nil {
		t.Fatal("tampered ciphertext passed checksum verification")
	}
}

func TestEnsureBackupIdentityCreatesOperatorOnlyIdentity(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("OPENLIA_CONFIG", configFile)
	config := defaultConfig()
	if err := ensureBackupIdentity(&config); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(config.BackupIdentityFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("identity file mode = %o, want 600", info.Mode().Perm())
	}
	if _, err := age.ParseX25519Recipient(config.BackupRecipient); err != nil {
		t.Fatalf("generated recipient is invalid: %v", err)
	}
	if err := validateBackupIdentity(config.BackupIdentityFile, config.BackupRecipient); err != nil {
		t.Fatal(err)
	}
}

func TestDecryptBackupArchiveRequiresMetadata(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	identityPath := filepath.Join(directory, "backup-identity.txt")
	if err := os.WriteFile(identityPath, []byte(identity.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(directory, "openlia-test.tar.gz.age")
	output, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := age.Encrypt(output, identity.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(validDurableArchive(t)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	config := defaultConfig()
	config.BackupIdentityFile = identityPath
	if _, err := decryptBackupArchive(config, archivePath, t.TempDir()); err == nil {
		t.Fatal("encrypted archive without metadata was accepted")
	}
}

func TestBackupRestoreSelectionParsesRemoteAndLocalForms(t *testing.T) {
	tests := []struct {
		args               []string
		archive, dest, obj string
		wantErr            bool
	}{
		{args: []string{"/backups/openlia.tar.gz.age"}, archive: "/backups/openlia.tar.gz.age"},
		{args: []string{"--from", "primary", "--latest"}, dest: "primary"},
		{args: []string{"--from", "nas", "--object", "openlia-1.tar.gz.age"}, dest: "nas", obj: "openlia-1.tar.gz.age"},
		{args: []string{"--from", "primary"}, wantErr: true},
		{args: []string{"--from", "primary", "--latest", "--object", "other"}, wantErr: true},
	}
	for _, test := range tests {
		archive, destination, object, err := backupRestoreSelection(test.args)
		if (err != nil) != test.wantErr || archive != test.archive || destination != test.dest || object != test.obj {
			t.Fatalf("backupRestoreSelection(%v) = %q,%q,%q,%v", test.args, archive, destination, object, err)
		}
	}
}

func validDurableArchive(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gz)
	manifest := []byte(`{"schema":2,"kind":"durable","reason":"test","created_at":"2026-10-01T04:20:00Z","secret_values":"excluded","capabilities":"excluded","components":["hermes","meta"],"exclusions":[]}`)
	entries := []*tar.Header{
		{Name: "manifest.json", Mode: 0o600, Size: int64(len(manifest)), Typeflag: tar.TypeReg},
		{Name: "hermes/", Mode: 0o700, Typeflag: tar.TypeDir},
		{Name: "meta/", Mode: 0o700, Typeflag: tar.TypeDir},
	}
	for index, header := range entries {
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			if _, err := tarWriter.Write(manifest); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
