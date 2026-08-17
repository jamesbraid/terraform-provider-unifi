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

	// Digests-only inputs, the same shape as compare-only: two artifacts the
	// caller already produced, and the parity question about them one
	// abstraction up.
	releasedDigests  string
	candidateDigests string
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
	flag.StringVar(&o.releasedDigests, "released-digests", "",
		"digests-only: the committed released digests")
	flag.StringVar(&o.candidateDigests, "candidate-digests", "",
		"digests-only: the freshly built candidate digests")
	flag.StringVar(&o.treeStateRaw, "tree-state", "",
		"JSON from evidence_tree_json describing the working tree; required, no default")
	flag.Parse()

	if err := run(o); err != nil {
		fmt.Fprintf(os.Stderr, "schema-parity: %v\n", err)
		os.Exit(1)
	}
}

// Receipt is a Go struct rather than a forty-line jq expression.
//
// The shape it replaces was assembled by shell, which made it unreviewable and
// untestable: nothing could construct one in a test, so nothing ever asserted
// what it contained.
type Receipt struct {
	FormatVersion   int                      `json:"format_version"`
	Gate            string                   `json:"gate"`
	TreeState       *catalogparity.TreeState `json:"tree_state,omitempty"`
	Result          string                   `json:"result"`
	ReleasedTag     string                   `json:"released_tag"`
	ReleasedCommit  string                   `json:"released_commit"`
	CandidateCommit string                   `json:"candidate_commit"`
	Terraform       string                   `json:"terraform_version"`
	Tofu            string                   `json:"tofu_version"`
	DeclaredChanges int                      `json:"declared_schema_changes"`
	Findings        []string                 `json:"findings"`
}

type projection struct {
	canonical any
	digests   []byte
	raw       map[string]any
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

	if o.releasedDigests != "" || o.candidateDigests != "" {
		return digestsOnly(o)
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

	var findings []schemaparity.Finding

	releasedCommit, err := gitOutput(repo, "rev-parse", o.releasedTag+"^{commit}")
	if err != nil {
		return err
	}
	candidateCommit, err := gitOutput(repo, "rev-parse", "HEAD")
	if err != nil {
		return err
	}

	builds := map[string]string{}
	for label, src := range map[string]string{"released": releasedSrc, "candidate": repo} {
		first, second, err := buildTwice(work, label, src)
		if err != nil {
			return err
		}
		findings = append(findings, schemaparity.CheckDeterminism(label, first, second)...)
		builds[label] = filepath.Join(work, label+"-one")
	}

	clis := map[string]string{"terraform": o.terraformBin, "tofu": o.tofuBin}
	proj := map[string]map[string]projection{}
	for label, binary := range builds {
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

	// PARITY -- the only assertion that loses byte-identity.
	for _, cliName := range []string{"terraform", "tofu"} {
		findings = append(findings, schemaparity.CheckParity(cliName,
			proj["released"][cliName].canonical, proj["candidate"][cliName].canonical, ledger)...)
	}

	// CROSS-CLI -- byte-identity on the surface both CLIs report.
	for _, label := range []string{"released", "candidate"} {
		findings = append(findings, schemaparity.CheckCrossCLI(label,
			sharedSurface(proj[label]["terraform"].canonical),
			proj[label]["tofu"].canonical)...)
	}

	// INVERTED CONTROL -- fails when the two projections MATCH. See its comment.
	findings = append(findings, schemaparity.CheckProjectionsDiffer(
		proj["candidate"]["terraform"].canonical, proj["candidate"]["tofu"].canonical)...)

	findings = append(findings, schemaparity.CheckShape(
		proj["candidate"]["terraform"].raw, proj["candidate"]["tofu"].raw, o.wantActions, o.wantLists)...)

	tfVersion, _ := cliVersion(o.terraformBin)
	tofuVersion, _ := cliVersion(o.tofuBin)

	receipt := Receipt{
		FormatVersion: 1, Gate: "schema-parity",
		TreeState:       treeState,
		Result:          "pass",
		ReleasedTag:     o.releasedTag,
		ReleasedCommit:  releasedCommit,
		CandidateCommit: candidateCommit,
		Terraform:       tfVersion, Tofu: tofuVersion,
		DeclaredChanges: len(ledger.Entries),
	}
	for _, f := range findings {
		receipt.Findings = append(receipt.Findings, f.String())
	}
	if len(findings) > 0 {
		receipt.Result = "fail"
	}

	encoded, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	if o.output != "" {
		if err := os.WriteFile(o.output, append(encoded, '\n'), 0o600); err != nil {
			return err
		}
	} else {
		fmt.Println(string(encoded))
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
	return projection{canonical: canonical, digests: digests, raw: doc}, nil
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

func cliVersion(bin string) (string, error) {
	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]), nil
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

// digestsOnly answers the parity question about two digest artifacts.
//
// Same shape as compareOnly and the same reason: the caller already built both,
// and re-deriving them here would put a second copy of the fact on a second set
// of bytes.
func digestsOnly(o options) error {
	if o.releasedDigests == "" || o.candidateDigests == "" {
		return fmt.Errorf("digests-only needs both -released-digests and -candidate-digests")
	}
	ledger, err := schemaparity.LoadLedger(o.ledgerPath)
	if err != nil {
		return err
	}
	var released, candidate schemaparity.Digests
	for _, pair := range []struct {
		path string
		into *schemaparity.Digests
	}{{o.releasedDigests, &released}, {o.candidateDigests, &candidate}} {
		raw, err := os.ReadFile(filepath.Clean(pair.path))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, pair.into); err != nil {
			return fmt.Errorf("parse %s: %w", pair.path, err)
		}
	}
	findings := schemaparity.CheckDigests(released, candidate, ledger)
	if len(findings) > 0 {
		for _, f := range findings {
			fmt.Fprintln(os.Stderr, f.String())
		}
		return fmt.Errorf("%d assertion(s) failed", len(findings))
	}
	fmt.Fprintf(os.Stderr, "schema-digests: pass. %d declared change(s), and the surfaces that moved are exactly those declared.\n",
		len(ledger.Entries))
	return nil
}
