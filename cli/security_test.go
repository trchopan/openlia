package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRedactSecrets(t *testing.T) {
	input := "Authorization: Bearer abc.def\nOPENAI_API_KEY=sk-test-secret\nTOKEN=ghp_test-value\nOPENLIA_GIT_TOKEN=github_pat_test-value\ncapability=host:http:secret\npassword_hash=$argon2id$v=19$m=65536,t=3,p=1$hidden-salt$hidden-key\n$argon2id$v=19$m=65536,t=3,p=1$raw-salt$raw-key"
	output := redact(input)
	if strings.Contains(output, "abc.def") || strings.Contains(output, "sk-test-secret") || strings.Contains(output, "ghp_test-value") || strings.Contains(output, "github_pat_test-value") || strings.Contains(output, "host:http:secret") || strings.Contains(output, "hidden-salt") || strings.Contains(output, "hidden-key") || strings.Contains(output, "raw-salt") || strings.Contains(output, "raw-key") {
		t.Fatalf("secret survived redaction: %q", output)
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Fatalf("redaction marker missing: %q", output)
	}
}

func TestShellQuote(t *testing.T) {
	for _, value := range []string{"plain", "a b", "a'b", "$(touch /tmp/nope)"} {
		quoted := shellQuote(value)
		if !strings.HasPrefix(quoted, "'") || !strings.HasSuffix(quoted, "'") {
			t.Fatalf("not shell quoted: %q", quoted)
		}
		if value == "$(touch /tmp/nope)" && quoted != "'$(touch /tmp/nope)'" {
			t.Fatalf("unexpected command-substitution quoting: %q", quoted)
		}
	}
}

func TestReleaseArchiveContainsWorkspaceMarkers(t *testing.T) {
	assets := fstest.MapFS{
		"workspace-template/inbox/.gitkeep":                      &fstest.MapFile{Data: []byte{}},
		"profile/SOUL.md":                                        &fstest.MapFile{Data: []byte("safe")},
		"profile/system-skills/openlia-skill-migration/SKILL.md": &fstest.MapFile{Data: []byte("protected")},
	}
	archive, digest, err := releaseArchive(assets)
	if err != nil {
		t.Fatal(err)
	}
	if len(digest) != 64 || len(archive) == 0 {
		t.Fatalf("invalid archive metadata: %d bytes, digest %q", len(archive), digest)
	}
	zipper, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(zipper)
	seen := map[string]bool{}
	for {
		header, readErr := reader.Next()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
		seen[header.Name] = true
	}
	if !seen["workspace-template/inbox/.gitkeep"] {
		t.Fatal("workspace marker was omitted from embedded release")
	}
	if !seen["profile/system-skills/openlia-skill-migration/SKILL.md"] {
		t.Fatal("protected migration skill was omitted from release")
	}
}

func TestSetSkill(t *testing.T) {
	got := setSkill([]string{"a", "b"}, "a", false)
	if contains(got, "a") || !contains(got, "b") {
		t.Fatalf("disable result = %#v", got)
	}
	got = setSkill(got, "a", true)
	if !contains(got, "a") || len(got) != 2 {
		t.Fatalf("enable result = %#v", got)
	}
}
