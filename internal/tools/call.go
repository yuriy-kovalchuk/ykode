package tools

import (
	"encoding/json"
	"fmt"
)

// toolError names the tool and the path or command it failed on.
func toolError(tool, target string, err error) error {
	return fmt.Errorf("%s %q: %w", tool, target, err)
}

// decodeArgs unmarshals the model's arguments, naming the failure usefully.
func decodeArgs(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("bad arguments: %w", err)
	}
	return nil
}
