package schemabehaviour

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The estate's real shapes, reduced to one file: a shared attribute map held in
// a local, a nested attribute, a block, a custom type, static defaults of three
// kinds, a validator message written as two literals, and an attribute built by
// a call rather than written out.
const fixture = `package unifi

import (
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/provider"
)

func (p *fixtureProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "unifi"
}

func (r *thingResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_thing"
}

var sharedPort = rschema.StringAttribute{
	Validators: []validator.String{
		stringvalidator.RegexMatches(regexp.MustCompile("^[0-9]+$"), "digits " +
			"only"),
	},
}

func (r *thingResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	endpointAttrs := map[string]rschema.Attribute{
		"port": sharedPort,
		"kind": rschema.StringAttribute{
			Default:    stringdefault.StaticString("any"),
			Validators: []validator.String{stringvalidator.OneOf("any", "one")},
		},
	}
	resp.Schema = rschema.Schema{
		Attributes: map[string]rschema.Attribute{
			"enabled": rschema.BoolAttribute{
				Default: booldefault.StaticBool(true),
			},
			"rekey": rschema.Int64Attribute{
				Default:    int64default.StaticInt64(3600),
				Validators: []validator.Int64{int64validator.Between(1, 255)},
			},
			"mac": rschema.StringAttribute{
				CustomType: hwtypes.MACAddressType{},
			},
			"source":      rschema.SingleNestedAttribute{Attributes: endpointAttrs},
			"destination": rschema.SingleNestedAttribute{Attributes: endpointAttrs},
			"timeouts":    timeouts.Attributes(ctx, timeouts.Opts{Create: true}),
			"computed":    buildAttribute(),
		},
		Blocks: map[string]rschema.Block{
			"schedule": rschema.ListNestedBlock{
				NestedObject: rschema.NestedBlockObject{
					Attributes: map[string]rschema.Attribute{
						"duration": rschema.StringAttribute{
							CustomType: timetypes.GoDurationType{},
							PlanModifiers: []planmodifier.String{
								stringplanmodifier.UseStateForUnknown(),
							},
						},
					},
				},
			},
		},
	}
}
`

func derived(t *testing.T) Surface {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "thing.go"), []byte(fixture), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	surfaces, err := DeriveDir(dir)
	if err != nil {
		t.Fatalf("DeriveDir: %v", err)
	}
	if len(surfaces) != 1 {
		t.Fatalf("expected one surface, got %d", len(surfaces))
	}
	return surfaces[0]
}

func facts(surface Surface) map[string]string {
	got := map[string]string{}
	for _, behaviour := range surface.Behaviours {
		key := behaviour.Path + " " + behaviour.Kind
		if existing, seen := got[key]; seen {
			got[key] = existing + " | " + behaviour.Expression
			continue
		}
		got[key] = behaviour.Expression
	}
	return got
}

// Test_derivesEveryShapeTheEstateUses checks each construct that actually
// appears in the provider, named individually so a failure says which one
// broke rather than that a count moved.
func Test_derivesEveryShapeTheEstateUses(t *testing.T) {
	surface := derived(t)
	if surface.TypeName != "unifi_thing" {
		t.Errorf("resource type name is %q, want unifi_thing — it is built from the "+
			"provider's own Metadata plus the resource's, not assumed", surface.TypeName)
	}
	got := facts(surface)

	for _, want := range []struct{ key, expression, why string }{
		{"enabled default", "booldefault.StaticBool(true)", "a plain default"},
		{"rekey validator", "int64validator.Between(1, 255)", "a validator with numeric arguments"},
		{"mac custom_type", "hwtypes.MACAddressType{}",
			"a custom type, which stays a string on the wire and so is invisible to a schema comparison"},
		{"schedule.duration custom_type", "timetypes.GoDurationType{}",
			"a custom type INSIDE A BLOCK, which needs both the block walk and the custom type read"},
		{"schedule.duration plan_modifier", "stringplanmodifier.UseStateForUnknown()",
			"a plan modifier inside a block"},
		{"source.kind validator", `stringvalidator.OneOf("any", "one")`,
			"an attribute reached through a shared map held in a local variable"},
		{"destination.kind validator", `stringvalidator.OneOf("any", "one")`,
			"the SECOND user of that shared map — firewall_policy had two and a walker " +
				"that followed neither lost twenty-four behaviours in silence"},
		{"source.port validator",
			`stringvalidator.RegexMatches(regexp.MustCompile("^[0-9]+$"), "digits only")`,
			"an attribute that is itself a package-level value, with a message written " +
				"as two literals and joined here"},
	} {
		if got[want.key] != want.expression {
			t.Errorf("%s\n  path+kind: %s\n  derived:   %s\n  want:      %s",
				want.why, want.key, got[want.key], want.expression)
		}
	}
}

