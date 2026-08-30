package unifi

// ValidateConfig warns about the one attribute this resource accepts but,
// measured against this controller generation, cannot make take effect:
// port_override.tagged_networkconf_ids. The value is accepted (200/rc:ok)
// and discarded, and forward is additionally reverted to "all" -- see the
// tagged_networkconf_ids description in provider-codegen/policy/device.json
// and the comment on devicePortOverrideEncode (device_descriptor.go) for the
// full finding. Scoped to this controller generation deliberately: a
// different or newer controller may honor it, so this is a warning, not an
// error -- the configuration is not wrong.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func (r *deviceKitResource) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var model deviceKitModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Unknown at plan time (e.g. derived from another resource) can't be
	// judged yet -- flagging it here would risk a false alarm on a value
	// that resolves to null by apply.
	if model.PortOverride.IsNull() || model.PortOverride.IsUnknown() {
		return
	}

	var overrides []portOverrideModel
	resp.Diagnostics.Append(model.PortOverride.ElementsAs(ctx, &overrides, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	for _, po := range overrides {
		if po.TaggedNetworkIDs.IsNull() || po.TaggedNetworkIDs.IsUnknown() {
			continue
		}
		if len(po.TaggedNetworkIDs.Elements()) == 0 {
			continue
		}
		resp.Diagnostics.AddWarning(
			"tagged_networkconf_ids Has No Effect On This Controller Generation",
			"A port_override block sets tagged_networkconf_ids. Measured on this "+
				"controller generation: the value is accepted (200/rc:ok) and discarded, "+
				"and forward is additionally reverted to \"all\". The apply will succeed "+
				"and this attribute will have no effect. This is scoped to this controller "+
				"generation, not asserted as a permanent property of the field -- a "+
				"different or newer controller may honor it.",
		)
	}
}

// This assertion is the guard, not decoration: the framework only calls
// ValidateConfig if the type satisfies this interface, so a mistyped
// signature would otherwise silently drop the warning above.
var _ resource.ResourceWithValidateConfig = &deviceKitResource{}
