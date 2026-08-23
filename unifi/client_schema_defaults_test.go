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
// migration created.
//
// The hand-written schema wrote `booldefault.StaticBool(defaultAllowExisting)`,
// so the schema default and the state-setting path at clientToModel could not
// disagree -- both read one constant. The code specification cannot carry a
// reference to a package constant, and a custom default naming one would not
// compile in the generated package, which imports nothing from `unifi` and
// cannot: `unifi` imports the generated packages, so the dependency runs the
// other way.
//
// So the policy states the values, and the constants remain the source for the
// state-setting path. That is one fact with two homes, which is the failure mode
// this repository keeps finding -- and the only reason it is acceptable here is
// that the two are compared. Change either alone and this goes red.
func Test_clientGeneratedDefaultsStillEqualTheConstants(t *testing.T) {
	resp := &fwresource.SchemaResponse{}
	(&clientResource{}).Schema(context.Background(), fwresource.SchemaRequest{}, resp)

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
