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

// TestEverySurfaceServesItsFrozenTypeName drives one subtest per surface
// against metadatacontract.FrozenTypeNames.
//
// It is not subsumed by TestEveryGeneratedSurfaceIsServedUnderItsDeclaredName,
// which compares the SET of served names against the SET the go:generate
// directives declare. Two facts a set can't see live only here: a
// receiver swapping names with another receiver (unifi_ap_group is served by
// both a resource and a data source), and a surface renamed in the directive
// and in Metadata together, which keeps the sets agreeing despite being a
// practitioner-visible break.
func TestEverySurfaceServesItsFrozenTypeName(t *testing.T) {
	served := servedSurfaces(t)

	// The provider serves a name too. Added here, not in servedSurfaces: the
	// agreement check compares against go:generate directives, and no
	// directive declares the provider.
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

	// The other direction: a frozen row whose receiver no longer serves
	// anything is a stale promise that would otherwise read as coverage
	// forever.
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

	// The provider is not a surface: the count is 42 surfaces plus the
	// provider row (which serves "unifi" and satisfies the same shape as a
	// resource), not 43 surfaces.
	const wantSurfaces = 42
	providerRows := 0
	for receiver, name := range metadatacontract.FrozenTypeNames {
		if name == "unifi" {
			providerRows++
			if receiver != "unifiProvider" {
				t.Errorf("%s serves the bare provider name %q; only the provider should",
					receiver, name)
			}
		}
	}
	if providerRows != 1 {
		t.Errorf("%d receiver(s) serve the bare name \"unifi\", want exactly 1 (the provider)",
			providerRows)
	}
	if surfaces := len(served) - providerRows; surfaces != wantSurfaces {
		t.Errorf("%d surface(s) plus %d provider row(s), want exactly %d surfaces",
			surfaces, providerRows, wantSurfaces)
	}

	t.Logf("%d receiver(s) checked against %d frozen name(s): %d surfaces and the provider",
		len(served), len(metadatacontract.FrozenTypeNames), len(served)-providerRows)
}

// servedSurfaces returns the Go type name of every surface the provider
// registers, mapped to the type name it serves. It is the one enumerator;
// servedTypeNames derives its set from this rather than walking the provider
// a second time.
//
// Returns the registered surfaces only, not the provider: servedTypeNames
// shares this enumerator, and adding the provider here would report a
// surface nobody broke there (no go:generate directive declares "unifi").
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
