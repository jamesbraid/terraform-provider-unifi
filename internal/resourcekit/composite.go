package resourcekit

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Section is one attribute of a Composite: whether the plan configures it,
// and how to write and read its document(s). A Composite drives every
// section through this interface, in Sections order, so a section's
// implementation can be replaced independently of the others. Unlike
// Backend[S], a Section reaches whatever client it needs on its own --
// Composite has no SDK type of its own to hand it one.
type Section[M any] interface {
	Name() string
	Configured(ctx context.Context, plan *M) bool
	// Write sends this section's document. Create and Update share it --
	// verb ("Creating"/"Updating") only reaches diagnostic text. state is
	// nil on Create, since nothing prior exists yet.
	Write(ctx context.Context, site string, plan, state *M, verb string) diag.Diagnostics
	// Read fetches this section's document, or -- when Configured(plan) is
	// false -- writes a null value onto out. Called for every section on
	// every read, not just the configured ones: that split is the
	// section's own to make, not the Composite's.
	Read(ctx context.Context, site string, plan, out *M) diag.Diagnostics
}

// Composite is the framework half of a resource whose object is not one
// document but a fixed set of independently-written sections -- e.g.
// unifi_setting: one Terraform resource, many per-section controller
// documents. It supplies Create, Read, Update and Delete; Metadata, Schema,
// ImportState and UpgradeState stay hand-written on the embedding type; see
// every Resource[M,S] descriptor for why -- the repo never relies on a
// promoted Metadata, since descriptor_policy_test.go's kitServedSurfaces
// parses it directly out of this package's own source.
type Composite[M any] struct {
	// DefaultSite is used when Site(model) is empty.
	DefaultSite string
	// Site and ID reach the two attributes every Composite has. Update
	// resolves site from the STATE model, not the plan: site has
	// UseStateForUnknown, so an unrelated apply mustn't stall on it being
	// unknown just because no section touched it yet. ID is set to the
	// resolved site on every read.
	Site func(*M) *types.String
	ID   func(*M) *types.String
	// Timeouts reaches the model's timeouts block the way Spec.Timeouts
	// does for Resource[M,S].
	Timeouts func(*M) *timeouts.Value
	// Sections is every section this Composite serves, in write order.
	Sections []Section[M]
}

// resolveSite mirrors Resource[M,S].Site: an empty configured site means
// "wherever the provider points", not the empty string.
func (c *Composite[M]) resolveSite(model *M) string {
	if site := c.Site(model).ValueString(); site != "" {
		return site
	}
	return c.DefaultSite
}

// write sends every configured section's document, in Sections order,
// stopping at the first error.
func (c *Composite[M]) write(
	ctx context.Context, site string, plan, state *M, verb string, diags *diag.Diagnostics,
) {
	for _, section := range c.Sections {
		if !section.Configured(ctx, plan) {
			continue
		}
		diags.Append(section.Write(ctx, site, plan, state, verb)...)
		if diags.HasError() {
			return
		}
	}
}

// read sets the resolved site onto id and site, then re-fetches every
// section in Sections order -- including the unconfigured ones, each of
// which reports itself null rather than being skipped here.
func (c *Composite[M]) read(
	ctx context.Context, site string, plan, out *M, diags *diag.Diagnostics,
) {
	*c.ID(out) = types.StringValue(site)
	*c.Site(out) = types.StringValue(site)
	for _, section := range c.Sections {
		diags.Append(section.Read(ctx, site, plan, out)...)
		if diags.HasError() {
			return
		}
	}
}

func (c *Composite[M]) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan M
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := (*c.Timeouts(&plan)).Create(ctx, DefaultTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	site := c.resolveSite(&plan)
	// original is the plan as the practitioner wrote it, before write()
	// overwrites plan's own section attributes with each document's
	// response. read() below needs original, not plan, as the source of
	// Configured() and of AfterReceive's prior -- see SpecSection.Write's own
	// comment (spec_section.go).
	original := plan
	c.write(ctx, site, &plan, nil, "Creating", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	c.read(ctx, site, &original, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (c *Composite[M]) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data M
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := (*c.Timeouts(&data)).Read(ctx, DefaultTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	site := c.resolveSite(&data)
	c.read(ctx, site, &data, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (c *Composite[M]) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var state, plan M
	// State first, and it matters: site is read from state below, not plan.
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := (*c.Timeouts(&plan)).Update(ctx, DefaultTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Site comes from state, not the plan: site has UseStateForUnknown, so
	// the plan can carry unknown where state carries the real name.
	site := c.resolveSite(&state)
	// original is the plan as the practitioner wrote it, before write()
	// overwrites plan's own section attributes with each document's
	// response. read() below needs original, not plan, as the source of
	// Configured() and of AfterReceive's prior -- see SpecSection.Write's own
	// comment (spec_section.go).
	original := plan
	c.write(ctx, site, &plan, &state, "Updating", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	c.read(ctx, site, &original, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete is a no-op: a Composite's sections have nothing on the controller
// that "deleting the resource" should undo -- settings just keep their
// last-written values. Dropping the resource from state is the whole
// contract, and the framework does that once this returns without error.
func (c *Composite[M]) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}
