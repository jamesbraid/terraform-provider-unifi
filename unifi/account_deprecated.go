package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// deprecatedAccountResource wraps radiusUserKitResource to provide the old
// "unifi_account" resource type name as a deprecated alias. This avoids a
// breaking change for users who already have unifi_account in their state.
type deprecatedAccountResource struct {
	radiusUserKitResource
}

// NewDeprecatedAccountResource builds deprecatedAccountResource with its
// embedded radiusUserKitResource fully constructed — a zero-valued kit
// resource has no Spec, SchemaSpec or ListSurface, so every call would
// either panic or silently do nothing.
func NewDeprecatedAccountResource() resource.Resource {
	return &deprecatedAccountResource{radiusUserKitResource: *newRadiusUserKitResource()}
}

func (r *deprecatedAccountResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_account"
}

func (r *deprecatedAccountResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	r.radiusUserKitResource.Schema(ctx, req, resp)

	resp.Schema.DeprecationMessage = "Use unifi_radius_user instead. This resource will be removed in a future version."
}

// deprecatedAccountDataSource wraps radiusUserDataSource to provide the old
// "unifi_account" data source type name as a deprecated alias.
type deprecatedAccountDataSource struct {
	radiusUserDataSource
}

func NewDeprecatedAccountDataSource() datasource.DataSource {
	return &deprecatedAccountDataSource{}
}

func (d *deprecatedAccountDataSource) Metadata(
	ctx context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_account"
}

func (d *deprecatedAccountDataSource) Schema(
	ctx context.Context,
	req datasource.SchemaRequest,
	resp *datasource.SchemaResponse,
) {
	d.radiusUserDataSource.Schema(ctx, req, resp)

	resp.Schema.DeprecationMessage = "Use the unifi_radius_user data source instead. This data source will be removed in a future version."
}
