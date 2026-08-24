package unifi

import (
	"context"
	"reflect"
	"sort"
	"testing"

	fwaction "github.com/hashicorp/terraform-plugin-framework/action"
	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/metadatacontract"
)

// wantConfigurableSurfaces is the number of surfaces that accept provider
// data -- an exact count, not a floor. A surface that stops implementing
// Configure disappears from the enumeration below and every remaining row
// still passes, so this is what catches it. Raise it deliberately when a
// surface is added.
const wantConfigurableSurfaces = 42

// TestEveryConfigurableSurfaceAcceptsProviderData asserts, for every
// configurable surface, that Configure(nil) doesn't error, Configure(wrong
// type) does, and Configure(*Client) succeeds and changes the receiver
// (storesProviderData explains why "changed the receiver" rather than a
// named field).
func TestEveryConfigurableSurfaceAcceptsProviderData(t *testing.T) {
	surfaces := configurableSurfaces(t)

	if len(surfaces) != wantConfigurableSurfaces {
		t.Fatalf("%d configurable surface(s), want exactly %d.\n"+
			"    A surface that stops accepting provider data leaves every other row\n"+
			"    passing, so this count is the only thing that reports it.",
			len(surfaces), wantConfigurableSurfaces)
	}

	// A count alone doesn't survive a substitution: one surface dropping out
	// while another drops in leaves the total at 42 and every row passing, so
	// the set is checked too. The expected set is derived from the metadata
	// contract's receivers (minus the provider, which isn't configurable)
	// rather than frozen again here, so there's one list of surfaces in the
	// tree rather than two that can disagree.
	expected := map[string]bool{}
	for receiver, served := range metadatacontract.FrozenTypeNames {
		if served == "unifi" {
			continue
		}
		expected[receiver] = true
	}
	var arrived, departed []string
	for name := range surfaces {
		if !expected[name] {
			arrived = append(arrived, name)
		}
	}
	for name := range expected {
		if _, still := surfaces[name]; !still {
			departed = append(departed, name)
		}
	}
	sort.Strings(arrived)
	sort.Strings(departed)
	if len(arrived) > 0 || len(departed) > 0 {
		t.Errorf("the configurable set no longer matches the frozen surface contract.\n"+
			"    arrived, configurable but not in the contract: %v\n"+
			"    departed, in the contract but no longer configurable: %v\n\n"+
			"    Equal numbers of each leave the count at %d and every row passing,\n"+
			"    which is why the set is compared and not just its size.",
			arrived, departed, wantConfigurableSurfaces)
	}

	names := make([]string, 0, len(surfaces))
	for name := range surfaces {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		configure := surfaces[name]
		t.Run(name, func(t *testing.T) {
			t.Run("nil provider data does not error", func(t *testing.T) {
				if errored, _ := configure(nil); errored {
					t.Error("Configure() errored on nil provider data, which the framework " +
						"sends before the provider has configured itself")
				}
			})
			t.Run("wrong type produces error", func(t *testing.T) {
				if errored, _ := configure("wrong"); !errored {
					t.Error("Configure() accepted provider data of the wrong type without " +
						"a diagnostic, so a misconfigured provider would fail later and elsewhere")
				}
			})
			t.Run("correct type does not error", func(t *testing.T) {
				if errored, _ := configure(&Client{}); errored {
					t.Error("Configure() errored on a correct *Client")
				}
			})
			t.Run("correct type is stored", func(t *testing.T) {
				_, configured := configure(&Client{})
				if !storesProviderData(configured) {
					t.Error("Configure() accepted a *Client and left the receiver unchanged, " +
						"so the surface has no client to work with")
				}
			})
		})
	}

	t.Logf("%d configurable surface(s) x 4 assertions", len(surfaces))
}

// configurableSurfaces maps each surface's Go type name to a function that runs
// its Configure with the given provider data and reports whether it errored.
func configurableSurfaces(t *testing.T) map[string]func(any) (bool, any) {
	t.Helper()
	ctx := context.Background()
	provider := &unifiProvider{}
	out := map[string]func(any) (bool, any){}

	for _, newResource := range provider.Resources(ctx) {
		if s, ok := newResource().(fwresource.ResourceWithConfigure); ok {
			out[goTypeName(s)] = func(data any) (bool, any) {
				fresh, ok := freshCopy(s)
				if !ok {
					t.Errorf("%s: freshCopy produced a %T, not a fwresource.ResourceWithConfigure", goTypeName(s), fresh)
					return true, nil
				}
				resp := &fwresource.ConfigureResponse{}
				fresh.Configure(ctx, fwresource.ConfigureRequest{ProviderData: data}, resp)
				return resp.Diagnostics.HasError(), fresh
			}
		}
	}
	for _, newDataSource := range provider.DataSources(ctx) {
		if s, ok := newDataSource().(fwdatasource.DataSourceWithConfigure); ok {
			out[goTypeName(s)] = func(data any) (bool, any) {
				fresh, ok := freshCopy(s)
				if !ok {
					t.Errorf("%s: freshCopy produced a %T, not a fwdatasource.DataSourceWithConfigure", goTypeName(s), fresh)
					return true, nil
				}
				resp := &fwdatasource.ConfigureResponse{}
				fresh.Configure(ctx, fwdatasource.ConfigureRequest{ProviderData: data}, resp)
				return resp.Diagnostics.HasError(), fresh
			}
		}
	}
	for _, newAction := range provider.Actions(ctx) {
		if s, ok := newAction().(fwaction.ActionWithConfigure); ok {
			out[goTypeName(s)] = func(data any) (bool, any) {
				fresh, ok := freshCopy(s)
				if !ok {
					t.Errorf("%s: freshCopy produced a %T, not a fwaction.ActionWithConfigure", goTypeName(s), fresh)
					return true, nil
				}
				resp := &fwaction.ConfigureResponse{}
				fresh.Configure(ctx, fwaction.ConfigureRequest{ProviderData: data}, resp)
				return resp.Diagnostics.HasError(), fresh
			}
		}
	}
	return out
}

// storesProviderData reports whether Configure left any mark on the receiver.
//
// It compares a freshly configured surface against an untouched one of the same
// type rather than reading a named field, because the surfaces do not agree on
// what to call what they store.
func storesProviderData(configured any) bool {
	if configured == nil {
		return false
	}
	fresh, _ := freshCopy(configured)
	return !reflect.DeepEqual(fresh, configured)
}

// freshCopy returns a zero value of surface's concrete type, typed as T so
// no caller needs an assertion of its own. The assertion lives here, once:
// it cannot fail in practice, since the caller already established surface
// satisfies T, but reporting ok rather than panicking keeps that reasoning
// falsifiable instead of assumed.
func freshCopy[T any](surface T) (T, bool) {
	v := reflect.ValueOf(surface)
	var made any
	if v.Kind() == reflect.Ptr {
		made = reflect.New(v.Type().Elem()).Interface()
	} else {
		made = reflect.Zero(v.Type()).Interface()
	}
	fresh, ok := made.(T)
	return fresh, ok
}
