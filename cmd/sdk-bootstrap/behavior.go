package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// behaviorDocument wraps the SDK's measured-behaviour artifact
// (schemas/behavior.json in the module) with the same source record every
// structural bootstrap carries, so the committed copy says which module the
// facts were measured against. SpecificationSHA256 here digests the artifact
// bytes themselves -- the artifact is its own specification.
type behaviorDocument struct {
	FormatVersion int             `json:"format_version"`
	Source        bootstrapSource `json:"source"`
	Behavior      json.RawMessage `json:"behavior"`
}

// writeBehaviorArtifact copies the resolved module's schemas/behavior.json
// to output, wrapped with its source. The artifact travels verbatim: this
// step stamps provenance, it never edits measured facts.
func writeBehaviorArtifact(source bootstrapSource, moduleDir, output string) error {
	if moduleDir == "" {
		return fmt.Errorf("go list reported no directory for the SDK module")
	}
	path := filepath.Join(moduleDir, "schemas", "behavior.json")
	raw, err := os.ReadFile(path) // #nosec G304 -- path comes from resolving the -package build-time flag through go list
	if err != nil {
		return fmt.Errorf("read the SDK's behaviour artifact: %w", err)
	}
	if !json.Valid(raw) {
		return fmt.Errorf("%s is not valid JSON", path)
	}
	sum := sha256.Sum256(raw)
	source.SpecificationSHA256 = hex.EncodeToString(sum[:])
	document := behaviorDocument{FormatVersion: 1, Source: source, Behavior: raw}
	encoded := new(strings.Builder)
	encoder := json.NewEncoder(encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if err := os.WriteFile(output, []byte(encoded.String()), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", output, err)
	}
	return nil
}
