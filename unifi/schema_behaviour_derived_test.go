package unifi

import (
	"context"
	"fmt"
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

	setAside := 0
	dataSourceFacts := 0
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
		if excused(path, delegatedPrefixes) || excused(path, opaquePrefixes) {
			setAside += count
			continue
		}
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

	t.Logf("%d behaviour(s) matched across %d managed resource(s)", len(observed)-setAside, len(surfaces))
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
		t.Logf("%d attribute(s) the deriver named as unreadable, accounting for %d behaviour(s) "+
			"not compared:\n    %s", len(opaque), setAside, strings.Join(opaque, "\n    "))
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
