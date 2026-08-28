package resourcekit

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// compositeModel stands in for settingResourceModel: id/site/timeouts plus
// two independent section slots, each a plain string standing in for a
// section's document (types.Object would just add unrelated noise here).
type compositeModel struct {
	ID       types.String   `tfsdk:"id"`
	Site     types.String   `tfsdk:"site"`
	Timeouts timeouts.Value `tfsdk:"timeouts"`
	A        types.String   `tfsdk:"a"`
	B        types.String   `tfsdk:"b"`
}

func compositeTimeoutTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"create": types.StringType, "read": types.StringType,
		"update": types.StringType, "delete": types.StringType,
	}
}

func compositeSchema() schema.Schema {
	return schema.Schema{Attributes: map[string]schema.Attribute{
		"id":   schema.StringAttribute{Computed: true},
		"site": schema.StringAttribute{Optional: true, Computed: true},
		"a":    schema.StringAttribute{Optional: true},
		"b":    schema.StringAttribute{Optional: true},
		"timeouts": timeouts.Attributes(
			context.Background(),
			timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
		),
	}}
}

// fakeSection is a Section[compositeModel] whose every call is observable
// and controllable: writes/reads record their own name onto the shared
// *log, and each hook can be overridden per test.
type fakeSection struct {
	name    string
	field   func(*compositeModel) *types.String
	log     *[]string
	writeFn func(ctx context.Context, site string, plan, state *compositeModel, verb string) diag.Diagnostics
	readFn  func(ctx context.Context, site string, plan, out *compositeModel) diag.Diagnostics
}

func (f fakeSection) Name() string { return f.name }

func (f fakeSection) Configured(_ context.Context, plan *compositeModel) bool {
	v := f.field(plan)
	return !v.IsNull() && !v.IsUnknown()
}

func (f fakeSection) Write(
	ctx context.Context, site string, plan, state *compositeModel, verb string,
) diag.Diagnostics {
	*f.log = append(*f.log, "write:"+f.name)
	if f.writeFn != nil {
		return f.writeFn(ctx, site, plan, state, verb)
	}
	return nil
}

func (f fakeSection) Read(
	ctx context.Context, site string, plan, out *compositeModel,
) diag.Diagnostics {
	*f.log = append(*f.log, "read:"+f.name)
	if f.readFn != nil {
		return f.readFn(ctx, site, plan, out)
	}
	if f.Configured(ctx, plan) {
		*f.field(out) = types.StringValue("fetched:" + f.name)
	} else {
		*f.field(out) = types.StringNull()
	}
	return nil
}

func compositeResource(sections []Section[compositeModel]) *Composite[compositeModel] {
	return &Composite[compositeModel]{
		DefaultSite: "default",
		Site:        func(m *compositeModel) *types.String { return &m.Site },
		ID:          func(m *compositeModel) *types.String { return &m.ID },
		Timeouts:    func(m *compositeModel) *timeouts.Value { return &m.Timeouts },
		Sections:    sections,
	}
}

func compositeStateWith(t *testing.T, model compositeModel) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	state := tfsdk.State{Schema: compositeSchema()}
	model.Timeouts = timeouts.Value{Object: types.ObjectNull(compositeTimeoutTypes())}
	if diags := state.Set(ctx, &model); diags.HasError() {
		t.Fatalf("build state: %v", diags)
	}
	return state
}

func TestCompositeCreateWritesSectionsInOrder(t *testing.T) {
	var log []string
	a := fakeSection{name: "a", field: func(m *compositeModel) *types.String { return &m.A }, log: &log}
	b := fakeSection{name: "b", field: func(m *compositeModel) *types.String { return &m.B }, log: &log}
	c := compositeResource([]Section[compositeModel]{a, b})

	plan := compositeStateWith(t, compositeModel{
		Site: types.StringValue("site-1"),
		A:    types.StringValue("configured"),
		B:    types.StringValue("configured"),
	})
	resp := &resource.CreateResponse{State: compositeStateWith(t, compositeModel{})}
	c.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(plan)}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	want := []string{"write:a", "write:b", "read:a", "read:b"}
	if len(log) != len(want) {
		t.Fatalf("call log = %v, want %v", log, want)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Errorf("call log[%d] = %q, want %q (full log %v)", i, log[i], want[i], log)
		}
	}
}

func TestCompositeUnconfiguredSectionIsNotWrittenButIsStillRead(t *testing.T) {
	var log []string
	a := fakeSection{name: "a", field: func(m *compositeModel) *types.String { return &m.A }, log: &log}
	b := fakeSection{name: "b", field: func(m *compositeModel) *types.String { return &m.B }, log: &log}
	c := compositeResource([]Section[compositeModel]{a, b})

	// b is left null in the plan -- unconfigured.
	plan := compositeStateWith(t, compositeModel{
		Site: types.StringValue("site-1"),
		A:    types.StringValue("configured"),
	})
	resp := &resource.CreateResponse{State: compositeStateWith(t, compositeModel{})}
	c.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(plan)}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	for _, call := range log {
		if call == "write:b" {
			t.Fatalf("unconfigured section b was written; call log %v", log)
		}
	}
	found := false
	for _, call := range log {
		if call == "read:b" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unconfigured section b was never read; call log %v", log)
	}

	var got compositeModel
	if diags := resp.State.Get(context.Background(), &got); diags.HasError() {
		t.Fatalf("read back state: %v", diags)
	}
	if !got.B.IsNull() {
		t.Errorf("b = %v, want null (unconfigured section's Read must produce null)", got.B)
	}
	if got.A.ValueString() != "fetched:a" {
		t.Errorf("a = %v, want the fetched value", got.A)
	}
}

