package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCredentialsFile(t *testing.T, path string) {
	t.Helper()
	contents := "[reader]\naws_access_key_id = reader-key\naws_secret_access_key = reader-secret\n\n[writer]\naws_access_key_id = writer-key\naws_secret_access_key = writer-secret\naws_session_token = writer-token\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitS3CredentialsOverrideAmbientEnvironment(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "credentials")
	writeCredentialsFile(t, path)
	t.Setenv("AWS_ACCESS_KEY_ID", "ambient-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "ambient-secret")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	config, err := loadS3AWSConfig(context.Background(), "us-east-1", path, "reader", "reader")
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := config.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessKeyID != "reader-key" || credentials.SecretAccessKey != "reader-secret" {
		t.Fatalf("credentials = %#v, want reader profile", credentials)
	}
}

func TestSyncBackupWriterCredentialsDeploysOnlySelectedProfile(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "profile")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(configDir, "aws-credentials")
	writeCredentialsFile(t, source)
	t.Setenv("OPENLIA_CONFIG", filepath.Join(configDir, "config.toml"))
	config := defaultConfig()
	config.InstallRoot = filepath.Join(root, "install")
	config.BackupDestinations = []BackupDestinationConfig{{
		Name:                  "archive",
		Type:                  "s3",
		Bucket:                "backups",
		Region:                "us-east-1",
		WriterCredentialsFile: "aws-credentials",
		WriterAWSProfile:      "writer",
	}}
	if err := syncBackupWriterCredentials(context.Background(), Local{Config: config}, config); err != nil {
		t.Fatal(err)
	}
	deployed := writerCredentialTarget(config, "archive")
	data, err := os.ReadFile(deployed)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "[writer]") || strings.Contains(text, "[reader]") || strings.Contains(text, "reader-secret") || !strings.Contains(text, "writer-secret") {
		t.Fatalf("deployed credentials contain the wrong profiles: %s", text)
	}
	if info, err := os.Stat(deployed); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("deployed credentials mode = %v, want 0600", err)
	}
}

func TestResolveBackupCredentialPathUsesProfileDirectory(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	t.Setenv("OPENLIA_CONFIG", configPath)
	if got := resolveBackupCredentialPath("credentials/aws"); got != filepath.Join(root, "credentials", "aws") {
		t.Fatalf("resolved credential path = %q", got)
	}
}
