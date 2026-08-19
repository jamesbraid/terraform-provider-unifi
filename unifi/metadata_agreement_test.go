package unifi

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestEveryGeneratedSurfaceIsServedUnderItsDeclaredName closes a gap between
// three copies of one fact, two of which nothing compared.
//
// A resource type name is written down three times: as the -resource argument
// of a go:generate directive, as the TypeName that Metadata() returns, and as a
// literal inside that resource's own Metadata test. The generated artifacts --
// catalog-surface-contracts.json and the parity ledger among them -- take their
// names from the DIRECTIVE, because cmd/catalog-parity builds them from a locked
// schema file and never reads the running provider.
//
// SO Metadata() COULD RETURN ANYTHING AND `go generate` PLUS `git diff
// --exit-code` WOULD NOT MOVE. The committed contract would keep saying
// unifi_ap_group because a directive says so, while the provider served
// something else, and every generated artifact downstream would describe a
// surface that is not the one being offered.
//
// The per-resource Metadata tests do catch a change to Metadata() alone, but
// each carries its own copy of the name, so they are a third home rather than a
// comparison between the first two. This is one test for all of them, and it
// asks the provider what it actually serves rather than reading what it says.
//
// IT CALLS Metadata() RATHER THAN PARSING IT, deliberately. A parse would agree
// with a source file; this agrees with the value Terraform will receive.
func TestEveryGeneratedSurfaceIsServedUnderItsDeclaredName(t *testing.T) {
	served := servedTypeNames(t)
	declared := declaredResourceNames(t)

	if len(served) == 0 || len(declared) == 0 {
		t.Fatalf("served=%d declared=%d; one side is empty, so this comparison proves nothing",
			len(served), len(declared))
	}

	var missing []string
	for name := range declared {
		if !served[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("%d surface(s) are generated under a name the provider does not serve:\n    %s\n\n"+
			"    The go:generate directive and Metadata() are the only two homes for this name\n"+
			"    that matter, and nothing else compares them: the generated contract takes its\n"+
			"    names from the directive, so a wrong TypeName leaves every artifact describing\n"+
			"    a surface that is not being offered. Fix whichever is wrong -- and note that\n"+
			"    the resource's own Metadata test will not tell you, because it carries a third\n"+
			"    copy of the same literal.",
			len(missing), strings.Join(missing, "\n    "))
	}

	// THE REVERSE DIRECTION, AND IT IS WHAT MAKES THIS CHECK BITE. Without it
	// the comparison is set-membership only: unifi_ap_group is served by BOTH a
	// resource and a data source, so changing one of them to something else
	// leaves the other still serving the declared name and the forward check
	// still passes. Measured, not supposed -- the first mutation run did exactly
	// that and came back green.
	//
	// Going the other way, a surface serving a name no directive declares is
	// visible immediately, whether that is a typo in Metadata() or a new surface
	// nobody wired into the generator.
	var undeclared []string
	for name := range served {
		if declared[name] || surfacesWithoutDirectives[name] {
			continue
		}
		undeclared = append(undeclared, name)
	}
	sort.Strings(undeclared)
	if len(undeclared) > 0 {
		t.Errorf("%d surface(s) are served under a name no go:generate directive declares:\n    %s\n\n"+
			"    Either Metadata() disagrees with its directive, or a new surface was added\n"+
			"    without wiring it into the generator. Add the directive, fix the name, or\n"+
			"    record it in surfacesWithoutDirectives with the reason it is hand-written.",
			len(undeclared), strings.Join(undeclared, "\n    "))
	}

	// The same ledger in the other direction, so an entry cannot outlive what it
	// describes.
	var stale []string
	for name := range surfacesWithoutDirectives {
		if !served[name] {
			stale = append(stale, name+" (no longer served)")
		} else if declared[name] {
			stale = append(stale, name+" (now has a directive)")
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("%d exemption(s) no longer describe anything:\n    %s\n\n"+
			"    Remove them; an entry that has stopped being true overstates what is\n"+
			"    hand-written.", len(stale), strings.Join(stale, "\n    "))
	}

	t.Logf("%d declared surface(s) checked against %d served type name(s)", len(declared), len(served))
}

// surfacesWithoutDirectives are served surfaces that no generator directive
// declares, because they are hand-written rather than generated.
var surfacesWithoutDirectives = map[string]bool{
	"unifi_account": true,
}

// servedTypeNames asks every resource, data source and list resource the
// provider offers what it calls itself.
func servedTypeNames(t *testing.T) map[string]bool {
	t.Helper()
	// DERIVED FROM servedSurfaces RATHER THAN WALKING THE PROVIDER AGAIN. This
	// function used to do its own walk of Resources, DataSources, Actions and
	// ListResources; the contract driver needed the same walk keyed by receiver,
	// and two walks of one truth is exactly how the pin package came to exist
	// twice. The comment about actions and list resources counting too now lives
	// on the single walk in metadata_contract_test.go.
	names := map[string]bool{}
	for _, name := range servedSurfaces(t) {
		names[name] = true
	}
	return names
}

// declaredResourceNames reads the -resource arguments out of the go:generate
// directives, which is where every generated artifact gets its surface names.
func declaredResourceNames(t *testing.T) map[string]bool {
	t.Helper()
	body, err := os.ReadFile("../provider-codegen/generate.go")
	if err != nil {
		t.Fatalf("reading the generator directives: %v", err)
	}
	pattern := regexp.MustCompile(`//go:generate\s+sdkbootstrap\b[^\n]*?-resource\s+([a-z0-9_]+)`)
	names := map[string]bool{}
	for _, match := range pattern.FindAllStringSubmatch(string(body), -1) {
		names[match[1]] = true
	}
	return names
}
