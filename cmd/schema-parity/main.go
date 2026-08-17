// Command schema-parity decides whether the candidate provider's served schema
// may differ from the released one, and reports exactly how.
//
// It replaces the schema half of .woodpecker/scripts/catalog-build-schema.sh.
// That script asserted parity with `cmp`, which reports the first differing
// byte and stops: it could say "differ: char 139633, line 1" and nothing about
// which attribute, how many, or whether anyone intended it.
//
// EVERYTHING IN main() IS ORCHESTRATION. Extracting the released tag, building
// providers, invoking the two CLIs, canonicalising. No judgement lives here.
// Every assertion is a pure function in internal/schemaparity, testable without
// a provider build, a CLI, or a network -- which is what lets the assertions run
// in the fast loop on every push rather than only in a manual pipeline. The gate
// running only manually is how six undeclared schema changes reached a release
// candidate unnoticed.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/schemabaseline"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/schemaparity"
)

type options struct {
	repo         string
	releasedTag  string
	terraformBin string
	tofuBin      string
	ledgerPath   string
	workRoot     string
	output       string
	wantActions  int
	wantLists    int

	// baselinePath is build/m0/provider-baseline.json: the toolchain, platform
	// and CLI identities a promotable build must have been produced with, plus
	// the commit and published-archive digest the released tag must resolve to.
	// It is the EXPECTATION side of every promotion check, which is why nothing
	// derives it from the tree.
	baselinePath string
	// inventoryPath is the evidence inventory this run is about. The receipt
	// records its digest, which is how a later reader can tell whether the
	// inventory has moved since the schema was measured.
	inventoryPath string
	// releasedAuthorityBinary is the PUBLISHED ARCHIVE, when a run has one.
	//
	// Without it the released side is a source rebuild, which is a different
	// authority: rebuilding proves the tag still compiles to the same schema,
	// and the archive is what users actually ran. A run that only rebuilt
	// records released_authority: source_rebuild and blocks promotion, rather
	// than presenting the rebuild as the release.
	releasedAuthorityBinary string

	// Compare-only inputs. When both are set, main does NO orchestration: no
	// tag extraction, no provider build, no CLI invocation. It reads two
	// canonical projections somebody else produced and answers the parity
	// question about them.
	//
	// That mode exists so the shell gate can keep the orchestration it already
	// does. Invoking the full binary from inside that script would build both
	// providers a second time and dump both CLIs again, and the script's own
	// determinism, cross-CLI and inverted-control assertions would then be
	// judging artifacts this binary never saw. Two copies of one fact, on two
	// sets of bytes, is the defect this package was written to remove.
	releasedCanonical  string
	candidateCanonical string
	cli                string

	// treeStateRaw is the JSON from evidence_tree_json, and it has no default.
	//
	// This binary writes a receipt naming candidate_commit, taken from
	// git rev-parse HEAD. On a dirty tree that commit does not describe the
	// bytes that were measured, so the receipt would be honest about what it
	// saw and wrong about what exists -- and it heals silently once the files
	// land, leaving no trace of the window where it was wrong. The measurement
	// stays in tree-state.sh; this side is handed the answer and refuses
	// without one.
	treeStateRaw string
}

func main() {
	var o options
	flag.StringVar(&o.repo, "repo", ".", "repository root holding the candidate tree")
	flag.StringVar(&o.releasedTag, "released-tag", "v0.101.2", "tag whose tree is the released side")
	flag.StringVar(&o.terraformBin, "terraform", "terraform", "terraform binary")
	flag.StringVar(&o.tofuBin, "tofu", "tofu", "tofu binary")
	flag.StringVar(&o.ledgerPath, "ledger", "provider-codegen/schema-changes/v0.101.2-to-next.json",
		"schema change ledger, relative to -repo; a missing file declares nothing")
	flag.StringVar(&o.workRoot, "work", "", "scratch directory (default: a temp dir, removed on exit)")
	flag.StringVar(&o.output, "output", "", "write the receipt here (default: stdout)")
	flag.IntVar(&o.wantActions, "want-actions", 1, "action schemas terraform must report")
	flag.IntVar(&o.wantLists, "want-lists", 25, "list resource schemas terraform must report")
	flag.StringVar(&o.releasedCanonical, "released-canonical", "",
		"compare-only: an already canonicalised released projection")
	flag.StringVar(&o.candidateCanonical, "candidate-canonical", "",
		"compare-only: an already canonicalised candidate projection")
	flag.StringVar(&o.cli, "cli", "terraform", "compare-only: which CLI produced the two projections")
	flag.StringVar(&o.treeStateRaw, "tree-state", "",
		"JSON from evidence_tree_json describing the working tree; required, no default")
	flag.StringVar(&o.baselinePath, "baseline-manifest", "build/m0/provider-baseline.json",
		"promotion expectations, relative to -repo")
	flag.StringVar(&o.inventoryPath, "inventory", "",
		"catalog evidence inventory whose digest this run records")
	flag.StringVar(&o.releasedAuthorityBinary, "released-provider-binary", "",
		"published release archive; without it the released side is a source rebuild and cannot promote")
	flag.Parse()

	if err := run(o); err != nil {
		fmt.Fprintf(os.Stderr, "schema-parity: %v\n", err)
		os.Exit(1)
	}
}

