// Package catalogparity defines the released provider catalog and the
// admission and migration artifacts that account for it.
package catalogparity

type SurfaceKind string

const (
	ManagedResource SurfaceKind = "managed_resource"
	DataSource      SurfaceKind = "data_source"
	ListResource    SurfaceKind = "list_resource"
	Action          SurfaceKind = "action"
)

const CanonicalProviderAddress = "registry.terraform.io/ubiquiti-community/unifi"

type SurfaceKey struct {
	Kind SurfaceKind `json:"kind"`
	Name string      `json:"name"`
}

type Surface struct {
	SurfaceKey
	BaselineSchemaSHA256 string `json:"baseline_schema_sha256"`
}

type Baseline struct {
	FormatVersion         int       `json:"format_version"`
	ProviderAddress       string    `json:"provider_address"`
	SourceSHA256          string    `json:"source_sha256"`
	CanonicalSchemaSHA256 string    `json:"canonical_schema_sha256"`
	Surfaces              []Surface `json:"surfaces"`
}

func (b Baseline) Counts() map[SurfaceKind]int {
	counts := map[SurfaceKind]int{
		ManagedResource: 0,
		DataSource:      0,
		ListResource:    0,
		Action:          0,
	}
	for _, surface := range b.Surfaces {
		counts[surface.Kind]++
	}
	return counts
}

func (b Baseline) Surface(key SurfaceKey) (Surface, bool) {
	for _, surface := range b.Surfaces {
		if surface.SurfaceKey == key {
			return surface, true
		}
	}
	return Surface{}, false
}
