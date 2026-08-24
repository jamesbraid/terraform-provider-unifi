package unifi

// The list surface's filter behaviour, and specifically the one thing the
// cutover changed rather than reproduced: the hand-written resource ignored
// a filter it didn't recognize (its List only read postFilters["name"],
// ["record_type"] and ["enabled"]), so an unrecognized filter matched every
// record on the site instead of failing -- a wrong answer, which is worse
// than an error. The kit refuses it instead, asserted here as a deliberate
// divergence from the shipped behaviour.
//
// The probe name is not_a_field rather than a plausible typo, since a
// realistic misspelling of a real field name trips the spellcheck linter
// same as an accidental one.

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

	// tfsdk.Config has no Set (a configuration is something Terraform hands
	// the provider, not something the provider builds), so the value is
	// built via a State and moved across -- both carry the same Raw and
	// schema.
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
func drain(stream *list.ListResultsStream) string {
	if stream.Results == nil {
		return ""
	}
	var messages strings.Builder
	stream.Results(func(result list.ListResult) bool {
		for _, d := range result.Diagnostics {
			messages.WriteString(d.Summary() + ": " + d.Detail() + "\n")
		}
		return true
	})
	return messages.String()
}

// TestListRefusesAFilterThatNamesNoField is the assertion for the divergence.
func TestListRefusesAFilterThatNamesNoField(t *testing.T) {
	backend := &fakeDNSRecordBackend{result: &ui.DNSRecord{
		ID: "rec-1", Key: "host.example", RecordType: "A", Value: "10.0.0.1",
	}}
	r, _, _ := dnsRecordHarness(t, backend)

	stream := &list.ListResultsStream{}
	r.List(context.Background(), list.ListRequest{
		Config: listConfigFor(t, r, map[string]string{"not_a_field": "A"}),
	}, stream)

	messages := drain(stream)
	if !strings.Contains(messages, "not_a_field") {
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
			messages := drain(stream)
			if strings.Contains(messages, "no filterable field") {
				t.Fatalf("%q is a declared filter and was refused: %q", name, messages)
			}
		})
	}
}
