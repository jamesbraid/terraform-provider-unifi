package resourcekit

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

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
		Filters: map[string]func(*kitSDK) string{
			"name": func(s *kitSDK) string { return s.Name },
		},
	}
	return r
}

// kitListRequest builds a request, optionally carrying one filter block, as a
// real config value rather than a field poked into the resource -- a
// shortcut there would test a path practitioners never take.
func kitListRequest(t *testing.T, filterName, filterValue string) list.ListRequest {
	t.Helper()
	ctx := context.Background()

	configSchema := kitListSchema(ctx)
	config := tfsdk.Config{Schema: configSchema}
	// Checked rather than forced: an unchecked assertion here would panic
	// with a message naming the type, not the schema, and this helper runs
	// for every List test.
	schemaObject, ok := configSchema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("the list config schema is not an object: %T",
			configSchema.Type().TerraformType(ctx))
	}
	filterType := schemaObject.AttributeTypes["filter"]
	filterValues := tftypes.NewValue(filterType, nil)
	if filterName != "" {
		filterList, ok := filterType.(tftypes.List)
		if !ok {
			t.Fatalf("the filter attribute is not a list: %T", filterType)
		}
		element := filterList.ElementType
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
	(&Resource[kitModel, kitSDK]{}).IdentitySchema(
		ctx,
		resource.IdentitySchemaRequest{},
		identityResp,
	)

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

func drainFiltered(
	t *testing.T,
	r *Resource[kitModel, kitSDK],
	name, value string,
) []list.ListResult {
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

func TestListYieldsOneResultPerMatchingObject(t *testing.T) {
	results := drain(t, kitListResource([]kitSDK{{ID: "1", Name: "a"}, {ID: "2", Name: "b"}}))
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d; the hook-count tests below assume "+
			"List actually yields objects", len(results))
	}
	for _, result := range results {
		if result.Diagnostics.HasError() {
			t.Errorf("result carried an error: %v", result.Diagnostics)
		}
	}
}

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
	if got := (*seen)["beforeSend"]; got != 0 {
		t.Errorf("BeforeSend ran %d time(s) during a list, which sends nothing", got)
	}
}

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
		t.Errorf("AfterReceive ran %d time(s) for 1 yielded object of 2 listed, "+
			"want 1; running it on a discarded object is wasted work a "+
			"surface's hook could still fail on", got)
	}
	if got := (*seen)["prefetch"]; got != 1 {
		t.Errorf("Prefetch ran %d time(s), want 1", got)
	}
}

func TestListStopsWhenPrefetchFails(t *testing.T) {
	r := kitListResource([]kitSDK{{ID: "1", Name: "a"}})
	r.Spec.Prefetch = func(context.Context, string) (any, diag.Diagnostics) {
		var diags diag.Diagnostics
		diags.AddError("Inventory unavailable", "the site network list could not be read")
		return nil, diags
	}
	afterReceiveRan := false
	r.Spec.AfterReceive = func(context.Context, *kitSDK, *kitModel, kitModel, any) diag.Diagnostics {
		afterReceiveRan = true
		return nil
	}

	results := drain(t, r)
	if afterReceiveRan {
		t.Error("AfterReceive ran after the prefetch it depends on had failed; " +
			"a half-built result looks like the site's real contents, which is worse than none")
	}
	var reported bool
	for _, result := range results {
		if result.Diagnostics.HasError() {
			reported = true
		}
	}
	if !reported {
		t.Fatal(
			"a failed prefetch produced no error diagnostic, so the list would read as an empty site",
		)
	}
}

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

func TestListWithNoHooksYieldsTheObjectItWasGiven(t *testing.T) {
	r := kitListResource([]kitSDK{{ID: "1", Name: "a"}})
	results := drain(t, r)
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d; every assertion above only exercised "+
			"a surface with hooks, and most surfaces have none", len(results))
	}
	var model kitModel
	if diags := results[0].Resource.Get(context.Background(), &model); diags.HasError() {
		t.Fatalf("reading the listed resource: %v", diags)
	}
	if model.Name.ValueString() != "a" {
		t.Errorf("Name = %q, want %q", model.Name.ValueString(), "a")
	}
	if !model.Timeouts.IsNull() {
		t.Error("a listed object carries timeouts, which belong to a managed resource")
	}
}

// TestListRefusesASpecMissingIDOrTimeouts proves the same guard Create,
// Read, Update and Delete use also covers List: list.go dereferences
// Backend.GetID and Spec.Timeouts directly in its stream callback, and a
// descriptor missing either would otherwise panic there instead of failing
// with a diagnostic naming the descriptor. Mirrors
// TestResourceRefusesASpecMissingIDSiteOrTimeouts's style.
func TestListRefusesASpecMissingIDOrTimeouts(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		nilOut func(*Resource[kitModel, kitSDK])
	}{
		{"ID", func(r *Resource[kitModel, kitSDK]) { r.Spec.ID = nil }},
		{"Timeouts", func(r *Resource[kitModel, kitSDK]) { r.Spec.Timeouts = nil }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := kitListResource([]kitSDK{{ID: "1", Name: "a"}})
			testCase.nilOut(r)

			results := drain(t, r)
			if len(results) != 1 {
				t.Fatalf("want 1 result carrying the error, got %d", len(results))
			}
			if !results[0].Diagnostics.HasError() {
				t.Fatalf("a Resource with a nil Spec.%s was accepted by List", testCase.name)
			}
			if !strings.Contains(results[0].Diagnostics.Errors()[0].Detail(), "kit_probe") {
				t.Errorf("the error does not name the descriptor: %q",
					results[0].Diagnostics.Errors()[0].Detail())
			}
			if !strings.Contains(results[0].Diagnostics.Errors()[0].Detail(), testCase.name) {
				t.Errorf("the error does not name the missing accessor %q: %q",
					testCase.name, results[0].Diagnostics.Errors()[0].Detail())
			}
		})
	}
}

var _ = types.StringValue
