package resourcekit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// DefaultTimeout is what every operation gets when the configuration sets none.
//
// One value rather than four, because all four hand-written resources used
// 20*time.Minute for all four operations and no reason was ever recorded for it
// to differ per operation. A resource that needs its own can set Timeouts.
const DefaultTimeout = 20 * time.Minute

// Backend is the SDK client, reached through closures rather than an interface
// the SDK would have to satisfy.
//
// GENERATED PER RESOURCE AND CHECKED BY THE COMPILER. The method names are the
// one part of a resource that no artifact carries: CreateDNSRecord and
// ListDNSRecord follow from the struct name, and UpdateDNSRecordFields does
// not -- a field-masked update is a different method from a whole-object one,
// and choosing it is a decision about what the provider may overwrite.
type Backend[S any] struct {
	Create       func(ctx context.Context, site string, in *S) (*S, error)
	Read         func(ctx context.Context, site, id string) (*S, error)
	UpdateFields func(ctx context.Context, site string, in *S, fields ...string) (*S, error)
	Delete       func(ctx context.Context, site, id string) error

	// ID reads and writes the SDK struct's identity. Separate accessors rather
	// than a field name, for the same reason as everything else here: a wrong
	// one does not compile.
	GetID func(*S) string
	SetID func(*S, string)
}

// Spec is everything about one resource that varies.
type Spec[M any, S any] struct {
	// TypeName is the suffix, without the provider prefix: "dns_record".
	TypeName string
	// Subject names the resource in diagnostics: "Dns Record".
	Subject string
	Fields  []Field[M, S]
	Backend Backend[S]
	// New builds a zero SDK struct. A generated one-liner, because Go cannot
	// instantiate S from a type parameter without a constraint that would
	// exclude the SDK's own types.
	New func() *S

	// ID, Site and Timeouts reach the three attributes every managed surface
	// has and no policy declares as a field -- they are provider_owned in the
	// mapping, which is why they are here rather than in Fields.
	//
	// CLOSURES RATHER THAN AN INTERFACE, and the reason is Go rather than
	// taste. An interface with setters needs pointer receivers, which makes the
	// constraint *M and leaves the generic code unable to declare a value of M
	// to decode into. The two-type-parameter workaround for that propagates
	// through every signature in this package. Accessors cost the generator
	// three lines and are checked the same way the fields are.
	ID       func(*M) *types.String
	Site     func(*M) *types.String
	Timeouts func(*M) *timeouts.Value
}

// WireFields lists the SDK names of every attribute the plan set.
//
// THE MASK IS THE POINT OF THE UPDATE PATH. Sending the whole object would
// overwrite controller-owned attributes this provider does not model, which is
// the defect a field-masked update exists to avoid. An empty mask is refused
// rather than sent, because an update that names no field is a whole-object
// write by another route.
func (s Spec[M, S]) WireFields(plan *M) ([]string, error) {
	fields := make([]string, 0, len(s.Fields))
	seen := make(map[string]struct{}, len(s.Fields))
	for _, field := range s.Fields {
		if !field.SetInPlan(plan) {
			continue
		}
		name := field.WireName()
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("%s patch names %q twice", s.TypeName, name)
		}
		seen[name] = struct{}{}
		fields = append(fields, name)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("%s patch needs at least one managed field", s.TypeName)
	}
	return fields, nil
}

// ToSDK renders a model as the SDK struct the controller is sent.
//
// THE DIAGNOSTICS ARE NOT DECORATION. A collection attribute converts through
// the framework's ElementsAs, which reports a type mismatch rather than
// panicking, and dropping that report would send a half-built object to the
// controller with nothing said. The scalar fields never produce one; the
// signature exists for the fields that can.
func (s Spec[M, S]) ToSDK(ctx context.Context, model *M) (*S, diag.Diagnostics) {
	var diags diag.Diagnostics
	sdk := s.New()
	for _, field := range s.Fields {
		diags.Append(field.ToSDK(ctx, model, sdk)...)
	}
	return sdk, diags
}

// ToModel writes what the controller returned back onto the model.
func (s Spec[M, S]) ToModel(ctx context.Context, sdk *S, model *M, site string) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, field := range s.Fields {
		diags.Append(field.ToModel(ctx, sdk, model)...)
	}
	*s.ID(model) = types.StringValue(s.Backend.GetID(sdk))
	*s.Site(model) = types.StringValue(site)
	return diags
}

