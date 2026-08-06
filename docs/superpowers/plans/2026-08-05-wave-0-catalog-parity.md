# Wave 0 Catalog Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the DNS-only admission view with deterministic catalog-wide
ledger, migration, compiler, differential, and evidence primitives without
changing provider runtime registration.

**Architecture:** Parse the canonical v0.101.2 schema digest manifest into a
typed inventory, then expand small reviewed policy overlays into complete
ledger and migration artifacts. Bind the existing compiler to those artifacts
and add a transport-neutral differential harness for later waves. Generated
artifacts fail closed on missing, duplicated, stale, or unmanifested surfaces.

**Tech Stack:** Go 1.25.8, Terraform Plugin Framework, Terraform 1.15.8,
OpenTofu 1.12.1, JSON, `go generate`, and `go test`.

## Global Constraints

- Keep `registry.terraform.io/ubiquiti-community/unifi` as the provider address.
- Treat v0.101.2 as the released catalog baseline: 28 managed resources, 13
  data sources, 25 list resources, and one action.
- Preserve controller identity and ownership. No generated migration may imply
  an undeclared replacement, deletion, or ownership transfer.
- Terraform and OpenTofu are the only state writers. Wave 0 emits reports and
  commands but does not mutate state.
- Keep all provider registrations and runtime adapter selection unchanged.
- Derive fleet priority from value-free surface references only. Do not retain
  fleet HCL, state, controller responses, internal host names, or credentials.
- Keep generated JSON canonical and reproducible under `go generate ./...`.
- Commit each task separately after its focused tests pass.

---

### Task 1: Parse the complete released catalog

**Files:**

- Create: `internal/catalogparity/types.go`
- Create: `internal/catalogparity/baseline.go`
- Create: `internal/catalogparity/baseline_test.go`

**Interfaces:**

- Consumes: `build/m0/provider-schema-digests.json`.
- Produces: `ParseBaseline(data []byte) (Baseline, error)`, `SurfaceKind`,
  `SurfaceKey`, `Surface`, and `Baseline.Surface(SurfaceKey)`.

- [ ] **Step 1: Write the failing parser and completeness tests**

```go
func TestParseBaselineFindsCompleteCatalog(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "build", "m0", "provider-schema-digests.json"))
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := ParseBaseline(data)
	if err != nil {
		t.Fatal(err)
	}
	want := map[SurfaceKind]int{
		ManagedResource: 28,
		DataSource:       13,
		ListResource:     25,
		Action:           1,
	}
	if got := baseline.Counts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Counts() = %#v, want %#v", got, want)
	}
}

func TestParseBaselineRejectsDuplicateOrUnknownSurfaceKeys(t *testing.T) {
	for _, key := range []string{"resource_schemas.unifi_dns_record.extra", "function_schemas.unifi_unknown"} {
		_, err := ParseBaseline(testBaselineWithKey(t, key))
		if err == nil {
			t.Fatalf("ParseBaseline(%q) succeeded", key)
		}
	}
}
```

Define the shared test helpers in `baseline_test.go` so later tasks use the
same released fixture:

```go
func releasedBaseline(t *testing.T) Baseline {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "build", "m0", "provider-schema-digests.json"))
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := ParseBaseline(data)
	if err != nil {
		t.Fatal(err)
	}
	return baseline
}

func testBaselineWithKey(t *testing.T, key string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "build", "m0", "provider-schema-digests.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		FormatVersion         int               `json:"format_version"`
		ProviderAddress       string            `json:"provider_address"`
		CanonicalSchemaSHA256 string            `json:"canonical_schema_sha256"`
		SchemaSHA256          map[string]string `json:"schema_sha256"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.SchemaSHA256[key] = strings.Repeat("a", 64)
	mutated, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return mutated
}
```

- [ ] **Step 2: Run the tests and verify the new package is absent**

Run: `go test ./internal/catalogparity -run TestParseBaseline -count=1`

Expected: FAIL because `ParseBaseline` and its types do not exist.

- [ ] **Step 3: Implement strict baseline parsing**

```go
type SurfaceKind string

const (
	ManagedResource SurfaceKind = "managed_resource"
	DataSource       SurfaceKind = "data_source"
	ListResource     SurfaceKind = "list_resource"
	Action           SurfaceKind = "action"
)

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
	CanonicalSchemaSHA256 string    `json:"canonical_schema_sha256"`
	Surfaces              []Surface `json:"surfaces"`
}