// Test_staticDefaultsCarryTheirValue checks the three literal kinds a default
// reduces to, since a default written as code instead of a value would make the
// generated schema build and behave differently.
func Test_staticDefaultsCarryTheirValue(t *testing.T) {
	byPath := map[string]Behaviour{}
	for _, behaviour := range derived(t).Behaviours {
		if behaviour.Kind == KindDefault {
			byPath[behaviour.Path] = behaviour
		}
	}

	if static := byPath["enabled"].Static; static == nil || static.Bool == nil || !*static.Bool {
		t.Errorf("enabled's default did not reduce to the boolean true: %+v", byPath["enabled"].Static)
	}
	if static := byPath["rekey"].Static; static == nil || static.Int64 == nil || *static.Int64 != 3600 {
		t.Errorf("rekey's default did not reduce to the integer 3600: %+v", byPath["rekey"].Static)
	}
	if static := byPath["source.kind"].Static; static == nil || static.String == nil || *static.String != "any" {
		t.Errorf("source.kind's default did not reduce to the string \"any\": %+v", byPath["source.kind"].Static)
	}
}

// Test_customTypeCarriesItsValueType pins the one thing this package works out
// by convention rather than reads. It is checked here, against the two
// migrated policies, and finally by the generated code compiling.
func Test_customTypeCarriesItsValueType(t *testing.T) {
	want := map[string]string{
		"mac":               "hwtypes.MACAddress",
		"schedule.duration": "timetypes.GoDuration",
	}
	for _, behaviour := range derived(t).Behaviours {
		if behaviour.Kind != KindCustomType {
			continue
		}
		if expected, ok := want[behaviour.Path]; ok && behaviour.ValueType != expected {
			t.Errorf("%s: value type derived as %q, want %q",
				behaviour.Path, behaviour.ValueType, expected)
		}
		delete(want, behaviour.Path)
	}
	for path := range want {
		t.Errorf("%s: no custom type derived at all", path)
	}
}

// Test_unreadableAttributesAreNamed is the negative this package must be able
// to produce.
//
// An attribute whose value is a call cannot be read from source, and the
// failure that matters is not that it is unread but that it could be unread
// SILENTLY -- which is what a policy written from a quiet tool would inherit.
// Both estate shapes are here: the timeouts helper, and a call standing for any
// attribute built rather than written.
func Test_unreadableAttributesAreNamed(t *testing.T) {
	surface := derived(t)
	named := map[string]string{}
	for _, unread := range surface.Opaque {
		named[unread.Path] = unread.Reason
	}

	for _, path := range []string{"timeouts", "computed"} {
		reason, ok := named[path]
		if !ok {
			var paths []string
			for known := range named {
				paths = append(paths, known)
			}
			sort.Strings(paths)
			t.Errorf("%s was not read and was not reported either, so a policy written from "+
				"this would omit whatever it carries with nothing said; reported: %v", path, paths)
			continue
		}
		if !strings.Contains(reason, "not a literal") {
			t.Errorf("%s is reported as %q, which does not say what stopped it being read",
				path, reason)
		}
	}

	if len(surface.Behaviours) == 0 {
		t.Fatal("nothing was derived at all, so the checks above would pass against an empty result")
	}
}

// Test_derivedExpressionsParse guards the promise that what is written into a
// policy is Go. A schema_definition that does not parse fails at the generator,
// several steps from the mistake.
func Test_derivedExpressionsParse(t *testing.T) {
	for _, behaviour := range derived(t).Behaviours {
		if strings.TrimSpace(behaviour.Expression) == "" {
			t.Errorf("%s %s derived an empty expression", behaviour.Path, behaviour.Kind)
		}
		if strings.Contains(behaviour.Expression, "\n") {
			t.Errorf("%s %s spans lines, which a policy carries as an escaped string: %q",
				behaviour.Path, behaviour.Kind, behaviour.Expression)
		}
	}
}

// Test_importsComeFromTheDefiningFile checks that a behaviour's imports are
// resolved where the expression is written. A shared attribute lives in one
// file and is used in another, and taking the user's imports would attribute
// the wrong package or none at all.
func Test_importsComeFromTheDefiningFile(t *testing.T) {
	for _, behaviour := range derived(t).Behaviours {
		if behaviour.Path != "source.port" {
			continue
		}
		got := strings.Join(behaviour.Imports, " ")
		for _, want := range []string{
			"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator",
			"regexp",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("source.port's validator should import %s; it lists %q", want, got)
			}
		}
		return
	}
	t.Error("source.port carried no validator, so its imports were never checked")
}
