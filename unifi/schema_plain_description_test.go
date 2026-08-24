package unifi

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// Test_plainDescriptions covers the shapes the four plain-description surfaces
// reach that ap_group alone does not: a nested attribute, a block, and a block
// nested inside a block. ap_group is flat, so migrating device and port_profile
// would otherwise be the first time any of this ran.
func Test_plainDescriptions(t *testing.T) {
	built := schema.Schema{
		MarkdownDescription: "the surface",
		Attributes: map[string]schema.Attribute{
			"flat": schema.StringAttribute{
				Description:         "a flat attribute",
				MarkdownDescription: "a flat attribute",
			},
			"nested": schema.SingleNestedAttribute{
				Description:         "a nested attribute",
				MarkdownDescription: "a nested attribute",
				Attributes: map[string]schema.Attribute{
					"inner": schema.BoolAttribute{
						Description:         "inside a nested attribute",
						MarkdownDescription: "inside a nested attribute",
					},
				},
			},
			"listnested": schema.ListNestedAttribute{
				Description:         "a list nested attribute",
				MarkdownDescription: "a list nested attribute",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"inner": schema.Int64Attribute{
							Description:         "inside a list nested attribute",
							MarkdownDescription: "inside a list nested attribute",
						},
					},
				},
			},
		},
		Blocks: map[string]schema.Block{
			"block": schema.ListNestedBlock{
				Description:         "a block",
				MarkdownDescription: "a block",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"inner": schema.StringAttribute{
							Description:         "inside a block",
							MarkdownDescription: "inside a block",
						},
					},
					Blocks: map[string]schema.Block{
						"deeper": schema.ListNestedBlock{
							Description:         "a block inside a block",
							MarkdownDescription: "a block inside a block",
							NestedObject: schema.NestedBlockObject{
								Attributes: map[string]schema.Attribute{
									"deepest": schema.StringAttribute{
										Description:         "two blocks down",
										MarkdownDescription: "two blocks down",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	plainDescriptions(&built)

	// The schema's own description moves rather than being dropped: the
	// generator writes it to MarkdownDescription only.
	if got := built.GetDescription(); got != "the surface" {
		t.Errorf("schema description = %q, want %q", got, "the surface")
	}
	if got := built.GetMarkdownDescription(); got != "" {
		t.Errorf("schema markdown description = %q, want empty", got)
	}

	// Navigated explicitly, not generically: GetNestedObject().GetAttributes()
	// returns an unexported map type that can't be named outside the module.
	block := mustBe[schema.ListNestedBlock](t, built.Blocks["block"], `Blocks["block"]`)
	deeper := mustBe[schema.ListNestedBlock](t, block.NestedObject.Blocks["deeper"],
		`Blocks["block"].NestedObject.Blocks["deeper"]`)

	for _, member := range []struct {
		path string
		// described is the interface both attributes and blocks satisfy for
		// the two fields under test.
		described interface {
			GetDescription() string
			GetMarkdownDescription() string
		}
		want string
	}{
		{"flat", built.Attributes["flat"], "a flat attribute"},
		{"nested", built.Attributes["nested"], "a nested attribute"},
		{
			"nested.inner",
			mustBe[schema.SingleNestedAttribute](t, built.Attributes["nested"],
				`Attributes["nested"]`).Attributes["inner"],
			"inside a nested attribute",
		},
		{"listnested", built.Attributes["listnested"], "a list nested attribute"},
		{
			"listnested.inner",
			mustBe[schema.ListNestedAttribute](t, built.Attributes["listnested"],
				`Attributes["listnested"]`).NestedObject.Attributes["inner"],
			"inside a list nested attribute",
		},
		{"block", block, "a block"},
		{"block.inner", block.NestedObject.Attributes["inner"], "inside a block"},
		{"block.deeper", deeper, "a block inside a block"},
		{"block.deeper.deepest", deeper.NestedObject.Attributes["deepest"], "two blocks down"},
	} {
		if got := member.described.GetMarkdownDescription(); got != "" {
			t.Errorf("%s: markdown description = %q, want empty", member.path, got)
		}
		if got := member.described.GetDescription(); got != member.want {
			t.Errorf("%s: description = %q, want %q", member.path, got, member.want)
		}
	}
}

// mustBe asserts one of the framework's schema types and says what it found:
// an unchecked assertion panics with "interface conversion" and no context
// on which attribute was wrong.
func mustBe[T any](t *testing.T, value any, path string) T {
	t.Helper()
	typed, ok := value.(T)
	if !ok {
		t.Fatalf("%s is %T, want %T", path, value, typed)
	}
	return typed
}
