// Package schemabaseline canonicalizes provider schema output from Terraform
// and OpenTofu without discarding provider-exposed semantics.
package schemabaseline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

var schemaCategories = []string{
	"resource_schemas",
	"resource_identity_schemas",
	"data_source_schemas",
	"list_resource_schemas",
	"functions",
	"action_schemas",
	"ephemeral_resource_schemas",
}

type digestManifest struct {
	FormatVersion         int               `json:"format_version"`
	ProviderAddress       string            `json:"provider_address"`
	CanonicalSchemaSHA256 string            `json:"canonical_schema_sha256"`
	SchemaSHA256          map[string]string `json:"schema_sha256"`
}

// Canonicalize returns the selected provider projection and its digest
// manifest as compact, newline-terminated JSON.
func Canonicalize(raw []byte, providerAddress string) ([]byte, []byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var envelope map[string]any
	if err := decoder.Decode(&envelope); err != nil {
		return nil, nil, fmt.Errorf("decode provider schema envelope: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, nil, fmt.Errorf("unexpected data after top-level provider schema object")
		}
		return nil, nil, fmt.Errorf("decode data after top-level provider schema object: %w", err)
	}

	providers, ok := envelope["provider_schemas"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("provider_schemas must be an object")
	}
	projectionValue, ok := providers[providerAddress]
	if !ok {
		return nil, nil, fmt.Errorf("provider_schemas does not contain %q", providerAddress)
	}
	projection, ok := projectionValue.(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("provider projection for %q must be an object", providerAddress)
	}
	if _, ok := projection["provider"].(map[string]any); !ok {
		return nil, nil, fmt.Errorf("provider projection for %q has no provider object", providerAddress)
	}

	canonical, err := marshalLine(projection)
	if err != nil {
		return nil, nil, fmt.Errorf("encode canonical provider schema: %w", err)
	}

	digests := map[string]string{
		"provider": digestJSON(projection["provider"]),
	}
	for _, category := range schemaCategories {
		entries, ok := projection[category].(map[string]any)
		if !ok {
			continue
		}
		for name, schema := range entries {
			digests[category+"."+name] = digestJSON(schema)
		}
	}

	manifest, err := marshalLine(digestManifest{
		FormatVersion:         1,
		ProviderAddress:       providerAddress,
		CanonicalSchemaSHA256: digestBytes(canonical),
		SchemaSHA256:          digests,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("encode provider schema digests: %w", err)
	}
	return canonical, manifest, nil
}

func marshalLine(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func digestJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return digestBytes(encoded)
}

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
