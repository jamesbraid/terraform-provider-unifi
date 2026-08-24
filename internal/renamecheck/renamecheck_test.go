package renamecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture reproduces every conversion shape the estate actually uses, including
// the two that must NOT resolve.
const fixture = `package thing

import unifi "github.com/ubiquiti-community/go-unifi/unifi"

type Str struct{ v string }

func (s Str) ValueString() string     { return s.v }
func (s Str) ValueStringPointer() *string { return &s.v }
func (s Str) IsNull() bool            { return false }

type model struct {
	ID       Str ` + "`tfsdk:\"id\"`" + `
	Kind     Str ` + "`tfsdk:\"kind\"`" + `
	Nexthop  Str ` + "`tfsdk:\"next_hop\"`" + `
	Named    Str ` + "`tfsdk:\"name\"`" + `
	Joined   Str ` + "`tfsdk:\"joined\"`" + `
	Other    Str ` + "`tfsdk:\"other\"`" + `
}

func build(m *model) *unifi.Routing {
	// Composite literal: the common shape.
	r := &unifi.Routing{
		Name:            m.Named.ValueString(),
		StaticRouteType: m.Kind.ValueString(),
	}
	// Plain assignment, model -> SDK.
	r.StaticRouteNexthop = m.Nexthop.ValueString()
	// Two model fields in one expression: must NOT resolve, because taking the
	// first would invent a pairing.
	r.GatewayDevice = m.Joined.ValueString() + m.Other.ValueString()
	return r
}

func read(r *unifi.Routing, m *model) {
	// Reverse direction, SDK -> model.
	m.ID = Str{v: r.ID}
}
`

// goDirective reads the repository's own go directive so the fixture module
// cannot drift out of range of it. Hard-coding a version here made both tests
// skip silently the first time the repository moved past it.
func goDirective(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if version, ok := strings.CutPrefix(strings.TrimSpace(line), "go "); ok {
			return strings.TrimSpace(version)
		}
	}
	t.Fatal("go.mod has no go directive")
	return ""
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

// derived loads the fixture as a real package so the type information the walker
// depends on is real rather than mocked. It reuses the repository's own module
// so go-unifi resolves from the module cache.
func derived(t *testing.T) map[string]map[string]bool {
	t.Helper()
	pairs := map[string]map[string]bool{}
	for _, b := range deriveFixture(t).Bindings {
		if pairs[b.TerraformName] == nil {
			pairs[b.TerraformName] = map[string]bool{}
		}
		pairs[b.TerraformName][b.StructuralName] = true
	}
	return pairs
}

func deriveFixture(t *testing.T) Result {
	t.Helper()
	dir := t.TempDir()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	write(t, dir, "go.mod", "module fixture\n\ngo "+goDirective(t)+"\n\nrequire github.com/ubiquiti-community/go-unifi v1.103.0\n")
	write(t, dir, "go.work", "go "+goDirective(t)+"\n\nuse (\n\t.\n\t"+root+"\n)\n")
	write(t, dir, "thing.go", fixture)

	result, err := Derive(dir)
	if err != nil {
		// Fatal rather than Skip. A skip here is a test that cannot fail, which
		// is the same shape as the defects this package exists to catch: it
		// would go green whether the walker worked or not.
		t.Fatalf("fixture package would not load: %v", err)
	}
	return result
}

// Test_readsEveryConversionShape checks the three shapes that carry real
// pairings, named individually so a failure says which one broke.
func Test_readsEveryConversionShape(t *testing.T) {
	pairs := derived(t)

	for _, want := range []struct{ terraform, structural, why string }{
		{"name", "name", "composite literal, the shape most resources build their request with"},
		{"kind", "static-route_type", "composite literal carrying a RENAME -- the case the tool exists for"},
		{"next_hop", "static-route_nexthop", "plain assignment, model to SDK"},
		{"id", "_id", "reverse direction, SDK to model, which is the only place some fields appear"},
	} {
		if !pairs[want.terraform][want.structural] {
			t.Errorf("%s -> %s was not read (%s); got %v",
				want.terraform, want.structural, want.why, pairs[want.terraform])
		}
	}
}

// Test_refusesAnAmbiguousExpression is the other half, and the more important
// one. An expression mentioning two model fields has no single pairing, and
// resolving it to the first would manufacture exactly the wrong bind this
// package exists to catch.
func Test_refusesAnAmbiguousExpression(t *testing.T) {
	pairs := derived(t)

	if got, claimed := pairs["joined"]; claimed {
		t.Errorf("an expression mentioning two model fields resolved to %v; "+
			"a guessed pairing is worse than none, because it licenses the bind "+
			"the referee exists to refuse", got)
	}
	if got, claimed := pairs["other"]; claimed {
		t.Errorf("the second field of an ambiguous expression resolved to %v", got)
	}
}

// Test_namesWhatItCouldNotRead is the guard on the guard: Unread must
// actually get populated, not merely declared and sorted. The ambiguous
// expression must be named, not merely absent from the bindings -- absent
// and skipped are the same observation otherwise, and only one is a report.
func Test_namesWhatItCouldNotRead(t *testing.T) {
	result := deriveFixture(t)

	if len(result.Unread) == 0 {
		t.Fatal("nothing was reported as unread, though the fixture contains an expression " +
			"naming two model fields; a report that is always empty is not a report")
	}

	var ambiguous string
	for _, u := range result.Unread {
		if strings.Contains(u.Detail, "gateway_device") {
			ambiguous = u.Detail
		}
	}
	if ambiguous == "" {
		t.Fatalf("the ambiguous assignment to gateway_device was skipped without being named; got %v",
			result.Unread)
	}
	// Both candidates, so a reader can see what the choice was between rather
	// than only that a choice existed.
	for _, want := range []string{"joined", "other", "would be a guess"} {
		if !strings.Contains(ambiguous, want) {
			t.Errorf("the report of an ambiguous conversion does not mention %q: %s", want, ambiguous)
		}
	}
	for _, u := range result.Unread {
		if u.File == "" {
			t.Errorf("an unread entry names no file: %s", u.Detail)
		}
	}
}
