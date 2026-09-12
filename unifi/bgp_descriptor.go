package unifi

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"text/template"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_bgp "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_bgp"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// frrConfigTemplate renders FRR config from the structured attributes.
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

// renderFRRConfig renders the FRR config from the structured attributes.
func renderFRRConfig(ctx context.Context, model *bgpKitModel) (string, diag.Diagnostics) {
	var diags diag.Diagnostics

	var peers []bgpPeersModel
	diags.Append(model.Peers.ElementsAs(ctx, &peers, false)...)
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
			diags.Append(peer.Networks.ElementsAs(ctx, &nd.Networks, false)...)
			if diags.HasError() {
				return "", diags
			}
		}

		data.Neighbors = append(data.Neighbors, nd)
	}

	var buf bytes.Buffer
	if err := frrConfigTemplate.Execute(&buf, data); err != nil {
		diags.AddError("Error Rendering FRR Config", resourcekit.DiagErrorText(err))
		return "", diags
	}

	return buf.String(), diags
}

// bgpBeforeSend renders frr_bgpd_config from asn, router_id and peers when
// the structured attributes are in use. Derived from effective, not config:
// on update the merged state carries the values an unrelated apply left
// unmentioned.
func bgpBeforeSend(
	ctx context.Context,
	_, effective *bgpKitModel,
	_ bgpKitModel,
	sdk *ui.BGPConfig,
	_ any,
) diag.Diagnostics {
	if effective.ASN.IsNull() || effective.ASN.IsUnknown() {
		return nil
	}
	rendered, diags := renderFRRConfig(ctx, effective)
	if diags.HasError() {
		return diags
	}
	sdk.Config = rendered
	return diags
}

func bgpKitSpec() resourcekit.Spec[bgpKitModel, ui.BGPConfig] {
	return resourcekit.Spec[bgpKitModel, ui.BGPConfig]{
		TypeName: "bgp",
		Subject:  "BGP Configuration",
		IDWire:   "_id",
		New:      func() *ui.BGPConfig { return &ui.BGPConfig{} },
		ID:       func(m *bgpKitModel) *types.String { return &m.ID },
		Site:     func(m *bgpKitModel) *types.String { return &m.Site },
		Timeouts: func(m *bgpKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: resourcekit.Override(bgpGenFields(), []resourcekit.Field[bgpKitModel, ui.BGPConfig]{
			resourcekit.StringField[bgpKitModel, ui.BGPConfig]{
				Wire:  "frr_bgpd_config",
				Model: func(m *bgpKitModel) *types.String { return &m.Config },
				SDK:   func(s *ui.BGPConfig) *string { return &s.Config },
				Elide: resourcekit.KeepZero,
			},
		}),
		BeforeSend: bgpBeforeSend,
		// frr_bgpd_config is set by bgpBeforeSend when the structured
		// attributes are in use, and asn/router_id/peers are not Fields, so
		// nothing else would put the rendered config on the update mask.
		AlwaysWire: []string{"frr_bgpd_config"},
		// Seeded here as well as in bgpKitBackend, so a unit test calling
		// ToModel on an unconfigured spec does not dereference nil.
		Backend: resourcekit.Backend[ui.BGPConfig]{
			GetID: func(s *ui.BGPConfig) string { return s.ID },
			SetID: func(s *ui.BGPConfig, id string) { s.ID = id },
		},
	}
}

func bgpKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_bgp.BgpResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

// bgpKitBackend binds the site's one BGP configuration. The endpoint is a
// per-site singleton: reads and deletes address the site, so the id the kit
// hands over goes unused.
func bgpKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.BGPConfig] {
	return resourcekit.Backend[ui.BGPConfig]{
		Create: func(ctx context.Context, site string, in *ui.BGPConfig) (*ui.BGPConfig, error) {
			// The endpoint can answer a create with not-found while the BGP
			// feature is still coming up; retry those, as the hand-written
			// resource did, and fail on anything else immediately.
			var created *ui.BGPConfig
			var err error
			for attempt := 0; attempt <= 3; attempt++ {
				created, err = client.CreateBGPConfig(ctx, site, in)
				if err == nil {
					return created, nil
				}
				var notFound *ui.NotFoundError
				if !errors.As(err, &notFound) {
					return nil, err
				}
			}
			return nil, err
		},
		Read: func(ctx context.Context, site, _ string) (*ui.BGPConfig, error) {
			return client.GetBGPConfig(ctx, site)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.BGPConfig, fields ...string,
		) (*ui.BGPConfig, error) {
			return client.UpdateBGPConfigFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, _ string) error {
			return client.DeleteBGPConfig(ctx, site)
		},
		GetID: func(s *ui.BGPConfig) string { return s.ID },
		SetID: func(s *ui.BGPConfig, id string) { s.ID = id },
	}
}
