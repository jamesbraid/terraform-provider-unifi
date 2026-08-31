package unifi

// validateMdnsConfig enforces mdns's one plan-time rule: mode != "custom"
// while custom_services or predefined_services is non-empty. Measured
// directly against the pinned simulation controller with a raw
// UpdateSettingFields/GetSetting probe that bypasses mdnsKitBackend and
// mdnsAfterReceive entirely, isolating controller behavior from provider
// behavior: under any mode other than "custom", a write naming
// custom_services is followed by a read where the controller has already
// dropped it (echoes back an empty list), and predefined_services comes
// back holding the controller's own full catalog of every predefined
// service it knows about, regardless of what was sent. Neither is a value
// the plan set, so a config that leaves either list non-empty under a
// non-"custom" mode plans a value apply can never actually produce.
// Without this check that surfaces as an opaque "Provider produced
// inconsistent result after apply" instead of a clear plan-time error
// naming the real rule -- the same idiom setting_syslog_validate.go's own
// validateSyslogConfig uses for its stopgap.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

func validateMdnsConfig(ctx context.Context, model settingResourceModel, resp *resource.ValidateConfigResponse) {
	if !settingSectionConfigured(model.Mdns) {
		return
	}

	var mdns settingMdnsModel
	resp.Diagnostics.Append(model.Mdns.As(ctx, &mdns, basetypes.ObjectAsOptions{})...)
	if resp.Diagnostics.HasError() {
		return
	}

	// An unknown mode (e.g. derived from another resource, or Computed with
	// nothing configured) can't be judged yet -- the same reasoning
	// validateSyslogConfig applies to enabled/ip. "custom" is the only
	// value under which either list is honored, so there is nothing to flag.
	if mdns.Mode.IsUnknown() || mdns.Mode.ValueString() == "custom" {
		return
	}

	if mdnsListConfiguredNonEmpty(mdns.CustomServices) {
		resp.Diagnostics.AddAttributeError(
			path.Root("mdns").AtName("custom_services"),
			`mDNS Custom Services Requires mode = "custom"`,
			`mdns.custom_services is only honored by the controller when mdns.mode is `+
				`"custom". Under any other mode, a write naming custom_services is followed `+
				`by a read where the controller has already dropped it, which fails apply `+
				`with "Provider produced inconsistent result after apply". Set mdns.mode = `+
				`"custom", or clear mdns.custom_services.`,
		)
	}
	if mdnsListConfiguredNonEmpty(mdns.PredefinedServices) {
		resp.Diagnostics.AddAttributeError(
			path.Root("mdns").AtName("predefined_services"),
			`mDNS Predefined Services Requires mode = "custom"`,
			`mdns.predefined_services is only honored by the controller when mdns.mode is `+
				`"custom". Under any other mode, the controller replaces it with its own `+
				`full catalog of predefined services regardless of what was configured, `+
				`which fails apply with "Provider produced inconsistent result after `+
				`apply". Set mdns.mode = "custom", or clear mdns.predefined_services.`,
		)
	}
}

// mdnsListConfiguredNonEmpty reports whether a plan-time list value is
// known, non-null and carries at least one element -- the only shape that
// could actually reach apply as a concrete, non-empty value the controller
// then can't honor. An unknown list (e.g. derived from another resource)
// can't be judged at plan time; an explicitly empty list is exactly the
// shape mdnsAfterReceive's own normalization already handles cleanly.
func mdnsListConfiguredNonEmpty(l types.List) bool {
	return !l.IsNull() && !l.IsUnknown() && len(l.Elements()) > 0
}
