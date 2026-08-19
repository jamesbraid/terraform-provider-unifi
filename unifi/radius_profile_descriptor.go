package unifi

// The radius_profile descriptor, and the FIRST kit surface with a nested block.
//
// Its two accounting and authentication server lists are ListNestedBlocks over
// []RADIUSProfileAcctServers and []RADIUSProfileAuthServers, which is what
// ObjectListField exists for: ObjectField carries one nested object, and three
// of the eleven blocked surfaces need that shape while the rest need a list.
//
// tls_enabled and the six x_client_* certificate fields are absent from the
// field list on purpose. The kit derives its mask from Spec.Fields, so a field
// with no entry has no wire name and cannot be written -- which is what ae8a994d
// achieved for tls_enabled with a hand-kept list. The x_client_* six were never
// at risk: they carry omitempty and drop out of the encoding rather than being
// sent as zeros.

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_radius_profile"
	resource_radius_profile "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_radius_profile"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type radiusProfileKitModel struct {
	ID                    types.String         `tfsdk:"id"`
	Site                  types.String         `tfsdk:"site"`
	Name                  types.String         `tfsdk:"name"`
	AccountingEnabled     types.Bool           `tfsdk:"accounting_enabled"`
	InterimUpdateEnabled  types.Bool           `tfsdk:"interim_update_enabled"`
	InterimUpdateInterval timetypes.GoDuration `tfsdk:"interim_update_interval"`
	UseUSGAcctServer      types.Bool           `tfsdk:"use_usg_acct_server"`
	UseUSGAuthServer      types.Bool           `tfsdk:"use_usg_auth_server"`
	VlanEnabled           types.Bool           `tfsdk:"vlan_enabled"`
	VlanWlanMode          types.String         `tfsdk:"vlan_wlan_mode"`
	AuthServer            types.List           `tfsdk:"auth_server"`
	AcctServer            types.List           `tfsdk:"acct_server"`
	Timeouts              timeouts.Value       `tfsdk:"timeouts"`
}

// radiusServerAttrTypes types ONE server, not the list. The Terraform name is
// `secret` and the controller's is `x_secret`; the SDK struct tag carries that,
// so nothing here has to.
func radiusServerAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"ip":     types.StringType,
		"port":   types.Int64Type,
		"secret": types.StringType,
	}
}

func radiusServerObject(ip string, port *int64, secret string) (types.Object, diag.Diagnostics) {
	portValue := types.Int64Null()
	if port != nil {
		portValue = types.Int64Value(*port)
	}
	return types.ObjectValue(radiusServerAttrTypes(), map[string]attr.Value{
		"ip":     types.StringValue(ip),
		"port":   portValue,
		"secret": types.StringValue(secret),
	})
}

// radiusServerParts reads one server object. Returning the parts rather than a
// typed struct is what lets the two element types share it -- they are
// structurally identical and separately declared in the SDK.
func radiusServerParts(object types.Object) (ip string, port *int64, secret string) {
	if value, ok := object.Attributes()["ip"].(types.String); ok {
		ip = value.ValueString()
	}
	if value, ok := object.Attributes()["port"].(types.Int64); ok && !value.IsNull() {
		p := value.ValueInt64()
		port = &p
	}
	if value, ok := object.Attributes()["secret"].(types.String); ok {
		secret = value.ValueString()
	}
	return ip, port, secret
}

