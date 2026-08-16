package catalogparity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type FileStatus string

const (
	FileIdentical FileStatus = "identical"
	FileChanged   FileStatus = "changed"
)

type SDKComparison struct {
	ModulePath             string `json:"module_path"`
	ReleasedVersion        string `json:"released_version"`
	ReleasedArchiveSHA256  string `json:"released_archive_sha256"`
	CandidateVersion       string `json:"candidate_version"`
	CandidateArchiveSHA256 string `json:"candidate_archive_sha256"`
}

type ReleasedProvider struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

type EvidenceInventoryInput struct {
	Baseline         Baseline
	Contracts        SurfaceContractCorpus
	ReleasedRoot     string
	CandidateRoot    string
	ReleasedProvider ReleasedProvider
	SDK              SDKComparison
}

type FileComparison struct {
	Path            string     `json:"path"`
	Status          FileStatus `json:"status"`
	ReleasedSHA256  string     `json:"released_sha256"`
	CandidateSHA256 string     `json:"candidate_sha256"`
}

type TestSignals struct {
	Constructor      bool `json:"constructor"`
	Acceptance       bool `json:"acceptance"`
	Import           bool `json:"import"`
	ListAcceptance   bool `json:"list_acceptance"`
	ActionAcceptance bool `json:"action_acceptance"`
}

type ScenarioStatus string

const (
	ScenarioIdentical ScenarioStatus = "identical"
	ScenarioChanged   ScenarioStatus = "changed"
	// ScenarioAdded means the candidate declares this acceptance test and the
	// released tree does not. It is deliberately not "changed": a scenario the
	// released provider never had cannot judge it, and calling that a change
	// invites reading a red as drift when it is absence.
	ScenarioAdded ScenarioStatus = "added"
)

// ScenarioComparison compares ONE acceptance test function between the
// released and candidate trees.
//
// It exists because file identity is the wrong granularity and was quietly
// standing in for this. A scenario owner file holds acceptance tests, which
// drive the provider through HCL, alongside unit tests, which reach into
// provider internals. Converting a surface necessarily changes the unit tests
// and need not touch the acceptance tests at all -- yet the file digest moves,
// and every scenario in it loses its standing.
//
// Measured on this tree, eight of the nine acceptance tests that survived from
// the released provider are BYTE-IDENTICAL to it, and all eight sat in files
// marked changed. They were being disqualified by their file-mates.
//
// The digest covers the function declaration only, from `func` to its closing
// brace, excluding any preceding doc comment. A comment above a test is not
// part of what it does; a comment inside it is, and stays in the digest.
type ScenarioComparison struct {
	Name string `json:"name"`
	// File names the scenario owner this function was read from. A surface can
	// own several, and a reader holding only the function name cannot say which
	// file has to be grafted to obtain it.
	File            string         `json:"file"`
	Status          ScenarioStatus `json:"status"`
	ReleasedSHA256  string         `json:"released_sha256,omitempty"`
	CandidateSHA256 string         `json:"candidate_sha256"`
}

