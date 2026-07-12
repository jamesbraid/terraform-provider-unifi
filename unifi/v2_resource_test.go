package unifi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// v2TestSchema is a minimal "id"/"site" schema.Schema, standing in for the
// real per-resource schema every v2 resource defines. It exists only so
// tests can construct a tfsdk.State whose zero value satisfies
// State.RemoveResource/SetAttribute/Set — those methods dereference
// State.Schema internally (tfsdk.State{} with a nil Schema panics), so a
// bare &resource.ReadResponse{}/&resource.ImportStateResponse{} is not
// enough to unit test v2FinishRead/v2ImportState against the real
// framework types.
func v2TestSchema() schema.Schema {
	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Computed: true},
			"site": schema.StringAttribute{Optional: true, Computed: true},
		},
	}
}

// newV2TestReadResponse builds a *resource.ReadResponse whose State is wired
// to v2TestSchema, seeded with an all-null root object, so v2FinishRead's
// resp.State.RemoveResource call has a real schema to work against.
func newV2TestReadResponse(t *testing.T) *resource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	s := v2TestSchema()
	nullRoot := tftypes.NewValue(s.Type().TerraformType(ctx), nil)
	return &resource.ReadResponse{
		State: tfsdk.State{
			Raw:    nullRoot,
			Schema: s,
		},
	}
}

// newV2TestImportStateResponse builds a *resource.ImportStateResponse whose
// State is wired to v2TestSchema, mirroring newV2TestReadResponse for
// v2ImportState's SetAttribute calls.
func newV2TestImportStateResponse(t *testing.T) *resource.ImportStateResponse {
	t.Helper()
	ctx := context.Background()
	s := v2TestSchema()
	nullRoot := tftypes.NewValue(s.Type().TerraformType(ctx), nil)
	return &resource.ImportStateResponse{
		State: tfsdk.State{
			Raw:    nullRoot,
			Schema: s,
		},
	}
}

func TestResolveV2Site(t *testing.T) {
	t.Run("configured non-null non-empty returns configured", func(t *testing.T) {
		got := resolveV2Site(types.StringValue("site-a"), "default-site")
		if got != "site-a" {
			t.Errorf("resolveV2Site() = %q, want %q", got, "site-a")
		}
	})

	t.Run("configured null returns default", func(t *testing.T) {
		got := resolveV2Site(types.StringNull(), "default-site")
		if got != "default-site" {
			t.Errorf("resolveV2Site() = %q, want %q", got, "default-site")
		}
	})

	t.Run("configured empty string returns default", func(t *testing.T) {
		got := resolveV2Site(types.StringValue(""), "default-site")
		if got != "default-site" {
			t.Errorf("resolveV2Site() = %q, want %q", got, "default-site")
		}
	})
}

func TestV2IsNotFound(t *testing.T) {
	t.Run("NotFoundError is not found", func(t *testing.T) {
		if !v2IsNotFound(&unifi.NotFoundError{}) {
			t.Error("v2IsNotFound(&unifi.NotFoundError{}) = false, want true")
		}
	})

	t.Run("other error is not not-found", func(t *testing.T) {
		if v2IsNotFound(errors.New("boom")) {
			t.Error("v2IsNotFound(errors.New(...)) = true, want false")
		}
	})

	t.Run("nil error is not not-found", func(t *testing.T) {
		if v2IsNotFound(nil) {
			t.Error("v2IsNotFound(nil) = true, want false")
		}
	})
}

func TestV2Configure(t *testing.T) {
	t.Run("nil ProviderData returns nil with no diagnostic", func(t *testing.T) {
		req := resource.ConfigureRequest{ProviderData: nil}
		resp := &resource.ConfigureResponse{}
		got := v2Configure(req, resp)
		if got != nil {
			t.Errorf("v2Configure() = %v, want nil", got)
		}
		if resp.Diagnostics.HasError() {
			t.Errorf("unexpected diagnostics: %v", resp.Diagnostics)
		}
	})

	t.Run("wrong ProviderData type returns nil with an error diagnostic", func(t *testing.T) {
		req := resource.ConfigureRequest{ProviderData: "not-a-client"}
		resp := &resource.ConfigureResponse{}
		got := v2Configure(req, resp)
		if got != nil {
			t.Errorf("v2Configure() = %v, want nil", got)
		}
		if !resp.Diagnostics.HasError() {
			t.Error("expected an error diagnostic for a mismatched ProviderData type")
		}
	})

	t.Run("*Client ProviderData returns the client with no diagnostic", func(t *testing.T) {
		want := &Client{}
		req := resource.ConfigureRequest{ProviderData: want}
		resp := &resource.ConfigureResponse{}
		got := v2Configure(req, resp)
		if got != want {
			t.Errorf("v2Configure() = %v, want %v", got, want)
		}
		if resp.Diagnostics.HasError() {
			t.Errorf("unexpected diagnostics: %v", resp.Diagnostics)
		}
	})
}

