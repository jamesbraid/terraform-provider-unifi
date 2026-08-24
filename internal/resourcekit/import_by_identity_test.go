package resourcekit

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func importByIdentity(
	t *testing.T,
	r *Resource[kitModel, kitSDK],
	id string,
) *resource.ImportStateResponse {
	t.Helper()
	ctx := context.Background()
	identity := kitIdentity(t)
	if diags := identity.SetAttribute(ctx, path.Root("id"), id); diags.HasError() {
		t.Fatalf("seeding identity: %v", diags)
	}
	resp := &resource.ImportStateResponse{
		State:    kitStateWith(t, kitModel{}),
		Identity: &identity,
	}
	resp.State.Raw = resp.State.Raw.Copy()
	r.ImportState(ctx, resource.ImportStateRequest{Identity: &identity}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("import by identity: %v", resp.Diagnostics)
	}
	return resp
}

func TestImportByIdentityRoutesTheHandleToID(t *testing.T) {
	resp := importByIdentity(t, kitResource(Backend[kitSDK]{}), "0123456789abcdef01234567")
	if got := importedAttr(t, resp, "id"); got.ValueString() != "0123456789abcdef01234567" {
		t.Errorf("id = %v, want the identity's handle", got)
	}
}

func TestImportByIdentityWithSiteStillSplitsFirst(t *testing.T) {
	resp := importByIdentity(t, kitResource(Backend[kitSDK]{}), "other:wlan1")
	if got := importedAttr(t, resp, "site"); got.ValueString() != "other" {
		t.Errorf("site = %v, want other", got)
	}
	if got := importedAttr(t, resp, "id"); got.ValueString() != "wlan1" {
		t.Errorf("id = %v, want wlan1", got)
	}
}

func TestImportByIdentityRoutesThroughNameLookupTheSameAsID(t *testing.T) {
	resp := importByIdentity(t, namedKitResource(nil), "wlan1")
	if got := importedAttr(t, resp, "name"); got.ValueString() != "wlan1" {
		t.Errorf("name = %v, want wlan1", got)
	}
	if got := importedAttr(t, resp, "id"); !got.IsNull() {
		t.Errorf("id = %v, want null", got)
	}
}
