package operator

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteJSONProducesOneNonHTMLEscapedDocument(t *testing.T) {
	var output bytes.Buffer
	if err := WriteJSON(&output, map[string]string{"text": "<tag> café"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(output.String(), "\n") || strings.Contains(output.String(), `\u003c`) {
		t.Fatalf("unexpected JSON encoding: %q", output.String())
	}
	var decoded map[string]string
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["text"] != "<tag> café" {
		t.Fatalf("decoded JSON = %#v", decoded)
	}
}
