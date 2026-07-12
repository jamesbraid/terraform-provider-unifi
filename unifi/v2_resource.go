package unifi

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// resolveV2Site returns the effective site for a v2 resource operation: the
// configured site value when it is neither null nor empty, otherwise
// providerDefault. It is a v2-resource-shaped wrapper over resolveSite
// (unifi/site.go), which already implements this rule for unifi_setting;
// resolveV2Site exists so v2 resources call the same logic instead of each
// inlining "site := x.ValueString(); if site == \"\" { site = def }".
func resolveV2Site(configuredSite types.String, providerDefault string) string {
	return resolveSite(configuredSite.ValueString(), providerDefault)
}

// v2IsNotFound reports whether err is the go-unifi SDK's NotFoundError,
// centralizing the type assertion every v2 resource's Read and Delete
// perform against controller errors.
func v2IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*unifi.NotFoundError)
	return ok
}

// v2Configure implements the shared Configure body every v2 resource uses:
// if req.ProviderData is nil (the provider has not been configured yet), it
// returns nil with no diagnostic, matching every existing resource's
// early-return; if req.ProviderData is not a *Client, it adds an error
// diagnostic and returns nil; otherwise it returns the *Client. Callers
// assign the result directly:
//
//	func (r *fooResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
//	    r.client = v2Configure(req, resp)
//	}
//
// Centralizes the byte-identical bodies in firewall_policy_resource.go:400-422
// and firewall_zone_resource.go:150-172.
func v2Configure(
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) *Client {
	if req.ProviderData == nil {
		return nil
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf(
				"Expected *Client, got: %T. Please report this issue to the provider developers.",
				req.ProviderData,
			),
		)
		return nil
	}
	return client
}

// v2TimeoutOp selects which of a resource's Create/Read/Update/Delete
// timeout budgets v2Timeout extracts.
type v2TimeoutOp int

const (
	v2TimeoutCreate v2TimeoutOp = iota
	v2TimeoutRead
	v2TimeoutUpdate
	v2TimeoutDelete
)

// v2DefaultTimeout is the fallback operation timeout used by every existing
// v2 resource (firewall_policy_resource.go, firewall_zone_resource.go,
// dns_record_resource.go, port_forward_resource.go, and others) when the
// user's configuration does not set one.
const v2DefaultTimeout = 20 * time.Minute

// v2Timeout extracts the timeout for op from t and returns a context bound
// to it and the corresponding cancel function, replacing the four-step
// "extract timeout -> check diagnostics -> context.WithTimeout -> defer
// cancel" block every existing v2 resource's CRUD methods repeat. The
// caller is still responsible for calling the returned CancelFunc (typically
// via defer), exactly as today's hand-written call sites do. An op value
// outside the four declared v2TimeoutOp constants is a programmer error
// (there is no user-facing way to produce one); v2Timeout reports it as an
// error diagnostic rather than panicking or silently defaulting to one of
// the four real op kinds, so a resource author who passes a stray value
// gets a clear diagnostic instead of a mismatched timeout.
func v2Timeout(
	ctx context.Context,
	t timeouts.Value,
	op v2TimeoutOp,
) (context.Context, context.CancelFunc, diag.Diagnostics) {
	var (
		d     time.Duration
		diags diag.Diagnostics
	)
	switch op {
	case v2TimeoutCreate:
		d, diags = t.Create(ctx, v2DefaultTimeout)
	case v2TimeoutRead:
		d, diags = t.Read(ctx, v2DefaultTimeout)
	case v2TimeoutUpdate:
		d, diags = t.Update(ctx, v2DefaultTimeout)
	case v2TimeoutDelete:
		d, diags = t.Delete(ctx, v2DefaultTimeout)
	default:
		diags.AddError(
			"Invalid v2TimeoutOp",
			fmt.Sprintf("v2Timeout called with unrecognized v2TimeoutOp value %d; this is a provider bug, not a user configuration error", int(op)),
		)
		return ctx, func() {}, diags
	}
	if diags.HasError() {
		return ctx, func() {}, diags
	}
	newCtx, cancel := context.WithTimeout(ctx, d)
	return newCtx, cancel, diags
}