// ApplyPlanToState moves the plan's set values onto the state, leaving the rest.
func (s Spec[M, S]) ApplyPlanToState(plan, state *M) {
	for _, field := range s.Fields {
		field.CopyPlanToState(plan, state)
	}
}

// Resource is the framework implementation every managed surface shares.
type Resource[M any, S any] struct {
	Spec        Spec[M, S]
	DefaultSite string
}

func (r *Resource[M, S]) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_" + r.Spec.TypeName
}

// Site resolves the configured site against the provider default. An empty
// site in state means "wherever the provider points", not the empty string.
func (r *Resource[M, S]) Site(model *M) string {
	if site := r.Spec.Site(model).ValueString(); site != "" {
		return site
	}
	return r.DefaultSite
}

func (r *Resource[M, S]) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data M
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := (*r.Spec.Timeouts(&data)).Create(ctx, DefaultTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	site := r.Site(&data)
	sdk, diags := r.Spec.ToSDK(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.Spec.Backend.Create(ctx, site, sdk)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating "+r.Spec.Subject, err.Error())
		return
	}
	resp.Diagnostics.Append(r.Spec.ToModel(ctx, created, &data, site)...)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), (*r.Spec.ID(&data)))...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *Resource[M, S]) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data M
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := (*r.Spec.Timeouts(&data)).Read(ctx, DefaultTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	site := r.Site(&data)
	id := (*r.Spec.ID(&data)).ValueString()
	found, err := r.Spec.Backend.Read(ctx, site, id)
	if err != nil {
		// A DELETED RESOURCE IS NOT AN ERROR, it is a state to record. Removing
		// it lets the next plan recreate it; reporting it makes the practitioner
		// remove it by hand.
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading "+r.Spec.Subject,
			"Could not read "+r.Spec.Subject+" with ID "+id+": "+err.Error())
		return
	}
	resp.Diagnostics.Append(r.Spec.ToModel(ctx, found, &data, site)...)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), (*r.Spec.ID(&data)))...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *Resource[M, S]) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var state, plan M
	// STATE FIRST AND IT MATTERS. The update sends the state's values with the
	// plan's changes applied, so an attribute the plan does not mention keeps
	// what the controller last reported rather than reverting to a zero.
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := (*r.Spec.Timeouts(&plan)).Update(ctx, DefaultTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	r.Spec.ApplyPlanToState(&plan, &state)
	site := r.Site(&state)

	fields, err := r.Spec.WireFields(&plan)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating "+r.Spec.Subject, err.Error())
		return
	}
	sdk, sdkDiags := r.Spec.ToSDK(ctx, &state)
	resp.Diagnostics.Append(sdkDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := (*r.Spec.ID(&state)).ValueString()
	if id == "" {
		resp.Diagnostics.AddError("Error Updating "+r.Spec.Subject,
			r.Spec.Subject+" patch has an empty ID")
		return
	}
	r.Spec.Backend.SetID(sdk, id)

	updated, err := r.Spec.Backend.UpdateFields(ctx, site, sdk, fields...)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating "+r.Spec.Subject, err.Error())
		return
	}
	resp.Diagnostics.Append(r.Spec.ToModel(ctx, updated, &state, site)...)
	*r.Spec.Timeouts(&state) = *r.Spec.Timeouts(&plan)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), (*r.Spec.ID(&state)))...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *Resource[M, S]) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data M
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := (*r.Spec.Timeouts(&data)).Delete(ctx, DefaultTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := r.Spec.Backend.Delete(ctx, r.Site(&data), (*r.Spec.ID(&data)).ValueString()); err != nil {
		resp.Diagnostics.AddError("Error Deleting "+r.Spec.Subject, err.Error())
		return
	}
}

// ImportState accepts "site:id" or "id".
//
// The form is the provider's, not the resource's: every managed surface is
// scoped by site and identified by an opaque controller id, so this is one
// implementation rather than twenty-seven identical ones.
func (r *Resource[M, S]) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	parts := strings.Split(req.ID, ":")
	switch len(parts) {
	case 2:
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), parts[0])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
	case 1:
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	default:
		resp.Diagnostics.AddError("Invalid Import ID",
			"Import ID must be in format 'site:id' or 'id'")
	}
}
