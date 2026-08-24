package unifi

import (
	"bytes"
	"context"
	"strings"
	"text/template"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_bgp"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// frrConfigTemplate is the Go template used to render FRR config from structured attributes.
var frrConfigTemplate = template.Must(template.New("frr").Parse(strings.TrimSpace(`
frr defaults traditional
log file stdout
!
router bgp {{.ASN}}
  bgp ebgp-requires-policy
  bgp router-id {{.RouterID}}
  bgp log-neighbor-changes
  bgp graceful-restart
  bgp bestpath as-path multipath-relax
{{- range .Neighbors}}
  !
  neighbor {{.Name}} peer-group
  neighbor {{.Name}} remote-as {{.RemoteAS}}
  neighbor {{.Name}} ebgp-multihop 2
  neighbor {{.Name}} timers 3 9
  neighbor {{.Name}} timers connect 5
  neighbor {{.Name}} soft-reconfiguration inbound
  {{- if .Description}}
  neighbor {{.Name}} description {{.Description}}
  {{- end}}
  !
  {{- $peer := .Name }}
  {{- range .Networks}}
  bgp listen range {{.}} peer-group {{$peer}}
  {{- end}}
  !
  address-family ipv4 unicast
    redistribute connected
    neighbor {{.Name}} activate
    neighbor {{.Name}} route-map {{.Name}}-IN in
    neighbor {{.Name}} route-map {{.Name}}-OUT out
    neighbor {{.Name}} maximum-prefix 1000
    neighbor {{.Name}} next-hop-self
  exit-address-family
  !
  address-family ipv6 unicast
    redistribute connected
    neighbor {{.Name}} activate
    neighbor {{.Name}} route-map {{.Name}}-IN-V6 in
    neighbor {{.Name}} route-map {{.Name}}-OUT-V6 out
    neighbor {{.Name}} maximum-prefix 1000
    neighbor {{.Name}} next-hop-self
  exit-address-family
!
route-map {{.Name}}-IN permit 10
!
route-map {{.Name}}-OUT permit 10
!
route-map {{.Name}}-IN-V6 permit 10
!
route-map {{.Name}}-OUT-V6 permit 10
{{- end}}
!
line vty
!
`)))

// frrTemplateData is the data structure passed to the FRR config template.
type frrTemplateData struct {
	ASN       int64
	RouterID  string
	Neighbors []frrNeighborData
}

type frrNeighborData struct {
	Name        string
	RemoteAS    int64
	Description string
	Networks    []string
}

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                = &bgpResource{}
	_ resource.ResourceWithImportState = &bgpResource{}
)

func NewBGPResource() resource.Resource {
	return &bgpResource{}
}

// bgpResource defines the resource implementation.
type bgpResource struct {
	client *Client
}

// bgpPeerModel describes a single BGP peer in the peers list.
type bgpPeerModel struct {
	Name        types.String `tfsdk:"name"`
	RemoteAS    types.Int64  `tfsdk:"remote_as"`
	Description types.String `tfsdk:"description"`
	Networks    types.List   `tfsdk:"networks"`
}

func (m bgpPeerModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"name":        types.StringType,
		"remote_as":   types.Int64Type,
		"description": types.StringType,
		"networks":    types.ListType{ElemType: types.StringType},
	}
}

