// Command m3-dns-qualification drives the M3 DNS managed-operation gate: it
// stands up a pinned UniFi controller, runs the full unifi_dns_record lifecycle
// through Terraform and OpenTofu against both the candidate and the released
// provider, replays a v0 state upgrade, and writes the lifecycle receipt that
// cmd/catalog-migration-recovery consumes.
//
// It replaces .woodpecker/scripts/m3-dns-qualification.sh and
// .woodpecker/scripts/m3-evidence-lib.sh.
//
// One behavioural difference from the shell is deliberate and is not a
// regression; see normalizeRecordState in checks.go. Everything else is a
// faithful port, including the pinned digests, which are copied across
// unchanged so the two implementations can be diffed on a tree carrying both.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasequalification"
)

const (
	gateName = "M3 DNS managed-operation qualification"
	platform = "linux/amd64"

	oldProviderCommit  = "eaf41ff7dbf39c01690eaca54e99f7f9cec867b9"
	providerVersion    = "0.101.2"
	oldProviderVersion = "0.41.11"

	providerArchive       = "terraform-provider-unifi_0.101.2_linux_amd64.zip"
	providerArchiveSHA256 = "9954f29512f0049f0e86908e2fc11a8a0e3cb11f980f3468d5e3da4005fab4b8"
	providerURL           = "https://github.com/jamesbraid/terraform-provider-unifi/releases/download/v0.101.2/" + providerArchive

	terraformVersion       = "1.15.8"
	terraformArchive       = "terraform_1.15.8_linux_amd64.zip"
	terraformArchiveSHA256 = "d25ce7b6902013ad905db3d2eab0be4cd905887fe88b81a6171b8d5503c31f3d"
	terraformURL           = "https://releases.hashicorp.com/terraform/1.15.8/" + terraformArchive

	tofuVersion       = "1.12.1"
	tofuArchive       = "tofu_1.12.1_linux_amd64.zip"
	tofuArchiveSHA256 = "1fc9af962e3632b7cd0ba27076cd9f1ced177567defe9e331ac37f5a40468575"
	tofuURL           = "https://github.com/opentofu/opentofu/releases/download/v1.12.1/" + tofuArchive

	goImage = "golang:1.25.8-bookworm@sha256:4557cf171e3cdf5053a298d5171b1a5f5734d920260c25f22c79e94760eb2084"

	controllerProduct        = "UniFi Network"
	controllerVersion        = "10.4.57"
	controllerIndexSHA256    = "sha256:584be3a2e45c4913e1bc373eff9c7330609c82085d4fc6f5ea365abdcdb3e664"
	controllerManifestSHA256 = "sha256:9d19c8d03948a77d28181743fb81a515aa51a5cea0856bc0491b34d92a01bcf4"
	controllerImage          = "ghcr.io/jamesbraid/unifi-network@" + controllerManifestSHA256

	dnsName = "m0-dns.example.invalid"

	healthAttempts = 240
	healthInterval = 5 * time.Second
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

type qualification struct {
	stdout io.Writer
	stderr io.Writer

	sourceCommit    string
	candidateBinary string
	proxyRoot       string
	workRoot        string

	network    string
	controller string
	volumes    map[string]string

	checks releasequalification.DNSLifecycleChecks
}

func run(argv []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("m3-dns-qualification", flag.ContinueOnError)
	flags.SetOutput(stderr)
	runID := flags.String("run-id", "",
		"pipeline number; scopes every Docker resource this run creates")
	sourceCommit := flags.String("source-commit", "",
		"commit the receipt describes; defaults to HEAD")
	candidateBinary := flags.String("candidate-provider", "",
		"prebuilt candidate provider binary; built from source when empty")
	proxyRoot := flags.String("go-unifi-proxy", "/tmp/go-unifi-proxy",
		"file GOPROXY holding the unreleased go-unifi module, used only when building from source")
	outputPath := flags.String("output", "",
		"path to write the lifecycle receipt to")
	treeStateRaw := flags.String("tree-state", "",
		"JSON from evidence_tree_json, generated in the same step as this receipt")
	if err := flags.Parse(argv); err != nil {
		return err
	}

	// No default. A receipt that cannot say which tree it describes is the
	// thing tree_state exists to prevent, so an absent flag is an error rather
	// than an empty field. This is what dissolves the tree-state-coverage
	// exemption the shell carried.
	treeState, err := catalogparity.ParseTreeState(*treeStateRaw)
	if err != nil {
		return err
	}
	if *runID == "" {
		return errors.New("-run-id is required: every Docker resource is named after it, and " +
			"an unscoped run would collide with a concurrent one")
	}
	if *outputPath == "" {
		return errors.New("-output is required")
	}

	resolvedCommit := *sourceCommit
	if resolvedCommit == "" {
		resolvedCommit, err = gitOutput("rev-parse", "HEAD")
		if err != nil {
			return err
		}
	}

	workRoot, err := os.MkdirTemp("", "provider-m3q.")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workRoot)

	prefix := "provider-m3q-" + *runID
	q := &qualification{
		stdout:          stdout,
		stderr:          stderr,
		sourceCommit:    resolvedCommit,
		candidateBinary: *candidateBinary,
		proxyRoot:       *proxyRoot,
		workRoot:        workRoot,
		network:         prefix + "-network",
		controller:      prefix + "-controller",
		volumes:         map[string]string{},
	}
	for _, name := range []string{
		"tools", "config",
		"terraform-state", "tofu-state",
		"terraform-upgrade-state", "tofu-upgrade-state",
		"terraform-legacy-state", "tofu-legacy-state",
		"source", "go-cache", "old-source", "old-go-cache",
	} {
		q.volumes[name] = prefix + "-" + name
	}

	// A SIGKILL never runs a deferred function, and Woodpecker re-queues a
	// killed task under the SAME pipeline number, so the previous attempt's
	// resources are already in the way. The network collides loudly. The
	// volumes do not: docker volume create succeeds for a name that already
	// exists, so a retry would silently reattach the killed run's Terraform and
	// OpenTofu state and produce a receipt describing a run that never
	// finished. Discard everything before creating any of it.
	q.discardScopedResources()
	defer q.discardScopedResources()

	receipt, err := q.execute()
	if err != nil {
		return err
	}
	receipt.TreeState = treeState

	// The ARTIFACT is rendered canonically: sorted keys, two-space indent, one
	// trailing newline. encoding/json emits struct fields in DECLARATION order,
	// so marshalling the struct directly would let a field moved for
	// readability change a release artifact that other gates compare byte for
	// byte.
	//
	// This duplicates catalogparity.MarshalReceipt, which does not exist on this
	// branch yet -- it arrives with the catalog-build-schema port. The algorithm
	// is copied from it deliberately so the two render identical bytes; when
	// that branch merges, delete renderCanonical and call MarshalReceipt.
	encoded, err := renderCanonical(receipt)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*outputPath, encoded, 0o644); err != nil {
		return err
	}

	// The LOG line stays compact and single-line. It is a convenience for
	// grepping a pipeline log, not the artifact, and a receipt spread over
	// twenty lines is worse at that job.
	compact, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "M3_DNS_RECEIPT=%s\n", compact)
	return nil
}

