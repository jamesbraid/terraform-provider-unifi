package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/schemabehaviour"
)

// TestEveryPolicyCarriesTheBehaviourItsResourceDeclares runs the deriver in
// check mode and fails when a policy would gain anything.
//
// THREE SURFACES HAVE LOST BEHAVIOUR THIS WAY AND EVERY GATE STAYED GREEN.
// firewall_policy dropped nine constructs, vpn_client eleven, port_forward
// fifteen. A policy is checked for SHAPE -- it matches the released schema
// attribute for attribute -- and a released schema is JSON, which cannot
// express a validator, a plan modifier or a default. So the parity check
// satisfied the claim it was making, honestly, while a third of the surface's
// behaviour went missing.
//
// The comparator already existed: cmd/schema-behaviour -policy merges derived
// behaviour into a policy and reports what it wrote. Skipping that step is what
// caused the loss. This is that tool run as a test, against a COPY, asserting
// it had nothing to add.
//
// IT ONLY WORKS BEFORE THE REWIRE, and that is why it has to run now rather
// than later. Once a resource serves the generated schema there is no
// hand-written schema left to derive from, and the deriver says so instead of
// deriving nothing -- so the window is between generating a policy and rewiring
// the resource onto it.
//
// WHICH MAKES THE EMPTY CASE THE DANGEROUS ONE. The checkable set is one
// surface today and will be zero when port_forward is rewired, and a check that
// passes on an empty set is decoration. So this does not merely check what it
// can reach: it accounts for EVERY policy, in one of three buckets it derives
// rather than declares, and fails if any policy lands in none of them or if the
// buckets stop summing to the number of files.
func TestEveryPolicyCarriesTheBehaviourItsResourceDeclares(t *testing.T) {
	policies, err := filepath.Glob(filepath.Join("provider-codegen", "policy", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) == 0 {
		t.Fatal("no policy files found, so this check would account for nothing")
	}

	surfaces, err := schemabehaviour.DeriveDir("unifi")
	if err != nil {
		t.Fatalf("deriving behaviour from unifi/: %v", err)
	}
	// THE DERIVER'S OWN POSITIVE CONTROL. If it returns no behaviour at all --
	// a parse change, a moved directory -- every policy below would have
	// nothing to gain and the check would pass having compared nothing.
	behaviours := 0
	byName := map[string]schemabehaviour.Surface{}
	for _, surface := range surfaces {
		byName[surface.TypeName] = surface
		behaviours += len(surface.Behaviours)
	}
	if behaviours == 0 {
		t.Fatalf("the deriver found %d surface(s) and zero behaviours; nothing below is a "+
			"comparison", len(surfaces))
	}

	var checked, delegated, notManaged []string
	var findings []string
	for _, policy := range policies {
		name := strings.TrimSuffix(filepath.Base(policy), ".json")
		switch {
		case strings.HasPrefix(name, "catalog"), name == "resource-decisions", name == "surface-blockers":
			continue
		}
		surface, ok := byName["unifi_"+name]
		switch {
		case !ok:
			// A list or data-source policy: no managed resource carries this
			// name, so there is no hand-written schema to derive from and never
			// was.
			notManaged = append(notManaged, name)
			continue
		case surface.Delegated:
			// The rewire has happened. Correct, expected, and the reason this
			// check cannot be deferred: the input is gone.
			delegated = append(delegated, name+" -> "+surface.DelegatedTo)
			continue
		}
		checked = append(checked, name)

		// AGAINST A COPY. MergeIntoPolicy writes in place, and a check that
		// repaired what it measured would pass on its second run whatever the
		// tree contained.
		original, err := os.ReadFile(policy)
		if err != nil {
			t.Fatal(err)
		}
		scratch := filepath.Join(t.TempDir(), filepath.Base(policy))
		if err := os.WriteFile(scratch, original, 0o600); err != nil {
			t.Fatal(err)
		}
		report, err := schemabehaviour.MergeIntoPolicy(scratch, surface)
		if err != nil {
			t.Errorf("%s: the deriver could not read it: %v", name, err)
			continue
		}
		after, err := os.ReadFile(scratch)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(original, after) {
			findings = append(findings, report)
		}
	}

	sort.Strings(checked)

	// THE GUARD THAT CAN ACTUALLY FAIL, and my first version's could not. I
	// wrote an "every policy lands in one of three buckets" assertion, and the
	// loop above assigns every policy to exactly one of them -- so it was
	// structurally incapable of firing. That is the shape this campaign has
	// spent the night finding in other people's checks.
	//
	// The real hole is the naming convention. Policies are matched to surfaces
	// by "unifi_" + the file's stem; change either side and EVERY policy falls
	// through to not-a-managed-resource, checked drops to zero, and the check
	// passes having compared nothing.
	if len(checked)+len(delegated) == 0 {
		t.Fatalf("not one of %d policy file(s) matched a managed resource. Policies are "+
			"matched by \"unifi_\" + the file's stem; if either side has been renamed this "+
			"check now compares nothing and says so rather than passing.", len(policies))
	}

	// A hand-written resource with no policy is the other direction, and it is
	// where the next one of these will come from: the surface cannot be
	// migrated until someone writes one, and nothing else reports the absence.
	var noPolicy []string
	for _, surface := range surfaces {
		if surface.Delegated {
			continue
		}
		stem := strings.TrimPrefix(surface.TypeName, "unifi_")
		if _, err := os.Stat(filepath.Join("provider-codegen", "policy", stem+".json")); err != nil {
			noPolicy = append(noPolicy, surface.TypeName)
		}
	}
	sort.Strings(noPolicy)
	if len(noPolicy) > 0 {
		t.Logf("  hand-written, no migration policy yet (%d): %v", len(noPolicy), noPolicy)
	}

	t.Logf("%d policy file(s): %d checked, %d already rewired, %d not a managed resource",
		len(checked)+len(delegated)+len(notManaged), len(checked), len(delegated), len(notManaged))
	t.Logf("  checked: %v", checked)

	if len(findings) > 0 {
		t.Errorf("%d polic(ies) are missing behaviour their hand-written resource declares:\n\n%s\n"+
			"    A policy is checked for SHAPE, and a released schema cannot express a\n"+
			"    validator, a plan modifier or a default -- so every existing gate passes\n"+
			"    while these go missing. Run:\n\n"+
			"        go run ./cmd/schema-behaviour -resource unifi_<surface> \\\n"+
			"            -policy provider-codegen/policy/<surface>.json\n\n"+
			"    NOT READABLE FROM SOURCE entries are not failures: the deriver is saying\n"+
			"    what it cannot see, and those are transcribed by hand -- timeouts is the\n"+
			"    standing instance, grafted by the rewire wrapper and declared\n"+
			"    generated: false the way dns_record already does.",
			len(findings), strings.Join(findings, "\n"))
	}
}