// bgpResourceModel describes the resource data model.
type bgpResourceModel struct {
	ID             types.String   `tfsdk:"id"`
	Site           types.String   `tfsdk:"site"`
	Enabled        types.Bool     `tfsdk:"enabled"`
	Config         types.String   `tfsdk:"config"`
	ASN            types.Int64    `tfsdk:"asn"`
	RouterID       types.String   `tfsdk:"router_id"`
	Peers          types.List     `tfsdk:"peers"`
	UploadFileName types.String   `tfsdk:"upload_file_name"`
	Description    types.String   `tfsdk:"description"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (r *bgpResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_bgp"
}

func (r *bgpResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_bgp.BgpResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

func (r *bgpResource) Configure(
	ctx context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}

	r.client = client
}

func (r *bgpResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data bgpResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, timeoutDiags := data.Timeouts.Create(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	bgpConfig, d := r.modelToBGP(ctx, &data)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	site := data.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	// Retries on "not found" errors; other failures return immediately.
	var createdBGPConfig *unifi.BGPConfig
	var err error

	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		createdBGPConfig, err = r.client.CreateBGPConfig(ctx, site, bgpConfig)
		if err == nil {
			break
		}

		if _, ok := err.(*unifi.NotFoundError); ok && attempt < maxRetries {
			continue
		}

		resp.Diagnostics.AddError(
			"Error Creating BGP Configuration",
			err.Error(),
		)
		return
	}

	r.bgpToModel(ctx, createdBGPConfig, &data, site)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *bgpResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data bgpResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readTimeout, timeoutDiags := data.Timeouts.Read(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	site := data.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	bgpConfig, err := r.client.GetBGPConfig(ctx, site)
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading BGP Configuration",
			"Could not read BGP configuration: "+err.Error(),
		)
		return
	}

	r.bgpToModel(ctx, bgpConfig, &data, site)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *bgpResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var state bgpResourceModel
	var plan bgpResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateTimeout, timeoutDiags := plan.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	r.applyPlanToState(ctx, &plan, &state)

	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	bgpConfig, d := r.modelToBGP(ctx, &state)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	bgpConfig.ID = state.ID.ValueString()

	updatedBGPConfig, err := r.client.UpdateBGPConfig(ctx, site, bgpConfig)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating BGP Configuration",
			err.Error(),
		)
		return
	}

	r.bgpToModel(ctx, updatedBGPConfig, &state, site)

	state.Timeouts = plan.Timeouts

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *bgpResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data bgpResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, timeoutDiags := data.Timeouts.Delete(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()

	site := data.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	err := r.client.DeleteBGPConfig(ctx, site)
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			return
		}
		resp.Diagnostics.AddError(
			"Error Deleting BGP Configuration",
			err.Error(),
		)
		return
	}
}

func (r *bgpResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(
		ctx,
		path.Root("id"),
		req,
		resp,
	)
}

// applyPlanToState merges plan values into state, preserving state values where plan is null/unknown.
func (r *bgpResource) applyPlanToState(
	_ context.Context,
	plan *bgpResourceModel,
	state *bgpResourceModel,
) {
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		state.Enabled = plan.Enabled
	}
	if !plan.Config.IsNull() && !plan.Config.IsUnknown() {
		state.Config = plan.Config
	}
	if !plan.ASN.IsNull() && !plan.ASN.IsUnknown() {
		state.ASN = plan.ASN
	}
	if !plan.RouterID.IsNull() && !plan.RouterID.IsUnknown() {
		state.RouterID = plan.RouterID
	}
	if !plan.Peers.IsNull() && !plan.Peers.IsUnknown() {
		state.Peers = plan.Peers
	}
	if !plan.UploadFileName.IsNull() && !plan.UploadFileName.IsUnknown() {
		state.UploadFileName = plan.UploadFileName
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		state.Description = plan.Description
	}
}

// renderFRRConfig renders the FRR config from the structured attributes.
func (r *bgpResource) renderFRRConfig(
	ctx context.Context,
	model *bgpResourceModel,
) (string, diag.Diagnostics) {
	var diags diag.Diagnostics

	var peers []bgpPeerModel
	d := model.Peers.ElementsAs(ctx, &peers, false)
	diags.Append(d...)
	if diags.HasError() {
		return "", diags
	}

	data := frrTemplateData{
		ASN:      model.ASN.ValueInt64(),
		RouterID: model.RouterID.ValueString(),
	}

	for _, peer := range peers {
		nd := frrNeighborData{
			Name:        peer.Name.ValueString(),
			RemoteAS:    peer.RemoteAS.ValueInt64(),
			Description: peer.Description.ValueString(),
		}

		if !peer.Networks.IsNull() && !peer.Networks.IsUnknown() {
			d := peer.Networks.ElementsAs(ctx, &nd.Networks, false)
			diags.Append(d...)
			if diags.HasError() {
				return "", diags
			}
		}

		data.Neighbors = append(data.Neighbors, nd)
	}

	var buf bytes.Buffer
	if err := frrConfigTemplate.Execute(&buf, data); err != nil {
		diags.AddError("Error Rendering FRR Config", err.Error())
		return "", diags
	}

	return buf.String(), diags
}

// modelToBGP converts the Terraform model to the API struct.
func (r *bgpResource) modelToBGP(
	ctx context.Context,
	model *bgpResourceModel,
) (*unifi.BGPConfig, diag.Diagnostics) {
	var diags diag.Diagnostics

	configStr := model.Config.ValueString()

	// If structured attributes are set, render the template.
	if !model.ASN.IsNull() && !model.ASN.IsUnknown() {
		rendered, d := r.renderFRRConfig(ctx, model)
		diags.Append(d...)
		if diags.HasError() {
			return nil, diags
		}
		configStr = rendered
	}

	bgpConfig := &unifi.BGPConfig{
		Enabled:          model.Enabled.ValueBool(),
		Config:           configStr,
		UploadedFileName: model.UploadFileName.ValueString(),
		Description:      model.Description.ValueString(),
	}

	return bgpConfig, diags
}

// bgpToModel converts the API struct to the Terraform model.
func (r *bgpResource) bgpToModel(
	_ context.Context,
	bgpConfig *unifi.BGPConfig,
	model *bgpResourceModel,
	site string,
) {
	model.ID = types.StringValue(bgpConfig.ID)
	model.Site = types.StringValue(site)
	model.Enabled = types.BoolValue(bgpConfig.Enabled)
	model.Config = util.StringValueOrNull(bgpConfig.Config)

	// ASN, RouterID, and Peers are preserved from state — the API only stores
	// the rendered config, so we don't attempt to parse it back.

	model.UploadFileName = util.StringValueOrNull(bgpConfig.UploadedFileName)
	model.Description = util.StringValueOrNull(bgpConfig.Description)
}
