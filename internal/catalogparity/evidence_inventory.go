package catalogparity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
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

type SurfaceEvidenceInventory struct {
	SurfaceKey
	Wave           int            `json:"wave"`
	Runtime        FileComparison `json:"runtime"`
	Tests          FileComparison `json:"tests"`
	ScenarioOwner  string         `json:"scenario_owner"`
	TestFunctions  []string       `json:"test_functions"`
	Signals        TestSignals    `json:"signals"`
	MissingSignals []string       `json:"missing_signals"`
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
		functions, fileData, err := parseTestFunctions(filepath.Join(input.CandidateRoot, filepath.FromSlash(testPath)))
		if err != nil {
			return EvidenceInventory{}, fmt.Errorf("scenario owner for %s/%s: %w", surface.Kind, surface.Name, err)
		}
		signals := classifyTestSignals(surface.Kind, functions, fileData)
		missing := missingTestSignals(surface.Kind, signals)
		entry := SurfaceEvidenceInventory{
			SurfaceKey:     surface.SurfaceKey,
			Wave:           contract.Wave,
			Runtime:        runtime,
			Tests:          tests,
			ScenarioOwner:  testPath,
			TestFunctions:  functions,
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
