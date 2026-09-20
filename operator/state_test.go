package operator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateRoundTripAcceptsShellNewline(t *testing.T) {
	runtime := filepath.Join(t.TempDir(), "runtime")
	config := testConfig(t.TempDir(), runtime)
	if err := os.MkdirAll(filepath.Dir(config.StateFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteState(config, StateRunning); err != nil {
		t.Fatal(err)
	}
	state, err := ReadState(config)
	if err != nil {
		t.Fatal(err)
	}
	if state != StateRunning {
		t.Fatalf("state = %q", state)
	}
}
