package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/gounifipin"
)

// The go-unifi pin used to have thirty-nine hand-maintained copies in
// provider-codegen/generate.go -- one per //go:generate line, each repeating the
// reviewed commit and the module path. A go:generate `-command` alias now
// declares both once and expands them into every call, so the file holds ONE
// copy of each.
//
// One copy is not zero. The alias is still literal text that no compiler
// relates to internal/gounifipin, and go:generate cannot read a Go constant.
// This is what relates them.
//
// THE AUTHORITY IS internal/gounifipin AND go.mod, NEVER generate.go. Reading
// the commit out of the file under test would agree with itself for any value,
// which is why the second test below exists.
//
// Not asserted: the ~97 generated artifacts under provider-codegen/bootstrap
// and provider-codegen/generated that also contain the commit. Those are
// OUTPUTS recording which pin produced them; regeneration rewrites them and a
// stale one is caught by regenerating. Only hand-written text can rot unseen.
var (
	// The alias definition: the single home of the commit and module path.
	sdkBootstrapAlias = regexp.MustCompile(`^//go:generate -command sdkbootstrap go run \.\./cmd/sdk-bootstrap `)
	// Each use of the alias. The version still lives once per line, inside
	// -output, because it names a distinct file per surface.
	sdkBootstrapCall = regexp.MustCompile(`^//go:generate sdkbootstrap `)
	// The pre-refactor shape. If it ever comes back, the alias has been undone
	// and the commit is being copied per line again.
	sdkBootstrapInline = regexp.MustCompile(`^//go:generate go run \.\./cmd/sdk-bootstrap `)
)

func TestGenerateDirectivesNameTheDeclaredPin(t *testing.T) {
	const path = "provider-codegen/generate.go"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")

	wantCommit := gounifipin.ExpectedCommit()
	wantPackage := gounifipin.ModulePath + "/unifi"
	wantVersion, err := gounifipin.DeclaredVersion(".")
	if err != nil {
		t.Fatalf("reading the declared version from go.mod: %v", err)
	}

	aliases, calls := 0, 0
	for number, line := range lines {
		switch {
		case sdkBootstrapInline.MatchString(line):
			t.Errorf("%s:%d: bootstraps go-unifi inline instead of through the sdkbootstrap alias, "+
				"so this line carries its own copy of the commit:\n\t%s", path, number+1, line)

		case sdkBootstrapAlias.MatchString(line):
			aliases++
			commit, okCommit := flagValue(line, "-commit")
			pkg, okPackage := flagValue(line, "-package")
			if !okCommit || !okPackage {
				t.Errorf("%s:%d: the sdkbootstrap alias declares no -commit or -package; every call "+
					"inherits from it, so an incomplete alias is worse than a missing one:\n\t%s",
					path, number+1, line)
				continue
			}
			if commit != wantCommit {
				t.Errorf("%s:%d: the alias names commit %s, but internal/gounifipin declares %s.\n"+
					"\tAll %d bootstraps inherit this line. Repoint it, or if the pin moved, move it "+
					"in gounifipin and regenerate.", path, number+1, commit, wantCommit, calls)
			}
			if pkg != wantPackage {
				t.Errorf("%s:%d: the alias names package %s, want %s (gounifipin.ModulePath + \"/unifi\")",
					path, number+1, pkg, wantPackage)
			}

		case sdkBootstrapCall.MatchString(line):
			calls++
			output, ok := flagValue(line, "-output")
			if !ok {
				t.Errorf("%s:%d: sdkbootstrap call has no -output:\n\t%s", path, number+1, line)
				continue
			}
			// The version is embedded per line because each names a different
			// file, so it did not collapse into the alias. A repoint that moves
			// go.mod and the alias but not these writes the new pin's bootstrap
			// into the old pin's filename.
			if !strings.Contains(output, "go-unifi-"+wantVersion+"-") {
				t.Errorf("%s:%d: writes to %s, which does not carry the version go.mod declares (%s)",
					path, number+1, output, wantVersion)
			}
		}
	}

	// Floors, both derived rather than counts that expire when a surface is
	// added. Exactly one alias: a second one would let half the calls inherit a
	// different commit, which is the defect this replaced, reintroduced with
	// fewer copies. At least one call: otherwise the alias is checked and
	// nothing uses it, and this test would pass having verified nothing that
	// runs.
	if aliases != 1 {
		t.Fatalf("found %d sdkbootstrap alias definitions in %s, want exactly 1; more than one "+
			"means different calls can inherit different pins", aliases, path)
	}
	if calls == 0 {
		t.Fatalf("found no sdkbootstrap calls in %s; the alias is checked but nothing uses it, so "+
			"this test verified nothing", path)
	}
	t.Logf("1 alias and %d call(s) checked against gounifipin (%s) and go.mod (%s)",
		calls, wantCommit[:12], wantVersion)
}

// flagValue returns the value following name in a go:generate line.
func flagValue(line, name string) (string, bool) {
	fields := strings.Fields(line)
	for i, field := range fields {
		if field == name && i+1 < len(fields) {
			return fields[i+1], true
		}
	}
	return "", false
}

// TestGenerateDirectivePinIsNotSelfDerived guards the property that makes the
// check above worth running: its expected values come from gounifipin and
// go.mod, not from the file under test.
//
// If someone later "simplifies" it by reading the commit out of generate.go,
// the check agrees with itself for any value and cannot fail. That is the shape
// this repository has spent a long time removing, so it is asserted rather than
// left to review.
func TestGenerateDirectivePinIsNotSelfDerived(t *testing.T) {
	source, err := os.ReadFile("check_generate_pin_test.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)

	marker := "func TestGenerateDirectivesNameTheDeclaredPin"
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatal("cannot find the test this one guards; it was renamed without updating this check")
	}
	checkBody := body[start:]
	if end := strings.Index(checkBody, "\nfunc flagValue"); end > 0 {
		checkBody = checkBody[:end]
	}

	for _, authority := range []string{
		"gounifipin.ExpectedCommit()",
		"gounifipin.ModulePath",
		"gounifipin.DeclaredVersion",
	} {
		if !strings.Contains(checkBody, authority) {
			t.Errorf("the directive check no longer reads %s; its expected values must come from "+
				"the pin package, or it compares generate.go to itself", authority)
		}
	}
}
