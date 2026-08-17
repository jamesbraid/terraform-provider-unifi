package schemaparity

import "github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"

// SchemaRun is everything one catalog-build-schema run observed.
//
// Named SchemaRun rather than Measured because Measured is already an
// EvidenceKind in this package -- the collision is worth noting rather than
// silently working around, since both names mean "observed" and a reader
// meeting them together should know they are unrelated.
//
// It is the input to BuildSchemaReceipt and it is deliberately flat and dumb:
// every field is something the orchestration measured, nothing is derived here,
// and no field has a default. A zero value produces a receipt that fails its own
// checks rather than a plausible one, which is the property the shell version
// lacked -- an unset variable there rendered as an empty JSON string and every
// consumer read it as a value.
type SchemaRun struct {
	SourceCommit   string
	ReleasedCommit string
	Platform       string
	GoVersion      string

	ReleasedSourceRebuildSHA256 string
	ReleasedAuthority           string
	ReleasedAuthoritySHA256     string
	CandidateSHA256             string

	Terraform catalogparity.SchemaCLIReceipt
	Tofu      catalogparity.SchemaCLIReceipt

	SharedSchemaSHA256 string
	InventorySHA256    string

	// SharedProjectionEqual and FullProjectionEqual are OPPOSITE claims and
	// both are recorded because both are asserted. The two CLIs must agree on
	// the surface they share and must DIFFER on the full projection, since
	// terraform reports action and list-resource schemas and tofu does not.
	// The shell wrote these as the literals true and false, so the receipt
	// asserted them regardless of what the run had found.
	SharedProjectionEqual bool
	FullProjectionEqual   bool

	ReleaseToCandidateWithinCLI bool

	// CleanBuilds is how many times each side was actually built.
	//
	// CARRIED, NOT A CONSTANT, and the first version of this file got that
	// wrong: it wrote {released: 2, candidate: 2} as a literal under a comment
	// claiming the number was "recorded rather than assumed". It was assumed.
	// The determinism assertion is only worth anything if a side was built more
	// than once, and a receipt that always says 2 cannot report the run where
	// somebody made it 1. That is the same defect as the projection literals in
	// the jq template, reproduced in Go by someone who had just finished
	// describing it.
	CleanBuilds catalogparity.BuildCounts

	TreeState         *catalogparity.TreeState
	PromotionBlockers []string
}

// terraformOnlyCategories are the keys terraform reports and tofu does not.
//
// Named here rather than written into the receipt at the call site because the
// inverted control depends on them: if this list ever became empty the two
// projections could legitimately match, and the assertion that they must differ
// would quietly become unsatisfiable.
var terraformOnlyCategories = []string{"action_schemas", "list_resource_schemas"}

// BuildSchemaReceipt assembles the receipt from what the run measured.
//
// The shell it replaces built this with a forty-line jq expression, which is
// why no test ever constructed one: there was nothing to call. Every consumer
// -- managementcontract, releasequalification, catalogparity -- already reads
// it as this exact struct, so the hand-assembly was the only untyped step in a
// chain that was typed at both ends.
//
// Result is derived from the blockers rather than passed in. The two cannot
// disagree here, which they could in the shell: result and promotion_blockers
// were rendered from separate variables, so a receipt saying "pass" beside a
// non-empty blocker list was expressible and nothing rejected it.
func BuildSchemaReceipt(m SchemaRun) catalogparity.BuildSchemaReceipt {
	result := "pass"
	if len(m.PromotionBlockers) > 0 {
		result = "blocked"
	}
	blockers := m.PromotionBlockers
	if blockers == nil {
		// An absent list must render as [] rather than null: consumers range
		// over it, and a null decodes to a nil slice that reads as "no
		// blockers" by accident rather than by measurement.
		blockers = []string{}
	}
	return catalogparity.BuildSchemaReceipt{
		FormatVersion:     1,
		Gate:              "catalog-build-schema",
		TreeState:         m.TreeState,
		Result:            result,
		PromotionBlockers: blockers,
		SourceCommit:      m.SourceCommit,
		ReleasedCommit:    m.ReleasedCommit,
		Platform:          m.Platform,
		GoVersion:         m.GoVersion,
		BuildNetwork:      "none",
		CleanBuilds:       m.CleanBuilds,
		ProviderBinaries: catalogparity.ProviderBinaryEvidence{
			ReleasedSourceRebuildSHA256: m.ReleasedSourceRebuildSHA256,
			ReleasedAuthority:           m.ReleasedAuthority,
			ReleasedAuthoritySHA256:     m.ReleasedAuthoritySHA256,
			CandidateSHA256:             m.CandidateSHA256,
		},
		InventorySHA256: m.InventorySHA256,
		SchemaEvidence: catalogparity.SchemaDifferentialEvidence{
			Terraform:                   m.Terraform,
			Tofu:                        m.Tofu,
			ReleaseToCandidateWithinCLI: m.ReleaseToCandidateWithinCLI,
			SharedCLIProjectionEqual:    m.SharedProjectionEqual,
			FullCLIProjectionEqual:      m.FullProjectionEqual,
			SharedSchemaSHA256:          m.SharedSchemaSHA256,
			TerraformOnlyCategories:     terraformOnlyCategories,
		},
	}
}