type SurfaceEvidenceInventory struct {
	SurfaceKey
	Wave    int            `json:"wave"`
	Runtime FileComparison `json:"runtime"`
	// Generated is every file OUTSIDE the runtime path that decides what this
	// surface serves -- in practice its package under internal/generated.
	//
	// Runtime alone stopped being sufficient when surfaces began delegating.
	// evidencePaths names one file, unifi/<base>.go, and 38 of the 41 files in
	// unifi/ that assign resp.Schema now assign it from a generated package. So
	// a schema-only change leaves the runtime digest untouched: measured on
	// dns_record, an attribute ceasing to be Required left
	// unifi/dns_record_resource.go at 3ffe7e13167de315 before and after, while
	// the served schema moved.
	//
	// That matters because sourceIdentity holds on an identical runtime and
	// means the released result carries over. Without this, a changed schema
	// inherits evidence from a provider that served a different one.
	//
	// EMPTY IS MEANINGFUL AND IS NOT THE SAME AS ABSENT. A surface serving a
	// hand-written schema has no generated package, and for those the runtime
	// path really is the whole story. Three surfaces are in that position today.
	Generated []FileComparison `json:"generated,omitempty"`
	Tests     FileComparison   `json:"tests"`
	// ScenarioOwners is every file this surface's acceptance evidence lives in,
	// sorted, and it replaced a single scenario_owner string.
	//
	// One file per surface was never a decision, only an assumption nobody had
	// needed to break: a surface whose acceptance tests do not exist on the
	// released side has to put them somewhere, and putting them in the
	// conventional test file makes that file differ, which under the old
	// single-file rule disqualified every scenario in it at once.
	//
	// A file may be owned by MORE THAN ONE SURFACE and that is not a defect.
	// evidencePaths maps a managed resource and its list companion to the same
	// base, so unifi/firewall_policy_resource_test.go is legitimately named by
	// both. Any check over these must therefore require at least one owner per
	// surface, and must NOT require at most one surface per owner -- a mistake
	// worth naming because the obvious both-directions guard fails on the
	// existing tree.
	ScenarioOwners []string `json:"scenario_owners"`
	TestFunctions  []string `json:"test_functions"`
	// Scenarios compares every TestAcc function in the scenario owner, without
	// applying the plan's kind filter. The inventory measures; deciding which
	// of them speak for a managed surface and which for its list companion is
	// the plan's job, and duplicating that rule here would be a second copy of
	// it.
	// omitempty is load-bearing, not tidiness. Catalog admission re-marshals a
	// parsed inventory and requires the result to reproduce the committed
	// file's bytes, so a field that always serialises would invalidate every
	// inventory generated before scenarios existed -- including the committed
	// one, which nobody in this lane can regenerate. Omitted when absent, the
	// old bytes still round-trip and the new field appears only once something
	// has measured it.
	Scenarios      []ScenarioComparison `json:"scenarios,omitempty"`
	Signals        TestSignals          `json:"signals"`
	MissingSignals []string             `json:"missing_signals"`
}

// Scenario returns the comparison for one acceptance test, and whether the
// inventory carries one at all. The second result matters: an inventory built
// before scenarios existed reports nothing, and a caller must be able to tell
// that from "measured, and it differs".
func (s SurfaceEvidenceInventory) Scenario(name string) (ScenarioComparison, bool) {
	for _, scenario := range s.Scenarios {
		if scenario.Name == name {
			return scenario, true
		}
	}
	return ScenarioComparison{}, false
}

type EvidenceInventory struct {
	FormatVersion              int                        `json:"format_version"`
	ProviderAddress            string                     `json:"provider_address"`
	BaselineSHA256             string                     `json:"baseline_sha256"`
	ReleasedProvider           ReleasedProvider           `json:"released_provider"`
	ReleasedRuntimeTreeSHA256  string                     `json:"released_runtime_tree_sha256"`
	CandidateRuntimeTreeSHA256 string                     `json:"candidate_runtime_tree_sha256"`
	SDK                        SDKComparison              `json:"sdk"`
	CoverageCounts             map[string]int             `json:"coverage_counts"`
	Surfaces                   []SurfaceEvidenceInventory `json:"surfaces"`
}

