package resourcekit

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// Document is one controller document a section reads and writes. A
// SpecSection's primary Spec is one; Extra holds the rest, over the same
// section model, each masking only the fields it maps.
type Document[SM any] interface {
	// Write sends the fields of plan that this document maps and that the
	// plan set; an empty mask is a no-op, not an error (the document is
	// unconfigured). prior accumulates the fields this document's own
	// response covers -- the same model Extra shares with the primary and
	// with each other, since they map disjoint fields of it.
	Write(ctx context.Context, site string, plan *SM, prior *SM) diag.Diagnostics
	// Read decodes this document's fields into model, leaving every other
	// field untouched. NotFound is reported through OnNotFound.
	Read(ctx context.Context, site string, model *SM) diag.Diagnostics
}

// SpecDocument adapts a Spec[SM, S] + Backend[S] into a Document[SM]. It is
// what an Extra is made of; the primary Spec on a SpecSection is one too,
// modulo the not-found handling described below.
type SpecDocument[SM any, S any] struct {
	Spec Spec[SM, S]
	// OnNotFound turns the backend's not-found into diagnostics; nil means
	// "leave the model's fields as they are, no diagnostic" -- the natural
	// default for a document that may simply not exist yet on this
	// controller (usg_geo, ips_suppression on a site that never configured
	// either).
	OnNotFound func() diag.Diagnostics
}

// specDocumentWrite is the mask/send/merge machinery a document's write
// needs, shared by SpecDocument.Write and SpecSection.Write's own primary
// step: build the SDK struct from plan, mask it down to what the plan
// actually set, send it, and merge the response's fields onto model. The
// returned *S is nil when the mask was empty -- a no-op, not an error --
// which is how the primary tells whether it has a response to hand its own
// AfterReceive hook.
func specDocumentWrite[SM any, S any](
	ctx context.Context, spec Spec[SM, S], site, errSummary string, plan, model *SM,
) (*S, diag.Diagnostics) {
	var diags diag.Diagnostics
	sdk, d := spec.ToSDK(ctx, plan)
	diags.Append(d...)
	if diags.HasError() {
		return nil, diags
	}

	fields, err := spec.maskFields(plan)
	if err != nil {
		diags.AddError(errSummary, err.Error())
		return nil, diags
	}
	if len(fields) == 0 {
		return nil, diags
	}

	updated, err := spec.Backend.UpdateFields(ctx, site, sdk, fields...)
	if err != nil {
		diags.AddError(errSummary, err.Error())
		return nil, diags
	}

	diags.Append(spec.ToModel(ctx, updated, model, "")...)
	return updated, diags
}

func (d SpecDocument[SM, S]) Write(ctx context.Context, site string, plan, prior *SM) diag.Diagnostics {
	_, diags := specDocumentWrite(ctx, d.Spec, site, "Error Writing "+d.Spec.Subject, plan, prior)
	if diags.HasError() {
		return diags
	}
	// The response doesn't outrank the plan for a value the plan set -- the
	// same rule the primary applies via Spec.ApplyPlanToState.
	d.Spec.ApplyPlanToState(plan, prior)
	return diags
}

func (d SpecDocument[SM, S]) Read(ctx context.Context, site string, model *SM) diag.Diagnostics {
	var diags diag.Diagnostics
	sdk, err := d.Spec.Backend.Read(ctx, site, "")
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			if d.OnNotFound != nil {
				diags.Append(d.OnNotFound()...)
			}
			return diags
		}
		diags.AddError("Error Reading "+d.Spec.Subject, err.Error())
		return diags
	}
	diags.Append(d.Spec.ToModel(ctx, sdk, model, "")...)
	return diags
}

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

	// Extra holds documents beyond the primary, over the same section
	// model: usg writes usg and (only when configured) usg_geo; ips writes
	// ips and ips_suppression. Written after Spec, in order, each masking
	// only the fields it maps -- an empty mask skips that Extra's write
	// without erroring, since it may simply be unconfigured. Read after
	// Spec, in order, and always: an Extra's fields hydrate from the
	// controller regardless of whether its write ran, the same way the
	// primary's own unconfigured fields do.
	Extra []Document[SM]

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

	// fresh starts as a copy of the plan so a field no Field on this Spec
	// covers -- an Extra-only attribute like usg_geo's -- carries the
	// plan's own value forward until that Extra's own Read or Write
	// contributes the controller's.
	fresh := planModel
	updated, d := specDocumentWrite(ctx, s.Spec, site, "Error "+verb+" "+s.Spec.Subject, &planModel, &fresh)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	if updated == nil && len(s.Extra) == 0 {
		// A configured-but-empty section object with nothing else to write
		// either: nothing the plan set, so nothing to do. WireFields would
		// refuse this for a whole resource; a section's empty object is a
		// legitimate no-op instead.
		return diags
	}
	if updated != nil {
		diags.Append(s.runAfterReceive(ctx, updated, &fresh, planModel)...)
		if diags.HasError() {
			return diags
		}
		// The response doesn't outrank the plan for a value the plan set,
		// the same rule Resource[M,S]'s Create/Update tail applies.
		s.Spec.ApplyPlanToState(&planModel, &fresh)
	}

	// Extra, in order; the first error aborts before later Extras, the same
	// rule Composite applies across sections.
	for _, extra := range s.Extra {
		diags.Append(extra.Write(ctx, site, &planModel, &fresh)...)
		if diags.HasError() {
			return diags
		}
	}

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
	if diags.HasError() {
		return diags
	}

	// Extra, in order and unconditionally -- an Extra's fields hydrate from
	// the controller even when its write was skipped as unconfigured, the
	// same way the primary's own unset fields do.
	for _, extra := range s.Extra {
		diags.Append(extra.Read(ctx, site, &model)...)
		if diags.HasError() {
			return diags
		}
	}

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
