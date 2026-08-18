package unifi

// The list surface's filter behaviour, and specifically the one thing the
// cutover CHANGED rather than reproduced.
//
// THE HAND-WRITTEN RESOURCE IGNORED A FILTER IT DID NOT RECOGNISE. Its List
// read `postFilters["name"]`, `["record_type"]` and `["enabled"]` and looked at
// nothing else, so `filter { name = "recrod_type" ... }` matched every record on
// the site and the practitioner got a complete list back. A WRONG ANSWER RATHER
// THAN A FAILURE, and the wrong answer is the worse of the two: a typo reads as
// "that value matched everything" instead of as a mistake.
//
// The kit refuses it. That is a deliberate divergence from the behaviour that
// shipped, so it is asserted here rather than described in a commit message.

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// listConfigFor builds the list configuration a practitioner would write.
func listConfigFor(t *testing.T, r *dnsRecordKitResource, filters map[string]string) tfsdk.Config {
	t.Helper()
	ctx := context.Background()

	schemaResp := &list.ListResourceSchemaResponse{}
	r.ListResourceConfigSchema(ctx, list.ListResourceSchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("build the list config schema: %v", schemaResp.Diagnostics)
	}

	entries := make([]resourcekit.ListFilter, 0, len(filters))
	for name, value := range filters {
		entries = append(entries, resourcekit.ListFilter{
			Name:  types.StringValue(name),
			Value: types.StringValue(value),
		})
	}
	filterType := types.ObjectType{AttrTypes: map[string]attr.Type{
		"name":  types.StringType,
		"value": types.StringType,
	}}
	built, diags := types.ListValueFrom(ctx, filterType, entries)
	if diags.HasError() {
		t.Fatalf("build the filter list: %v", diags)
	}

	// A tfsdk.Config is READ-ONLY -- it has no Set, because a configuration is
	// something Terraform hands the provider rather than something the provider
	// builds. A State does have one, so the value is built there and moved
	// across; both carry the same Raw and the same schema.
	staging := tfsdk.State{Schema: schemaResp.Schema}
	staging.Raw = tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), nil)
	if diags := staging.Set(ctx, resourcekit.ListConfig{
		Site:   types.StringValue("default"),
		Filter: built,
	}); diags.HasError() {
		t.Fatalf("set the list config: %v", diags)
	}
	return tfsdk.Config{Schema: schemaResp.Schema, Raw: staging.Raw}
}

// drain runs the stream and collects whatever diagnostics it produced.
func drain(stream *list.ListResultsStream) (int, string) {
	if stream.Results == nil {
		return 0, ""
	}
	count := 0
	var messages strings.Builder
	stream.Results(func(result list.ListResult) bool {
		count++
		for _, d := range result.Diagnostics {
			messages.WriteString(d.Summary() + ": " + d.Detail() + "\n")
		}
		return true
	})
	return count, messages.String()
}

// TestListRefusesAFilterThatNamesNoField is the assertion for the divergence.
func TestListRefusesAFilterThatNamesNoField(t *testing.T) {
	backend := &fakeDNSRecordBackend{result: &ui.DNSRecord{
		ID: "rec-1", Key: "host.example", RecordType: "A", Value: "10.0.0.1",
	}}
	r, _, _ := dnsRecordHarness(t, backend)

	stream := &list.ListResultsStream{}
	r.List(context.Background(), list.ListRequest{
		Config: listConfigFor(t, r, map[string]string{"recrod_type": "A"}),
	}, stream)

	_, messages := drain(stream)
	if !strings.Contains(messages, "recrod_type") {
		t.Fatalf("the refusal does not name the offending filter: %q.\n\n"+
			"A filter naming no field must be refused BY NAME. The hand-written resource "+
			"ignored it and returned every record, so a typo read as a value that matched "+
			"everything -- a wrong answer rather than a failure.", messages)
	}
	if !strings.Contains(messages, "no filterable field") {
		t.Fatalf("the refusal is not the unknown-filter one: %q", messages)
	}
}

// TestListAcceptsTheFiltersTheSurfaceDeclares is the control. Without it, the
// test above is satisfied by refusing EVERY filter, which would break the list
// surface entirely while looking like a stricter provider.
func TestListAcceptsTheFiltersTheSurfaceDeclares(t *testing.T) {
	backend := &fakeDNSRecordBackend{result: &ui.DNSRecord{
		ID: "rec-1", Key: "host.example", RecordType: "A", Value: "10.0.0.1",
	}}
	r, _, _ := dnsRecordHarness(t, backend)

	for _, name := range []string{"name", "record_type", "enabled"} {
		t.Run(name, func(t *testing.T) {
			stream := &list.ListResultsStream{}
			r.List(context.Background(), list.ListRequest{
				Config: listConfigFor(t, r, map[string]string{name: "unmatchable"}),
			}, stream)
			_, messages := drain(stream)
			if strings.Contains(messages, "no filterable field") {
				t.Fatalf("%q is a declared filter and was refused: %q", name, messages)
			}
		})
	}
}
