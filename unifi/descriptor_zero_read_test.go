package unifi

import (
	"context"
	"testing"
)

// EVERY KIT SURFACE MUST SURVIVE A READ OF AN OBJECT THAT SETS NOTHING.
//
// The two defects this holds shut both shipped past the unit suite and were
// found by the first acceptance run over the client and device cutovers: a
// nil New that made every device read panic the provider, and a kept zero
// that every client read turned into an IPv4Address("") its own type refuses.
// resourcekit.ZeroReadProblems proves it can catch both against deliberate
// probes; this applies it to what the provider actually serves.
//
// THE ENUMERATION IS THE PROVIDER'S OWN Resources LIST, through a type
// assertion the kit's Resource satisfies -- a new kit surface joins the walk
// by being served, with nothing to remember. The count is pinned against
// kitServedSurfaces so a surface slipping OUT of the walk is a failure
// rather than a silent shrink.
func TestEveryKitSurfaceSurvivesAZeroRead(t *testing.T) {
	ctx := context.Background()
	type zeroReadable interface{ ZeroReadProblems(context.Context) []string }

	checked := 0
	for _, constructor := range New().Resources(ctx) {
		surface, ok := constructor().(zeroReadable)
		if !ok {
			continue
		}
		checked++
		for _, problem := range surface.ZeroReadProblems(ctx) {
			t.Error(problem)
		}
	}

	// +1: unifi_account is a deprecated alias embedding the radius_user kit
	// resource, so it joins the walk as a twenty-first resource over the same
	// twenty surfaces. Its checks duplicate radius_user's, which is harmless;
	// what the pin holds is that nothing DROPS from the walk unannounced.
	if want := len(kitServedSurfaces(t)) + 1; checked != want {
		t.Errorf("zero-read walked %d resource(s) against %d kit-served surface(s) plus the "+
			"account alias; a surface outside the walk is one whose reads nobody has proven "+
			"survivable", checked, want)
	}
}
