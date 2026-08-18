package resourcekit

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// THE LIST PATH HAD NO TESTS AT ALL, which is how it came to be the one
// lifecycle method the hooks were never wired into. Create, Read and Update
// each have a case asserting the hooks run; List had none, so its omission was
// not a regression anything could catch -- it was a gap nothing was looking at.

func kitListSchema(context.Context) listschema.Schema {
	return listschema.Schema{Attributes: map[string]listschema.Attribute{
		"site": listschema.StringAttribute{Optional: true},
		"filter": listschema.ListNestedAttribute{
			Optional: true,
			NestedObject: listschema.NestedAttributeObject{
				Attributes: map[string]listschema.Attribute{
					"name":  listschema.StringAttribute{Required: true},
					"value": listschema.StringAttribute{Required: true},
				},
			},
		},
	}}
}

// kitListResource wires the probe resource up for listing, with the objects the
// backend will return under the test's control.
func kitListResource(objects []kitSDK) *Resource[kitModel, kitSDK] {
	r := kitResource(Backend[kitSDK]{
		List: func(context.Context, string) ([]kitSDK, error) { return objects, nil },
	})
	r.ListSurface = ListSpec[kitSDK]{
		ConfigSchema: kitListSchema,
		DisplayName:  func(s *kitSDK) string { return s.Name },
		Filters:      map[string]func(*kitSDK) string{"name": func(s *kitSDK) string { return s.Name }},
	}
	return r
}

// kitListRequest builds a request, optionally carrying one filter block. The
// filter is a real config value rather than a field poked into the resource,
// because the code under test reads it out of the config and a shortcut there
// would test a path practitioners never take.
func kitListRequest(t *testing.T, filterName, filterValue string) list.ListRequest {
	t.Helper()
	ctx := context.Background()

	configSchema := kitListSchema(ctx)
	config := tfsdk.Config{Schema: configSchema}
	filterType := configSchema.Type().TerraformType(ctx).(tftypes.Object).AttributeTypes["filter"]
	filterValues := tftypes.NewValue(filterType, nil)
	if filterName != "" {
		element := filterType.(tftypes.List).ElementType
		filterValues = tftypes.NewValue(filterType, []tftypes.Value{
			tftypes.NewValue(element, map[string]tftypes.Value{
				"name":  tftypes.NewValue(tftypes.String, filterName),
				"value": tftypes.NewValue(tftypes.String, filterValue),
			}),
		})
	}
	config.Raw = tftypes.NewValue(configSchema.Type().TerraformType(ctx), map[string]tftypes.Value{
		"site":   tftypes.NewValue(tftypes.String, "default"),
		"filter": filterValues,
	})

	identityResp := &resource.IdentitySchemaResponse{}
	(&Resource[kitModel, kitSDK]{}).IdentitySchema(ctx, resource.IdentitySchemaRequest{}, identityResp)

	return list.ListRequest{
		Config:                 config,
		ResourceSchema:         kitSchema(ctx),
		ResourceIdentitySchema: identityResp.IdentitySchema,
		IncludeResource:        true,
	}
}

// drain runs List and collects every result the stream yields.
func drain(t *testing.T, r *Resource[kitModel, kitSDK]) []list.ListResult {
	t.Helper()
	return drainFiltered(t, r, "", "")
}

func drainFiltered(t *testing.T, r *Resource[kitModel, kitSDK], name, value string) []list.ListResult {
	t.Helper()
	stream := &list.ListResultsStream{}
	r.List(context.Background(), kitListRequest(t, name, value), stream)
	var out []list.ListResult
	if stream.Results == nil {
		return out
	}
	stream.Results(func(result list.ListResult) bool {
		out = append(out, result)
		return true
	})
	return out
}

// THE CONTROL. Every case below counts hook calls, so a List that yielded
// nothing would satisfy "prefetch ran once" trivially by running it zero times
// for zero objects -- this fixes how many results the probe produces.
func TestListYieldsOneResultPerMatchingObject(t *testing.T) {
	results := drain(t, kitListResource([]kitSDK{{ID: "1", Name: "a"}, {ID: "2", Name: "b"}}))
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	for _, result := range results {
		if result.Diagnostics.HasError() {
			t.Errorf("result carried an error: %v", result.Diagnostics)
		}
	}
}

// PREFETCH ONCE, AfterReceive PER OBJECT. The counts are asserted separately
// and exactly, because the plausible wrong fix -- prefetching inside the loop --
// produces correct models while turning one request into one per result.
func TestListPrefetchesOnceAndRunsAfterReceivePerObject(t *testing.T) {
	r := kitListResource([]kitSDK{{ID: "1", Name: "a"}, {ID: "2", Name: "b"}, {ID: "3", Name: "c"}})
	hooks, seen := hookSpy(t)
	r.Spec.Prefetch, r.Spec.AfterReceive = hooks.Prefetch, hooks.AfterReceive

	if results := drain(t, r); len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}
	if got := (*seen)["prefetch"]; got != 1 {
		t.Errorf("Prefetch ran %d time(s) for 3 objects, want exactly 1; "+
			"per-object prefetching is one request per result", got)
	}
	if got := (*seen)["afterReceive"]; got != 3 {
		t.Errorf("AfterReceive ran %d time(s) for 3 objects, want 3; "+
			"a listed object must reach state through the same steps as a read one", got)
	}
	// BeforeSend must NOT run: listing sends nothing, and a hook that mutates
	// an outbound object has no outbound object here.
	if got := (*seen)["beforeSend"]; got != 0 {
		t.Errorf("BeforeSend ran %d time(s) during a list, which sends nothing", got)
	}
}

