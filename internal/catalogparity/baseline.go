package catalogparity

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

var surfaceNamePattern = regexp.MustCompile(`^unifi_[a-z0-9_]+$`)

var surfacePrefixes = []struct {
	prefix string
	kind   SurfaceKind
}{
	{prefix: "resource_schemas.", kind: ManagedResource},
	{prefix: "data_source_schemas.", kind: DataSource},
	{prefix: "list_resource_schemas.", kind: ListResource},
	{prefix: "action_schemas.", kind: Action},
}

type baselineDocument struct {
	FormatVersion         int               `json:"format_version"`
	ProviderAddress       string            `json:"provider_address"`
	CanonicalSchemaSHA256 string            `json:"canonical_schema_sha256"`
	SchemaSHA256          map[string]string `json:"schema_sha256"`
}

func ParseBaseline(data []byte) (Baseline, error) {
	var document baselineDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Baseline{}, fmt.Errorf("decode baseline: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Baseline{}, fmt.Errorf("decode baseline: %w", err)
	}
	if document.FormatVersion != 1 {
		return Baseline{}, fmt.Errorf("unsupported baseline format version %d", document.FormatVersion)
	}
	if document.ProviderAddress != CanonicalProviderAddress {
		return Baseline{}, fmt.Errorf("provider address %q is not canonical", document.ProviderAddress)
	}
	if !validSHA256(document.CanonicalSchemaSHA256) {
		return Baseline{}, fmt.Errorf("canonical schema SHA-256 is invalid")
	}
	if len(document.SchemaSHA256) == 0 {
		return Baseline{}, fmt.Errorf("schema digest map is empty")
	}

	surfaces := make([]Surface, 0, len(document.SchemaSHA256))
	seen := make(map[SurfaceKey]struct{})
	for key, digest := range document.SchemaSHA256 {
		if !validSHA256(digest) {
			return Baseline{}, fmt.Errorf("schema digest %q is invalid", key)
		}
		if key == "provider" {
			continue
		}
		if strings.HasPrefix(key, "resource_identity_schemas.") {
			name := strings.TrimPrefix(key, "resource_identity_schemas.")
			if !surfaceNamePattern.MatchString(name) {
				return Baseline{}, fmt.Errorf("resource identity name %q is invalid", name)
			}
			continue
		}

		kind, name, ok := splitSurfaceDigestKey(key)
		if !ok {
			return Baseline{}, fmt.Errorf("unknown schema digest category %q", key)
		}
		if !surfaceNamePattern.MatchString(name) {
			return Baseline{}, fmt.Errorf("surface name %q is invalid", name)
		}
		surfaceKey := SurfaceKey{Kind: kind, Name: name}
		if _, duplicate := seen[surfaceKey]; duplicate {
			return Baseline{}, fmt.Errorf("duplicate surface %s/%s", kind, name)
		}
		seen[surfaceKey] = struct{}{}
		surfaces = append(surfaces, Surface{
			SurfaceKey:           surfaceKey,
			BaselineSchemaSHA256: digest,
		})
	}

	sort.Slice(surfaces, func(i, j int) bool {
		left := surfaceKindOrder(surfaces[i].Kind)
		right := surfaceKindOrder(surfaces[j].Kind)
		if left != right {
			return left < right
		}
		return surfaces[i].Name < surfaces[j].Name
	})
	baseline := Baseline{
		FormatVersion:         1,
		ProviderAddress:       document.ProviderAddress,
		CanonicalSchemaSHA256: document.CanonicalSchemaSHA256,
		Surfaces:              surfaces,
	}
	if err := validateReleasedCounts(baseline.Counts()); err != nil {
		return Baseline{}, err
	}
	return baseline, nil
}

func splitSurfaceDigestKey(key string) (SurfaceKind, string, bool) {
	for _, candidate := range surfacePrefixes {
		if strings.HasPrefix(key, candidate.prefix) {
			return candidate.kind, strings.TrimPrefix(key, candidate.prefix), true
		}
	}
	return "", "", false
}

func surfaceKindOrder(kind SurfaceKind) int {
	switch kind {
	case ManagedResource:
		return 0
	case DataSource:
		return 1
	case ListResource:
		return 2
	case Action:
		return 3
	default:
		return 4
	}
}

func validateReleasedCounts(counts map[SurfaceKind]int) error {
	want := map[SurfaceKind]int{
		ManagedResource: 28,
		DataSource:      13,
		ListResource:    25,
		Action:          1,
	}
	for _, kind := range []SurfaceKind{ManagedResource, DataSource, ListResource, Action} {
		if counts[kind] != want[kind] {
			return fmt.Errorf("%s count is %d, want %d", kind, counts[kind], want[kind])
		}
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}
