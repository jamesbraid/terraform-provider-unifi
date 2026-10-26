package resourcekit

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// readWithIdentity runs Read the way the framework does: the response
// identity starts as a copy of the stored one.
func readWithIdentity(t *testing.T, storedID *string) types.String {
	t.Helper()
	ctx := context.Background()
	r := kitResource(Backend[kitSDK]{
		Read: func(context.Context, string, string) (*kitSDK, error) {
			return &kitSDK{ID: "id-1", Name: "probe"}, nil
		},
	})
	stored := kitIdentity(t)
	if storedID != nil {
		if diags := stored.SetAttribute(ctx, path.Root("id"), *storedID); diags.HasError() {
			t.Fatalf("build stored identity: %v", diags)
		}
	}
	respIdentity := stored
	respIdentity.Raw = stored.Raw.Copy()
	state := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("probe"),
	})
	resp := &resource.ReadResponse{State: state, Identity: &respIdentity}
	r.Read(ctx, resource.ReadRequest{State: state, Identity: &stored}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	var got types.String
	if diags := resp.Identity.GetAttribute(ctx, path.Root("id"), &got); diags.HasError() {
		t.Fatalf("reading identity id: %v", diags)
	}
	return got
}

func TestReadFillsAnIdentityThatWasNeverStored(t *testing.T) {
	if got := readWithIdentity(t, nil); got.ValueString() != "id-1" {
		t.Errorf("identity id = %v, want id-1", got)
	}
}

func TestReadNeverRewritesAStoredIdentity(t *testing.T) {
	// The framework refuses any change to a stored identity on refresh, so a
	// Read that recomputes one older releases wrote differently fails the
	// whole plan.
	stored := "written-by-an-older-release"
	if got := readWithIdentity(t, &stored); got.ValueString() != stored {
		t.Errorf("identity id = %v, want the stored %q", got, stored)
	}
}
