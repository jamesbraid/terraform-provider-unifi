package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/gounifipin"
)

// The go-unifi pin is one fact with thirty-nine hand-maintained copies.
//
// provider-codegen/generate.go carries a //go:generate line per bootstrapped
// struct, and each repeats the reviewed commit, the module path and the pinned
// version. go:generate lines are literal text: they cannot read a Go constant,
// so the duplication is structural rather than careless. That is exactly why it
// needs a check -- nothing else connects those copies to the package that
// declares the pin, and a repoint that updates thirty-eight of thirty-nine
// leaves a tree that builds locally and fails an hour into CI. The header of
// the retired go-unifi-pin.sh records that happening.
//
// THE AUTHORITY IS internal/gounifipin AND go.mod, NEVER generate.go. Reading
// the commit out of generate.go and comparing it to generate.go would agree
// with itself no matter how wrong it was.
//
// Not asserted here: the ~97 generated artifacts under provider-codegen/
// bootstrap and provider-codegen/generated that also contain the commit. Those
// are OUTPUTS that record which pin produced them; regeneration rewrites them,
// and a stale one is caught by regenerating rather than by reading. Only the
// hand-written directives can drift silently.

var sdkBootstrapDirective = regexp.MustCompile(`^//go:generate go run \.\./cmd/sdk-bootstrap .*$`)

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

	// The floor is derived, not a number that expires. Every line that LOOKS
	// like an sdk-bootstrap directive must also parse as one; if the field
	// regexps stop matching, parsed falls below candidates and this fails
	// instead of quietly checking nothing. A hardcoded count would have to be
	// edited every time a surface is added, and #161 is what that becomes.
	candidates, parsed := 0, 0

	for number, line := range lines {
		if !sdkBootstrapDirective.MatchString(line) {
			continue
		}
		candidates++

		commit, okCommit := flagValue(line, "-commit")
		pkg, okPackage := flagValue(line, "-package")
		output, okOutput := flagValue(line, "-output")
		if !okCommit || !okPackage || !okOutput {
			t.Errorf("%s:%d: directive is missing -commit, -package or -output; "+
				"if its shape changed on purpose, this parser has to change with it:\n\t%s",
				path, number+1, line)
			continue
		}
		parsed++

		if commit != wantCommit {
			t.Errorf("%s:%d: names commit %s, but internal/gounifipin declares %s.\n"+
				"\tThe pin has one authority and this line is a copy of it. Repoint the copy, "+
				"or if the pin moved, move it in gounifipin and regenerate.",
				path, number+1, commit, wantCommit)
		}
		if pkg != wantPackage {
			t.Errorf("%s:%d: names package %s, want %s (gounifipin.ModulePath + \"/unifi\")",
				path, number+1, pkg, wantPackage)
		}
		// The version is embedded in the output path, so a repoint that updates
		// go.mod and the commit but not the filenames writes the new pin's
		// bootstrap into the old pin's name.
		if !strings.Contains(output, "go-unifi-"+wantVersion+"-") {
			t.Errorf("%s:%d: writes to %s, which does not carry the version go.mod declares (%s)",
				path, number+1, output, wantVersion)
		}
	}

	if candidates == 0 {
		t.Fatalf("no sdk-bootstrap directives found in %s; the check is reading the wrong file "+
			"or the directive shape changed, and either way it is not checking anything", path)
	}
	if parsed != candidates {
		t.Fatalf("%d of %d sdk-bootstrap directive(s) could not be parsed; the ones that failed "+
			"are unchecked", candidates-parsed, candidates)
	}
	t.Logf("%d sdk-bootstrap directive(s) checked against gounifipin (%s) and go.mod (%s)",
		parsed, wantCommit[:12], wantVersion)
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

	for _, authority := range []string{"gounifipin.ExpectedCommit()", "gounifipin.ModulePath", "gounifipin.DeclaredVersion"} {
		if !strings.Contains(checkBody, authority) {
			t.Errorf("the directive check no longer reads %s; its expected values must come from "+
				"the pin package, or it compares generate.go to itself", authority)
		}
	}
}