func ParseBaseline(data []byte) (Baseline, error)
func (b Baseline) Counts() map[SurfaceKind]int
func (b Baseline) Surface(key SurfaceKey) (Surface, bool)
```

Map only `resource_schemas.`, `data_source_schemas.`,
`list_resource_schemas.`, and `action_schemas.` keys. Ignore provider and
resource-identity digests. Reject malformed names, invalid SHA-256 values,
unknown schema categories, duplicate `(kind, name)` pairs, the wrong provider
address, and counts other than 28/13/25/1. Sort by kind and name.

- [ ] **Step 4: Run the focused and package tests**

Run: `go test ./internal/catalogparity -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the catalog parser**

```bash
git add internal/catalogparity/types.go internal/catalogparity/baseline.go internal/catalogparity/baseline_test.go
git commit -m "catalogparity: parse the released provider catalog"
```

### Task 2: Expand a complete admission ledger

**Files:**

- Create: `internal/catalogparity/ledger.go`
- Create: `internal/catalogparity/ledger_test.go`
- Create: `provider-codegen/parity/status.json`

**Interfaces:**

- Consumes: `Baseline` from Task 1 and a `StatusOverlay` JSON document.
- Produces: `BuildLedger(Baseline, StatusOverlay) (Ledger, error)`,
  `ParseLedger([]byte) (Ledger, error)`, and
  `Ledger.Require(SurfaceKey, ...AdmissionState) error`.

- [ ] **Step 1: Write failing expansion and fail-closed tests**

```go
func TestBuildLedgerExpandsEverySurface(t *testing.T) {
	baseline := releasedBaseline(t)
	overlay := StatusOverlay{
		FormatVersion: 1,
		DefaultState: LegacyAuthoritative,
		Overrides: []StatusOverride{
			{
				SurfaceKey:    SurfaceKey{Kind: ManagedResource, Name: "unifi_dns_record"},
				State:         Admitted,
				ReceiptSHA256: strings.Repeat("a", 64),
			},
			{SurfaceKey: SurfaceKey{Kind: ManagedResource, Name: "unifi_port_forward"}, State: ShadowOnly},
			{SurfaceKey: SurfaceKey{Kind: ListResource, Name: "unifi_dns_record"}, State: ShadowOnly},
		},
	}
	ledger, err := BuildLedger(baseline, overlay)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Entries) != 67 {
		t.Fatalf("entries = %d, want 67", len(ledger.Entries))
	}
	if err := ledger.Require(SurfaceKey{Kind: ManagedResource, Name: "unifi_dns_record"}, Admitted); err != nil {
		t.Fatal(err)
	}
}
```

Add table cases for duplicate overrides, stale surface names, invalid states,
an empty default, a missing baseline digest, and an override that claims
`release_ready` without a receipt digest.

- [ ] **Step 2: Run the focused test and verify failure**

Run: `go test ./internal/catalogparity -run 'TestBuildLedger|TestLedger' -count=1`

Expected: FAIL because the ledger API does not exist.

- [ ] **Step 3: Implement admission states and deterministic expansion**

```go
type AdmissionState string

const (
	BaselineState       AdmissionState = "baseline"
	Cataloged           AdmissionState = "cataloged"
	PolicyComplete      AdmissionState = "policy_complete"
	GeneratedShadow     AdmissionState = "generated_shadow"
	AdapterParity       AdmissionState = "adapter_parity"
	Admitted            AdmissionState = "admitted"
	ContractParity      AdmissionState = "contract_parity"
	ReleaseReady        AdmissionState = "release_ready"
	ShadowOnly          AdmissionState = "shadow_only"
	LegacyAuthoritative AdmissionState = "legacy_authoritative"
)

type LedgerEntry struct {
	Surface
	State          AdmissionState `json:"state"`
	ReceiptSHA256  string         `json:"receipt_sha256,omitempty"`
	Implementation string         `json:"implementation"`
}

type StatusOverride struct {
	SurfaceKey
	State         AdmissionState `json:"state"`
	ReceiptSHA256 string         `json:"receipt_sha256,omitempty"`
}

type StatusOverlay struct {
	FormatVersion int              `json:"format_version"`
	DefaultState  AdmissionState   `json:"default_state"`
	Overrides     []StatusOverride `json:"overrides"`
}

type Ledger struct {
	FormatVersion   int           `json:"format_version"`
	ProviderAddress string        `json:"provider_address"`
	BaselineSHA256  string        `json:"baseline_sha256"`
	Entries         []LedgerEntry `json:"entries"`
}
```