func BuildEvidenceInventory(input EvidenceInventoryInput) (EvidenceInventory, error) {
	if input.Baseline.FormatVersion != 1 || input.Baseline.ProviderAddress != CanonicalProviderAddress {
		return EvidenceInventory{}, fmt.Errorf("baseline identity is invalid")
	}
	if input.Contracts.ProviderAddress != input.Baseline.ProviderAddress || len(input.Contracts.Contracts) != len(input.Baseline.Surfaces) {
		return EvidenceInventory{}, fmt.Errorf("surface contracts do not match the baseline")
	}
	if err := validateSDKComparison(input.SDK); err != nil {
		return EvidenceInventory{}, err
	}
	if input.ReleasedProvider.Version == "" || len(input.ReleasedProvider.Commit) != 40 {
		return EvidenceInventory{}, fmt.Errorf("released provider identity is incomplete")
	}

	contracts := make(map[SurfaceKey]SurfaceContract, len(input.Contracts.Contracts))
	for _, contract := range input.Contracts.Contracts {
		if _, duplicate := contracts[contract.SurfaceKey]; duplicate {
			return EvidenceInventory{}, fmt.Errorf("duplicate surface contract for %s/%s", contract.Kind, contract.Name)
		}
		contracts[contract.SurfaceKey] = contract
	}

	counts := map[string]int{
		"scenario_owner":    0,
		"constructor":       0,
		"acceptance":        0,
		"import":            0,
		"list_acceptance":   0,
		"action_acceptance": 0,
	}
	surfaces := make([]SurfaceEvidenceInventory, 0, len(input.Baseline.Surfaces))
	for _, surface := range input.Baseline.Surfaces {
		contract, ok := contracts[surface.SurfaceKey]
		if !ok {
			return EvidenceInventory{}, fmt.Errorf("surface contract is missing for %s/%s", surface.Kind, surface.Name)
		}
		runtimePath, testPath, err := evidencePaths(surface.SurfaceKey)
		if err != nil {
			return EvidenceInventory{}, err
		}
		runtime, err := compareEvidenceFile(input.ReleasedRoot, input.CandidateRoot, runtimePath)
		if err != nil {
			return EvidenceInventory{}, fmt.Errorf("runtime for %s/%s: %w", surface.Kind, surface.Name, err)
		}
		tests, err := compareEvidenceFile(input.ReleasedRoot, input.CandidateRoot, testPath)
		if err != nil {
			return EvidenceInventory{}, fmt.Errorf("scenario owner for %s/%s: %w", surface.Kind, surface.Name, err)
		}
		owners, err := scenarioOwners(input.CandidateRoot, surface.SurfaceKey)
		if err != nil {
			return EvidenceInventory{}, fmt.Errorf("scenario owners for %s/%s: %w", surface.Kind, surface.Name, err)
		}
		// Signals are classified over EVERY owner, not just the conventional
		// file. A surface whose acceptance test lives in a companion file would
		// otherwise be reported as having no acceptance signal while the test
		// sits next to it -- which is precisely the case this change exists to
		// support.
		functions := make([]string, 0)
		fileData := make([]byte, 0)
		for _, owner := range owners {
			ownerFunctions, ownerData, err := parseTestFunctions(filepath.Join(input.CandidateRoot, filepath.FromSlash(owner)))
			if err != nil {
				return EvidenceInventory{}, fmt.Errorf("scenario owner %s for %s/%s: %w", owner, surface.Kind, surface.Name, err)
			}
			functions = append(functions, ownerFunctions...)
			fileData = append(fileData, ownerData...)
		}
		sort.Strings(functions)
		scenarios, err := compareScenarios(input.ReleasedRoot, input.CandidateRoot, owners)
		if err != nil {
			return EvidenceInventory{}, fmt.Errorf("scenarios for %s/%s: %w", surface.Kind, surface.Name, err)
		}
		signals := classifyTestSignals(surface.Kind, functions, fileData)
		missing := missingTestSignals(surface.Kind, signals)
		generated, err := compareGeneratedFiles(input.ReleasedRoot, input.CandidateRoot, surface.SurfaceKey)
		if err != nil {
			return EvidenceInventory{}, fmt.Errorf("generated package for %s/%s: %w", surface.Kind, surface.Name, err)
		}
		entry := SurfaceEvidenceInventory{
			SurfaceKey:     surface.SurfaceKey,
			Wave:           contract.Wave,
			Runtime:        runtime,
			Generated:      generated,
			Tests:          tests,
			ScenarioOwners: owners,
			TestFunctions:  functions,
			Scenarios:      scenarios,
			Signals:        signals,
			MissingSignals: missing,
		}
		surfaces = append(surfaces, entry)
		counts["scenario_owner"]++
		if signals.Constructor {
			counts["constructor"]++
		}
		switch surface.Kind {
		case ManagedResource:
			if signals.Acceptance {
				counts["acceptance"]++
			}
			if signals.Import {
				counts["import"]++
			}
		case DataSource:
			if signals.Acceptance {
				counts["acceptance"]++
			}
		case ListResource:
			if signals.ListAcceptance {
				counts["list_acceptance"]++
			}
		case Action:
			if signals.ActionAcceptance {
				counts["action_acceptance"]++
			}
		}
	}
	releasedRuntimeTreeSHA256, candidateRuntimeTreeSHA256 := runtimeTreeDigests(surfaces)
	return EvidenceInventory{
		FormatVersion:              1,
		ProviderAddress:            input.Baseline.ProviderAddress,
		BaselineSHA256:             input.Baseline.SourceSHA256,
		ReleasedProvider:           input.ReleasedProvider,
		ReleasedRuntimeTreeSHA256:  releasedRuntimeTreeSHA256,
		CandidateRuntimeTreeSHA256: candidateRuntimeTreeSHA256,
		SDK:                        input.SDK,
		CoverageCounts:             counts,
		Surfaces:                   surfaces,
	}, nil
}