// renderCanonical round-trips through a generic value so that map key ordering,
// not Go struct field order, decides the bytes. json.Marshal sorts map keys; it
// never sorts struct fields, which is the whole reason this exists.
func renderCanonical(value any) ([]byte, error) {
	direct, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal receipt: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(direct))
	decoder.UseNumber()
	var generic any
	if err := decoder.Decode(&generic); err != nil {
		return nil, fmt.Errorf("re-read receipt for canonical ordering: %w", err)
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(generic); err != nil {
		return nil, fmt.Errorf("render canonical receipt: %w", err)
	}
	return out.Bytes(), nil
}

func (q *qualification) execute() (releasequalification.DNSLifecycleReceipt, error) {
	var receipt releasequalification.DNSLifecycleReceipt

	if err := q.verifyPreconditions(); err != nil {
		return receipt, err
	}
	digests, err := q.provisionTools()
	if err != nil {
		return receipt, err
	}
	if err := q.createVolumes(); err != nil {
		return receipt, err
	}
	if err := q.populateVolumes(); err != nil {
		return receipt, err
	}
	if err := q.buildProviders(); err != nil {
		return receipt, err
	}

	if err := q.dockerRun("network", "create", q.network); err != nil {
		return receipt, err
	}
	if err := q.dockerRun("pull", "--platform", platform, controllerImage); err != nil {
		return receipt, err
	}
	controllerConfigSHA256, err := q.dockerOutput("image", "inspect", "--format", "{{.Id}}", controllerImage)
	if err != nil {
		return receipt, err
	}
	digests.candidateProvider, err = q.digestInVolume(
		"/tools/provider/terraform-provider-unifi_v" + providerVersion)
	if err != nil {
		return receipt, err
	}
	digests.oldProvider, err = q.digestInVolume(
		"/tools/provider-old/terraform-provider-unifi_v" + oldProviderVersion)
	if err != nil {
		return receipt, err
	}

	// Four lifecycle passes: each CLI against the candidate provider, then each
	// CLI against the released one. The cross_config argument is the OTHER
	// provider, planned mid-pass against state the first one wrote -- that is
	// the adapter half of the round trip.
	normalized := map[string][]byte{}
	for _, pass := range []struct {
		label       string
		cli         string
		stateVolume string
		cliConfig   string
		crossConfig string
	}{
		{"terraform-current", "terraform", "terraform-state", "cli.tfrc", "cli-legacy.tfrc"},
		{"tofu-current", "tofu", "tofu-state", "cli.tfrc", "cli-legacy.tfrc"},
		{"terraform-legacy", "terraform", "terraform-legacy-state", "cli-legacy.tfrc", "cli.tfrc"},
		{"tofu-legacy", "tofu", "tofu-legacy-state", "cli-legacy.tfrc", "cli.tfrc"},
	} {
		state, err := q.qualifyCLI(pass.cli, pass.stateVolume, pass.cliConfig, pass.crossConfig, pass.label)
		if err != nil {
			return receipt, fmt.Errorf("%s: %w", pass.label, err)
		}
		normalized[pass.label] = state
	}

	for _, upgrade := range []struct {
		cli         string
		stateVolume string
	}{
		{"terraform", "terraform-upgrade-state"},
		{"tofu", "tofu-upgrade-state"},
	} {
		if err := q.qualifyStateUpgrade(upgrade.cli, upgrade.stateVolume); err != nil {
			return receipt, fmt.Errorf("%s state upgrade: %w", upgrade.cli, err)
		}
	}

	// The three comparisons behind bidirectional_adapter_state_round_trip:
	// same record, same normalized attributes, whichever CLI wrote it and
	// whichever provider read it back.
	reference := normalized["terraform-current"]
	for _, label := range []string{"tofu-current", "terraform-legacy", "tofu-legacy"} {
		if !bytes.Equal(reference, normalized[label]) {
			return receipt, fmt.Errorf("normalized state from %s differs from terraform-current:\n"+
				"--- terraform-current\n%s\n--- %s\n%s",
				label, reference, label, normalized[label])
		}
	}
	q.checks.BidirectionalAdapterStateRoundTrip = true

	if err := q.dockerRun("network", "rm", q.network); err != nil {
		return receipt, err
	}
	if q.dockerQuiet("network", "inspect", q.network) == nil {
		return receipt, fmt.Errorf("network %s still resolves after removal", q.network)
	}
	q.checks.Cleanup = true

	if err := q.verifyChecksComplete(); err != nil {
		return receipt, err
	}

	referenceDigest := sha256.Sum256(reference)
	return releasequalification.DNSLifecycleReceipt{
		FormatVersion:        1,
		Gate:                 gateName,
		Result:               "pass",
		SourceCommit:         q.sourceCommit,
		Platform:             platform,
		ProviderVersion:      providerVersion,
		ProviderBinarySHA256: digests.candidateProvider,
		LegacyProvider: releasequalification.ToolArtifact{
			Version:       providerVersion,
			ArchiveSHA256: providerArchiveSHA256,
			BinarySHA256:  digests.legacyProvider,
		},
		StateUpgradeSource: releasequalification.StateUpgradeArtifact{
			Version:      oldProviderVersion,
			Commit:       oldProviderCommit,
			BinarySHA256: digests.oldProvider,
		},
		Terraform: releasequalification.ToolArtifact{
			Version:       terraformVersion,
			ArchiveSHA256: terraformArchiveSHA256,
			BinarySHA256:  digests.terraform,
		},
		Tofu: releasequalification.ToolArtifact{
			Version:       tofuVersion,
			ArchiveSHA256: tofuArchiveSHA256,
			BinarySHA256:  digests.tofu,
		},
		Target: releasequalification.DNSTargetReceipt{
			Product:                controllerProduct,
			Version:                controllerVersion,
			IndexSHA256:            controllerIndexSHA256,
			PlatformManifestSHA256: controllerManifestSHA256,
			ConfigSHA256:           controllerConfigSHA256,
		},
		Lifecycle:                 q.checks,
		NormalizedStateSHA256:     hex.EncodeToString(referenceDigest[:]),
		CLIOutcomesEquivalent:     true,
		AdapterOutcomesEquivalent: true,
	}, nil
}