// measureEnvironment reports what this run is, so PromotionBlockers can compare
// it against what the baseline manifest says a promotable run must be.
//
// Every value is measured here and none is defaulted. A missing CLI or an
// unreadable binary is an error rather than an empty string: an empty version
// compares unequal to its pin and would be reported as a version blocker,
// naming the wrong cause for a run that never found the tool at all.
func measureEnvironment(o options, releasedAuthority string) (schemaparity.Environment, error) {
	var env schemaparity.Environment
	env.ReleasedAuthority = releasedAuthority

	goos, err := goEnv("GOOS")
	if err != nil {
		return env, err
	}
	goarch, err := goEnv("GOARCH")
	if err != nil {
		return env, err
	}
	env.Platform = goos + "/" + goarch
	if env.GoVersion, err = goEnv("GOVERSION"); err != nil {
		return env, err
	}

	for _, cli := range []struct {
		bin     string
		version *string
		digest  *string
	}{
		{o.terraformBin, &env.TerraformVersion, &env.TerraformSHA256},
		{o.tofuBin, &env.TofuVersion, &env.TofuSHA256},
	} {
		version, err := cliVersion(cli.bin)
		if err != nil {
			return env, err
		}
		path, err := exec.LookPath(cli.bin)
		if err != nil {
			return env, fmt.Errorf("locate %s: %w", cli.bin, err)
		}
		digest, err := fileDigest(path)
		if err != nil {
			return env, err
		}
		*cli.version, *cli.digest = version, digest
	}
	return env, nil
}

func goEnv(name string) (string, error) {
	out, err := exec.Command("go", "env", name).Output()
	if err != nil {
		return "", fmt.Errorf("go env %s: %w", name, err)
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return "", fmt.Errorf("go env %s is empty", name)
	}
	return value, nil
}

// sharedSurfaceDigest identifies the surface both CLIs report.
//
// The shell derived this from a jq-rendered file, so the digest here names the
// same content under a different renderer and will not equal the frozen one.
// That is a difference the migration causes and not a defect: nothing compares
// this value against anything, and the alternative -- reproducing jq's exact
// spacing to keep a digest nobody reads -- would tie the Go implementation to
// the formatting of a tool being deleted.
func sharedSurfaceDigest(canonical any) (string, error) {
	encoded, err := catalogparity.MarshalReceipt(sharedSurface(canonical))
	if err != nil {
		return "", err
	}
	return bytesDigest(encoded), nil
}

type projection struct {
	canonical any
	digests   []byte
	raw       map[string]any

	// rawSHA256 digests the CLI's untouched output and canonicalSHA256 digests
	// what canonicalisation made of it. Both are recorded because they answer
	// different questions: the raw digest says which bytes the CLI produced,
	// the canonical one says what the comparison was actually about. A run
	// where the raw digests move and the canonical ones do not is a CLI
	// changing its formatting, which is not a schema change.
	rawSHA256       string
	canonicalSHA256 string
}