func runtimeTreeDigests(surfaces []SurfaceEvidenceInventory) (string, string) {
	released := sha256.New()
	candidate := sha256.New()
	for _, surface := range surfaces {
		prefix := string(surface.Kind) + "\x00" + surface.Name + "\x00" + surface.Runtime.Path + "\x00"
		released.Write([]byte(prefix + surface.Runtime.ReleasedSHA256 + "\n"))
		candidate.Write([]byte(prefix + surface.Runtime.CandidateSHA256 + "\n"))
	}
	return hex.EncodeToString(released.Sum(nil)), hex.EncodeToString(candidate.Sum(nil))
}

func (i *EvidenceInventory) Surface(key SurfaceKey) *SurfaceEvidenceInventory {
	for index := range i.Surfaces {
		if i.Surfaces[index].SurfaceKey == key {
			return &i.Surfaces[index]
		}
	}
	return nil
}

func validateSDKComparison(comparison SDKComparison) error {
	if comparison.ModulePath == "" || comparison.ReleasedVersion == "" || comparison.CandidateVersion == "" {
		return fmt.Errorf("SDK comparison identity is incomplete")
	}
	if !validSHA256(comparison.ReleasedArchiveSHA256) || !validSHA256(comparison.CandidateArchiveSHA256) {
		return fmt.Errorf("SDK comparison archive SHA-256 is invalid")
	}
	return nil
}

func evidencePaths(key SurfaceKey) (string, string, error) {
	stem := strings.TrimPrefix(key.Name, "unifi_")
	if stem == key.Name || stem == "" {
		return "", "", fmt.Errorf("surface %s/%s has no path mapping", key.Kind, key.Name)
	}
	var base string
	switch key.Kind {
	case ManagedResource, ListResource:
		base = stem + "_resource"
	case DataSource:
		base = stem + "_data_source"
	case Action:
		base = stem + "_action"
	default:
		return "", "", fmt.Errorf("surface %s/%s has no path mapping", key.Kind, key.Name)
	}
	if key.Name == "unifi_account" {
		base = "account_deprecated"
	}
	return "unifi/" + base + ".go", "unifi/" + base + "_test.go", nil
}

// scenarioOwners lists every acceptance-evidence file a surface owns, sorted,
// starting from the conventional one evidencePaths names.
//
// DERIVED FROM THE TREE, NOT DECLARED IN POLICY. The filesystem already knows
// which files exist; a policy list would be a second copy of that, and a second
// copy of a fact is the defect this project keeps paying for. The rule is the
// existing convention widened by one step: <base>_test.go, plus any sibling
// <base>_<suffix>_test.go.
//
// The cost of deriving rather than declaring is that a file named outside the
// convention is silently not evidence. That is why UnclaimedScenarioFiles
// exists -- it turns the quiet failure into a named one.
func scenarioOwners(candidateRoot string, key SurfaceKey) ([]string, error) {
	_, testPath, err := evidencePaths(key)
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(path.Base(testPath), "_test.go")
	entries, err := os.ReadDir(filepath.Join(candidateRoot, "unifi"))
	if err != nil {
		return nil, err
	}
	owners := make([]string, 0, 2)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		stem := strings.TrimSuffix(name, "_test.go")
		switch {
		case stem == base:
			// The conventional file is the surface's test file by definition
			// and is already what the graft copies today. It stays an owner
			// whether or not it carries an acceptance test, so no surface
			// without a companion changes behaviour at all.
			owners = append(owners, "unifi/"+name)
		case strings.HasPrefix(stem, base+"_"):
			// A COMPANION MUST ACTUALLY CARRY ACCEPTANCE EVIDENCE. Matching the
			// name is not enough, and the tree already holds the case that
			// proves it: unifi/port_action_merge_test.go matches port_action_*,
			// declares NO TestAcc function, and exists only to unit-test
			// mergePortOverride.
			//
			// Enrolling it would have been silent and expensive. action/
			// unifi_port carries the one declared shared-scenario exception --
			// the single piece of grafting machinery with evidence behind it --
			// so the graft would have begun copying a unit-test file bound to a
			// candidate-side function into the released tree. No scenario to
			// gain, a compile failure to lose.
			digests, err := acceptanceScenarioDigests(filepath.Join(candidateRoot, "unifi", name))
			if err != nil {
				return nil, err
			}
			if len(digests) > 0 {
				owners = append(owners, "unifi/"+name)
			}
		}
	}
	if len(owners) == 0 {
		return nil, fmt.Errorf("surface %s/%s owns no scenario file; expected at least %s",
			key.Kind, key.Name, testPath)
	}
	sort.Strings(owners)
	return owners, nil
}

