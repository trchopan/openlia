package cli

import (
	"testing"
)

func TestWorkspaceExportRequiresZipExtension(t *testing.T) {
	options := Options{JSON: true}
	code := commandWorkspaceExport(options, []string{"my-export.tar.gz"})
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage (%d) for non-zip destination, got %d", ExitUsage, code)
	}
}

func TestWorkspaceExportRejectsMultipleDestinations(t *testing.T) {
	options := Options{JSON: true}
	code := commandWorkspaceExport(options, []string{"one.zip", "two.zip"})
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage (%d) for multiple destinations, got %d", ExitUsage, code)
	}
}

func TestFormatExportBytes(t *testing.T) {
	cases := []struct {
		bytes int64
		want  string
	}{
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{2621440, "2.5 MB"},
	}
	for _, tc := range cases {
		got := formatExportBytes(tc.bytes)
		if got != tc.want {
			t.Errorf("formatExportBytes(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}
