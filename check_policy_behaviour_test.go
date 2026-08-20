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
// WHICH MAKES THE EMPTY CASE THE DANGEROUS ONE, AND IT HAS NOW ARRIVED. The
// checkable set reached zero when port_forward was rewired -- the last of them
// -- and a check that passes on an empty set is decoration. So this does not
// merely check what it can reach: it accounts for EVERY policy, in one of three
// buckets it derives rather than declares, and fails if any policy lands in none
// of them.
//
// THIS IS A MIGRATION-WINDOW CHECK AND IT IS CURRENTLY INERT, BY SKIP AND WITH
// THE REASON PRINTED. That is deliberate rather than a retirement: the set is
// empty because the work succeeded, not because the check broke, and a new
// hand-written surface with a policy puts a member back in and resumes it
// unedited. What guards a rewired surface afterwards is the behaviour golden in
// unifi/testdata/schema_behaviour.txt, which pins what the provider actually
// serves; this one guards the gap before that golden has anything to say.
//
// THE TWO ZEROS ARE NOT THE SAME AND THE ORDER OF THE CHECKS BELOW SEPARATES
// THEM. "No policy is still checkable" is benign and skips. "The deriver
// returned nothing for a policy that IS still checkable" is a parse fault and is
// fatal. An earlier version summed behaviours across every surface before
// knowing which were checkable, so the moment the last rewire landed it read a
// correctly migrated tree as a broken deriver -- and it was RIGHT to refuse,
// because on its own terms it could not tell the difference.
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
	// THE DERIVER READ SOMETHING. A moved directory or a parse change makes
	// every classification below vacuous, and unlike a zero behaviour count
	// this stays a fault no matter how much of the estate is migrated.
	if len(surfaces) == 0 {
		t.Fatal("the deriver read unifi/ and found no surfaces at all, so every " +
			"classification below is about an empty set")
	}
	byName := map[string]schemabehaviour.Surface{}
	for _, surface := range surfaces {
		byName[surface.TypeName] = surface
	}

	// CLASSIFY FIRST, COMPARE SECOND. The two were one loop, which put the
	// deriver's positive control -- zero behaviours across every surface -- ahead
	// of knowing whether anything was left to compare. Those are different
	// conditions and only one is a fault: after the last rewire the deriver
	// SHOULD report zero behaviours everywhere, because no hand-written schema
	// remains for it to read.
	var checked, delegated, notManaged []string
	checkedSurfaces := map[string]schemabehaviour.Surface{}
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
		case surface.Delegated:
			// The rewire has happened. Correct, expected, and the reason this
			// check cannot be deferred: the input is gone.
			delegated = append(delegated, name+" -> "+surface.DelegatedTo)
		default:
			checked = append(checked, name)
			checkedSurfaces[policy] = surface
		}
	}
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

	// THE SET EMPTIES AS THE WORK SUCCEEDS, AND THAT IS NOT A FAULT.
	//
	// This check only ever had a window: between a policy being written and the
	// resource being rewired onto it. Step 8 destroys the deriver's input, so a
	// rewired surface is permanently unavailable to it -- by design, and stated
	// in the comment at the top. When the last surface is rewired the checkable
	// set is empty and the deriver reports zero behaviours EVERYWHERE, which is
	// the correct reading of a fully migrated tree.
	//
	// IT IS NOT PERMANENTLY EMPTY, WHICH IS WHY THIS SKIPS RATHER THAN BEING
	// DELETED. A new hand-written surface with a policy puts a member back in,
	// and the check resumes with no edit.
	//
	// THE ORDER HERE IS LOAD-BEARING. The naming-convention guard above runs
	// FIRST and is fatal: if a rename made every policy fall through, checked is
	// also zero, and skipping there would hide the fault this file exists to
	// report. Only checked==0 WITH delegated>0 is benign.
	if len(checked) == 0 {
		t.Skip("every managed policy already serves a generated schema, so there is no " +
			"hand-written schema left to derive from; this check resumes if a surface is " +
			"added hand-written")
	}

	// THE DERIVER'S POSITIVE CONTROL, SCOPED TO WHAT IS BEING COMPARED.
	//
	// It used to sum behaviours across every surface and fire on zero, which
	// conflated "the deriver broke" with "nothing is left to check" -- and the
	// second became true the moment the last surface was rewired. Summed over the
	// checked surfaces only, a zero means the deriver read a hand-written schema
	// and found no validator, plan modifier or default in it, which is what a
	// parse change looks like.
	behaviours := 0
	for _, surface := range checkedSurfaces {
		behaviours += len(surface.Behaviours)
	}
	if behaviours == 0 {
		t.Fatalf("the deriver found zero behaviours across the %d surface(s) still checkable "+
			"(%v); nothing below is a comparison", len(checked), checked)
	}

	var findings []string
	for policy, surface := range checkedSurfaces {
		name := strings.TrimSuffix(filepath.Base(policy), ".json")
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
	sort.Strings(findings)

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