// UnclaimedScenarioFiles returns every file under unifi/ that declares a TestAcc
// function and that no surface's convention claims.
//
// This is the freshness check on the derivation above. Deriving the owner set
// by name is safe only while every acceptance file actually matches the
// convention, and nothing enforces the convention. Without this, a new
// acceptance file with an unexpected name is simply not evidence, the inventory
// is quietly short, and every count computed from it is quietly wrong.
//
// It deliberately does NOT report a file claimed by several surfaces. A managed
// resource and its list companion share a base by design and legitimately name
// the same file.
func UnclaimedScenarioFiles(candidateRoot string, keys []SurfaceKey) ([]string, error) {
	claimed := map[string]struct{}{}
	for _, key := range keys {
		owners, err := scenarioOwners(candidateRoot, key)
		if err != nil {
			return nil, err
		}
		for _, owner := range owners {
			claimed[owner] = struct{}{}
		}
	}
	entries, err := os.ReadDir(filepath.Join(candidateRoot, "unifi"))
	if err != nil {
		return nil, err
	}
	unclaimed := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		relative := "unifi/" + name
		if _, ok := claimed[relative]; ok {
			continue
		}
		digests, err := acceptanceScenarioDigests(filepath.Join(candidateRoot, "unifi", name))
		if err != nil {
			return nil, err
		}
		if len(digests) > 0 {
			unclaimed = append(unclaimed, relative)
		}
	}
	sort.Strings(unclaimed)
	return unclaimed, nil
}

// generatedPackage returns the internal/generated directory a surface delegates
// its schema to, or "" if it serves a hand-written one.
//
// DERIVED FROM THE TREE RATHER THAN DECLARED. The directory either exists or it
// does not, and the naming is the generator's own convention; a policy list
// would be a second copy of a fact the filesystem already holds, which is the
// failure this project keeps paying for. It also means a surface migrating
// starts being covered the moment its package appears, with nothing to remember.
func generatedPackage(key SurfaceKey) string {
	stem := strings.TrimPrefix(key.Name, "unifi_")
	if stem == key.Name || stem == "" {
		return ""
	}
	switch key.Kind {
	case ManagedResource:
		return "internal/generated/resource_" + stem
	case DataSource:
		return "internal/generated/datasource_" + stem
	case ListResource:
		return "internal/generated/listresource_" + stem
	case Action:
		return "internal/generated/action_" + stem
	default:
		return ""
	}
}