func run(o options) error {
	// Parsed before anything else in both modes. tree-state.sh already refuses a
	// dirty tree before this binary is reached, so this is the seam rather than
	// a second measurement: a call site that forgot the flag fails here instead
	// of producing a receipt nobody can check.
	treeState, err := catalogparity.ParseTreeState(o.treeStateRaw)
	if err != nil {
		return err
	}

	if o.releasedCanonical != "" || o.candidateCanonical != "" {
		return compareOnly(o)
	}

	repo, err := filepath.Abs(o.repo)
	if err != nil {
		return err
	}

	work := o.workRoot
	if work == "" {
		work, err = os.MkdirTemp("", "schema-parity-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(work)
	}

	ledger, err := schemaparity.LoadLedger(filepath.Join(repo, o.ledgerPath))
	if err != nil {
		return err
	}

	// The released side is the tag's tree, rebuilt. The frozen baseline in
	// provider-contracts/schema is NOT used as the released side here, and that
	// is deliberate: rebuilding is what proves the frozen file still describes
	// the tag. Nothing else checks that, and dropping it would let the anchor
	// drift silently while every comparison against it kept passing.
	releasedSrc := filepath.Join(work, "released-source")
	if err := os.MkdirAll(releasedSrc, 0o750); err != nil {
		return err
	}
	if err := extractTag(repo, o.releasedTag, releasedSrc); err != nil {
		return err
	}

	baseline, err := schemaparity.LoadBaselineManifest(filepath.Join(repo, o.baselinePath))
	if err != nil {
		return err
	}

	var findings []schemaparity.Finding

	// PROVENANCE. The manifest says which commit the released tag names; this
	// resolves the tag and requires them to agree. Taking the commit from git
	// alone -- which this binary did until now -- makes the released side of
	// every comparison below an unidentified tree, and the receipt would report
	// whatever it found as though it were the release.
	resolvedTag, err := gitOutput(repo, "rev-parse", o.releasedTag+"^{commit}")
	if err != nil {
		return err
	}
	findings = append(findings, schemaparity.CheckProvenance(
		o.releasedTag, baseline.Provider.ReleasedCommit, resolvedTag)...)
	releasedCommit := baseline.Provider.ReleasedCommit
	candidateCommit, err := gitOutput(repo, "rev-parse", "HEAD")
	if err != nil {
		return err
	}

	const cleanBuildsPerSide = 2
	rebuilt := map[string]string{}
	for label, src := range map[string]string{"released": releasedSrc, "candidate": repo} {
		first, second, err := buildTwice(work, label, src)
		if err != nil {
			return err
		}
		findings = append(findings, schemaparity.CheckDeterminism(label, first, second)...)
		rebuilt[label] = filepath.Join(work, label+"-one")
	}

	// WHICH BINARY THE RELEASED SCHEMA COMES FROM is a choice, not a detail.
	//
	// With a published archive the released projection is dumped from the
	// bytes users ran. Without one it comes from a rebuild of the tag, which
	// answers a different question -- whether the tag still compiles to that
	// schema -- and is recorded as a different authority so a reader can tell
	// the two apart. The blocker for source_rebuild lives in PromotionBlockers;
	// here the run only has to be honest about which it used.
	releasedAuthority := "source_rebuild"
	releasedStaged := rebuilt["released"]
	if o.releasedAuthorityBinary != "" {
		releasedAuthority = "published_archive"
		releasedStaged = o.releasedAuthorityBinary
		injected, err := fileDigest(releasedStaged)
		if err != nil {
			return err
		}
		findings = append(findings, schemaparity.CheckProvenance(
			"published-archive", baseline.Provider.ReleaseBinarySHA256, injected)...)
	}
	authoritySHA256, err := fileDigest(releasedStaged)
	if err != nil {
		return err
	}
	releasedRebuildSHA256, err := fileDigest(rebuilt["released"])
	if err != nil {
		return err
	}
	candidateSHA256, err := fileDigest(rebuilt["candidate"])
	if err != nil {
		return err
	}

	clis := map[string]string{"terraform": o.terraformBin, "tofu": o.tofuBin}
	proj := map[string]map[string]projection{}
	for label, binary := range map[string]string{"released": releasedStaged, "candidate": rebuilt["candidate"]} {
		if err := stageProvider(work, label, binary); err != nil {
			return err
		}
		proj[label] = map[string]projection{}
		for cliName, cli := range clis {
			p, err := dumpSchema(work, label, cliName, cli)
			if err != nil {
				return fmt.Errorf("%s/%s: %w", label, cliName, err)
			}
			proj[label][cliName] = p
		}
	}

	// EACH GROUP IS KEPT SEPARATE because the receipt records one boolean per
	// group, and those booleans have to be the outcome of the assertion rather
	// than a value written beside it. The shell wrote all three as literals in
	// its jq template, so the receipt asserted them however the run had gone.
	var parity, cross, inverted []schemaparity.Finding

	// PARITY -- the only assertion that loses byte-identity.
	for _, cliName := range []string{"terraform", "tofu"} {
		parity = append(parity, schemaparity.CheckParity(cliName,
			proj["released"][cliName].canonical, proj["candidate"][cliName].canonical, ledger)...)
	}

	// CROSS-CLI -- byte-identity on the surface both CLIs report.
	for _, label := range []string{"released", "candidate"} {
		cross = append(cross, schemaparity.CheckCrossCLI(label,
			sharedSurface(proj[label]["terraform"].canonical),
			proj[label]["tofu"].canonical)...)
	}

	// INVERTED CONTROL -- fails when the two projections MATCH. See its comment.
	inverted = schemaparity.CheckProjectionsDiffer(
		proj["candidate"]["terraform"].canonical, proj["candidate"]["tofu"].canonical)

	findings = append(findings, parity...)
	findings = append(findings, cross...)
	findings = append(findings, inverted...)
	findings = append(findings, schemaparity.CheckShape(
		proj["candidate"]["terraform"].raw, proj["candidate"]["tofu"].raw, o.wantActions, o.wantLists)...)

	env, err := measureEnvironment(o, releasedAuthority)
	if err != nil {
		return err
	}
	sharedSHA256, err := sharedSurfaceDigest(proj["candidate"]["terraform"].canonical)
	if err != nil {
		return err
	}
	// Named rather than left to fail as `open : no such file`. The receipt
	// records this digest so a later reader can tell whether the inventory
	// moved since the schema was measured, and an empty path means nobody
	// decided which inventory this run is about.
	if o.inventoryPath == "" {
		return fmt.Errorf("-inventory is required: the receipt records which evidence inventory " +
			"this run was measured against, and there is no sensible default for that")
	}
	inventorySHA256, err := fileDigest(o.inventoryPath)
	if err != nil {
		return err
	}

	receipt := schemaparity.BuildSchemaReceipt(schemaparity.SchemaRun{
		SourceCommit:   candidateCommit,
		ReleasedCommit: releasedCommit,
		Platform:       env.Platform,
		GoVersion:      env.GoVersion,

		ReleasedSourceRebuildSHA256: releasedRebuildSHA256,
		ReleasedAuthority:           releasedAuthority,
		ReleasedAuthoritySHA256:     authoritySHA256,
		CandidateSHA256:             candidateSHA256,

		Terraform: catalogparity.SchemaCLIReceipt{
			Version:            env.TerraformVersion,
			BinarySHA256:       env.TerraformSHA256,
			ReleasedRawSHA256:  proj["released"]["terraform"].rawSHA256,
			CandidateRawSHA256: proj["candidate"]["terraform"].rawSHA256,
			CanonicalSHA256:    proj["candidate"]["terraform"].canonicalSHA256,
		},
		Tofu: catalogparity.SchemaCLIReceipt{
			Version:            env.TofuVersion,
			BinarySHA256:       env.TofuSHA256,
			ReleasedRawSHA256:  proj["released"]["tofu"].rawSHA256,
			CandidateRawSHA256: proj["candidate"]["tofu"].rawSHA256,
			CanonicalSHA256:    proj["candidate"]["tofu"].canonicalSHA256,
		},

		SharedSchemaSHA256: sharedSHA256,
		InventorySHA256:    inventorySHA256,

		ReleaseToCandidateWithinCLI: len(parity) == 0,
		SharedProjectionEqual:       len(cross) == 0,
		// CheckProjectionsDiffer reports a finding exactly when the two full
		// projections MATCH, so its finding IS this claim. Recomputing the
		// comparison here would put a second copy of the fact beside the
		// assertion that produced it, free to disagree with it.
		FullProjectionEqual: len(inverted) > 0,

		CleanBuilds:       catalogparity.BuildCounts{Released: cleanBuildsPerSide, Candidate: cleanBuildsPerSide},
		TreeState:         treeState,
		PromotionBlockers: schemaparity.PromotionBlockers(env, baseline),
	})

	// FINDINGS GO TO STDERR AND THE EXIT CODE, not into the receipt.
	//
	// BuildSchemaReceipt is decoded with DisallowUnknownFields by around eighty
	// consumers, so a findings key would break every one of them. It is also
	// what the shell did: a failing cmp killed the script and no receipt was
	// written at all. This is the same contract with a better message.
	// MarshalReceipt already terminates with a newline, and it is the only
	// renderer either branch uses, so the file and the log carry the same bytes.
	encoded, err := catalogparity.MarshalReceipt(receipt)
	if err != nil {
		return err
	}
	if o.output != "" {
		if err := os.MkdirAll(filepath.Dir(o.output), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(o.output, encoded, 0o600); err != nil {
			return err
		}
	} else {
		fmt.Print(string(encoded))
	}

	if len(findings) > 0 {
		for _, f := range findings {
			fmt.Fprintln(os.Stderr, f.String())
		}
		return fmt.Errorf("%d assertion(s) failed", len(findings))
	}
	fmt.Fprintf(os.Stderr, "schema-parity: pass. %d declared schema change(s), all observed.\n", len(ledger.Entries))
	return nil
}

// sharedSurface drops the keys terraform reports and tofu does not, so the two
// can be compared on what they have in common. The inverted control asserts
// separately that they still differ on the full projection.
func sharedSurface(canonical any) any {
	doc, ok := canonical.(map[string]any)
	if !ok {
		return canonical
	}
	shared := make(map[string]any, len(doc))
	for k, v := range doc {
		if k == "action_schemas" || k == "list_resource_schemas" {
			continue
		}
		shared[k] = v
	}
	return shared
}

func extractTag(repo, tag, dest string) error {
	archive := exec.Command("git", "-C", repo, "archive", "--format=tar", tag)
	untar := exec.Command("tar", "-x", "-C", dest)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	untar.Stdin = pipe
	if err := untar.Start(); err != nil {
		return err
	}
	if err := archive.Run(); err != nil {
		return fmt.Errorf("git archive %s: %w", tag, err)
	}
	return untar.Wait()
}

// buildTwice returns both digests so the caller can assert determinism. It does
// not compare them itself: the assertion belongs with the other assertions,
// where a test can exercise it without building anything.
func buildTwice(work, label, src string) (string, string, error) {
	var digests [2]string
	for i, suffix := range []string{"one", "two"} {
		target := filepath.Join(work, label+"-"+suffix)
		cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-buildid=", "-o", target, ".")
		cmd.Dir = src
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-mod=readonly", "GOTOOLCHAIN=local")
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", "", fmt.Errorf("build %s (%s): %w\n%s", label, suffix, err, out)
		}
		sum, err := fileDigest(target)
		if err != nil {
			return "", "", err
		}
		digests[i] = sum
	}
	return digests[0], digests[1], nil
}

func stageProvider(work, label, binary string) error {
	dir := filepath.Join(work, "provider", label)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Clean(binary))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "terraform-provider-unifi_v0.101.2"), raw, 0o700)
}