func TestCompositeSectionErrorAbortsTheApply(t *testing.T) {
	var log []string
	a := fakeSection{
		name: "a", field: func(m *compositeModel) *types.String { return &m.A }, log: &log,
		writeFn: func(context.Context, string, *compositeModel, *compositeModel, string) diag.Diagnostics {
			var diags diag.Diagnostics
			diags.AddError("boom", "section a refused to write")
			return diags
		},
	}
	b := fakeSection{name: "b", field: func(m *compositeModel) *types.String { return &m.B }, log: &log}
	c := compositeResource([]Section[compositeModel]{a, b})

	plan := compositeStateWith(t, compositeModel{
		Site: types.StringValue("site-1"),
		A:    types.StringValue("configured"),
		B:    types.StringValue("configured"),
	})
	resp := &resource.CreateResponse{State: compositeStateWith(t, compositeModel{})}
	c.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(plan)}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected a diagnostic error from section a's Write")
	}
	for _, call := range log {
		if call == "write:b" || call == "read:a" || call == "read:b" {
			t.Fatalf("section a's error should have aborted the apply; call log %v", log)
		}
	}
}

func TestCompositeReadSetsIDToTheResolvedSite(t *testing.T) {
	var log []string
	a := fakeSection{name: "a", field: func(m *compositeModel) *types.String { return &m.A }, log: &log}
	c := compositeResource([]Section[compositeModel]{a})

	state := compositeStateWith(t, compositeModel{
		ID: types.StringValue("stale"), Site: types.StringValue("site-9"),
	})
	resp := &resource.ReadResponse{State: state}
	c.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var got compositeModel
	if diags := resp.State.Get(context.Background(), &got); diags.HasError() {
		t.Fatalf("read back state: %v", diags)
	}
	if got.ID.ValueString() != "site-9" {
		t.Errorf("id = %q, want the resolved site %q", got.ID.ValueString(), "site-9")
	}
}

func TestCompositeUpdateUsesStatesSiteWhenThePlansIsUnknown(t *testing.T) {
	var log []string
	sawSite := make(map[string]string)
	a := fakeSection{
		name: "a", field: func(m *compositeModel) *types.String { return &m.A }, log: &log,
		writeFn: func(_ context.Context, site string, _, _ *compositeModel, _ string) diag.Diagnostics {
			sawSite["write"] = site
			return nil
		},
		readFn: func(_ context.Context, site string, _, out *compositeModel) diag.Diagnostics {
			sawSite["read"] = site
			out.A = types.StringValue("fetched:a")
			return nil
		},
	}
	c := compositeResource([]Section[compositeModel]{a})

	state := compositeStateWith(t, compositeModel{
		ID: types.StringValue("real-site"), Site: types.StringValue("real-site"),
		A: types.StringValue("configured"),
	})
	plan := compositeStateWith(t, compositeModel{
		Site: types.StringUnknown(), A: types.StringValue("configured"),
	})
	resp := &resource.UpdateResponse{State: state}
	c.Update(context.Background(), resource.UpdateRequest{State: state, Plan: tfsdk.Plan(plan)}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	if sawSite["write"] != "real-site" {
		t.Errorf("write saw site %q, want %q (state's site, not the plan's unknown one)", sawSite["write"], "real-site")
	}
	if sawSite["read"] != "real-site" {
		t.Errorf("read saw site %q, want %q", sawSite["read"], "real-site")
	}

	var got compositeModel
	if diags := resp.State.Get(context.Background(), &got); diags.HasError() {
		t.Fatalf("read back state: %v", diags)
	}
	if got.Site.ValueString() != "real-site" {
		t.Errorf("final site = %q, want %q", got.Site.ValueString(), "real-site")
	}
	if got.ID.ValueString() != "real-site" {
		t.Errorf("final id = %q, want %q", got.ID.ValueString(), "real-site")
	}
}

func TestCompositeDeleteLeavesNoState(t *testing.T) {
	var log []string
	a := fakeSection{name: "a", field: func(m *compositeModel) *types.String { return &m.A }, log: &log}
	c := compositeResource([]Section[compositeModel]{a})

	state := compositeStateWith(t, compositeModel{
		ID: types.StringValue("site-1"), Site: types.StringValue("site-1"),
		A: types.StringValue("configured"),
	})
	resp := &resource.DeleteResponse{State: state}
	c.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if len(log) != 0 {
		t.Errorf("Delete called a section: %v, want none (delete is a no-op)", log)
	}
}
