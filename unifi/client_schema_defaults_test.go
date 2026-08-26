package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/defaults"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Test_clientGeneratedDefaultsStillEqualTheConstants closes a hazard this
// migration created: the code specification can't reference a package
// constant (the generated package can't import `unifi`; the dependency
// runs the other way), so the schema default and clientToModel's constant
// are now two homes for one fact instead of a single
// `booldefault.StaticBool(defaultAllowExisting)`. That's acceptable only
// because the two are compared here -- change either alone and this goes
// red.
func Test_clientGeneratedDefaultsStillEqualTheConstants(t *testing.T) {
	resp := &fwresource.SchemaResponse{}
	newClientKitResource().Schema(context.Background(), fwresource.SchemaRequest{}, resp)

	for name, want := range map[string]bool{
		"allow_existing":         defaultAllowExisting,
		"skip_forget_on_destroy": defaultSkipForgetOnDestroy,
	} {
		attribute, present := resp.Schema.Attributes[name]
		if !present {
			t.Fatalf("the generated client schema has no %q attribute", name)
		}
		boolAttribute, ok := attribute.(schema.BoolAttribute)
		if !ok {
			t.Fatalf("%s is %T, want a bool attribute", name, attribute)
		}
		if boolAttribute.Default == nil {
			t.Fatalf("%s carries no default; the hand-written schema defaulted it to %v", name, want)
		}
		got := defaultBoolValue(t, boolAttribute.Default)
		if got != want {
			t.Fatalf("the generated schema defaults %s to %v, and the constant the resource "+
				"still reads says %v; the policy and the constant have diverged",
				name, got, want)
		}
	}
}

func defaultBoolValue(t *testing.T, d defaults.Bool) bool {
	t.Helper()
	resp := defaults.BoolResponse{}
	d.DefaultBool(context.Background(), defaults.BoolRequest{}, &resp)
	if resp.PlanValue.IsNull() || resp.PlanValue.IsUnknown() {
		t.Fatalf("the default produced %v rather than a known bool", resp.PlanValue)
	}
	return resp.PlanValue.Equal(types.BoolValue(true))
}
