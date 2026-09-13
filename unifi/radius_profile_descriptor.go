package unifi

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
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

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
	// The controller's port pattern rejects a literal 0. Its trailing |^$ arm
	// accepts an empty string, but a numeric field never sends "", so omitting
	// the value is the only way to say "unset" -- the empty-string alternative
	// is moot here. port is Optional+Computed, so an omitted one arrives Unknown,
	// which a plain ValueInt64Pointer would send as 0 (api.err.InvalidValue).
	// Omit zero/unknown so port's omitempty tag drops it and the controller keeps
	// its own, the OmitZero rule an Int64PtrField would carry.
	if value, ok := object.Attributes()["port"].(types.Int64); ok {
		port = util.OmitZeroInt64Pointer(value)
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

		// tls_enabled and the six x_client_* certificate fields are absent
		// from the field list on purpose: the mask is derived from Spec.Fields,
		// so an entry's absence means it can never be written. The x_client_*
		// six are additionally safe because they carry omitempty and drop out
		// of encoding on their own.
		Fields: resourcekit.Override(radiusProfileGenFields(), []resourcekit.Field[radiusProfileKitModel, ui.RADIUSProfile]{
			resourcekit.DurationPtrField[radiusProfileKitModel, ui.RADIUSProfile]{
				Wire:  "interim_update_interval",
				Model: func(m *radiusProfileKitModel) *timetypes.GoDuration { return &m.InterimUpdateInterval },
				SDK:   func(s *ui.RADIUSProfile) **int64 { return &s.InterimUpdateInterval },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			// The wire names are plural and the Terraform names are not:
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
		}),
		// Seeded here as well as in radiusProfileKitBackend, because Configure binds
		// the real Backend and a unit test calling ToModel on an unconfigured
		// spec would otherwise dereference nil.
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