func TestV2Timeout(t *testing.T) {
	t.Run("null timeouts value falls back to the 20-minute default per op", func(t *testing.T) {
		cases := []struct {
			name string
			op   v2TimeoutOp
		}{
			{"create", v2TimeoutCreate},
			{"read", v2TimeoutRead},
			{"update", v2TimeoutUpdate},
			{"delete", v2TimeoutDelete},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				ctx := context.Background()
				tv := timeoutsNullValue()
				newCtx, cancel, diags := v2Timeout(ctx, tv, tc.op)
				defer cancel()
				if diags.HasError() {
					t.Fatalf("unexpected diagnostics: %v", diags)
				}
				deadline, ok := newCtx.Deadline()
				if !ok {
					t.Fatal("expected context to have a deadline")
				}
				remaining := time.Until(deadline)
				if remaining <= 19*time.Minute || remaining > 20*time.Minute {
					t.Errorf("deadline %v from now, want ~20m", remaining)
				}
			})
		}
	})

	t.Run("invalid op value returns an error diagnostic, not a panic or silent default", func(t *testing.T) {
		ctx := context.Background()
		tv := timeoutsNullValue()
		const invalidOp v2TimeoutOp = 99
		newCtx, cancel, diags := v2Timeout(ctx, tv, invalidOp)
		defer cancel()
		if !diags.HasError() {
			t.Fatal("expected an error diagnostic for an invalid v2TimeoutOp value")
		}
		if _, ok := newCtx.Deadline(); ok {
			t.Error("expected no deadline to be set for an invalid op value")
		}
	})
}

type fakeIdentitySetter struct {
	calledPath path.Path
	calledVal  any
}

func (f *fakeIdentitySetter) SetAttribute(_ context.Context, p path.Path, v any) diag.Diagnostics {
	f.calledPath = p
	f.calledVal = v
	return nil
}

type fakeStateSetter struct {
	called    bool
	calledVal any
}

func (f *fakeStateSetter) Set(_ context.Context, v any) diag.Diagnostics {
	f.called = true
	f.calledVal = v
	return nil
}

// fakeErroringIdentitySetter always returns a diagnostic error from
// SetAttribute, simulating an identity-write failure.
type fakeErroringIdentitySetter struct{}

func (f *fakeErroringIdentitySetter) SetAttribute(_ context.Context, _ path.Path, _ any) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.AddError("Error Setting Identity", "boom")
	return diags
}

func TestV2SetIdentityAndState(t *testing.T) {
	type model struct {
		ID types.String
	}

	t.Run("identity success sets state", func(t *testing.T) {
		m := &model{ID: types.StringValue("obj-123")}

		ident := &fakeIdentitySetter{}
		state := &fakeStateSetter{}

		diags := v2SetIdentityAndState(context.Background(), ident, state, m.ID, m)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if ident.calledPath.String() != "id" {
			t.Errorf("identity path = %q, want %q", ident.calledPath.String(), "id")
		}
		if ident.calledVal != m.ID {
			t.Errorf("identity value = %v, want %v", ident.calledVal, m.ID)
		}
		if !state.called {
			t.Error("state setter was not called, want called")
		}
		if state.calledVal != m {
			t.Errorf("state value = %v, want %v", state.calledVal, m)
		}
	})

	t.Run("identity error halts before state is set", func(t *testing.T) {
		m := &model{ID: types.StringValue("obj-123")}

		ident := &fakeErroringIdentitySetter{}
		state := &fakeStateSetter{}

		diags := v2SetIdentityAndState(context.Background(), ident, state, m.ID, m)
		if !diags.HasError() {
			t.Fatal("expected diagnostics to report the identity error")
		}
		if state.called {
			t.Error("state setter was called after an identity error, want NOT called (halt-before-state invariant)")
		}
	})
}

func TestV2FinishRead(t *testing.T) {
	t.Run("NotFoundError removes resource from state and returns true", func(t *testing.T) {
		resp := newV2TestReadResponse(t)
		handled := v2FinishRead(context.Background(), resp, &unifi.NotFoundError{}, "Error Reading Thing")
		if !handled {
			t.Error("v2FinishRead() = false, want true (NotFound should be handled)")
		}
		if resp.Diagnostics.HasError() {
			t.Errorf("NotFound must not produce an error diagnostic, got: %v", resp.Diagnostics)
		}
	})

	t.Run("other error adds a diagnostic and returns true", func(t *testing.T) {
		resp := newV2TestReadResponse(t)
		handled := v2FinishRead(context.Background(), resp, errors.New("boom"), "Error Reading Thing")
		if !handled {
			t.Error("v2FinishRead() = false, want true (error should be handled)")
		}
		if !resp.Diagnostics.HasError() {
			t.Error("expected an error diagnostic")
		}
	})

	t.Run("nil error returns false and adds no diagnostic", func(t *testing.T) {
		resp := newV2TestReadResponse(t)
		handled := v2FinishRead(context.Background(), resp, nil, "Error Reading Thing")
		if handled {
			t.Error("v2FinishRead() = true, want false (nil error means caller continues)")
		}
		if resp.Diagnostics.HasError() {
			t.Errorf("nil error must not produce a diagnostic, got: %v", resp.Diagnostics)
		}
	})
}
