package operator

import (
	"encoding/json"
	"fmt"
	"io"
)

const (
	ExitOK       = 0
	ExitFailure  = 1
	ExitUsage    = 2
	ExitPrereq   = 3
	ExitInternal = 4
)

// WriteJSON uses the CLI's stable non-HTML-escaped encoding and always emits
// one newline-terminated document.
func WriteJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

// EncodeJSON is an alias for callers that prefer encoding terminology.
func EncodeJSON(writer io.Writer, value any) error {
	return WriteJSON(writer, value)
}

func writeError(writer io.Writer, jsonOutput bool, code int, err error) int {
	if jsonOutput {
		payload := map[string]any{"schema": 1, "ok": false, "error": err.Error()}
		if encodeErr := WriteJSON(writer, payload); encodeErr != nil {
			return ExitInternal
		}
		return code
	}
	return code
}

func formatError(err error) error {
	if err == nil {
		return fmt.Errorf("unknown operator error")
	}
	return err
}
