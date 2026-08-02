package unifi

// REGRESSION GUARD — read before "fixing" a failure here.
//
// This file pins a terraform-plugin-framework / Terraform Core contract that
// the settings engine's best-effort state recovery depends on: when a
// resource's Update writes new state into resp.State AND also appends an
// error diagnostic, Terraform Core still PERSISTS that partial state
// alongside the error, rather than discarding it or rolling back to the
// prior state.
//
// Verified in source (terraform-plugin-framework v1.19.0):
//
//	internal/fwserver/server_updateresource.go:154
//	    resp.NewState = &updateResp.State
//
// That assignment is UNCONDITIONAL — it runs regardless of
// updateResp.Diagnostics.HasError(). The null-state guard immediately below
// it (:157) only fires when there is NO error, so an errored Update with a
// non-null state sails through untouched. This test proves Terraform Core
// (not just the framework's internal return value) actually keeps that
// state, by driving a full apply through the plugin-testing harness.
//
// This is a THROWAWAY in-memory resource. It has nothing to do with the
// unifi provider or the settings engine — it exists solely to pin the
// framework/Core contract end-to-end so a future terraform-plugin-framework
// or terraform-plugin-testing bump cannot silently break it out from under
// the engine.
//
// If this test ever FAILS after a dependency bump: STOP. Do not "fix" it by
// loosening the assertion, and do not go "fix" the engine by turning its
// failed-apply-with-partial-state path into a warning/success. That would be
// a spec change (the read-back-failure recovery path requires the operation
// stay failed while best-effort state is preserved), not an implementation
// detail. Escalate to the maintainer with the diff in framework behavior.

import (
	"context"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	tftesting "github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// --- throwaway provider -----------------------------------------------

var _ provider.Provider = &stateProbeProvider{}

// stateProbeProvider is a minimal, self-contained provider that exists only
// to host stateProbeResource for this regression guard. It has no
// relationship to the real unifi provider (see provider.go) and requires no
// controller/credentials.
type stateProbeProvider struct{}

func (p *stateProbeProvider) Metadata(
	ctx context.Context,
	req provider.MetadataRequest,
	resp *provider.MetadataResponse,
) {
	resp.TypeName = "frameworkstateprobe"
}

func (p *stateProbeProvider) Schema(
	ctx context.Context,
	req provider.SchemaRequest,
	resp *provider.SchemaResponse,
) {
	// No provider-level configuration needed.
}

func (p *stateProbeProvider) Configure(
	ctx context.Context,
	req provider.ConfigureRequest,
	resp *provider.ConfigureResponse,
) {
	// No-op: the throwaway resource keeps all of its state in-memory via
	// Terraform state itself, so there is nothing to configure.
}

func (p *stateProbeProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewStateProbeResource,
	}
}

func (p *stateProbeProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

// --- throwaway resource --------------------------------------------------

var _ resource.Resource = &stateProbeResource{}

// stateProbeResource is a throwaway in-memory resource whose sole purpose is
// to exercise the Update-writes-state-then-errors path. It talks to no
// external system: Create/Update just echo the planned value into state,
// Read is a no-op (trusts state), Delete is a no-op.
type stateProbeResource struct{}

func NewStateProbeResource() resource.Resource {
	return &stateProbeResource{}
}

type stateProbeResourceModel struct {
	ID    types.String `tfsdk:"id"`
	Value types.String `tfsdk:"value"`
}

func (r *stateProbeResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_probe"
}

func (r *stateProbeResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"value": schema.StringAttribute{
				Optional: true,
				Computed: true,
			},
		},
	}
}

func (r *stateProbeResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data stateProbeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.ID = types.StringValue("probe-fixed-id")
	// data.Value already carries the planned value from config.

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *stateProbeResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	// Trivial: trust whatever is already in state. This resource has no
	// external system to read back from, so Read is a pass-through. This
	// matters for the RefreshState step below: `terraform refresh` calls
	// Read, and since Read doesn't mutate anything, the persisted
	// post-error state survives refresh unchanged — which is exactly what
	// this test needs to observe.
	var data stateProbeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update is the crux of this regression guard: it writes the NEW planned
// value into resp.State and THEN appends an error diagnostic. Per the
// settled framework behavior documented at the top of this file, Terraform
// Core persists resp.State regardless of the error.
func (r *stateProbeResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var data stateProbeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Write the new planned value into state FIRST...
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)

	// ...THEN fail. This mirrors the settings engine's read-back-failure
	// scenario: a mutation partially lands (state reflects it) but the
	// operation as a whole must still be reported as failed.
	resp.Diagnostics.AddError(
		"Simulated Partial Update Failure",
		"stateProbeResource.Update intentionally fails after writing state, "+
			"to pin the framework/Core partial-state-on-error contract.",
	)
}

func (r *stateProbeResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	// No-op: nothing external to clean up.
}

// --- the regression guard test -------------------------------------------

var stateProbeProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"frameworkstateprobe": providerserver.NewProtocol6WithError(&stateProbeProvider{}),
}

// TestFrameworkPartialStateOnUpdateError_C2_4 is an end-to-end regression
// guard (NOT a unifi-provider test) pinning the Terraform Core contract that
// the settings engine's best-effort recovery path relies on. See the
// file-level comment for full context and the escalation rule if this ever
// fails.
//
// This uses its own minimal provider/resource and factory
// (stateProbeProviderFactories) — deliberately NOT
// testAccProtoV6ProviderFactories (the real unifi provider, which requires a
// live controller) and deliberately NOT preCheck (which requires
// UNIFI_USERNAME/PASSWORD/API). It needs neither: run with
//
//	TF_ACC=1 UNIFI_SKIP_CONTAINER=1 go test ./unifi/ -run TestFrameworkPartialStateOnUpdateError_C2_4 -v
func TestFrameworkPartialStateOnUpdateError_C2_4(t *testing.T) {
	tftesting.Test(t, tftesting.TestCase{
		ProtoV6ProviderFactories: stateProbeProviderFactories,
		Steps: []tftesting.TestStep{
			// Step 1: create with value = "initial". No error expected.
			{
				Config: `
resource "frameworkstateprobe_probe" "test" {
  value = "initial"
}
`,
				Check: tftesting.ComposeTestCheckFunc(
					tftesting.TestCheckResourceAttrSet("frameworkstateprobe_probe.test", "id"),
					tftesting.TestCheckResourceAttr("frameworkstateprobe_probe.test", "value", "initial"),
				),
			},
			// Step 2: change value to "updated". This plans an in-place
			// Update. stateProbeResource.Update writes "updated" into
			// resp.State and THEN returns an error diagnostic. We expect
			// the apply to fail.
			{
				Config: `
resource "frameworkstateprobe_probe" "test" {
  value = "updated"
}
`,
				ExpectError: regexp.MustCompile(`Simulated Partial Update Failure`),
			},
			// Step 3: RefreshState (no Config of its own — mutually
			// exclusive with Config, and it calls `terraform refresh`,
			// i.e. Resource.Read, not Update). This is the Core-level
			// proof: it reads whatever Terraform Core actually persisted
			// to the state file after the errored apply in step 2, wholly
			// independent of the framework's in-process NewState return
			// value. If Core had discarded the partial state and rolled
			// back to "initial" (the pre-error value), this Check would
			// observe "initial" and fail. It observes "updated" — proving
			// Core persisted the state written during the errored Update.
			{
				RefreshState: true,
				Check: tftesting.ComposeTestCheckFunc(
					tftesting.TestCheckResourceAttr("frameworkstateprobe_probe.test", "value", "updated"),
				),
			},
		},
	})
}
