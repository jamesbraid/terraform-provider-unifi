package resourcekit

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// SpecSection serves one Composite[M] section from a Spec[SM, S] instead of
// a hand-written read/write pair (legacySection's shape). M is the whole
// resource's model (settingResourceModel); SM is this section's own model,
// decoded out of one types.Object attribute of M; S is the SDK struct
// Spec.Backend reads and writes.
type SpecSection[M any, SM any, S any] struct {
	SectionName string
	// Get and Set reach the section's own attribute on the whole model --
	// the types.Object a legacySection's Configured/Read null-arm also work
	// against.
	Get func(*M) *types.Object
	Set func(*M, types.Object)
	// AttrTypes types the section's object in state; it must match the
	// schema's nested object exactly, the same contract as ObjectField's.
	AttrTypes map[string]attr.Type

	Spec Spec[SM, S]

	// AfterReceive runs after every ToModel, on both Write and Read: model
	// has already been overwritten field-by-field from the SDK struct, and
	// prior is the section's own plan value -- what the practitioner
	// configured, decoded the same way Get/decode would. mgmt's descriptor
	// uses this to null every attribute the plan didn't set (parity with
	// today's mgmtSettingToModel) and to restore ssh_password, which the
	// controller never echoes back. Distinct from Spec.AfterReceive: that
	// one also carries a Prefetch result no section needs, and running it
	// from both Write and Read (as this field's contract requires) would
	// double the prefetch plumbing for no section's actual benefit.
	AfterReceive func(ctx context.Context, sdk *S, model *SM, prior SM) diag.Diagnostics
}

func (s SpecSection[M, SM, S]) Name() string { return s.SectionName }

func (s SpecSection[M, SM, S]) Configured(_ context.Context, plan *M) bool {
	object := *s.Get(plan)
	return !object.IsNull() && !object.IsUnknown()
}

func (s SpecSection[M, SM, S]) decode(ctx context.Context, object types.Object) (SM, diag.Diagnostics) {
	var model SM
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	return model, diags
}

func (s SpecSection[M, SM, S]) encode(ctx context.Context, model SM) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, s.AttrTypes, model)
}

func (s SpecSection[M, SM, S]) runAfterReceive(
	ctx context.Context, sdk *S, model *SM, prior SM,
) diag.Diagnostics {
	if s.AfterReceive == nil {
		return nil
	}
	return s.AfterReceive(ctx, sdk, model, prior)
}

// Write decodes the plan's section object, sends only the fields the plan
// set, and writes the refreshed result back onto plan's own attribute --
// mirroring Resource[M,S]'s Create/Update tail, even though a Composite's
// own Read runs again right after and will overwrite whatever this leaves,
// since a section is unit-tested through Write alone as often as through
// the whole Composite.
func (s SpecSection[M, SM, S]) Write(
	ctx context.Context, site string, plan, _ *M, verb string,
) diag.Diagnostics {
	var diags diag.Diagnostics
	planModel, d := s.decode(ctx, *s.Get(plan))
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	sdk, d := s.Spec.ToSDK(ctx, &planModel)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	fields, err := s.Spec.maskFields(&planModel)
	if err != nil {
		diags.AddError("Error "+verb+" "+s.Spec.Subject, err.Error())
		return diags
	}
	if len(fields) == 0 {
		// A configured-but-empty section object: nothing the plan set, so
		// nothing to write. WireFields would refuse this for a whole
		// resource; a section's empty object is a legitimate no-op instead.
		return diags
	}

	updated, err := s.Spec.Backend.UpdateFields(ctx, site, sdk, fields...)
	if err != nil {
		diags.AddError("Error "+verb+" "+s.Spec.Subject, err.Error())
		return diags
	}

	var fresh SM
	diags.Append(s.Spec.ToModel(ctx, updated, &fresh, "")...)
	diags.Append(s.runAfterReceive(ctx, updated, &fresh, planModel)...)
	if diags.HasError() {
		return diags
	}
	// The response doesn't outrank the plan for a value the plan set, the
	// same rule Resource[M,S]'s Create/Update tail applies.
	s.Spec.ApplyPlanToState(&planModel, &fresh)

	object, d := s.encode(ctx, fresh)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	s.Set(plan, object)
	return diags
}

// Read fetches the section fresh when the plan configures it, or writes
// null onto out when it does not -- match legacySection's own read arm.
func (s SpecSection[M, SM, S]) Read(
	ctx context.Context, site string, plan, out *M,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if !s.Configured(ctx, plan) {
		s.Set(out, types.ObjectNull(s.AttrTypes))
		return diags
	}

	planModel, d := s.decode(ctx, *s.Get(plan))
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	sdk, err := s.Spec.Backend.Read(ctx, site, "")
	if err != nil {
		diags.AddError("Error Reading "+s.Spec.Subject, err.Error())
		return diags
	}

	var model SM
	diags.Append(s.Spec.ToModel(ctx, sdk, &model, "")...)
	diags.Append(s.runAfterReceive(ctx, sdk, &model, planModel)...)
	if diags.HasError() {
		return diags
	}

	object, d := s.encode(ctx, model)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	s.Set(out, object)
	return diags
}
