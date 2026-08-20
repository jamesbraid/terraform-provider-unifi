package unifi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/schemabehaviour"
)

// Test_schemaBehaviourIsDerivable proves the source-reading deriver sees
// exactly the behaviour the running provider applies.
//
// The deriver exists to replace hand transcription when a surface is migrated:
// wlan alone carries eighty-three validators, plan modifiers, defaults and
// custom types that its policy has to restate, and firewall_policy showed what
// transcribing by hand costs -- nine validators lost, seven enum constraints
// among them, with the whole suite green.
//
// A tool for that is only worth having if it cannot quietly miss one. So this
// compares two independent readings of the same schemas: the deriver's, taken
// from Go source with go/ast, against the inventory's, taken from the
// registered schema by reflection at runtime. They share no code and reach the
// facts by different routes, so agreement across the whole estate is evidence
// rather than a tautology.
//
// The comparison is over (attribute path, kind) rather than the inventory's
// full line. The inventory records a behaviour's runtime type and its own
// description -- stringvalidator.oneOfValidator, "value must be one of ..." --
// which source cannot produce without reimplementing each library's
// Description method, and a reimplementation would be one more thing to be
// wrong. What source CAN promise is that it copied the expression out
// verbatim, and the deriver checks that itself.
//
// PROVEN TO FAIL, recorded here rather than only in the commit that proved it.
// Making resolveComposite fail everywhere -- which marks every surface delegated
// and sets every fact aside -- fails this test twice over: the floor reports 856
// behaviours observed and all of them excused, and the delegation check names
// unifi_device, unifi_port_forward and unifi_setting as excused with no
// generated package behind them. Before the floor existed that same mutation
// PASSED, having compared nothing. (b05f9f87)
//
// THE LIMIT THE PROOF DOES NOT COVER, and it grows as the migration succeeds.
// The floor asserts that SOMETHING was compared, never that enough was. Each
// surface that moves to a generated schema leaves this comparison legitimately,
// so the compared population shrinks with every migration: measured on the tree
// that added this note, 112 of 855 behaviour lines across 3 of 29 managed
// resources. The other 26 are carried by the inventory golden rather than by
// this oracle. The t.Logf below prints the split on every run, so the shrinkage
// is visible rather than something a reader has to infer.
func Test_schemaBehaviourIsDerivable(t *testing.T) {
	ctx := context.Background()
	observedLines, _ := schemaBehaviourFacts(ctx, t)
	observed := map[string]int{}
	for _, line := range observedLines {
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			t.Fatalf("inventory line is not tab separated: %q", line)
		}
		observed[parts[0]+"\t"+parts[1]]++
	}
	if len(observed) == 0 {
		t.Fatal("the inventory found nothing, so this comparison would pass against an empty set")
	}

	surfaces, err := schemabehaviour.DeriveDir(".")
	if err != nil {
		t.Fatalf("deriving behaviour from source: %v", err)
	}
	if len(surfaces) == 0 {
		t.Fatal("the deriver found no managed resources, so this comparison would pass vacuously")
	}

	derived := map[string]int{}
	var delegated, opaque []string
	for _, surface := range surfaces {
		if surface.Delegated {
			delegated = append(delegated, surface.TypeName+" -> "+surface.DelegatedTo)
			if len(surface.Behaviours) > 0 {
				t.Errorf("%s serves a generated schema yet %d behaviour(s) were derived from "+
					"its source, so something other than the served schema was read",
					surface.TypeName, len(surface.Behaviours))
			}
			continue
		}
		for _, behaviour := range surface.Behaviours {
			derived[surface.TypeName+"."+behaviour.Path+"\t"+behaviour.Kind]++
		}
		for _, unread := range surface.Opaque {
			opaque = append(opaque, surface.TypeName+"."+unread.Path+": "+unread.Reason)
		}
	}

	// A surface already serving a generated schema has no hand-written schema
	// to read, so its behaviour is out of reach here by construction rather
	// than by oversight. It is named, and its facts are set aside explicitly.
	delegatedPrefixes := map[string]bool{}
	for _, entry := range delegated {
		delegatedPrefixes[strings.SplitN(entry, " -> ", 2)[0]] = true
	}

	// An attribute the deriver could not read is not a pass. Its facts are set
	// aside only because the deriver NAMED it, which is what separates "this
	// attribute has no behaviour" from "this attribute keeps its behaviour
	// somewhere source cannot see".
	opaquePrefixes := map[string]bool{}
	for _, entry := range opaque {
		opaquePrefixes[strings.SplitN(entry, ": ", 2)[0]] = true
	}

	// DELEGATION HAS TO BE EARNED, BECAUSE "delegated" IS NOT WHAT THE DERIVER
	// MEANS BY IT.
	//
	// walk.go sets Delegated whenever resolveComposite fails, so it reads "the
	// Schema method assigns something I could not resolve to a literal" -- a
	// catch-all covering both a surface that genuinely serves a generated schema
	// and one whose schema was merely moved into a helper. Both are excused from
	// the comparison, and only the first deserves to be.
	//
	// That is what lets the whole oracle pass while reading nothing: make
	// resolveComposite fail everywhere and every surface becomes delegated, every
	// fact is set aside, and an empty comparison reports success.
	//
	// So a delegation is checked against the tree: the expression must name a
	// package that actually exists under internal/generated. That is derived from
	// the filesystem rather than declared, it cannot go stale as surfaces
	// convert, and it fails the moment a hand-written schema is refactored behind
	// a helper -- which is the migration this oracle exists to keep honest.
	for _, entry := range delegated {
		typeName, target, _ := strings.Cut(entry, " -> ")
		pkg, _, _ := strings.Cut(target, ".")
		if pkg == "" {
			t.Errorf("%s is excused as delegated to %q, which names no package", typeName, target)
			continue
		}
		if _, err := os.Stat(filepath.Join("..", "internal", "generated", pkg)); err != nil {
			// Truncated deliberately. DelegatedTo is the rendered expression, and
			// for a surface that assigns a literal that is the ENTIRE schema --
			// one failure printed 47KB when this was first run, which is a
			// failure nobody reads to the end of.
			t.Errorf("%s is excused from this comparison as \"serving a generated schema\", but it "+
				"assigns %s and there is no internal/generated/%s.\n"+
				"    The deriver marks a surface delegated whenever it cannot resolve the assigned\n"+
				"    expression, so this is \"could not read it\" wearing the label of \"nothing to\n"+
				"    read\". Its behaviour is being set aside rather than compared.",
				typeName, abbreviate(target), pkg)
		}
	}

	setAside := 0
	dataSourceFacts := 0
	delegatedFacts := 0
	opaqueFacts := 0
	compared := 0
	comparedSurfaces := map[string]bool{}
	var missing []string
	for fact, count := range observed {
		path := strings.SplitN(fact, "\t", 2)[0]
		// The deriver reads resource.Schema methods and nothing else: it pairs
		// Metadata and Schema by matching resource.MetadataResponse and
		// resource.SchemaResponse parameters, so a data source is invisible to
		// it by construction rather than by oversight.
		//
		// Set aside and COUNTED rather than skipped. The inventory covers data
		// sources so that a migration cannot drop one of their validators
		// silently; this comparison is about whether the DERIVER sees what the
		// provider applies, and for data sources the honest answer is "it does
		// not, and here is how many".
		if strings.HasPrefix(path, "data.") {
			dataSourceFacts += count
			setAside += count
			continue
		}
		// Counted apart, because one total cannot explain three causes. The
		// previous version added all three into setAside and then reported the
		// whole of it as attributable to the opaque attributes, which said three
		// attributes accounted for 744 behaviours when they account for far
		// fewer.
		if excused(path, delegatedPrefixes) {
			delegatedFacts += count
			setAside += count
			continue
		}
		if excused(path, opaquePrefixes) {
			opaqueFacts += count
			setAside += count
			continue
		}
		compared += count
		comparedSurfaces[strings.SplitN(path, ".", 2)[0]] = true
		if derived[fact] < count {
			missing = append(missing, fmt.Sprintf("%s (runs %d time(s), derived %d)",
				strings.ReplaceAll(fact, "\t", " "), count, derived[fact]))
		}
	}

	var spurious []string
	for fact, count := range derived {
		if observed[fact] < count {
			spurious = append(spurious, fmt.Sprintf("%s (derived %d time(s), runs %d)",
				strings.ReplaceAll(fact, "\t", " "), count, observed[fact]))
		}
	}

	sort.Strings(missing)
	sort.Strings(spurious)
	sort.Strings(delegated)
	sort.Strings(opaque)

	if len(missing) > 0 {
		t.Errorf("%d behaviour(s) the provider applies that the deriver did not read:\n    %s\n\n"+
			"    Each of these would be silently dropped from a policy this tool wrote.\n"+
			"    Either the source expresses it in a form the walker does not follow, in\n"+
			"    which case follow it, or the attribute holding it should be reported as\n"+
			"    opaque so that it is at least visible.",
			len(missing), strings.Join(missing, "\n    "))
	}
	if len(spurious) > 0 {
		t.Errorf("%d behaviour(s) the deriver read that the provider does not apply:\n    %s\n\n"+
			"    The deriver is reading a literal that is not part of a served schema.",
			len(spurious), strings.Join(spurious, "\n    "))
	}

	// THE FLOOR. Everything above reports what did not match; nothing until now
	// asserted that anything was compared at all.
	//
	// Both existing guards are unfirable: observed comes from the runtime
	// inventory and surfaces from DeriveDir, and neither can be empty while the
	// provider registers resources and the package has files. So the test could
	// -- and did -- pass having compared nothing, which is the same shape as a
	// referee that passes on a broken tree.
	//
	// This is deliberately a floor on the COMPARED population rather than on the
	// inputs, because that is the population the result rests on.
	if compared == 0 {
		// TWO WAYS TO COMPARE NOTHING, AND ONLY ONE IS A FAULT. The comment above
		// anticipated this population shrinking with every migration; it reached
		// zero when the last surface was rewired. A tree where EVERY managed
		// surface serves a generated schema has no hand-written behaviour left to
		// compare, and reading that as a broken harness would block the migration
		// it exists to protect.
		//
		// DISCRIMINATED ON THE SURFACES, NOT ON THE FACT COUNTS. The tally below
		// mixes populations counted in different units -- the note further down
		// records what that cost last time -- so the question asked here is the
		// one that has a single unit: is there a managed surface that is not
		// delegated?
		if len(delegated) == len(surfaces) {
			t.Skipf("all %d managed surface(s) serve a generated schema, so no hand-written "+
				"behaviour remains to compare; this resumes if a surface is added "+
				"hand-written", len(surfaces))
		}
		t.Fatalf("nothing was compared: %d behaviour(s) observed, all of them set aside "+
			"(%d data source, %d delegated, %d opaque), while %d of %d managed surface(s) are "+
			"still hand-written. Every assertion above passes vacuously when the deriver reads "+
			"nothing, so this is the harness failing rather than the provider passing.",
			len(observedLines), dataSourceFacts, delegatedFacts, opaqueFacts,
			len(surfaces)-len(delegated), len(surfaces))
	}

	// Reported as three separate figures because they have three separate causes,
	// and in the units they are actually counted in.
	//
	// The old line printed len(observed)-setAside: a count of distinct fact KEYS
	// minus a count of LINES. Subtracting one unit from another produced 66,
	// which was neither the number of facts compared nor the number of keys.
	t.Logf("%d behaviour line(s) compared across %d of %d managed resource(s); "+
		"%d set aside (%d data source, %d delegated to a generated schema, %d opaque)",
		compared, len(comparedSurfaces), len(surfaces),
		setAside, dataSourceFacts, delegatedFacts, opaqueFacts)
	if dataSourceFacts > 0 {
		t.Logf("%d data source behaviour(s) set aside: the deriver reads resource.Schema "+
			"methods only, so migrating a data source has to transcribe its behaviour by "+
			"hand -- the inventory is what catches a mistake there", dataSourceFacts)
	}
	if len(delegated) > 0 {
		t.Logf("%d surface(s) already serve a generated schema, so there is no hand-written "+
			"schema to derive from:\n    %s", len(delegated), strings.Join(delegated, "\n    "))
	}
	if len(opaque) > 0 {
		t.Logf("%d attribute(s) the deriver named as unreadable, accounting for %d behaviour "+
			"line(s) not compared:\n    %s", len(opaque), opaqueFacts, strings.Join(opaque, "\n    "))
	}
}

// excused reports whether a path is one of the given roots or sits underneath
// one. Descent is matched on the separator rather than on the bare string, so
// naming "timeouts" does not also excuse "timeouts_extra".
func excused(path string, roots map[string]bool) bool {
	for root := range roots {
		if path == root || strings.HasPrefix(path, root+".") {
			return true
		}
	}
	return false
}

// abbreviate shortens a rendered expression for a failure message. A schema
// assigned as a literal renders as the whole schema, and a diagnostic that
// prints tens of kilobytes buries the sentence that matters.
func abbreviate(expression string) string {
	const limit = 90
	flattened := strings.Join(strings.Fields(expression), " ")
	if len(flattened) <= limit {
		return flattened
	}
	return flattened[:limit] + "... (" + fmt.Sprint(len(flattened)) + " chars)"
}
