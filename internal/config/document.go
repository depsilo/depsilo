package config

import "fmt"

// ValidateDocument applies the same decoding, normalization, defaults, and
// settings validation used when Depsilo loads a configuration file. It does
// not apply environment overrides or mutate process state.
func ValidateDocument(document []byte) error {
	if _, err := decodeConfigDocument(document); err != nil {
		return fmt.Errorf("validate config document: %w", err)
	}
	return nil
}