Set `implementation` to `candidate` only for admitted-or-later entries,
`shadow` for `shadow_only`, and `legacy` otherwise. Require a 64-character
receipt digest for `admitted`, `contract_parity`, and `release_ready`. The
tracked status overlay records the current DNS and port-forward exceptions and
uses `legacy_authoritative` for all other surfaces.

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/catalogparity -count=1`

Expected: PASS with 67 deterministic ledger entries.

- [ ] **Step 5: Commit the ledger**

```bash
git add internal/catalogparity/ledger.go internal/catalogparity/ledger_test.go provider-codegen/parity/status.json
git commit -m "catalogparity: expand catalog-wide admission status"
```

### Task 3: Compile the catalog-wide migration contract

**Files:**

- Create: `internal/catalogparity/migration.go`
- Create: `internal/catalogparity/migration_test.go`
- Create: `provider-codegen/migrations/v0.101.2-to-next.json`

**Interfaces:**

- Consumes: `Baseline` and `MigrationPolicy`.
- Produces: `ExpandMigration(Baseline, MigrationPolicy)
  (MigrationManifest, error)` and `BuildMigrationReport(MigrationManifest)
  (MigrationReport, error)`.

- [ ] **Step 1: Write failing identity-expansion and safety tests**

```go
func TestExpandMigrationRecordsEveryReleasedSurface(t *testing.T) {
	manifest, err := ExpandMigration(releasedBaseline(t), MigrationPolicy{
		FormatVersion:   1,
		FromVersion:     "0.101.2",
		ToVersion:       "next",
		ProviderAddress: canonicalProviderAddress,
		DefaultStrategy: IdentityTransform,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 67 {
		t.Fatalf("entries = %d, want 67", len(manifest.Entries))
	}
	for _, entry := range manifest.Entries {
		if entry.Strategy != IdentityTransform || entry.OldName != entry.NewName || entry.DestructiveRisk != RiskNone {
			t.Fatalf("unsafe identity entry: %#v", entry)
		}
	}
}
```

Add rejection cases for an unknown surface, duplicate override, provider
address change, unsupported strategy, missing import identity, state address
change without a state move, missing recovery procedure, and any undeclared
destructive risk.

- [ ] **Step 2: Run the focused tests and verify failure**

Run: `go test ./internal/catalogparity -run 'TestExpandMigration|TestMigration' -count=1`

Expected: FAIL because migration expansion is not implemented.

- [ ] **Step 3: Implement explicit expanded migration entries**

```go
type MigrationStrategy string

const (
	IdentityTransform MigrationStrategy = "identity"
	StateUpgrader     MigrationStrategy = "state_upgrader"
	AddressRecipe     MigrationStrategy = "address_recipe"
	ImportBridge      MigrationStrategy = "import_bridge"
	Unsupported       MigrationStrategy = "unsupported"
)

type MigrationEntry struct {
	SurfaceKey
	OldName           string            `json:"old_name"`
	NewName           string            `json:"new_name"`
	Strategy          MigrationStrategy `json:"strategy"`
	OldSchemaVersion  int64             `json:"old_schema_version"`
	NewSchemaVersion  int64             `json:"new_schema_version"`
	AttributeMapping  map[string]string `json:"attribute_mapping"`
	StateMoves        []StateMove       `json:"state_moves"`
	ImportTransform   string            `json:"import_transform,omitempty"`
	ConfigurationEdit string            `json:"configuration_edit"`
	DestructiveRisk   Risk              `json:"destructive_risk"`
	ForwardAssertions []string          `json:"forward_assertions"`
	Recovery          Recovery          `json:"recovery"`
}

type Risk string

const (
	RiskNone     Risk = "none"
	RiskDeclared Risk = "declared"
)

type StateMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Recovery struct {
	Mode       string   `json:"mode"`
	Assertions []string `json:"assertions"`
}

type MigrationPolicy struct {
	FormatVersion   int                  `json:"format_version"`
	FromVersion     string               `json:"from_version"`
	ToVersion       string               `json:"to_version"`
	ProviderAddress string               `json:"provider_address"`
	DefaultStrategy MigrationStrategy    `json:"default_strategy"`
	Overrides       []MigrationEntry      `json:"overrides"`
}

type MigrationManifest struct {
	FormatVersion   int              `json:"format_version"`
	FromVersion     string           `json:"from_version"`
	ToVersion       string           `json:"to_version"`
	ProviderAddress string           `json:"provider_address"`
	Entries         []MigrationEntry `json:"entries"`
}

type MigrationReport struct {
	FormatVersion  int            `json:"format_version"`
	SurfaceCount   int            `json:"surface_count"`
	StrategyCounts map[string]int `json:"strategy_counts"`
	RiskCounts     map[string]int `json:"risk_counts"`
	StateCommands  []string       `json:"state_commands"`
}
```

The initial policy expands all 67 surfaces to explicit identity transforms.
Use `snapshot_restore` as the recovery mode, require the assertions
`identity_preserved`, `no_undeclared_replace`, `no_undeclared_delete`, and
`first_plan_empty`, and emit no state commands for identity entries.

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/catalogparity -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the migration contract**

```bash
git add internal/catalogparity/migration.go internal/catalogparity/migration_test.go provider-codegen/migrations/v0.101.2-to-next.json
git commit -m "catalogparity: define the catalog migration contract"
```

### Task 4: Generate and verify canonical Wave 0 artifacts

**Files:**

- Create: `cmd/catalog-parity/main.go`
- Create: `cmd/catalog-parity/main_test.go`
- Modify: `provider-codegen/generate.go`
- Generate: `provider-codegen/generated/catalog-parity-ledger.json`
- Generate: `provider-codegen/generated/catalog-migration-manifest.json`
- Generate: `provider-codegen/generated/catalog-migration-report.json`

**Interfaces:**

- Consumes: baseline, status overlay, and migration policy paths.
- Produces: three canonical JSON files through `run(args []string,
  stderr io.Writer) int` and atomic file replacement.

- [ ] **Step 1: Write failing deterministic CLI tests**

```go
func TestRunProducesDeterministicCatalogArtifacts(t *testing.T) {
	first := runCatalogParity(t)
	second := runCatalogParity(t)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("catalog parity outputs differ across identical runs")
	}
	var ledger catalogparity.Ledger
	decodeOutput(t, first["catalog-parity-ledger.json"], &ledger)
	if len(ledger.Entries) != 67 {
		t.Fatalf("ledger entries = %d, want 67", len(ledger.Entries))
	}
}
```

Define the command helpers in `main_test.go`:

```go
func runCatalogParity(t *testing.T) map[string][]byte {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", ".."))
	outputDir := t.TempDir()
	exitCode := run([]string{
		"-baseline", filepath.Join(root, "build", "m0", "provider-schema-digests.json"),
		"-status", filepath.Join(root, "provider-codegen", "parity", "status.json"),
		"-migration", filepath.Join(root, "provider-codegen", "migrations", "v0.101.2-to-next.json"),
		"-output-dir", outputDir,
	}, io.Discard)
	if exitCode != 0 {
		t.Fatalf("run() exit code = %d", exitCode)
	}
	outputs := map[string][]byte{}
	for _, name := range []string{
		"catalog-parity-ledger.json",
		"catalog-migration-manifest.json",
		"catalog-migration-report.json",
	} {
		data, err := os.ReadFile(filepath.Join(outputDir, name))
		if err != nil {
			t.Fatal(err)
		}
		outputs[name] = data
	}
	return outputs
}

func decodeOutput(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
```

Test missing arguments, malformed inputs, an output directory containing a
`catalog-*.json` file outside the three-name output allow-list, and a second
generation that leaves the tracked worktree unchanged.

- [ ] **Step 2: Run the command tests and verify failure**

Run: `go test ./cmd/catalog-parity -count=1`

Expected: FAIL because the command does not exist.

- [ ] **Step 3: Implement the command and canonical encoder**

Required flags:

```text
-baseline build/m0/provider-schema-digests.json
-status provider-codegen/parity/status.json
-migration provider-codegen/migrations/v0.101.2-to-next.json
-output-dir provider-codegen/generated
```

Encode with `json.Marshal`, append one newline, write through a temporary file,
`fsync` the file, rename it, then `fsync` the directory. Before writing, reject
unexpected files matching `catalog-*.json` in the output directory.

- [ ] **Step 4: Add the generation entrypoint and regenerate twice**

Add this directive before the DNS compiler directives:

```go
//go:generate go run ../cmd/catalog-parity -baseline ../build/m0/provider-schema-digests.json -status parity/status.json -migration migrations/v0.101.2-to-next.json -output-dir generated
```

Run: `go generate ./... && git diff --exit-code`

Expected after staging generated outputs: the second generation exits zero and
produces no diff.

- [ ] **Step 5: Run focused tests and commit**

Run: `go test ./cmd/catalog-parity ./internal/catalogparity -count=1`

```bash
git add cmd/catalog-parity internal/catalogparity provider-codegen/generate.go provider-codegen/generated/catalog-*.json
git commit -m "catalogparity: generate complete parity artifacts"
```

### Task 5: Bind the provider compiler to catalog admission

**Files:**

- Modify: `internal/providercompiler/types.go`
- Modify: `internal/providercompiler/compile.go`
- Modify: `internal/providercompiler/compile_test.go`
- Modify: `cmd/provider-spec-compiler/main.go`
- Modify: `cmd/provider-spec-compiler/main_test.go`
- Modify: `provider-codegen/policy/dns_record.json`
- Modify: `provider-codegen/generate.go`
- Regenerate: `provider-codegen/generated/dns_record.provider-code-spec.json`
- Regenerate: `provider-codegen/generated/dns_record.mapping.json`
- Regenerate: `provider-codegen/generated/dns_record.impact.json`

**Interfaces:**

- Consumes: `CompileInput.Ledger []byte`, policy `surface_kind`, and the Task 4
  ledger artifact.
- Produces: surface-kind-aware mapping and impact reports. The CLI accepts
  `-ledger` and `-artifact-prefix` instead of hard-coding DNS output names.

- [ ] **Step 1: Write failing ledger-binding tests**

```go
func TestCompileRequiresAdmittedLedgerEntry(t *testing.T) {
	input := pinnedDNSInput(t)
	input.Ledger = testLedger(t, catalogparity.LegacyAuthoritative)
	_, err := Compile(input)
	if err == nil || !strings.Contains(err.Error(), "admission") {
		t.Fatalf("Compile() error = %v, want admission failure", err)
	}
}

func TestCompileReportsManagedResourceKind(t *testing.T) {
	result, err := Compile(pinnedDNSInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result.ImpactReport, []byte(`"surface_kind":"managed_resource"`)) {
		t.Fatalf("impact lacks surface kind: %s", result.ImpactReport)
	}
}
```

Add cases for missing ledger input, unknown kind, policy/ledger name mismatch,
baseline digest mismatch, `shadow_only`, and a stale artifact prefix.

- [ ] **Step 2: Run compiler tests and verify failure**

Run: `go test ./internal/providercompiler ./cmd/provider-spec-compiler -count=1`

Expected: FAIL because the compiler has no ledger input or surface kind.

- [ ] **Step 3: Add ledger and surface-kind validation**

```go
type CompileInput struct {
	Bootstrap       []byte
	Catalog         []byte
	Policy          []byte
	BaselineDigests []byte
	Ledger          []byte
}

type policy struct {
	FormatVersion int                       `json:"format_version"`
	SurfaceKind  catalogparity.SurfaceKind `json:"surface_kind"`
	Resource     string                    `json:"resource"`
}
```

Add `SurfaceKind` to the existing policy struct rather than replacing its
catalog, field-policy, provider-owned, or baseline members. Define these test
helpers in `compile_test.go`:

```go
func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func pinnedDNSInput(t *testing.T) CompileInput {
	t.Helper()
	root := filepath.Join("..", "..")
	return CompileInput{
		Catalog:         mustReadFile(t, filepath.Join(root, "provider-codegen", "catalog", "go-unifi-v1.102.0-dns-record.catalog.json")),
		Policy:          mustReadFile(t, filepath.Join(root, "provider-codegen", "policy", "dns_record.json")),
		BaselineDigests: mustReadFile(t, filepath.Join(root, "build", "m0", "provider-schema-digests.json")),
		Ledger:          mustReadFile(t, filepath.Join(root, "provider-codegen", "generated", "catalog-parity-ledger.json")),
	}
}

func testLedger(t *testing.T, state catalogparity.AdmissionState) []byte {
	t.Helper()
	var ledger catalogparity.Ledger
	if err := json.Unmarshal(pinnedDNSInput(t).Ledger, &ledger); err != nil {
		t.Fatal(err)
	}
	for i := range ledger.Entries {
		entry := &ledger.Entries[i]
		if entry.Kind == catalogparity.ManagedResource && entry.Name == "unifi_dns_record" {
			entry.State = state
			entry.ReceiptSHA256 = ""
		}
	}
	data, err := json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
```

Parse the ledger before structural compilation. Require the policy surface to
exist with the same baseline resource digest and one of `admitted`,
`contract_parity`, or `release_ready`. Add `surface_kind` and `surface_name` to
mapping and impact reports. Keep the generated DNS schema byte-identical apart
from those review reports.

- [ ] **Step 4: Generalize CLI artifact naming and generation**

Require `-ledger provider-codegen/generated/catalog-parity-ledger.json` and
`-artifact-prefix dns_record`. Write `<prefix>.provider-code-spec.json`,
`<prefix>.impact.json`, and `<prefix>.mapping.json`. Reject prefixes outside
`[a-z0-9_]+`.

- [ ] **Step 5: Regenerate and run focused tests**

Run: `go generate ./...`

Run: `go test ./internal/providercompiler ./cmd/provider-spec-compiler ./provider-contracts -count=1`

Expected: PASS. The DNS Provider Code Specification and generated Go digest
remain unchanged. Only the mapping and impact report digests may change, and
their dependent tracked receipts must be updated in Task 8 rather than here.

- [ ] **Step 6: Commit compiler admission binding**

```bash
git add internal/providercompiler cmd/provider-spec-compiler provider-codegen/policy/dns_record.json provider-codegen/generate.go provider-codegen/generated/dns_record.*.json
git commit -m "providercompiler: bind compilation to catalog admission"
```

### Task 6: Add transport-neutral differential attempts

**Files:**

- Create: `internal/paritydiff/types.go`
- Create: `internal/paritydiff/compare.go`
- Create: `internal/paritydiff/compare_test.go`

**Interfaces:**

- Consumes: two independently named `Observation` values for one `Scenario`.
- Produces: `Compare(Scenario, Observation, Observation) Attempt` with one of
  `pass`, `uncovered`, `divergent`, `inconclusive`, or `invalid`.

- [ ] **Step 1: Write failing result-classification tests**

```go
func TestCompareClassifiesEquivalentObservations(t *testing.T) {
	attempt := Compare(Scenario{
		ID:      "dns-record-read",
		Surface: catalogparity.SurfaceKey{Kind: catalogparity.ManagedResource, Name: "unifi_dns_record"},
	}, Observation{AdapterID: "released", State: json.RawMessage(`{"id":"1"}`)},
		Observation{AdapterID: "candidate", State: json.RawMessage(`{"id":"1"}`)})
	if attempt.Result != Pass {
		t.Fatalf("result = %q, differences = %#v", attempt.Result, attempt.Differences)
	}
}
```

Add cases proving different adapter IDs are required, malformed JSON is
`invalid`, missing required observation dimensions are `uncovered`, execution
errors are `inconclusive`, semantic differences are `divergent`, and retries
produce additional attempts rather than replacing prior results.

- [ ] **Step 2: Run the tests and verify failure**

Run: `go test ./internal/paritydiff -count=1`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement canonical comparison and fail-closed results**

```go
type Observation struct {
	AdapterID        string          `json:"adapter_id"`
	Request          json.RawMessage `json:"request,omitempty"`
	ControllerResult json.RawMessage `json:"controller_result,omitempty"`
	State            json.RawMessage `json:"state,omitempty"`
	Diagnostics      json.RawMessage `json:"diagnostics,omitempty"`
	Plan             json.RawMessage `json:"plan,omitempty"`
	ExecutionError   string          `json:"execution_error,omitempty"`
}

type AttemptResult string

const (
	Pass         AttemptResult = "pass"
	Uncovered    AttemptResult = "uncovered"
	Divergent    AttemptResult = "divergent"
	Inconclusive AttemptResult = "inconclusive"
	Invalid      AttemptResult = "invalid"
)

type Scenario struct {
	ID                 string                   `json:"id"`
	Surface            catalogparity.SurfaceKey `json:"surface"`
	RequiredDimensions []string                 `json:"required_dimensions"`
}

type Difference struct {
	Dimension string `json:"dimension"`
	Pointer   string `json:"pointer"`
	Baseline  string `json:"baseline,omitempty"`
	Candidate string `json:"candidate,omitempty"`
}

type Attempt struct {
	FormatVersion int          `json:"format_version"`
	Scenario      Scenario     `json:"scenario"`
	BaselineID    string       `json:"baseline_adapter_id"`
	CandidateID   string       `json:"candidate_adapter_id"`
	Result        AttemptResult `json:"result"`
	Differences   []Difference `json:"differences"`
}
```

Canonicalize JSON objects recursively while preserving array order. Scenario
requirements select which observation dimensions must exist for managed,
data-source, list-resource, and action comparisons. Sort differences by JSON
pointer. Never call another adapter after either observation is captured.

- [ ] **Step 4: Run focused tests and commit**

Run: `go test ./internal/paritydiff -count=1`

```bash
git add internal/paritydiff
git commit -m "paritydiff: classify catalog differential attempts"
```

### Task 7: Generalize management contracts across the catalog

**Files:**

- Create: `internal/managementcontract/catalog.go`
- Create: `internal/managementcontract/catalog_test.go`
- Preserve: `internal/managementcontract/contract.go`
- Preserve: `provider-contracts/unifi_dns_record.v1.json`

**Interfaces:**

- Consumes: a complete `catalogparity.Ledger`, the expanded migration-manifest
  digest, exact provider evidence, and per-surface evidence digests.
- Produces: `VerifyCatalog(CatalogContract, catalogparity.Ledger,
  CatalogEvidence) error` without changing the existing DNS `Verify` API.

- [ ] **Step 1: Write failing complete-coverage and binding tests**

```go
func TestVerifyCatalogAccountsForEveryLedgerSurface(t *testing.T) {
	ledger := testCatalogLedger(t)
	contract, evidence := testCatalogContract(t, ledger)
	if err := VerifyCatalog(contract, ledger, evidence); err != nil {
		t.Fatal(err)
	}
	contract.Surfaces = contract.Surfaces[:len(contract.Surfaces)-1]
	if err := VerifyCatalog(contract, ledger, evidence); err == nil || !strings.Contains(err.Error(), "missing surface") {
		t.Fatalf("VerifyCatalog() error = %v, want missing surface", err)
	}
}

func TestVerifyCatalogRequiresEvidenceForAdmittedSurface(t *testing.T) {
	ledger := testCatalogLedger(t)
	contract, evidence := testCatalogContract(t, ledger)
	for i := range contract.Surfaces {
		if contract.Surfaces[i].State == catalogparity.Admitted {
			contract.Surfaces[i].EvidenceSHA256 = ""
		}
	}
	if err := VerifyCatalog(contract, ledger, evidence); err == nil || !strings.Contains(err.Error(), "evidence") {
		t.Fatalf("VerifyCatalog() error = %v, want evidence failure", err)
	}
}
```

Define the catalog helpers in `catalog_test.go`:

```go
func testCatalogLedger(t *testing.T) catalogparity.Ledger {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "provider-codegen", "generated", "catalog-parity-ledger.json"))
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := catalogparity.ParseLedger(data)
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

func testCatalogContract(t *testing.T, ledger catalogparity.Ledger) (CatalogContract, CatalogEvidence) {
	t.Helper()
	provider := testContract().Provider
	contract := CatalogContract{
		FormatVersion:           1,
		Provider:                provider,
		LedgerSHA256:            strings.Repeat("c", 64),
		MigrationManifestSHA256: strings.Repeat("d", 64),
	}
	evidence := CatalogEvidence{
		ProviderBinarySHA256:    provider.Binary.SHA256,
		LedgerSHA256:            contract.LedgerSHA256,
		MigrationManifestSHA256: contract.MigrationManifestSHA256,
		SchemaToolchains:        provider.Schema.Toolchains,
		SurfaceEvidence:         map[catalogparity.SurfaceKey]string{},
	}
	for _, entry := range ledger.Entries {
		surface := SurfaceContract{SurfaceKey: entry.SurfaceKey, State: entry.State}
		if entry.State == catalogparity.Admitted || entry.State == catalogparity.ContractParity || entry.State == catalogparity.ReleaseReady {
			surface.EvidenceSHA256 = strings.Repeat("e", 64)
			evidence.SurfaceEvidence[entry.SurfaceKey] = surface.EvidenceSHA256
		}
		if entry.State == catalogparity.ReleaseReady {
			surface.AttemptResult = "pass"
		}
		contract.Surfaces = append(contract.Surfaces, surface)
	}
	return contract, evidence
}
```

Add cases for duplicate surfaces, ledger digest mismatch, migration-manifest
digest mismatch, provider binary mismatch, schema-toolchain mismatch, state
mismatch, malformed evidence digest, and a `release_ready` claim whose attempt
result is not `pass`.

- [ ] **Step 2: Run the focused tests and verify failure**

Run: `go test ./internal/managementcontract -run TestVerifyCatalog -count=1`

Expected: FAIL because the catalog contract API does not exist.

- [ ] **Step 3: Implement the catalog contract types and verifier**

```go
type CatalogContract struct {
	FormatVersion           int               `json:"format_version"`
	Provider                Provider          `json:"provider"`
	LedgerSHA256            string            `json:"ledger_sha256"`
	MigrationManifestSHA256 string            `json:"migration_manifest_sha256"`
	Surfaces                []SurfaceContract `json:"surfaces"`
}

type SurfaceContract struct {
	catalogparity.SurfaceKey
	State          catalogparity.AdmissionState `json:"state"`
	EvidenceSHA256 string                        `json:"evidence_sha256,omitempty"`
	AttemptResult string                        `json:"attempt_result,omitempty"`
}

type CatalogEvidence struct {
	ProviderBinarySHA256    string
	LedgerSHA256            string
	MigrationManifestSHA256 string
	SchemaToolchains        map[string]SchemaToolchain
	SurfaceEvidence         map[catalogparity.SurfaceKey]string
}
```

Require exactly one contract entry for every ledger entry and require matching
states. `admitted`, `contract_parity`, and `release_ready` entries need bound
evidence. `release_ready` also requires `attempt_result: pass`. Blocking states
remain valid development-contract entries but cannot be promoted. Reuse the
existing provider binary and Terraform/OpenTofu schema binding rules.

- [ ] **Step 4: Run management-contract and provider-contract tests**

Run: `go test ./internal/managementcontract ./provider-contracts -count=1`

Expected: PASS, including the unchanged DNS lighthouse verifier.

- [ ] **Step 5: Commit the generalized contract verifier**

```bash
git add internal/managementcontract/catalog.go internal/managementcontract/catalog_test.go
git commit -m "managementcontract: verify complete catalog contracts"
```

### Task 8: Reconcile Wave 0 evidence and run the no-runtime-change gate

**Files:**

- Create: `internal/catalogparity/artifacts_test.go`
- Create: `build/wave0/catalog-parity.json`
- Modify only if digests changed: `build/m1/dns-compiler-receipt.json`
- Modify only if digests changed: `build/m2/dns-catalog-cutover.json`
- Modify only if digests changed: `build/m3/dns-operation-receipt.json`
- Modify only if digests changed: `provider-contracts/unifi_dns_record.v1.json`
- Modify only if the contract changed: `provider-contracts/unifi_dns_record.v1.sha256`

**Interfaces:**

- Consumes: canonical baseline, generated ledger, expanded migration manifest,
  migration report, DNS compiler outputs, and existing retained receipts.
- Produces: one Wave 0 receipt whose digests are checked by Go tests.

- [ ] **Step 1: Write the failing artifact-binding test**

```go
func TestWave0ReceiptBindsCatalogArtifacts(t *testing.T) {
	receipt := readWave0Receipt(t)
	requireDigestMatches(t, receipt.BaselineSHA256, "../../build/m0/provider-schema-digests.json")
	requireDigestMatches(t, receipt.LedgerSHA256, "../../provider-codegen/generated/catalog-parity-ledger.json")
	requireDigestMatches(t, receipt.MigrationManifestSHA256, "../../provider-codegen/generated/catalog-migration-manifest.json")
	requireDigestMatches(t, receipt.MigrationReportSHA256, "../../provider-codegen/generated/catalog-migration-report.json")
	if receipt.RuntimeChanged {
		t.Fatal("Wave 0 receipt claims a runtime change")
	}
}
```

Define the receipt and digest helpers in the same test file:

```go
type wave0Receipt struct {
	FormatVersion           int            `json:"format_version"`
	Wave                    int            `json:"wave"`
	Result                  string         `json:"result"`
	RuntimeChanged          bool           `json:"runtime_changed"`
	SurfaceCounts           map[string]int `json:"surface_counts"`
	BaselineSHA256          string         `json:"baseline_sha256"`
	LedgerSHA256            string         `json:"ledger_sha256"`
	MigrationManifestSHA256 string         `json:"migration_manifest_sha256"`
	MigrationReportSHA256   string         `json:"migration_report_sha256"`
}

func readWave0Receipt(t *testing.T) wave0Receipt {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "build", "wave0", "catalog-parity.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt wave0Receipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}

func requireDigestMatches(t *testing.T, want, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("digest mismatch for %s", path)
	}
}
```

Also assert exact surface counts, zero unmanifested surfaces, 67 identity
migrations, zero destructive migrations, and no absolute path or restricted
identifier in public generated artifacts.

- [ ] **Step 2: Run the artifact test and verify failure**

Run: `go test ./internal/catalogparity -run TestWave0Receipt -count=1`

Expected: FAIL because the Wave 0 receipt does not exist.

- [ ] **Step 3: Create the receipt and update measured dependent digests**

Record `format_version: 1`, `wave: 0`, `result: pass`,
`runtime_changed: false`, catalog counts, exact artifact SHA-256 values,
compiler output digests, and `migration_strategy_counts.identity: 67`. Update
only receipts whose measured compiler artifacts changed. Do not invent CI,
controller, binary, or lifecycle results.

- [ ] **Step 4: Run deterministic generation and the full local suite**

Run: `go generate ./...`

Stage the intended Task 8 receipt and dependent digest updates, then run:
`git diff --exit-code`.

Run: `go test ./... -count=1`

Run: `go vet ./...`

Run: `git diff --check`

Expected: every command exits zero. The provider registration lists and all
runtime resource files remain unchanged.

- [ ] **Step 5: Commit Wave 0 evidence**

```bash
git add internal/catalogparity/artifacts_test.go build/wave0 provider-codegen/generated provider-contracts build/m1 build/m2 build/m3
git commit -m "build: bind the catalog-wide Wave 0 evidence"
```

## Wave 0 completion checkpoint

Wave 0 is complete when the generated ledger accounts for all 67 released
surfaces, every surface has an explicit migration entry, the compiler requires
admitted catalog state, differential attempts fail closed, generation is
deterministic, all tests pass, and `unifi/provider.go` is unchanged. The next
plan covers Wave 1 data sources and list resources.