// compareGeneratedFiles compares every Go file in a surface's generated package.
//
// A RELEASED FILE THAT DOES NOT EXIST IS NOT AN ERROR, and that is the common
// case rather than the edge one: the conversion created these packages, so at
// v0.101.2 almost none of them are there. Absent on the released side is
// recorded as changed with an empty released digest, which is the truthful
// reading -- it certainly is not identical, and inventing a digest for a file
// that never existed would be worse than leaving the field empty.
//
// The candidate side decides which files exist. A file deleted by the candidate
// therefore disappears from the comparison rather than being reported, which is
// a real limit: a surface that loses a generated file looks the same as one that
// never had it. Catching that needs the released listing too, and the released
// listing is only meaningful once these packages exist on both sides.
func compareGeneratedFiles(releasedRoot, candidateRoot string, key SurfaceKey) ([]FileComparison, error) {
	directory := generatedPackage(key)
	if directory == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(filepath.Join(candidateRoot, filepath.FromSlash(directory)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read generated package %s: %w", directory, err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	comparisons := make([]FileComparison, 0, len(names))
	for _, name := range names {
		relativePath := directory + "/" + name
		candidateData, err := os.ReadFile(filepath.Join(candidateRoot, filepath.FromSlash(relativePath)))
		if err != nil {
			return nil, fmt.Errorf("read candidate %s: %w", relativePath, err)
		}
		candidateDigest := sha256.Sum256(candidateData)
		comparison := FileComparison{
			Path:            relativePath,
			Status:          FileChanged,
			CandidateSHA256: hex.EncodeToString(candidateDigest[:]),
		}
		releasedData, err := os.ReadFile(filepath.Join(releasedRoot, filepath.FromSlash(relativePath)))
		switch {
		case err == nil:
			releasedDigest := sha256.Sum256(releasedData)
			comparison.ReleasedSHA256 = hex.EncodeToString(releasedDigest[:])
			if releasedDigest == candidateDigest {
				comparison.Status = FileIdentical
			}
		case os.IsNotExist(err):
			// Left as changed with no released digest.
		default:
			return nil, fmt.Errorf("read released %s: %w", relativePath, err)
		}
		comparisons = append(comparisons, comparison)
	}
	return comparisons, nil
}

func compareEvidenceFile(releasedRoot, candidateRoot, relativePath string) (FileComparison, error) {
	releasedData, err := os.ReadFile(filepath.Join(releasedRoot, filepath.FromSlash(relativePath)))
	if err != nil {
		return FileComparison{}, fmt.Errorf("read released %s: %w", relativePath, err)
	}
	candidateData, err := os.ReadFile(filepath.Join(candidateRoot, filepath.FromSlash(relativePath)))
	if err != nil {
		return FileComparison{}, fmt.Errorf("read candidate %s: %w", relativePath, err)
	}
	releasedDigest := sha256.Sum256(releasedData)
	candidateDigest := sha256.Sum256(candidateData)
	status := FileIdentical
	if releasedDigest != candidateDigest {
		status = FileChanged
	}
	return FileComparison{
		Path:            relativePath,
		Status:          status,
		ReleasedSHA256:  hex.EncodeToString(releasedDigest[:]),
		CandidateSHA256: hex.EncodeToString(candidateDigest[:]),
	}, nil
}

// compareScenarios digests every TestAcc function in the candidate scenario
// owner and compares it with the same function in the released tree.
//
// A released file that does not exist is not an error: the candidate may have
// added a scenario owner outright, and every scenario in it is then added
// rather than changed.
// compareScenarios compares every TestAcc function across ALL of a surface's
// scenario owners.
//
// The result stays a flat list keyed by function name rather than becoming a
// per-file structure, and that is a fact about Go rather than a preference: two
// functions in one package cannot share a name, unifi is one package, so a
// scenario name identifies exactly one function however many files a surface
// owns. Keeping it flat also means the identical-only lending rule reads
// exactly what it read before.
//
// A per-file status would additionally force an invented aggregation -- what a
// surface's status is when one file is identical and another changed -- which
// the lending rule would then have to interpret.
func compareScenarios(releasedRoot, candidateRoot string, relativePaths []string) ([]ScenarioComparison, error) {
	comparisons := make([]ScenarioComparison, 0)
	for _, relativePath := range relativePaths {
		found, err := compareScenariosInFile(releasedRoot, candidateRoot, relativePath)
		if err != nil {
			return nil, err
		}
		comparisons = append(comparisons, found...)
	}
	sort.Slice(comparisons, func(a, b int) bool { return comparisons[a].Name < comparisons[b].Name })
	return comparisons, nil
}

func compareScenariosInFile(releasedRoot, candidateRoot, relativePath string) ([]ScenarioComparison, error) {
	candidate, err := acceptanceScenarioDigests(filepath.Join(candidateRoot, filepath.FromSlash(relativePath)))
	if err != nil {
		return nil, err
	}
	released, err := acceptanceScenarioDigests(filepath.Join(releasedRoot, filepath.FromSlash(relativePath)))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	names := make([]string, 0, len(candidate))
	for name := range candidate {
		names = append(names, name)
	}
	sort.Strings(names)
	comparisons := make([]ScenarioComparison, 0, len(names))
	for _, name := range names {
		comparison := ScenarioComparison{Name: name, File: relativePath, CandidateSHA256: candidate[name]}
		switch before, existed := released[name]; {
		case !existed:
			comparison.Status = ScenarioAdded
		case before == candidate[name]:
			comparison.Status, comparison.ReleasedSHA256 = ScenarioIdentical, before
		default:
			comparison.Status, comparison.ReleasedSHA256 = ScenarioChanged, before
		}
		comparisons = append(comparisons, comparison)
	}
	return comparisons, nil
}

// acceptanceScenarioDigests maps each TestAcc function in a file to the SHA-256
// of its declaration source.
//
// The span runs from the `func` keyword to the closing brace, which excludes a
// doc comment above the function and includes any comment inside it. That is
// the line between prose about a test and the test itself: unifi_device's only
// acceptance-test change adds "state" to ImportStateVerifyIgnore together with
// an inline comment explaining why, and both belong to what the test does.
func acceptanceScenarioDigests(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, data, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	digests := make(map[string]string)
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(function.Name.Name, "TestAcc") {
			continue
		}
		start := fileSet.Position(function.Pos()).Offset
		end := fileSet.Position(function.End()).Offset
		if start < 0 || end > len(data) || start >= end {
			return nil, fmt.Errorf("%s: cannot read the source of %s", path, function.Name.Name)
		}
		digest := sha256.Sum256(data[start:end])
		digests[function.Name.Name] = hex.EncodeToString(digest[:])
	}
	return digests, nil
}

func parseTestFunctions(path string) ([]string, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), path, data, 0)
	if err != nil {
		return nil, nil, err
	}
	functions := make([]string, 0)
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(function.Name.Name, "Test") {
			continue
		}
		functions = append(functions, function.Name.Name)
	}
	sort.Strings(functions)
	return functions, data, nil
}

