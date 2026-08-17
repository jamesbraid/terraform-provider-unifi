package main

import (
	"os"
	"testing"
)

// TestWoodpeckerSetsTheCIEnvironmentFlag settles an assumption another lane's
// port depends on and could not check from a workstation.
//
// catalog-dependency-publishability's receipt records resolution_runner. The
// shell wrote the literal "remote_ci" and nothing measured where the run
// happened, so the field asserted itself (#156). The Go port measures it, and
// the only signal available from inside the process is the environment. If it
// defaults from CI and Woodpecker does not set CI, then every real CI run
// records local_workstation and releasequalification correctly REJECTS it --
// a wrong default that blocks rather than passes, but still a break that would
// only appear an hour into a campaign.
//
// This cannot be answered by reading documentation, which is the habit this
// repository has spent a long time unlearning. It is answered by asserting the
// contract where the contract applies.
//
// It SKIPS off CI deliberately. The question "does Woodpecker set CI" is
// meaningless on a laptop, and a test that invented an answer there would be
// worse than one that declines to. CI_PIPELINE_NUMBER is the probe for "am I
// in Woodpecker" because it is already proven set: m3-dns-qualification.sh
// reads it as ${CI_PIPELINE_NUMBER:?...}, which aborts when unset, and that
// script ran to 258 lines of output on pipeline 232.
func TestWoodpeckerSetsTheCIEnvironmentFlag(t *testing.T) {
	pipeline, inWoodpecker := os.LookupEnv("CI_PIPELINE_NUMBER")
	if !inWoodpecker {
		t.Skip("not running under Woodpecker (CI_PIPELINE_NUMBER is unset); this asserts a " +
			"CI-only contract and has nothing to measure here")
	}

	value, present := os.LookupEnv("CI")
	if !present {
		t.Fatalf("CI_PIPELINE_NUMBER=%s, so this IS a Woodpecker run, but CI is unset.\n"+
			"\tAnything defaulting a runner identity from CI will record a local workstation on a "+
			"real CI run, and the release gate will reject its own evidence.\n"+
			"\tUse CI_PIPELINE_NUMBER as the signal instead -- it is proven set, which is how this "+
			"test knew to run at all.", pipeline)
	}
	if value == "" {
		t.Fatalf("CI is set but empty on pipeline %s; a truthiness check on it reads the same as "+
			"unset, so treat it as absent and use CI_PIPELINE_NUMBER", pipeline)
	}
	t.Logf("Woodpecker sets CI=%q alongside CI_PIPELINE_NUMBER=%s; defaulting a runner identity "+
		"from CI is safe here", value, pipeline)
}
