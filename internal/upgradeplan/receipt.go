package upgradeplan

import (
	"errors"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

var (
	errUnmeasuredProvider = errors.New("one of the two providers was not digested, so nothing " +
		"establishes that the run compared two different programs")
	errIdenticalProviders = errors.New("the released and candidate providers are byte-identical; " +
		"this measurement would be void")
)

// Receipt is what one upgrade-plan run recorded.
//
// NO GO TYPE EXISTED FOR THIS RECEIPT. Twenty-one other receipts in this
// repository are typed at the consumer end and this one was not, because
// nothing decodes it -- the workflow prints it with `jq '.'` and stops. A
// receipt nobody parses is a receipt nobody can be wrong about, which is
// comfortable and worth exactly nothing.
//
// The tree state key is tree_state, matching every other receipt here. The
// shell called it `tree`, which is the same odd spelling the dependency
// publishability receipt used -- and there that difference was enough to make
// the file undecodable by its own consumer. Nothing decodes this one, so the
// rename costs nothing and removes the trap before somebody adds a consumer.
type Receipt struct {
	FormatVersion int                      `json:"format_version"`
	Gate          string                   `json:"gate"`
	TreeState     *catalogparity.TreeState `json:"tree_state,omitempty"`

	// CLI is what the binary SAYS IT IS, not the variable that held it. The
	// variable is called TERRAFORM_BIN and on these machines it holds OpenTofu;
	// a receipt reporting "terraform" because of a variable's name is a receipt
	// that lies about the tool that produced it.
	CLI string `json:"cli"`

	ReleasedRef    string `json:"released_ref"`
	ReleasedCommit string `json:"released_commit"`
	// ReleasedProvenance is published-binary or source-build. An operator
	// upgrades from the ARTIFACT that was released, not from a fresh compile of
	// the tag it was cut at, and those are not guaranteed to be the same
	// program. The run records which one it measured rather than presenting
	// either as the release.
	ReleasedProvenance      string `json:"released_provenance"`
	CandidateCommit         string `json:"candidate_commit"`
	ReleasedProviderSHA256  string `json:"released_provider_sha256"`
	CandidateProviderSHA256 string `json:"candidate_provider_sha256"`

	FixtureFiles        int `json:"fixture_files"`
	ExpectedControlExit int `json:"expected_control_exit"`
	ControlExit         int `json:"control_exit"`
	SubjectExit         int `json:"subject_exit"`
	// DestroyExit is reported rather than hidden: a fixture left behind
	// poisons the next run, and silence about it is how that becomes somebody
	// else's mystery.
	DestroyExit int `json:"destroy_exit"`

	Result  string `json:"result"`
	Verdict string `json:"verdict"`
}

// Run is what the orchestration measured.
type Run struct {
	CLI                     string
	ReleasedRef             string
	ReleasedCommit          string
	ReleasedProvenance      string
	CandidateCommit         string
	ReleasedProviderSHA256  string
	CandidateProviderSHA256 string
	Fixture                 Fixture
	ControlExit             int
	SubjectExit             int
	DestroyExit             int
	TreeState               *catalogparity.TreeState
}

// BuildReceipt derives the verdict from the run rather than beside it.
func BuildReceipt(run Run) Receipt {
	verdict := Decide(run.ReleasedRef, run.Fixture.Expected, run.ControlExit, run.SubjectExit)
	return Receipt{
		FormatVersion:           1,
		Gate:                    "catalog-upgrade-plan",
		TreeState:               run.TreeState,
		CLI:                     run.CLI,
		ReleasedRef:             run.ReleasedRef,
		ReleasedCommit:          run.ReleasedCommit,
		ReleasedProvenance:      run.ReleasedProvenance,
		CandidateCommit:         run.CandidateCommit,
		ReleasedProviderSHA256:  run.ReleasedProviderSHA256,
		CandidateProviderSHA256: run.CandidateProviderSHA256,
		FixtureFiles:            run.Fixture.Files,
		ExpectedControlExit:     int(run.Fixture.Expected),
		ControlExit:             run.ControlExit,
		SubjectExit:             run.SubjectExit,
		DestroyExit:             run.DestroyExit,
		Result:                  verdict.Result,
		Verdict:                 verdict.Verdict,
	}
}

// TwoProvidersAreDistinct refuses a comparison of a program with itself.
//
// dev_overrides silently uses whatever binary sits in the directory it names,
// so a stale one is indistinguishable from a fresh one -- and a green run where
// both directories hold the same bytes means only that a program agrees with
// itself. This nearly measured the wrong tree once.
func TwoProvidersAreDistinct(releasedSHA256, candidateSHA256 string) error {
	if releasedSHA256 == "" || candidateSHA256 == "" {
		return errUnmeasuredProvider
	}
	if releasedSHA256 == candidateSHA256 {
		return errIdenticalProviders
	}
	return nil
}