func classifyTestSignals(kind SurfaceKind, functions []string, data []byte) TestSignals {
	hasImport := strings.Contains(string(data), "ImportState")
	hasAcceptance := false
	hasNonListAcceptance := false
	hasListAcceptance := false
	hasManagedConstructor := false
	hasDataSourceConstructor := false
	hasListConstructor := false
	hasActionConstructor := false
	for _, name := range functions {
		if strings.HasPrefix(name, "TestNew") {
			switch {
			case strings.Contains(name, "ListResource"):
				hasListConstructor = true
			case strings.Contains(name, "DataSource"):
				hasDataSourceConstructor = true
			case strings.Contains(name, "Action"):
				hasActionConstructor = true
			default:
				hasManagedConstructor = true
			}
		}
		if strings.HasPrefix(name, "TestAcc") {
			hasAcceptance = true
			if strings.Contains(name, "List") {
				hasListAcceptance = true
			} else {
				hasNonListAcceptance = true
			}
		}
		if strings.Contains(name, "ImportState") {
			hasImport = true
		}
	}
	switch kind {
	case ManagedResource:
		return TestSignals{Constructor: hasManagedConstructor, Acceptance: hasNonListAcceptance, Import: hasImport}
	case DataSource:
		return TestSignals{Constructor: hasDataSourceConstructor, Acceptance: hasAcceptance}
	case ListResource:
		return TestSignals{Constructor: hasListConstructor, ListAcceptance: hasListAcceptance}
	case Action:
		return TestSignals{Constructor: hasActionConstructor, ActionAcceptance: hasAcceptance}
	default:
		return TestSignals{}
	}
}

func missingTestSignals(kind SurfaceKind, signals TestSignals) []string {
	missing := make([]string, 0, 3)
	if !signals.Constructor {
		missing = append(missing, "constructor")
	}
	switch kind {
	case ManagedResource:
		if !signals.Acceptance {
			missing = append(missing, "acceptance")
		}
		if !signals.Import {
			missing = append(missing, "import")
		}
	case DataSource:
		if !signals.Acceptance {
			missing = append(missing, "acceptance")
		}
	case ListResource:
		if !signals.ListAcceptance {
			missing = append(missing, "list_acceptance")
		}
	case Action:
		if !signals.ActionAcceptance {
			missing = append(missing, "action_acceptance")
		}
		missing = append(missing, "hardware_claim")
	}
	return missing
}
