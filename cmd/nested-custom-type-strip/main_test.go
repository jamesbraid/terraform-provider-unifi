package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// fixture writes a one-file package carrying both kinds of CustomType.
func fixture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	source := `package fixture

import (
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type DhcpRelayType struct{ types.ObjectType }

var s = schema.Schema{Attributes: map[string]schema.Attribute{
` + body + `}}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestStripsGeneratedNestedTypeAndKeepsImportedScalar(t *testing.T) {
	dir := fixture(t, `	"dhcp_relay": schema.SingleNestedAttribute{
		CustomType: DhcpRelayType{
			ObjectType: types.ObjectType{},
		},
		Optional: true,
	},
	"ttl": schema.StringAttribute{
		CustomType: timetypes.GoDurationType{},
		Optional:   true,
	},
`)
	stripped, kept, err := stripDir(dir, false, io.Discard)
	if err != nil {
		t.Fatalf("stripDir() error = %v", err)
	}
	if stripped != 1 || kept != 1 {
		t.Fatalf("stripped %d kept %d, want 1 and 1", stripped, kept)
	}
	out, err := os.ReadFile(filepath.Join(dir, "fixture.go"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if strings.Contains(got, "DhcpRelayType{") {
		t.Error("the generated nested-object binding survived")
	}
	if !strings.Contains(got, "timetypes.GoDurationType{}") {
		t.Fatal("the imported scalar custom type was removed; that is the validation")
	}
	// The attribute itself, and everything around the removed field, must be
	// untouched -- this is surgery on one field, not a reformat.
	for _, keep := range []string{`"dhcp_relay": schema.SingleNestedAttribute{`, "Optional: true,", `"ttl": schema.StringAttribute{`} {
		if !strings.Contains(got, keep) {
			t.Errorf("%q did not survive the cut", keep)
		}
	}
}

// TestRefusesWhatItCannotClassify is the whole safety argument. A
// post-processor that skips a shape it does not recognise leaves the binding in
// place, the apply still fails, and the run looks clean -- a check that cannot
// fail.
func TestRefusesWhatItCannotClassify(t *testing.T) {
	for name, test := range map[string]struct {
		body string
		want string
	}{
		"unqualified type this package does not declare": {
			body: `	"a": schema.SingleNestedAttribute{
		CustomType: SomethingUndeclaredType{},
	},
`,
			want: "does not declare it",
		},
		"CustomType that is not a composite literal": {
			body: `	"a": schema.SingleNestedAttribute{
		CustomType: someFactory(),
	},
`,
			want: "not a composite literal",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := stripDir(fixture(t, test.body), false, io.Discard)
			if err == nil {
				t.Fatal("stripDir() accepted a shape it cannot classify")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want it to mention %q", err, test.want)
			}
			// A refusal must name where to look.
			if !regexp.MustCompile(`fixture\.go:\d+:\d+`).MatchString(err.Error()) {
				t.Fatalf("error = %v, want it to name the file and position", err)
			}
		})
	}
}

// TestGeneratedTreeKeepsEveryImportedScalarCustomType is the number rather than
// the vibe. These are the custom types that ARE the validation -- MAC
// addresses, durations, IP addresses and prefixes -- and dropping one is
// invisible to every schema comparison we own, because the protocol renders
// both as a plain string.
//
// If a surface is added or regenerated and this count moves, that is either new
// validation to record or lost validation to explain. Either way it should be
// noticed here rather than in a controller run.
func TestGeneratedTreeKeepsEveryImportedScalarCustomType(t *testing.T) {
	const root = "../../internal/generated"
	const want = 35

	qualified := regexp.MustCompile(`CustomType:\s+[a-z][A-Za-z0-9_]*\.[A-Za-z0-9]+\{`)
	unqualified := regexp.MustCompile(`CustomType:\s+[A-Z][A-Za-z0-9]*Type\{`)
	got, leftover := 0, make([]string, 0)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got += len(qualified.FindAll(source, -1))
		if n := len(unqualified.FindAll(source, -1)); n > 0 {
			leftover = append(leftover, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("the generated tree carries %d imported scalar custom types, expected %d", got, want)
	}
	if len(leftover) != 0 {
		t.Errorf("generated nested-object CustomType bindings survive in %v; run nested-custom-type-strip", leftover)
	}
}
