package operator

import "testing"

func TestConfiguredToolCommand(t *testing.T) {
	got := configuredToolCommand([]string{"ocr", "pdf", "media-transcripts", "office"})
	want := "command -v ffmpeg >/dev/null && command -v yt-dlp >/dev/null && command -v tesseract >/dev/null && command -v libreoffice >/dev/null && command -v pdftotext >/dev/null && command -v pdfinfo >/dev/null"
	if got != want {
		t.Fatalf("configuredToolCommand() = %q, want %q", got, want)
	}
}

func TestConfiguredToolCommandRejectsUnknownCapability(t *testing.T) {
	if got := configuredToolCommand([]string{"unknown"}); got != "" {
		t.Fatalf("configuredToolCommand() = %q for unknown capability", got)
	}
}
