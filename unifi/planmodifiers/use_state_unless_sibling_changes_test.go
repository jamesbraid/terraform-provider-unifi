package planmodifiers_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/planmodifiers"
)

const (
	rateAttr = "minimum_data_rate_5g_kbps"
	prefAttr = "minrate_setting_preference"
)

func testSchema() schema.Schema {
	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			rateAttr: schema.Int64Attribute{Optional: true, Computed: true},
			prefAttr: schema.StringAttribute{Optional: true, Computed: true},
		},
	}
}

func objType() tftypes.Object {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		rateAttr: tftypes.Number,
		prefAttr: tftypes.String,
	}}
}

// rawValue builds a tftypes object. A nil rate means unknown.
func rawValue(rate *int64, pref string) tftypes.Value {
	rateVal := tftypes.NewValue(tftypes.Number, tftypes.UnknownValue)
	if rate != nil {
		rateVal = tftypes.NewValue(tftypes.Number, *rate)
	}
	return tftypes.NewValue(objType(), map[string]tftypes.Value{
		rateAttr: rateVal,
		prefAttr: tftypes.NewValue(tftypes.String, pref),
	})
}

func int64p(v int64) *int64 { return &v }

func pathToRate() path.Path { return path.Root(rateAttr) }

// request builds the plan-modifier request for an unknown planned rate, a null
// config rate (the practitioner did not name one), a "manual" state value of
// the sibling preference attribute (every test case here holds it fixed --
// only the planned value varies), and the given state rate and planned
// sibling preference.
func request(planPref string, stateRate int64) planmodifier.Int64Request {
	s := testSchema()
	return planmodifier.Int64Request{
		Path:        pathToRate(),
		ConfigValue: types.Int64Null(),
		StateValue:  types.Int64Value(stateRate),
		PlanValue:   types.Int64Unknown(),
		State:       tfsdk.State{Schema: s, Raw: rawValue(int64p(stateRate), "manual")},
		Plan:        tfsdk.Plan{Schema: s, Raw: rawValue(nil, planPref)},
		Config:      tfsdk.Config{Schema: s, Raw: rawValue(nil, planPref)},
	}
}

// TestUseStateUnlessSiblingChanges pins the behaviour that keeps an apply
// from dying when the controller recomputes a derived attribute. The
// control arm demonstrates the defect: the stock
// int64planmodifier.UseStateForUnknown commits to a stale value when the
// sibling preference attribute changes.
func TestUseStateUnlessSiblingChanges(t *testing.T) {
	ctx := context.Background()
	mod := planmodifiers.UseStateUnlessSiblingChanges{Sibling: prefAttr}

	t.Run("sibling changing leaves the value unknown", func(t *testing.T) {
		req := request("auto", 0)
		resp := &planmodifier.Int64Response{PlanValue: req.PlanValue}

		mod.PlanModifyInt64(ctx, req, resp)

		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
		}
		if !resp.PlanValue.IsUnknown() {
			t.Errorf("plan value = %v, want unknown.\n"+
				"The controller recomputes this attribute when %s changes, so the plan "+
				"must not promise the prior value. Committing to it is what produced "+
				"'was cty.NumberIntVal(0), but now cty.NumberIntVal(6000)'.",
				resp.PlanValue, prefAttr)
		}
	})

	t.Run("sibling unchanged keeps the state value", func(t *testing.T) {
		req := request("manual", 6000)
		resp := &planmodifier.Int64Response{PlanValue: req.PlanValue}

		mod.PlanModifyInt64(ctx, req, resp)

		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
		}
		if resp.PlanValue.IsUnknown() {
			t.Error("plan value is unknown; want the state value kept. Leaving it " +
				"unknown when nothing changed shows a spurious '(known after apply)' " +
				"on every plan.")
		}
		if got := resp.PlanValue; !got.Equal(types.Int64Value(6000)) {
			t.Errorf("plan value = %v, want 6000", got)
		}
	})

	t.Run("configured value is never touched", func(t *testing.T) {
		req := request("auto", 0)
		req.ConfigValue = types.Int64Value(12000)
		req.PlanValue = types.Int64Value(12000)
		resp := &planmodifier.Int64Response{PlanValue: req.PlanValue}

		mod.PlanModifyInt64(ctx, req, resp)

		if !resp.PlanValue.Equal(types.Int64Value(12000)) {
			t.Errorf("plan value = %v, want the configured 12000 left alone", resp.PlanValue)
		}
	})

	t.Run("a missing sibling errors rather than assuming unchanged", func(t *testing.T) {
		req := request("auto", 0)
		bad := planmodifiers.UseStateUnlessSiblingChanges{Sibling: "no_such_attribute"}
		resp := &planmodifier.Int64Response{PlanValue: req.PlanValue}

		bad.PlanModifyInt64(ctx, req, resp)

		if !resp.Diagnostics.HasError() {
			t.Error("a sibling that does not exist produced no error. Treating it as " +
				"'unchanged' would silently reintroduce the failure this modifier prevents.")
		}
	})

	// Control: the stock modifier, same request, demonstrating the defect.
	t.Run("CONTROL int64planmodifier.UseStateForUnknown commits to the stale value", func(t *testing.T) {
		req := request("auto", 0)
		resp := &planmodifier.Int64Response{PlanValue: req.PlanValue}

		int64planmodifier.UseStateForUnknown().PlanModifyInt64(ctx, req, resp)

		if resp.PlanValue.IsUnknown() {
			t.Skip("the stock modifier no longer pins the state value; the framework " +
				"changed and this control needs revisiting")
		}
		if !resp.PlanValue.Equal(types.Int64Value(0)) {
			t.Errorf("control: stock modifier planned %v, expected it to pin the stale 0 "+
				"-- if this changed, re-derive whether the custom modifier is still needed",
				resp.PlanValue)
		}
	})
}
