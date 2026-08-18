package unifi

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	fwaction "github.com/hashicorp/terraform-plugin-framework/action"
	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/metadatacontract"
)

// TestEverySurfaceServesItsFrozenTypeName replaces forty-three hand-written
// tests with forty-three generated rows and one driver.
//
// EACH OF THE FORTY-THREE ASSERTED ONE FACT: construct a receiver, call
// Metadata, compare TypeName to a literal. Most did it in eleven lines and some
// in a forty-three-line table for the same single assertion. The fact is worth
// keeping; the forty-three copies of the scaffolding are not.
//
// THREE OF THEM ASSERTED NOTHING AT ALL. testaudit had already recorded
// Test_clientQosRateResource_Metadata, Test_clientResource_Metadata and
// Test_deviceResource_Metadata as no-assertion entries in unfailable-tests.txt.
// All forty-three rows here can fail, so this is a coverage increase and not
// merely a move.
//
// IT IS NOT SUBSUMED BY TestEveryGeneratedSurfaceIsServedUnderItsDeclaredName,
// which was the first thing I checked before deleting anything. That check
// compares the SET of served names against the SET declared by go:generate
// directives. Two facts live only here:
//
//   - PER RECEIVER. A set cannot see two receivers swapping each other's names,
//     and unifi_ap_group is served by both a resource and a data source, so the
//     set survives the swap unchanged.
//   - THE LITERAL. Rename a surface in the directive AND in Metadata together
//     and the sets still agree. That is a practitioner-visible break that
//     nothing else in the tree reports.
//
// EACH SURFACE IS ITS OWN NAMED SUBTEST, so a failure names the receiver
// exactly as the deleted function did, and forty-three cases still run.
func TestEverySurfaceServesItsFrozenTypeName(t *testing.T) {
	served := servedSurfaces(t)

	// The provider serves a name too, and it was the forty-third deleted test.
	// It is added here rather than inside servedSurfaces because the agreement
	// check compares against go:generate directives, and no directive declares
	// the provider.
	ctx := context.Background()
	providerResponse := &fwprovider.MetadataResponse{}
	rootProvider := &unifiProvider{}
	rootProvider.Metadata(ctx, fwprovider.MetadataRequest{}, providerResponse)
	served[goTypeName(rootProvider)] = providerResponse.TypeName

	if len(served) == 0 || len(metadatacontract.FrozenTypeNames) == 0 {
		t.Fatalf("served=%d frozen=%d; one side is empty, so this comparison proves nothing",
			len(served), len(metadatacontract.FrozenTypeNames))
	}

	receivers := make([]string, 0, len(served))
	for receiver := range served {
		receivers = append(receivers, receiver)
	}
	sort.Strings(receivers)

	for _, receiver := range receivers {
		t.Run(receiver, func(t *testing.T) {
			want, frozen := metadatacontract.FrozenTypeNames[receiver]
			if !frozen {
				t.Fatalf("%s serves %q but no row freezes its name.\n"+
					"    A new surface reaches practitioners without its name being pinned.\n"+
					"    Run go generate ./... to freeze it.", receiver, served[receiver])
			}
			if served[receiver] != want {
				t.Errorf("TypeName = %q, want %q\n\n"+
					"    This name is what practitioners write in their configuration, so a\n"+
					"    change here breaks every configuration using it. If the rename is\n"+
					"    deliberate, regenerating the contract records it as a visible diff.",
					served[receiver], want)
			}
		})
	}

	// THE OTHER DIRECTION. A frozen row whose receiver no longer serves anything
	// is a promise about a surface that has gone, and it would sit here reading
	// as coverage forever.
	var orphaned []string
	for receiver, name := range metadatacontract.FrozenTypeNames {
		if _, still := served[receiver]; !still {
			orphaned = append(orphaned, fmt.Sprintf("%s (froze %q)", receiver, name))
		}
	}
	sort.Strings(orphaned)
	if len(orphaned) > 0 {
		t.Errorf("%d frozen row(s) name a receiver the provider no longer serves:\n    %s\n\n"+
			"    Either the surface was removed, in which case regenerate, or it stopped\n"+
			"    being registered with the provider, which is a surface that vanished\n"+
			"    without anything reporting it.",
			len(orphaned), strings.Join(orphaned, "\n    "))
	}

	t.Logf("%d surface(s) checked against %d frozen name(s)",
		len(served), len(metadatacontract.FrozenTypeNames))
}

// servedSurfaces returns the Go type name of every surface the provider
// registers, mapped to the type name it serves.
//
// ONE ENUMERATOR, TWO CONSUMERS. servedTypeNames derives its set from this
// rather than walking the provider a second time -- two walks of one truth is
// how the pin package came to exist twice.
//
// IT RETURNS THE REGISTERED SURFACES AND NOT THE PROVIDER ITSELF. Sharing this
// with servedTypeNames widened that check's set the moment I added the provider
// here: "unifi" is served by the provider and declared by no go:generate
// directive, so the agreement check reported a surface nobody had broken. The
// provider is the contract driver's concern and is added there.
func servedSurfaces(t *testing.T) map[string]string {
	t.Helper()
	ctx := context.Background()
	provider := &unifiProvider{}
	served := map[string]string{}

	record := func(surface any, name string) {
		served[goTypeName(surface)] = name
	}

	for _, newResource := range provider.Resources(ctx) {
		response := &fwresource.MetadataResponse{}
		surface := newResource()
		surface.Metadata(ctx, fwresource.MetadataRequest{ProviderTypeName: "unifi"}, response)
		record(surface, response.TypeName)
	}
	for _, newDataSource := range provider.DataSources(ctx) {
		response := &fwdatasource.MetadataResponse{}
		surface := newDataSource()
		surface.Metadata(ctx, fwdatasource.MetadataRequest{ProviderTypeName: "unifi"}, response)
		record(surface, response.TypeName)
	}
	for _, newAction := range provider.Actions(ctx) {
		response := &fwaction.MetadataResponse{}
		surface := newAction()
		surface.Metadata(ctx, fwaction.MetadataRequest{ProviderTypeName: "unifi"}, response)
		record(surface, response.TypeName)
	}
	for _, newList := range provider.ListResources(ctx) {
		response := &fwresource.MetadataResponse{}
		surface := newList()
		surface.Metadata(ctx, fwresource.MetadataRequest{ProviderTypeName: "unifi"}, response)
		record(surface, response.TypeName)
	}

	return served
}

// goTypeName renders a surface's concrete type without its pointer or package,
// so it matches the receiver name the generator read out of the source.
func goTypeName(surface any) string {
	name := fmt.Sprintf("%T", surface)
	name = strings.TrimPrefix(name, "*")
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		name = name[dot+1:]
	}
	return name
}