// v2SetIdentityAndState writes the resource identity's "id" attribute and,
// only if that succeeds, the full resource state — the order every existing
// v2 resource's Create/Read/Update already uses:
//
//	diags.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), model.ID)...)
//	if diags.HasError() { return diags }
//	diags.Append(resp.State.Set(ctx, model)...)
//
// If identitySetter.SetAttribute returns a diagnostic error,
// v2SetIdentityAndState returns immediately with those diagnostics and does
// NOT call stateSetter.Set: an identity-write failure must never be
// followed by a state write, per the halt-before-state invariant (design
// §5.1/§199). id must be the model's own ID field (callers pass model.ID,
// never a derived value), keeping "identity == persisted id" explicit at
// the call site. identitySetter and stateSetter are satisfied by
// *resource.CreateResponse.Identity/.State (and the Read/Update
// equivalents); the narrow interfaces here exist only so this function is
// unit-testable without constructing a full framework response.
func v2SetIdentityAndState[T any](
	ctx context.Context,
	identitySetter interface {
		SetAttribute(context.Context, path.Path, any) diag.Diagnostics
	},
	stateSetter interface {
		Set(context.Context, any) diag.Diagnostics
	},
	id types.String,
	model *T,
) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(identitySetter.SetAttribute(ctx, path.Root("id"), id)...)
	if diags.HasError() {
		return diags
	}
	diags.Append(stateSetter.Set(ctx, model)...)
	return diags
}

// v2FinishRead applies the standard v2 resource Read epilogue for a
// controller error: if err is a NotFoundError, the resource is removed from
// state (resp.State.RemoveResource) and v2FinishRead returns true; if err is
// any other non-nil error, an error diagnostic titled errSummary is added to
// resp.Diagnostics and v2FinishRead returns true; if err is nil,
// v2FinishRead adds no diagnostic and returns false. In both true cases the
// caller's Read method must return immediately, mirroring the
//
//	if _, ok := err.(*unifi.NotFoundError); ok {
//	    resp.State.RemoveResource(ctx)
//	    return
//	}
//	resp.Diagnostics.AddError(errSummary, "...: "+err.Error())
//	return
//
// block every existing v2 resource's Read method repeats today.
func v2FinishRead(
	ctx context.Context,
	resp *resource.ReadResponse,
	err error,
	errSummary string,
) bool {
	if err == nil {
		return false
	}
	if v2IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return true
	}
	resp.Diagnostics.AddError(errSummary, err.Error())
	return true
}

// v2ImportState implements the shared "<id>" / "<site>:<id>" import grammar
// for a v2 resource whose schema has top-level "site" and "id" attributes.
// It delegates parsing to parseSiteID (unifi/site.go) — the same grammar
// unifi_nat_rule and unifi_content_filtering use for their own composite
// identity — so a v2 resource's ImportState body reduces to:
//
//	func (r *fooResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
//	    v2ImportState(ctx, req, resp, r.client.Site)
//	}
//
// An empty id, or an id with more than one ":"-separated site prefix, is an
// error diagnostic (never a silent default) — matching parseSiteID's
// existing contract.
func v2ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
	providerDefault string,
) {
	site, id, err := parseSiteID(req.ID, providerDefault)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), site)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// objectAsOptions decodes obj into a new value of type T using the
// framework's standard (zero-value) basetypes.ObjectAsOptions, replacing the
// repeated
//
//	var m someModel
//	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
//
// call-site pattern found throughout unifi/*.go. On non-empty diagnostics,
// callers MUST append them and return BEFORE any API request, any
// model→API conversion use of the returned value, or any State.Set —
// objectAsOptions does not itself decide whether a conversion failure halts
// the caller's operation, matching every existing modelToX conversion
// function's contract. Canonical call site:
//
//	m, diags := objectAsOptions[fooModel](ctx, model.Foo)
//	resp.Diagnostics.Append(diags...)
//	if resp.Diagnostics.HasError() {
//	    return
//	}
//	// ... use m in an API request, conversion, or resp.State.Set ...
//
// obj.IsNull() or obj.IsUnknown() both decode to T's zero value with no
// diagnostics. This is a deliberate new helper semantic: the framework's
// own zero-value basetypes.ObjectAsOptions errors when a null or unknown
// value can't be represented in T, and objectAsOptions intentionally
// preempts that error by short-circuiting to the zero value first, so every
// caller gets consistent null/unknown handling for free.
func objectAsOptions[T any](ctx context.Context, obj types.Object) (T, diag.Diagnostics) {
	var out T
	if obj.IsNull() || obj.IsUnknown() {
		return out, nil
	}
	diags := obj.As(ctx, &out, basetypes.ObjectAsOptions{})
	return out, diags
}

// objectListAsOptions is the ListNestedAttribute analogue of objectAsOptions:
// it decodes every element of list into []T using ElementsAs under the same
// options contract. list.IsNull() or list.IsUnknown() both decode to a nil
// slice with no diagnostics — the same deliberate preemption of the
// framework's null/unknown error behavior that objectAsOptions applies. On
// non-empty diagnostics, callers MUST append them and return BEFORE any API
// request, any model→API conversion use of the returned slice, or any
// State.Set, for the same reason as objectAsOptions.
func objectListAsOptions[T any](ctx context.Context, list types.List) ([]T, diag.Diagnostics) {
	if list.IsNull() || list.IsUnknown() {
		return nil, nil
	}
	var out []T
	diags := list.ElementsAs(ctx, &out, false)
	return out, diags
}
