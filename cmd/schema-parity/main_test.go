package main

import "testing"

// The two functions tested here are the only judgement left in this command;
// everything else is orchestration that needs a provider build and two CLIs.
// They run in the fast loop precisely because they do not.

// TestSharedSurfaceDropsOnlyTheTerraformOnlyKeys guards the input to the
// cross-CLI comparison. Dropping too much would make the two CLIs agree by
// removing whatever they disagreed about, which reads as a pass; dropping too
// little would make them disagree on keys tofu never reports, which reads as
// the provider serving two contracts.
func TestSharedSurfaceDropsOnlyTheTerraformOnlyKeys(t *testing.T) {
	full := map[string]any{
		"provider_schemas":      map[string]any{"unifi": "x"},
		"resource_schemas":      map[string]any{"unifi_network": "y"},
		"action_schemas":        map[string]any{"a": 1},
		"list_resource_schemas": map[string]any{"l": 1},
	}
	shared, ok := sharedSurface(full).(map[string]any)
	if !ok {
		t.Fatalf("sharedSurface returned %T, want a map", sharedSurface(full))
	}
	for _, gone := range []string{"action_schemas", "list_resource_schemas"} {
		if _, present := shared[gone]; present {
			t.Fatalf("%q survived into the shared surface; tofu never reports it, so the "+
				"cross-CLI comparison would fail on a key that is not a disagreement", gone)
		}
	}
	for _, kept := range []string{"provider_schemas", "resource_schemas"} {
		if _, present := shared[kept]; !present {
			t.Fatalf("%q was dropped from the shared surface. Removing a key both CLIs report "+
				"removes whatever they might disagree about there, and the comparison passes "+
				"by seeing less", kept)
		}
	}
}

// TestTheSharedDigestChangesWithTheSharedContent is the assertion that makes
// the digest worth recording at all.
//
// A digest that did not move with the content would be a stable-looking value
// in every receipt, and a reader comparing two runs would conclude the shared
// surface was unchanged when nothing had been measured.
func TestTheSharedDigestChangesWithTheSharedContent(t *testing.T) {
	base := map[string]any{"resource_schemas": map[string]any{"unifi_network": "y"}}
	first, err := sharedSurfaceDigest(base)
	if err != nil {
		t.Fatalf("digest the shared surface: %v", err)
	}
	again, err := sharedSurfaceDigest(map[string]any{
		"resource_schemas": map[string]any{"unifi_network": "y"},
	})
	if err != nil {
		t.Fatalf("digest an equal shared surface: %v", err)
	}
	if first != again {
		t.Fatalf("two equal surfaces digested differently (%s then %s). Receipts are compared "+
			"byte for byte, so an unstable digest makes identical runs look different", first, again)
	}

	moved, err := sharedSurfaceDigest(map[string]any{
		"resource_schemas": map[string]any{"unifi_network": "CHANGED"},
	})
	if err != nil {
		t.Fatalf("digest a changed shared surface: %v", err)
	}
	if moved == first {
		t.Fatal("the shared digest did not move when the shared surface did, so it records nothing")
	}

	// The terraform-only keys are dropped before digesting, so adding one must
	// NOT move the digest. Without this the value would depend on the CLI that
	// produced it rather than on the surface both CLIs share.
	withTerraformOnly, err := sharedSurfaceDigest(map[string]any{
		"resource_schemas": map[string]any{"unifi_network": "y"},
		"action_schemas":   map[string]any{"a": 1},
	})
	if err != nil {
		t.Fatalf("digest a surface carrying terraform-only keys: %v", err)
	}
	if withTerraformOnly != first {
		t.Fatal("a terraform-only key changed the SHARED digest, so the value describes one CLI's " +
			"projection rather than the surface both report")
	}
}