// verifyChecksComplete is what keeps the thirteen booleans honest. In the shell
// every one of them was the literal `true` in a jq object; they meant "the
// script reached its last line" only because set -e aborted before the receipt
// was written. Here each is set at the point its work succeeds, and this
// refuses to emit a receipt with any of them still false -- so deleting a step
// fails loudly instead of shipping a receipt that claims it ran.
func (q *qualification) verifyChecksComplete() error {
	var missing []string
	for _, check := range []struct {
		name string
		done bool
	}{
		{"fresh_target_per_cli_and_adapter", q.checks.FreshTargetPerCLIAndAdapter},
		{"create", q.checks.Create},
		{"update", q.checks.Update},
		{"omitted_optional_fields", q.checks.OmittedOptionalFields},
		{"configured_optional_fields", q.checks.ConfiguredOptionalFields},
		{"replacement_plan", q.checks.ReplacementPlan},
		{"restart_refresh", q.checks.RestartRefresh},
		{"import", q.checks.Import},
		{"v0_integer_ttl_state_upgrade", q.checks.V0IntegerTTLStateUpgrade},
		{"no_op_plan", q.checks.NoOpPlan},
		{"delete", q.checks.Delete},
		{"cleanup", q.checks.Cleanup},
		{"bidirectional_adapter_state_round_trip", q.checks.BidirectionalAdapterStateRoundTrip},
	} {
		if !check.done {
			missing = append(missing, check.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("refusing to write a receipt: %d lifecycle check(s) never ran: %s",
			len(missing), strings.Join(missing, ", "))
	}
	return nil
}

type toolDigests struct {
	candidateProvider string
	legacyProvider    string
	oldProvider       string
	terraform         string
	tofu              string
}

func (q *qualification) verifyPreconditions() error {
	architecture, err := q.dockerOutput("info", "--format", "{{.Architecture}}")
	if err != nil {
		return err
	}
	if architecture != "x86_64" {
		return fmt.Errorf("docker reports architecture %q, want x86_64: the pinned controller and "+
			"provider archives are linux/amd64 and emulation would make the timings meaningless",
			architecture)
	}
	for _, commit := range []string{q.sourceCommit, oldProviderCommit} {
		if err := gitQuiet("cat-file", "-e", commit+"^{commit}"); err != nil {
			return fmt.Errorf("commit %s is not present in this clone: %w", commit, err)
		}
	}
	return nil
}

func (q *qualification) provisionTools() (toolDigests, error) {
	var digests toolDigests

	for _, download := range []struct {
		url    string
		sha256 string
		name   string
	}{
		{providerURL, providerArchiveSHA256, providerArchive},
		{terraformURL, terraformArchiveSHA256, terraformArchive},
		{tofuURL, tofuArchiveSHA256, tofuArchive},
	} {
		if err := fetchVerified(download.url, download.sha256, filepath.Join(q.workRoot, download.name)); err != nil {
			return digests, err
		}
	}

	toolsRoot := filepath.Join(q.workRoot, "tools")
	for _, directory := range []string{"provider", "provider-legacy", "provider-old"} {
		if err := os.MkdirAll(filepath.Join(toolsRoot, directory), 0o755); err != nil {
			return digests, err
		}
	}
	if err := os.MkdirAll(filepath.Join(q.workRoot, "config"), 0o755); err != nil {
		return digests, err
	}

	if err := unzipInto(filepath.Join(q.workRoot, providerArchive), filepath.Join(toolsRoot, "provider-legacy")); err != nil {
		return digests, err
	}
	for _, archive := range []string{terraformArchive, tofuArchive} {
		if err := unzipInto(filepath.Join(q.workRoot, archive), toolsRoot); err != nil {
			return digests, err
		}
	}

	legacyBinary := filepath.Join(toolsRoot, "provider-legacy", "terraform-provider-unifi_v"+providerVersion)
	for _, binary := range []string{
		filepath.Join(toolsRoot, "terraform"),
		filepath.Join(toolsRoot, "tofu"),
		legacyBinary,
	} {
		if err := os.Chmod(binary, 0o755); err != nil {
			return digests, err
		}
	}

	if q.candidateBinary != "" {
		if err := installPrebuiltCandidate(q.candidateBinary,
			filepath.Join(toolsRoot, "provider", "terraform-provider-unifi_v"+providerVersion)); err != nil {
			return digests, err
		}
	}

	for name, directory := range map[string]string{
		"cli.tfrc":        "/tools/provider",
		"cli-legacy.tfrc": "/tools/provider-legacy",
		"cli-old.tfrc":    "/tools/provider-old",
	} {
		contents := fmt.Sprintf(`provider_installation {
  dev_overrides {
    "registry.terraform.io/ubiquiti-community/unifi" = "%s"
  }
  direct {}
}
`, directory)
		if err := os.WriteFile(filepath.Join(toolsRoot, name), []byte(contents), 0o644); err != nil {
			return digests, err
		}
	}

	if err := copyTree(filepath.Join(".woodpecker", "fixtures", "m3"), filepath.Join(q.workRoot, "config")); err != nil {
		return digests, err
	}

	var err error
	if digests.legacyProvider, err = fileDigest(legacyBinary); err != nil {
		return digests, err
	}
	if digests.terraform, err = fileDigest(filepath.Join(toolsRoot, "terraform")); err != nil {
		return digests, err
	}
	if digests.tofu, err = fileDigest(filepath.Join(toolsRoot, "tofu")); err != nil {
		return digests, err
	}
	return digests, nil
}

func (q *qualification) createVolumes() error {
	for _, volume := range q.volumes {
		if err := q.dockerRun("volume", "create", volume); err != nil {
			return err
		}
	}
	return nil
}

func (q *qualification) populateVolumes() error {
	for directory, volume := range map[string]string{
		filepath.Join(q.workRoot, "tools"):  q.volumes["tools"],
		filepath.Join(q.workRoot, "config"): q.volumes["config"],
	} {
		if err := q.populateVolume(directory, volume); err != nil {
			return err
		}
	}
	return nil
}

func (q *qualification) buildProviders() error {
	if q.candidateBinary == "" {
		// The candidate depends on the unreleased go-unifi module. The workflow
		// bootstraps it into a file GOPROXY on the host, but this build runs in
		// a container, so the proxy is mounted read-only. Without it Go falls
		// through to a direct GitHub lookup that the isolated build network
		// cannot reach.
		if info, err := os.Stat(q.proxyRoot); err != nil || !info.IsDir() {
			return fmt.Errorf("go-unifi proxy root %s is missing: bootstrap it on this host with "+
				"bootstrap-go-unifi-proxy.sh, or pass a prebuilt candidate via -candidate-provider",
				q.proxyRoot)
		}
		if err := q.loadCommitIntoVolume(q.sourceCommit, q.volumes["source"]); err != nil {
			return err
		}
		if err := q.dockerRun("run", "--rm", "--platform", platform,
			"--env", "CGO_ENABLED=0",
			"--env", "GOCACHE=/go/build-cache", "--env", "GOMODCACHE=/go/module-cache",
			"--env", "GOTELEMETRY=off", "--env", "GOTOOLCHAIN=local",
			"--env", "GOPROXY=file:///go-unifi-proxy,https://proxy.golang.org",
			"--env", "GOSUMDB=off", "--env", "GOVCS=*:off", "--env", "GIT_TERMINAL_PROMPT=0",
			"--mount", "type=volume,src="+q.volumes["source"]+",dst=/source,readonly",
			"--mount", "type=volume,src="+q.volumes["go-cache"]+",dst=/go",
			"--mount", "type=volume,src="+q.volumes["tools"]+",dst=/tools",
			"--mount", "type=bind,src="+q.proxyRoot+",dst=/go-unifi-proxy,readonly",
			"--workdir", "/source", goImage,
			"go", "build", "-trimpath", "-buildvcs=false",
			"-o", "/tools/provider/terraform-provider-unifi_v"+providerVersion, "."); err != nil {
			return err
		}
	}

	if err := q.loadCommitIntoVolume(oldProviderCommit, q.volumes["old-source"]); err != nil {
		return err
	}
	return q.dockerRun("run", "--rm", "--platform", platform,
		"--env", "GOCACHE=/go/build-cache", "--env", "GOMODCACHE=/go/module-cache",
		"--env", "GOTELEMETRY=off", "--env", "GOTOOLCHAIN=local",
		"--mount", "type=volume,src="+q.volumes["old-source"]+",dst=/source,readonly",
		"--mount", "type=volume,src="+q.volumes["old-go-cache"]+",dst=/go",
		"--mount", "type=volume,src="+q.volumes["tools"]+",dst=/tools",
		"--workdir", "/source", goImage,
		"go", "build", "-trimpath",
		"-o", "/tools/provider-old/terraform-provider-unifi_v"+oldProviderVersion, ".")
}

// qualifyCLI runs one full lifecycle pass and returns the normalized state it
// ended with.
func (q *qualification) qualifyCLI(cli, stateVolume, cliConfig, crossConfig, label string) ([]byte, error) {
	if err := q.startController(); err != nil {
		return nil, err
	}
	q.checks.FreshTargetPerCLIAndAdapter = true

	// create, from a fixture that omits every optional field
	if err := q.runCLI(cli, stateVolume, "initial", cliConfig,
		"apply", "-auto-approve", "-input=false", "-state=/state/state.tfstate",
		"-var", "dns_name="+dnsName); err != nil {
		return nil, fmt.Errorf("create: %w", err)
	}
	q.checks.Create = true
	q.checks.OmittedOptionalFields = true

	// update, to a fixture that configures them
	if err := q.expectChanges(cli, stateVolume, "updated", "update.tfplan", cliConfig); err != nil {
		return nil, fmt.Errorf("update plan: %w", err)
	}
	if err := q.runCLI(cli, stateVolume, "updated", cliConfig,
		"apply", "-auto-approve", "-input=false", "-state=/state/state.tfstate",
		"-var", "dns_name="+dnsName); err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	if err := q.expectNoChanges(cli, stateVolume, "updated", cliConfig); err != nil {
		return nil, fmt.Errorf("no-op plan after update: %w", err)
	}
	q.checks.Update = true
	q.checks.ConfiguredOptionalFields = true
	q.checks.NoOpPlan = true

	// replacement: a changed name must force create+delete, not an in-place edit
	if err := q.expectChanges(cli, stateVolume, "replacement", "replacement.tfplan", cliConfig); err != nil {
		return nil, fmt.Errorf("replacement plan: %w", err)
	}
	planJSON, err := q.captureCLI(cli, stateVolume, "replacement", cliConfig,
		"show", "-json", "/state/replacement.tfplan")
	if err != nil {
		return nil, err
	}
	if err := assertReplacementPlan(planJSON); err != nil {
		return nil, err
	}
	q.checks.ReplacementPlan = true

	// restart the controller, then prove a refresh sees no drift
	if err := q.dockerRun("restart", q.controller); err != nil {
		return nil, err
	}
	if err := q.waitHealthy(); err != nil {
		return nil, err
	}
	if err := q.runCLI(cli, stateVolume, "updated", cliConfig,
		"apply", "-refresh-only", "-auto-approve", "-input=false",
		"-state=/state/state.tfstate", "-var", "dns_name="+dnsName); err != nil {
		return nil, fmt.Errorf("refresh after restart: %w", err)
	}
	if err := q.expectNoChanges(cli, stateVolume, "updated", cliConfig); err != nil {
		return nil, fmt.Errorf("no-op plan after restart: %w", err)
	}
	q.checks.RestartRefresh = true

	// drop the record from state and import it back by its controller id
	recordID, err := q.captureCLI(cli, stateVolume, "updated", cliConfig,
		"output", "-state=/state/state.tfstate", "-raw", "dns_record_id")
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(recordID)) == 0 {
		return nil, errors.New("output dns_record_id is empty, so there is no id to import by")
	}
	if err := q.runCLI(cli, stateVolume, "updated", cliConfig,
		"state", "rm", "-state=/state/state.tfstate", recordAddress); err != nil {
		return nil, err
	}
	if err := q.runCLI(cli, stateVolume, "imported", cliConfig,
		"import", "-input=false", "-state=/state/state.tfstate",
		"-var", "dns_name="+dnsName, recordAddress, string(bytes.TrimSpace(recordID))); err != nil {
		return nil, fmt.Errorf("import: %w", err)
	}
	if err := q.expectNoChanges(cli, stateVolume, "imported", cliConfig); err != nil {
		return nil, fmt.Errorf("no-op plan after import: %w", err)
	}
	// The adapter half: the OTHER provider planning against the state this one
	// wrote must also see no changes.
	if err := q.expectNoChanges(cli, stateVolume, "imported", crossConfig); err != nil {
		return nil, fmt.Errorf("no-op plan under the cross provider (%s): %w", crossConfig, err)
	}
	if err := q.expectNoChanges(cli, stateVolume, "imported", cliConfig); err != nil {
		return nil, fmt.Errorf("no-op plan after the cross-provider plan: %w", err)
	}
	q.checks.Import = true

	stateJSON, err := q.captureCLI(cli, stateVolume, "imported", cliConfig,
		"show", "-json", "/state/state.tfstate")
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeRecordState(stateJSON)
	if err != nil {
		return nil, err
	}

	if err := q.runCLI(cli, stateVolume, "imported", cliConfig,
		"destroy", "-auto-approve", "-input=false", "-state=/state/state.tfstate",
		"-var", "dns_name="+dnsName); err != nil {
		return nil, fmt.Errorf("destroy: %w", err)
	}
	q.checks.Delete = true

	if err := q.stopController(); err != nil {
		return nil, err
	}
	return normalized, nil
}

// qualifyStateUpgrade applies with the 0.41.11 provider, which writes schema
// version 0 with an integer ttl, then applies the current one over it and
// requires the upgraded state to carry the duration string.
func (q *qualification) qualifyStateUpgrade(cli, stateVolume string) error {
	if err := q.startController(); err != nil {
		return err
	}
	if err := q.runCLI(cli, stateVolume, "upgrade-old", "cli-old.tfrc",
		"apply", "-auto-approve", "-input=false", "-state=/state/state.tfstate",
		"-var", "dns_name="+dnsName); err != nil {
		return err
	}
	v0State, err := q.captureCLI(cli, stateVolume, "upgrade-old", "cli-old.tfrc",
		"show", "-json", "/state/state.tfstate")
	if err != nil {
		return err
	}
	if err := assertUpgradeState(v0State, 0, "300"); err != nil {
		return err
	}

	if err := q.runCLI(cli, stateVolume, "upgrade-current", "cli.tfrc",
		"apply", "-auto-approve", "-input=false", "-state=/state/state.tfstate",
		"-var", "dns_name="+dnsName); err != nil {
		return err
	}
	if err := q.expectNoChanges(cli, stateVolume, "upgrade-current", "cli.tfrc"); err != nil {
		return err
	}
	v1State, err := q.captureCLI(cli, stateVolume, "upgrade-current", "cli.tfrc",
		"show", "-json", "/state/state.tfstate")
	if err != nil {
		return err
	}
	if err := assertUpgradeState(v1State, 1, `"5m0s"`); err != nil {
		return err
	}
	q.checks.V0IntegerTTLStateUpgrade = true

	if err := q.runCLI(cli, stateVolume, "upgrade-current", "cli.tfrc",
		"destroy", "-auto-approve", "-input=false", "-state=/state/state.tfstate",
		"-var", "dns_name="+dnsName); err != nil {
		return err
	}
	return q.stopController()
}

func newDigest() *digestWriter { return &digestWriter{hash: sha256.New()} }

type digestWriter struct{ hash hash.Hash }

func (d *digestWriter) Write(p []byte) (int, error) { return d.hash.Write(p) }
func (d *digestWriter) hex() string                 { return hex.EncodeToString(d.hash.Sum(nil)) }

func fetchVerified(url, wantSHA256, destination string) error {
	response, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: HTTP %s", url, response.Status)
	}

	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer file.Close()

	got, err := copyAndDigest(file, response.Body)
	if err != nil {
		return err
	}
	if got != wantSHA256 {
		return fmt.Errorf("%s has sha256 %s, want %s", url, got, wantSHA256)
	}
	return nil
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return copyAndDigest(io.Discard, file)
}

func unzipInto(archivePath, destination string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, entry := range reader.File {
		// A zip entry naming ../ would write outside the destination. These
		// archives are digest-pinned, so this cannot trigger today; it is here
		// so it still cannot trigger if a digest is ever updated carelessly.
		target := filepath.Join(destination, entry.Name)
		if !strings.HasPrefix(target, filepath.Clean(destination)+string(os.PathSeparator)) {
			return fmt.Errorf("zip entry %q escapes %s", entry.Name, destination)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, entry.Mode())
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(file, source)
		source.Close()
		file.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func copyTree(source, destination string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, info.Mode().Perm())
	})
}
