package unifi

// ValidateConfig enforces syslog's one plan-time rule: enabled=true with no
// ip. Measured on 10.4.57 (Task 1): the controller rejects that combination
// with api.err.Invalid at apply time, so this catches it at plan time
// instead, the same idiom wan_resource.go's own ValidateConfig uses for its
// stopgap.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

func (r *settingResource) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var model settingResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !settingSectionConfigured(model.Syslog) {
		return
	}

	var syslog settingSyslogModel
	resp.Diagnostics.Append(model.Syslog.As(ctx, &syslog, basetypes.ObjectAsOptions{})...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only when both values are known: an unknown enabled or ip at plan
	// time (e.g. derived from another resource) can't be judged yet, and
	// flagging it here would be a false alarm the apply-time error already
	// covers correctly.
	if syslog.Enabled.IsUnknown() || syslog.IP.IsUnknown() {
		return
	}
	if !syslog.Enabled.ValueBool() {
		return
	}
	if !syslog.IP.IsNull() && syslog.IP.ValueString() != "" {
		return
	}

	resp.Diagnostics.AddAttributeError(
		path.Root("syslog").AtName("ip"),
		"Syslog Requires An Address",
		"syslog.enabled = true requires syslog.ip to be set. The controller rejects a "+
			"syslog target with no address (api.err.Invalid).",
	)
}

// This assertion is the guard, not decoration: the framework only calls
// ValidateConfig if the type satisfies this interface, so a mistyped
// signature would otherwise silently drop the check above.
var _ resource.ResourceWithValidateConfig = &settingResource{}