func dumpSchema(work, label, cliName, cli string) (projection, error) {
	fixture := filepath.Join(work, "fixture")
	if err := os.MkdirAll(fixture, 0o750); err != nil {
		return projection{}, err
	}
	mainTF := fmt.Sprintf(`terraform {
  required_providers {
    unifi = {
      source  = %q
      version = "0.101.2"
    }
  }
}
provider "unifi" {}
`, catalogparity.CanonicalProviderAddress)
	if err := os.WriteFile(filepath.Join(fixture, "main.tf"), []byte(mainTF), 0o600); err != nil {
		return projection{}, err
	}
	rc := fmt.Sprintf(`provider_installation {
  dev_overrides { %q = %q }
  direct { exclude = [%q] }
}
`, catalogparity.CanonicalProviderAddress, filepath.Join(work, "provider", label), catalogparity.CanonicalProviderAddress)
	rcPath := filepath.Join(fixture, label+".tfrc")
	if err := os.WriteFile(rcPath, []byte(rc), 0o600); err != nil {
		return projection{}, err
	}

	cmd := exec.Command(cli, "-chdir="+fixture, "providers", "schema", "-json")
	cmd.Env = append(os.Environ(),
		"CHECKPOINT_DISABLE=1", "TF_IN_AUTOMATION=1", "GIT_TERMINAL_PROMPT=0",
		"TF_CLI_CONFIG_FILE="+rcPath,
		"TF_DATA_DIR="+filepath.Join(fixture, "."+label+"-"+cliName))
	out, err := cmd.Output()
	if err != nil {
		return projection{}, fmt.Errorf("%s providers schema: %w", cliName, err)
	}

	canonicalRaw, digests, err := schemabaseline.Canonicalize(out, catalogparity.CanonicalProviderAddress)
	if err != nil {
		return projection{}, fmt.Errorf("canonicalize %s/%s: %w", label, cliName, err)
	}
	// Persist both projections beside the run. They are the evidence behind
	// every finding, and a reader who wants to know what "six differences"
	// means needs the two files rather than a summary of them. With -work they
	// outlive the process; without it they go with the temp directory.
	for name, blob := range map[string][]byte{
		label + "." + cliName + ".raw.json":       out,
		label + "." + cliName + ".canonical.json": canonicalRaw,
		label + "." + cliName + ".digests.json":   digests,
	} {
		if err := os.WriteFile(filepath.Join(work, name), blob, 0o600); err != nil {
			return projection{}, err
		}
	}
	var canonical, rawDoc any
	if err := json.Unmarshal(canonicalRaw, &canonical); err != nil {
		return projection{}, err
	}
	if err := json.Unmarshal(canonicalRaw, &rawDoc); err != nil {
		return projection{}, err
	}
	doc, _ := rawDoc.(map[string]any)
	return projection{
		canonical:       canonical,
		digests:         digests,
		raw:             doc,
		rawSHA256:       bytesDigest(out),
		canonicalSHA256: bytesDigest(canonicalRaw),
	}, nil
}

func bytesDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func fileDigest(path string) (string, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func gitOutput(repo string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// cliVersion reads `version -json`, not the first line of `version`.
//
// The bare command prints "Terraform v1.15.8" and the receipt records
// "1.15.8" -- the string the baseline manifest pins. Taking the human line
// would put a version in the receipt that never equals its expectation, so
// PromotionBlockers would name terraform_version on every run and the gate
// would be one that cannot pass. OpenTofu reports itself under the same
// terraform_version key, which is why one decode serves both.
func cliVersion(bin string) (string, error) {
	out, err := exec.Command(bin, "version", "-json").Output()
	if err != nil {
		return "", fmt.Errorf("%s version -json: %w", bin, err)
	}
	var reported struct {
		Version string `json:"terraform_version"`
	}
	if err := json.Unmarshal(out, &reported); err != nil {
		return "", fmt.Errorf("parse %s version -json: %w", bin, err)
	}
	if reported.Version == "" {
		return "", fmt.Errorf("%s reported no terraform_version", bin)
	}
	return reported.Version, nil
}

// compareOnly answers the parity question about two projections that already
// exist, and asserts nothing else.
//
// It deliberately does NOT re-run determinism, cross-CLI or the inverted
// control. Those are separate claims about artifacts the caller produced and
// still holds; asserting them here as well would put a second copy of each in a
// second place, judging a second set of bytes.
func compareOnly(o options) error {
	if o.releasedCanonical == "" || o.candidateCanonical == "" {
		return fmt.Errorf("compare-only needs both -released-canonical and -candidate-canonical")
	}
	ledger, err := schemaparity.LoadLedger(o.ledgerPath)
	if err != nil {
		return err
	}
	released, err := readJSON(o.releasedCanonical)
	if err != nil {
		return err
	}
	candidate, err := readJSON(o.candidateCanonical)
	if err != nil {
		return err
	}
	findings := schemaparity.CheckParity(o.cli, released, candidate, ledger)
	if len(findings) > 0 {
		for _, f := range findings {
			fmt.Fprintln(os.Stderr, f.String())
		}
		return fmt.Errorf("%d assertion(s) failed", len(findings))
	}
	fmt.Fprintf(os.Stderr, "schema-parity/%s: pass. %d declared schema change(s), all observed.\n",
		o.cli, len(ledger.Entries))
	return nil
}

func readJSON(path string) (any, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return doc, nil
}
