package operator

import (
	"errors"
	"reflect"
	"testing"
)

func TestReloadAndEnableSystemdTimerUsesSeparateCommands(t *testing.T) {
	var calls [][]string
	run := func(args ...string) error {
		calls = append(calls, args)
		return nil
	}

	if err := reloadAndEnableSystemdTimer(run, "openlia-backup-project"); err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"daemon-reload"},
		{"enable", "--now", "openlia-backup-project.timer"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("systemctl calls = %#v, want %#v", calls, want)
	}
}

func TestReloadAndEnableSystemdTimerStopsWhenReloadFails(t *testing.T) {
	wantErr := errors.New("reload failed")
	var calls [][]string
	run := func(args ...string) error {
		calls = append(calls, args)
		return wantErr
	}

	if err := reloadAndEnableSystemdTimer(run, "openlia-backup-project"); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], []string{"daemon-reload"}) {
		t.Fatalf("systemctl calls = %#v, want only daemon-reload", calls)
	}
}

func TestReloadAndEnableSystemdTimerReturnsEnableFailure(t *testing.T) {
	wantErr := errors.New("enable failed")
	var calls [][]string
	run := func(args ...string) error {
		calls = append(calls, args)
		if len(calls) == 2 {
			return wantErr
		}
		return nil
	}

	if err := reloadAndEnableSystemdTimer(run, "openlia-backup-project"); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	want := [][]string{
		{"daemon-reload"},
		{"enable", "--now", "openlia-backup-project.timer"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("systemctl calls = %#v, want %#v", calls, want)
	}
}
