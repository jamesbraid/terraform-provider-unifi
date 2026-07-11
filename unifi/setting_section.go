package unifi

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// settingSection is one nested section of the unifi_setting resource backed
// by its own controller setting object (rest/setting/<key>). Sections
// register in settingSections; Schema, Create, Update, and readSettings
// iterate the registry, so adding a section is one new file plus one
// registry entry.
type settingSection interface {
	// key is both the nested attribute name and the controller setting key.
	key() string
	attrTypes() map[string]attr.Type
	schemaAttribute() schema.SingleNestedAttribute
	// get/set access this section's object on the resource model.
	get(m *settingResourceModel) types.Object
	set(m *settingResourceModel, obj types.Object)
	// overlay writes only the user-configured fields into the section's raw
	// JSON document. Fields already in data — including ones go-unifi does
	// not model — are preserved and sent back in the PUT.
	overlay(ctx context.Context, obj types.Object, data map[string]any) diag.Diagnostics
	// read fetches the section and converts it to a model object. A missing
	// section yields a null object without error.
	read(ctx context.Context, client *Client, site string) (types.Object, diag.Diagnostics)
}

// settingSections is the registry of sections using the raw-merge engine.
var settingSections = []settingSection{
	localeSection{},
	globalNatSection{},
	globalSwitchSection{},
	mdnsSection{},
	teleportSection{},
	magicSiteToSiteVpnSection{},
	trafficFlowSection{},
}

// applySections performs the read-modify-write for every configured registry
// section. The raw settings list is fetched once; a fetch failure aborts
// before any PUT so a transient read error can never clobber unmanaged
// fields with a zero-value base.
func (r *settingResource) applySections(
	ctx context.Context,
	site string,
	m *settingResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics

	var active []settingSection
	for _, s := range settingSections {
		if obj := s.get(m); !obj.IsNull() && !obj.IsUnknown() {
			active = append(active, s)
		}
	}
	if len(active) == 0 {
		return diags
	}

	raws, err := r.client.ListSettings(ctx, site)
	if err != nil {
		diags.AddError("Error Reading Settings", err.Error())
		return diags
	}
	byKey := make(map[string]settings.RawSetting, len(raws))
	for _, raw := range raws {
		byKey[raw.GetKey()] = raw
	}

	for _, s := range active {
		raw := byKey[s.key()]
		if raw.Data == nil {
			raw.Data = map[string]any{}
		}
		raw.SetKey(s.key())
		diags.Append(s.overlay(ctx, s.get(m), raw.Data)...)
		if diags.HasError() {
			return diags
		}
		if err := r.client.UpdateSetting(ctx, site, &raw); err != nil {
			diags.AddError(
				fmt.Sprintf("Error Updating %s Setting", s.key()),
				err.Error(),
			)
			return diags
		}
	}
	return diags
}

// readSections refreshes every registry section present in the model,
// mirroring the inline sections' behavior: sections absent from
// configuration stay null.
func (r *settingResource) readSections(
	ctx context.Context,
	site string,
	m *settingResourceModel,
	diags *diag.Diagnostics,
) {
	for _, s := range settingSections {
		obj := s.get(m)
		if obj.IsNull() || obj.IsUnknown() {
			s.set(m, types.ObjectNull(s.attrTypes()))
			continue
		}
		value, d := s.read(ctx, r.client, site)
		diags.Append(d...)
		if diags.HasError() {
			return
		}
		s.set(m, value)
	}
}
