package unifi

// The tripwire that makes graftPreservedCollections a stopgap rather than a
// permanent hand-edit.
//
// That function sets ip_aliases, nat_outbound_ip_addresses and
// ipv6_pd_prefixid to Optional+Computed with UseStateForUnknown, because the
// generated schema cannot currently carry them: regenerating unifi_network with
// the pinned generator emits CustomType bindings nothing in the provider
// produces and the suite rejects the result (#195). The declarations live in
// provider-codegen/policy/network.json and the compiled specification already
// agrees; only the Go artifact lags.
//
// Its comment says "delete this function when the regenerate is clean". That is
// an instruction to a human, and the graft is idempotent with what the
// generator would emit, so nothing would fail on the day it became redundant --
// it would simply sit there forever.
//
// So this asserts the graft is STILL NECESSARY. It passes while the generated
// schema lacks the three. The day somebody fixes #195 and regenerates, it fails
// and names the function to delete.

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	resource_network "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_network"
)

// graftedAttributeState reports whether an attribute already carries both
// halves the graft applies.
func graftedAttributeState(attribute schema.Attribute) (computed, planModified bool) {
	switch typed := attribute.(type) {
	case schema.ListAttribute:
		return typed.Computed, len(typed.PlanModifiers) > 0
	case schema.ListNestedAttribute:
		return typed.Computed, len(typed.PlanModifiers) > 0
	case schema.StringAttribute:
		return typed.Computed, len(typed.PlanModifiers) > 0
	}
	return false, false
}

func TestGraftPreservedCollectionsIsStillNecessary(t *testing.T) {
	generated := resource_network.NetworkResourceSchema(context.Background())

	for _, name := range []string{"ip_aliases", "nat_outbound_ip_addresses", "ipv6_pd_prefixid"} {
		attribute, ok := generated.Attributes[name]
		if !ok {
			t.Errorf("%s is not in the generated schema at all; the graft asserts "+
				"something about an attribute that no longer exists", name)
			continue
		}
		computed, planModified := graftedAttributeState(attribute)
		if computed && planModified {
			t.Errorf("the GENERATED schema now declares %s as Computed with a plan "+
				"modifier, so graftPreservedCollections in network_resource.go is "+
				"redundant. Delete it, delete this test, and check the policy in "+
				"provider-codegen/policy/network.json still says the same thing.", name)
		}
	}
}

// TestGraftPreservedCollectionsAppliesBothHalves is the other side: the graft is
// necessary AND it works. Without this, the test above would keep passing after
// somebody deleted the graft without fixing the generator, which is the failure
// it is meant to prevent rather than cause.
func TestGraftPreservedCollectionsAppliesBothHalves(t *testing.T) {
	ctx := context.Background()
	served := resource_network.NetworkResourceSchema(ctx)
	graftPreservedCollections(served.Attributes)

	for _, name := range []string{"ip_aliases", "nat_outbound_ip_addresses", "ipv6_pd_prefixid"} {
		computed, planModified := graftedAttributeState(served.Attributes[name])
		if !computed {
			t.Errorf("%s is not Computed after the graft, so an omitted attribute "+
				"still plans null and the write still clears the controller's value", name)
		}
		if !planModified {
			t.Errorf("%s has no plan modifier after the graft, so the unknown it now "+
				"plans resolves to null rather than to the value just read", name)
		}
	}
}
