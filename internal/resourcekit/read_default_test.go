package resourcekit

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ReadDefault came from static_route's gateway_type, which the hand-written
// mapper reported as "default" whenever the controller returned an empty
// string. Twelve attributes across five surfaces do the same thing, so it is a
// capability rather than one surface's quirk.
//
// The tests below fix the two things that can silently go wrong: the
// substitution not happening on the path nobody looks at, and the check
// accepting a schema that cannot legally carry a provider-supplied value.

type defaultModel struct {
	Gateway types.String `tfsdk:"gateway_type"`
}

type defaultSDK struct {
	Gateway string
}

func defaultField(readDefault string) StringField[defaultModel, defaultSDK] {
	return StringField[defaultModel, defaultSDK]{
		Wire:        "gateway_type",
		Model:       func(m *defaultModel) *types.String { return &m.Gateway },
		SDK:         func(s *defaultSDK) *string { return &s.Gateway },
		Elide:       KeepZero,
		ReadDefault: readDefault,
	}
}

func TestReadDefaultSubstitutesOnlyForAnEmptyRead(t *testing.T) {
	for _, testCase := range []struct {
		name string
		sdk  string
		want string
	}{
		{"empty read takes the default", "", "default"},
		{"a real value is untouched", "wan2", "wan2"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var model defaultModel
			sdk := defaultSDK{Gateway: testCase.sdk}
			if diags := defaultField("default").ToModel(context.Background(), &sdk, &model); diags.HasError() {
				t.Fatalf("ToModel: %v", diags)
			}
			if got := model.Gateway.ValueString(); got != testCase.want {
				t.Fatalf("gateway_type = %q, want %q", got, testCase.want)
			}
		})
	}
}

// THE CONTROL. Without a default declared, an empty read must still behave
// exactly as it did before -- otherwise the case above would pass for a field
// kind that had simply started inventing values everywhere.
func TestAFieldWithNoReadDefaultIsUnchanged(t *testing.T) {
	var model defaultModel
	sdk := defaultSDK{}
	if diags := defaultField("").ToModel(context.Background(), &sdk, &model); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if got := model.Gateway.ValueString(); got != "" {
		t.Fatalf("a field declaring no default substituted %q anyway", got)
	}
}

// THE DEFAULT MUST NOT REACH THE WIRE. A value the provider invented on read is
// not something the practitioner asked for, so an update whose plan does not
// mention the attribute must not name it in the field mask. This is the
// write-amplification guard applied to a value that has no author.
func TestReadDefaultDoesNotMakeAFieldLookPlanned(t *testing.T) {
	field := defaultField("default")
	var absent defaultModel
	absent.Gateway = types.StringNull()
	if field.SetInPlan(&absent) {
		t.Fatal("a null plan value reported as set, so the default would be sent on update")
	}
}

func defaultSchema(optional, computed bool) schema.Schema {
	return schema.Schema{Attributes: map[string]schema.Attribute{
		"gateway_type": schema.StringAttribute{Optional: optional, Computed: computed},
	}}
}

func defaultSpec(readDefault string) Spec[defaultModel, defaultSDK] {
	return Spec[defaultModel, defaultSDK]{
		TypeName: "probe_default",
		Fields:   []Field[defaultModel, defaultSDK]{defaultField(readDefault)},
	}
}

func TestReadDefaultIsAcceptedOnOptionalComputed(t *testing.T) {
	problems := ElideProblems(defaultSpec("default"), defaultSchema(true, true))
	if len(problems) != 0 {
		t.Fatalf("a legal default was reported: %v", problems)
	}
}

// The must-fail half. Each case is Optional+Computed with exactly one flag
// removed, and the message must name the attribute rather than say "a problem".
func TestReadDefaultIsRefusedWhereTheValueCannotLegallyBeSupplied(t *testing.T) {
	for _, testCase := range []struct {
		name               string
		optional, computed bool
		wantIn             string
	}{
		{"not computed", true, false, "Optional-only"},
		{"not optional", false, true, "Computed-only"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			problems := ElideProblems(defaultSpec("default"),
				defaultSchema(testCase.optional, testCase.computed))
			if len(problems) != 1 {
				t.Fatalf("want exactly 1 problem, got %d: %v", len(problems), problems)
			}
			if !strings.Contains(problems[0], "gateway_type") ||
				!strings.Contains(problems[0], testCase.wantIn) {
				t.Fatalf("problem %q does not name the attribute and its flags (%s)",
					problems[0], testCase.wantIn)
			}
		})
	}
}

// A field with a default no longer has a reachable elision, so the Elide rule
// must not also fire on it -- two problems for one field would make the message
// contradict itself, telling the author to set a value that does nothing.
func TestADefaultedFieldIsNotAlsoJudgedOnItsElide(t *testing.T) {
	// Optional+Computed wants KeepZero under the elide rule; this spec declares
	// NullZero, which WOULD be reported were the field not defaulted.
	spec := defaultSpec("default")
	field := defaultField("default")
	field.Elide = NullZero
	spec.Fields = []Field[defaultModel, defaultSDK]{field}
	if problems := ElideProblems(spec, defaultSchema(true, true)); len(problems) != 0 {
		t.Fatalf("a defaulted field was judged on its unreachable Elide: %v", problems)
	}

	// ...and the same spec WITHOUT the default is reported, which is what
	// proves the line above tested the exemption rather than a rule that never
	// fires on this shape.
	field.ReadDefault = ""
	spec.Fields = []Field[defaultModel, defaultSDK]{field}
	if problems := ElideProblems(spec, defaultSchema(true, true)); len(problems) != 1 {
		t.Fatalf("the elide rule does not fire on this shape at all, so the exemption above "+
			"proved nothing: %v", problems)
	}
}