func radiusProfileKitSpec() resourcekit.Spec[radiusProfileKitModel, ui.RADIUSProfile] {
	return resourcekit.Spec[radiusProfileKitModel, ui.RADIUSProfile]{
		TypeName: "radius_profile",
		Subject:  "RADIUS Profile",
		New:      func() *ui.RADIUSProfile { return &ui.RADIUSProfile{} },
		ID:       func(m *radiusProfileKitModel) *types.String { return &m.ID },
		Site:     func(m *radiusProfileKitModel) *types.String { return &m.Site },
		Timeouts: func(m *radiusProfileKitModel) *timeouts.Value { return &m.Timeouts },

		Fields: []resourcekit.Field[radiusProfileKitModel, ui.RADIUSProfile]{
			resourcekit.StringField[radiusProfileKitModel, ui.RADIUSProfile]{
				Wire:  "name",
				Model: func(m *radiusProfileKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.RADIUSProfile) *string { return &s.Name },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[radiusProfileKitModel, ui.RADIUSProfile]{
				Wire:  "accounting_enabled",
				Model: func(m *radiusProfileKitModel) *types.Bool { return &m.AccountingEnabled },
				SDK:   func(s *ui.RADIUSProfile) *bool { return &s.AccountingEnabled },
			},
			resourcekit.BoolField[radiusProfileKitModel, ui.RADIUSProfile]{
				Wire:  "interim_update_enabled",
				Model: func(m *radiusProfileKitModel) *types.Bool { return &m.InterimUpdateEnabled },
				SDK:   func(s *ui.RADIUSProfile) *bool { return &s.InterimUpdateEnabled },
			},
			resourcekit.DurationPtrField[radiusProfileKitModel, ui.RADIUSProfile]{
				Wire:  "interim_update_interval",
				Model: func(m *radiusProfileKitModel) *timetypes.GoDuration { return &m.InterimUpdateInterval },
				SDK:   func(s *ui.RADIUSProfile) **int64 { return &s.InterimUpdateInterval },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[radiusProfileKitModel, ui.RADIUSProfile]{
				Wire:  "use_usg_acct_server",
				Model: func(m *radiusProfileKitModel) *types.Bool { return &m.UseUSGAcctServer },
				SDK:   func(s *ui.RADIUSProfile) *bool { return &s.UseUsgAcctServer },
			},
			resourcekit.BoolField[radiusProfileKitModel, ui.RADIUSProfile]{
				Wire:  "use_usg_auth_server",
				Model: func(m *radiusProfileKitModel) *types.Bool { return &m.UseUSGAuthServer },
				SDK:   func(s *ui.RADIUSProfile) *bool { return &s.UseUsgAuthServer },
			},
			resourcekit.BoolField[radiusProfileKitModel, ui.RADIUSProfile]{
				Wire:  "vlan_enabled",
				Model: func(m *radiusProfileKitModel) *types.Bool { return &m.VlanEnabled },
				SDK:   func(s *ui.RADIUSProfile) *bool { return &s.VLANEnabled },
			},
			resourcekit.StringField[radiusProfileKitModel, ui.RADIUSProfile]{
				Wire:  "vlan_wlan_mode",
				Model: func(m *radiusProfileKitModel) *types.String { return &m.VlanWlanMode },
				SDK:   func(s *ui.RADIUSProfile) *string { return &s.VLANWLANMode },
				// NullZero, and the empty-string default is gone with it. The
				// two were a contradiction: OneOf forbids "" and the default
				// supplied it, so every apply that omitted the attribute planned
				// "" and the masked update sent a value the controller refuses.
				Elide: resourcekit.NullZero,
			},
			// THE WIRE NAMES ARE PLURAL AND THE TERRAFORM NAMES ARE NOT.
			// acct_server is the block; acct_servers is what the controller
			// calls it. A mask naming the Terraform spelling would name a key
			// the encoding does not carry, which WireNameProblems catches.
			resourcekit.ObjectListField[radiusProfileKitModel, ui.RADIUSProfile, ui.RADIUSProfileAcctServers]{
				Wire:      "acct_servers",
				Model:     func(m *radiusProfileKitModel) *types.List { return &m.AcctServer },
				SDK:       func(s *ui.RADIUSProfile) *[]ui.RADIUSProfileAcctServers { return &s.AcctServers },
				AttrTypes: radiusServerAttrTypes(),
				Encode: func(_ context.Context, o types.Object) (ui.RADIUSProfileAcctServers, diag.Diagnostics) {
					ip, port, secret := radiusServerParts(o)
					return ui.RADIUSProfileAcctServers{IP: ip, Port: port, Secret: secret}, nil
				},
				Decode: func(_ context.Context, e ui.RADIUSProfileAcctServers) (types.Object, diag.Diagnostics) {
					return radiusServerObject(e.IP, e.Port, e.Secret)
				},
				Elide: resourcekit.NullZero,
			},
			resourcekit.ObjectListField[radiusProfileKitModel, ui.RADIUSProfile, ui.RADIUSProfileAuthServers]{
				Wire:      "auth_servers",
				Model:     func(m *radiusProfileKitModel) *types.List { return &m.AuthServer },
				SDK:       func(s *ui.RADIUSProfile) *[]ui.RADIUSProfileAuthServers { return &s.AuthServers },
				AttrTypes: radiusServerAttrTypes(),
				Encode: func(_ context.Context, o types.Object) (ui.RADIUSProfileAuthServers, diag.Diagnostics) {
					ip, port, secret := radiusServerParts(o)
					return ui.RADIUSProfileAuthServers{IP: ip, Port: port, Secret: secret}, nil
				},
				Decode: func(_ context.Context, e ui.RADIUSProfileAuthServers) (types.Object, diag.Diagnostics) {
					return radiusServerObject(e.IP, e.Port, e.Secret)
				},
				Elide: resourcekit.NullZero,
			},
		},
		Backend: resourcekit.Backend[ui.RADIUSProfile]{
			GetID: func(s *ui.RADIUSProfile) string { return s.ID },
			SetID: func(s *ui.RADIUSProfile, id string) { s.ID = id },
		},
	}
}

func radiusProfileKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_radius_profile.RadiusProfileResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func radiusProfileKitList() resourcekit.ListSpec[ui.RADIUSProfile] {
	return resourcekit.ListSpec[ui.RADIUSProfile]{
		ConfigSchema: listresource_radius_profile.RadiusProfileListResourceSchema,
		DisplayName: func(s *ui.RADIUSProfile) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.RADIUSProfile) string{
			"name": func(s *ui.RADIUSProfile) string { return s.Name },
		},
	}
}

func radiusProfileKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.RADIUSProfile] {
	return resourcekit.Backend[ui.RADIUSProfile]{
		Create: func(ctx context.Context, site string, in *ui.RADIUSProfile) (*ui.RADIUSProfile, error) {
			return client.CreateRADIUSProfile(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.RADIUSProfile, error) {
			return client.GetRADIUSProfile(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.RADIUSProfile, fields ...string,
		) (*ui.RADIUSProfile, error) {
			return client.UpdateRADIUSProfileFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteRADIUSProfile(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.RADIUSProfile, error) {
			return client.ListRADIUSProfile(ctx, site)
		},
		GetID: func(s *ui.RADIUSProfile) string { return s.ID },
		SetID: func(s *ui.RADIUSProfile, id string) { s.ID = id },
	}
}
