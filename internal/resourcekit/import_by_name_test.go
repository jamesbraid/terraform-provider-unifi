package resourcekit

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

func namedKitResource(readByName func(context.Context, string, string) (*kitSDK, error)) *Resource[kitModel, kitSDK] {
	// Both members are required to opt in: routing without a resolver would
	// strand the handle on an attribute nothing reads. Tests exercising only
	// the routing get a stub resolver here.
	if readByName == nil {
		readByName = func(context.Context, string, string) (*kitSDK, error) {
			return nil, &ui.NotFoundError{}
		}
	}
	r := kitResource(Backend[kitSDK]{})
	r.Spec.Backend.ReadByName = readByName
	r.Spec.Name = func(m *kitModel) *types.String { return &m.Name }
	return r
}

func importInto(t *testing.T, r *Resource[kitModel, kitSDK], handle string) *resource.ImportStateResponse {
	t.Helper()
	ctx := context.Background()
	identity := kitIdentity(t)
	resp := &resource.ImportStateResponse{
		State:    kitStateWith(t, kitModel{}),
		Identity: &identity,
	}
	resp.State.Raw = resp.State.Raw.Copy()
	r.ImportState(ctx, resource.ImportStateRequest{ID: handle}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("import of %q: %v", handle, resp.Diagnostics)
	}
	return resp
}

func importedAttr(t *testing.T, resp *resource.ImportStateResponse, name string) types.String {
	t.Helper()
	var value types.String
	if diags := resp.State.GetAttribute(context.Background(), path.Root(name), &value); diags.HasError() {
		t.Fatalf("reading %s from imported state: %v", name, diags)
	}
	return value
}

func TestImportRoutesABareHandleToTheNameAttribute(t *testing.T) {
	resp := importInto(t, namedKitResource(nil), "wlan1")
	if got := importedAttr(t, resp, "name"); got.ValueString() != "wlan1" {
		t.Errorf("name = %v, want wlan1", got)
	}
	if got := importedAttr(t, resp, "id"); !got.IsNull() {
		t.Errorf("id = %v, want null; a name handle routed into id is what the read "+
			"then fails to find", got)
	}
}

func TestImportStripsTheExplicitNamePrefix(t *testing.T) {
	// Hex-shaped, but the prefix says name -- the practitioner wins.
	resp := importInto(t, namedKitResource(nil), "name=0123456789abcdef01234567")
	if got := importedAttr(t, resp, "name"); got.ValueString() != "0123456789abcdef01234567" {
		t.Errorf("name = %v, want the prefix stripped", got)
	}
}

func TestImportRoutesAHexHandleToTheIDAndItsIdentity(t *testing.T) {
	resp := importInto(t, namedKitResource(nil), "0123456789abcdef01234567")
	if got := importedAttr(t, resp, "id"); got.ValueString() != "0123456789abcdef01234567" {
		t.Errorf("id = %v, want the handle", got)
	}
	var identityID types.String
	if diags := resp.Identity.GetAttribute(context.Background(), path.Root("id"), &identityID); diags.HasError() {
		t.Fatalf("reading identity: %v", diags)
	}
	if identityID.ValueString() != "0123456789abcdef01234567" {
		t.Errorf("identity id = %v, want the handle; an import that sets no identity "+
			"leaves a not-found read reporting a framework error instead of "+
			"\"Cannot import non-existent remote object\"", identityID)
	}
}

func TestImportWithSiteStillSplitsFirst(t *testing.T) {
	resp := importInto(t, namedKitResource(nil), "other:wlan1")
	if got := importedAttr(t, resp, "site"); got.ValueString() != "other" {
		t.Errorf("site = %v, want other", got)
	}
	if got := importedAttr(t, resp, "name"); got.ValueString() != "wlan1" {
		t.Errorf("name = %v, want wlan1", got)
	}
}

func TestImportWithoutANameLookupRoutesEverythingToID(t *testing.T) {
	resp := importInto(t, kitResource(Backend[kitSDK]{}), "wlan1")
	if got := importedAttr(t, resp, "id"); got.ValueString() != "wlan1" {
		t.Errorf("id = %v; a surface without ReadByName must keep the old route", got)
	}
}

func TestReadResolvesByNameWhenTheStateCarriesNoID(t *testing.T) {
	var askedFor string
	r := namedKitResource(func(_ context.Context, _, name string) (*kitSDK, error) {
		askedFor = name
		return &kitSDK{ID: "resolved-1", Name: name}, nil
	})
	identity := kitIdentity(t)
	state := kitStateWith(t, kitModel{
		Site: types.StringValue("default"), Name: types.StringValue("wlan1"),
	})
	resp := &resource.ReadResponse{State: state, Identity: &identity}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read by name: %v", resp.Diagnostics)
	}
	if askedFor != "wlan1" {
		t.Fatalf("the lookup was asked for %q, want wlan1", askedFor)
	}
	var id types.String
	if diags := resp.State.GetAttribute(context.Background(), path.Root("id"), &id); diags.HasError() {
		t.Fatalf("reading id: %v", diags)
	}
	if id.ValueString() != "resolved-1" {
		t.Errorf("id = %v, want resolved-1", id)
	}
}

func TestReadReportsANameThatResolvesToNothing(t *testing.T) {
	r := namedKitResource(func(context.Context, string, string) (*kitSDK, error) {
		return nil, &ui.NotFoundError{}
	})
	identity := kitIdentity(t)
	state := kitStateWith(t, kitModel{
		Site: types.StringValue("default"), Name: types.StringValue("missing"),
	})
	resp := &resource.ReadResponse{State: state, Identity: &identity}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Error("a name lookup that found nothing was reported as success")
	}
}

func TestReadStillRemovesAMissingObjectByID(t *testing.T) {
	r := namedKitResource(nil)
	r.Spec.Backend.Read = func(context.Context, string, string) (*kitSDK, error) {
		return nil, &ui.NotFoundError{}
	}
	identity := kitIdentity(t)
	state := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("probe"),
	})
	resp := &resource.ReadResponse{State: state, Identity: &identity}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read errored on an absent object: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("state survived a not-found Read by id")
	}
}
