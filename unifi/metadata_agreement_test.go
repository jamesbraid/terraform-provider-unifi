package unifi

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestEveryGeneratedSurfaceIsServedUnderItsDeclaredName closes a gap between
// three copies of one name: the -resource argument of a go:generate
// directive, the TypeName Metadata() returns, and a literal in that
// resource's own Metadata test. Generated artifacts take their name from the
// directive, so Metadata() could disagree with it and `go generate` plus
// `git diff --exit-code` would not move -- each per-resource test only
// carries its own copy, not a comparison against the directive. This calls
// Metadata() rather than parsing source, so it agrees with the value
// Terraform will receive.
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

	// The reverse direction is what makes this check bite: without it, set
	// membership alone lets unifi_ap_group's resource and data source swap
	// declared names and still pass, since the other still serves it.
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
// provider offers what it calls itself, derived from servedSurfaces rather
// than walking the provider a second time.
func servedTypeNames(t *testing.T) map[string]bool {
	t.Helper()
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