// A filtered-out object must not be handed to AfterReceive. The hook exists to
// finish a model that will be returned, and running it for a discarded object
// is work nobody sees -- and, for a surface whose hook errors on unexpected
// data, a failure the practitioner cannot explain.
//
// The filter is supplied in the CONFIG, which is how a practitioner supplies
// one. The first version of this test shortened the object list instead and so
// asserted nothing about filtering at all: two counts of one, and a name that
// claimed more than the body did.
func TestListDoesNotRunAfterReceiveForFilteredOutObjects(t *testing.T) {
	r := kitListResource([]kitSDK{{ID: "1", Name: "keep"}, {ID: "2", Name: "drop"}})
	hooks, seen := hookSpy(t)
	r.Spec.Prefetch, r.Spec.AfterReceive = hooks.Prefetch, hooks.AfterReceive

	results := drainFiltered(t, r, "name", "keep")
	if len(results) != 1 {
		t.Fatalf("the filter yielded %d of 2 objects, want 1; if it matched both, "+
			"the count below proves nothing", len(results))
	}
	if got := (*seen)["afterReceive"]; got != 1 {
		t.Errorf("AfterReceive ran %d time(s) for 1 yielded object of 2 listed, want 1", got)
	}
	if got := (*seen)["prefetch"]; got != 1 {
		t.Errorf("Prefetch ran %d time(s), want 1", got)
	}
}

// A FAILING PREFETCH ENDS THE LIST rather than yielding models built without
// it. Half-built results are worse than none: they look like the site's real
// contents.
func TestListStopsWhenPrefetchFails(t *testing.T) {
	r := kitListResource([]kitSDK{{ID: "1", Name: "a"}})
	r.Spec.Prefetch = func(context.Context, string) (any, diag.Diagnostics) {
		var diags diag.Diagnostics
		diags.AddError("Inventory unavailable", "the site network list could not be read")
		return nil, diags
	}
	afterReceiveRan := false
	r.Spec.AfterReceive = func(context.Context, *kitSDK, *kitModel, any) diag.Diagnostics {
		afterReceiveRan = true
		return nil
	}

	results := drain(t, r)
	if afterReceiveRan {
		t.Error("AfterReceive ran after the prefetch it depends on had failed")
	}
	var reported bool
	for _, result := range results {
		if result.Diagnostics.HasError() {
			reported = true
		}
	}
	if !reported {
		t.Fatal("a failed prefetch produced no error diagnostic, so the list would read as an empty site")
	}
}

// A WARNING IS CARRIED, NOT DROPPED. Only an error ends the stream, so anything
// short of one would vanish here while Read reports it -- a new asymmetry
// inside the fix for one.
func TestListCarriesANonFatalPrefetchWarning(t *testing.T) {
	r := kitListResource([]kitSDK{{ID: "1", Name: "a"}, {ID: "2", Name: "b"}})
	r.Spec.Prefetch = func(context.Context, string) (any, diag.Diagnostics) {
		var diags diag.Diagnostics
		diags.AddWarning("Partial inventory", "some networks could not be read")
		return "partial", diags
	}

	results := drain(t, r)
	if len(results) != 2 {
		t.Fatalf("a warning ended the list: got %d results, want 2", len(results))
	}
	warnings := 0
	for _, result := range results {
		warnings += result.Diagnostics.WarningsCount()
	}
	if warnings != 1 {
		t.Errorf("the prefetch warning appears %d time(s), want exactly 1; "+
			"dropping it hides what Read would report, repeating it once per object is noise", warnings)
	}
}

// A surface declaring no hooks must still list its objects. Without this, every
// assertion above would be satisfied by a List that only worked when the hook
// machinery was present -- which is all but one of the surfaces served today.
//
// Named for what it checks rather than "is unchanged", which would promise a
// comparison against a pre-change baseline that this does not make.
func TestListWithNoHooksYieldsTheObjectItWasGiven(t *testing.T) {
	r := kitListResource([]kitSDK{{ID: "1", Name: "a"}})
	results := drain(t, r)
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	var model kitModel
	if diags := results[0].Resource.Get(context.Background(), &model); diags.HasError() {
		t.Fatalf("reading the listed resource: %v", diags)
	}
	if model.Name.ValueString() != "a" {
		t.Errorf("Name = %q, want %q", model.Name.ValueString(), "a")
	}
	if !model.Timeouts.Object.IsNull() {
		t.Error("a listed object carries timeouts, which belong to a managed resource")
	}
}

var _ = types.StringValue
