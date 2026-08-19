package unifi

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/sdkshape"
)

const goUnifiPackage = "github.com/ubiquiti-community/go-unifi/unifi"

// loadSDK resolves the go-unifi package once. It is the slowest thing here by
// an order of magnitude and both tests need it.
var loadSDK = sync.OnceValues(func() (*sdkshape.Package, error) {
	return sdkshape.Load(goUnifiPackage)
})

// The descriptors and the mapping artifacts must agree on which fields exist
// and what kind each one is.
//
// THIS IS A CONFORMANCE CHECK, NOT A GENERATOR. cmd/provider-spec-compiler will
// emit descriptors one day and this does not compete with it; a second producer
// of the same artifact is the defect recorded in task 164. What this answers is
// the question that has to be settled before a generator can be written
// honestly: which parts of a descriptor are derivable from artifacts that
// already exist, and which are decisions no artifact carries.
//
// It exists because the descriptors are hand-transcribed and the near-misses so
// far have all been transcription rather than logic -- a wire name, an elide
// value, a count that moved between readings. A generator removes the class; a
// check catches the instance, and only one of those can be had today.
//
// EVERY COLUMN IS RESOLVED, NONE IS INFERRED, AND THAT REPLACED A WORSE
// INSTRUMENT. The first version scored the Go identifiers against a naming
// convention with an initialism table and reported 17 of 18 with one recorded
// exception. The table was written after reading the descriptors it scored, so
// it partly measured a rule fitted to its own data -- and the "exception" was
// not one. dns_record's ttl is TTL on the model and Ttl on the SDK because those
// are the identifiers those two structs declare. Model names now come from the
// descriptor's own tfsdk tags and SDK names from internal/sdkshape, which
// resolves real types rather than matching text.
//
// IT READS THE SOURCE, NOT THE RUNNING SPEC, AND THAT IS A REAL LIMIT. The
// field kind and the SDK identifier are not on resourcekit's Field interface --
// only WireName() is -- so the columns measured here can only be had from the
// syntax. A descriptor whose Fields slice were appended to outside the literal
// would be invisible to this reader.
//
// The per-surface tests close that half: TestClientQosRateDescriptorCoversEveryManagedField
// walks clientQosRateKitSpec().Fields at runtime. It hardcodes its expectation
// where this derives one from the mapping, so neither subsumes the other -- the
// hardcoded one would keep passing if the mapping gained a managed field, and
// this one would keep passing if the compiled spec diverged from its own source.
// Do not delete either on the grounds that the other exists.
//
// THE ELIDE VALUES ARE DELIBERATELY NOT CHECKED HERE.
// TestEveryDescriptorElideAgreesWithItsSchema already compares them against the
// generated schema, which is the artifact that decides them. Re-checking them
// against the mapping would be a second opinion from a document that does not
// carry the fact.

// descriptorField is one entry of a Spec's Fields slice, read off the source.
type descriptorField struct {
	Kind    string // StringField, Int64PtrField, DurationField, ...
	Wire    string
	Model   string // the model struct field the closure returns
	SDK     string // the SDK struct field the closure returns
	Wrapper string // ReadOnly, or empty
}

// descriptor is one parsed *_descriptor.go.
type descriptor struct {
	TypeName string
	// AlwaysWire is the Spec's list of wires sent on every request. A managed
	// field can round-trip through it instead of through a Fields entry, which
	// is how a write-only secret and a translated set are carried.
	AlwaysWire map[string]bool
	SDKType    string            // the Spec's second type argument, e.g. ClientGroup
	ModelTags  map[string]string // model Go field -> tfsdk tag
	Fields     []descriptorField
}

// mappingField is one entry of a *.mapping.json.
type mappingField struct {
	StructuralName string `json:"structural_name"`
	TerraformName  string `json:"terraform_name"`
	StructuralType string `json:"structural_type"`
	TerraformType  string `json:"terraform_type"`
	Disposition    string `json:"disposition"`
}

// TestEveryDescriptorAgreesWithItsSources is the assertion half.
//
// Two directions, for the reason descriptor_policy_test.go gives for its own
// pair: a descriptor naming a wire the mapping does not have is a typo that
// compiles, and a mapping field with no descriptor entry is an attribute that
// silently stops round-tripping. Neither direction can see the other's failure.
func TestEveryDescriptorAgreesWithItsSources(t *testing.T) {
	descriptors := loadDescriptors(t)
	if len(descriptors) == 0 {
		t.Fatal("no descriptors were parsed, so every verdict below would be vacuous")
	}

	sdk, err := loadSDK()
	if err != nil {
		t.Fatalf("resolving %s: %v", goUnifiPackage, err)
	}

	for _, name := range sortedDescriptorNames(descriptors) {
		desc := descriptors[name]
		t.Run(name, func(t *testing.T) {
			mapping := loadMapping(t, name)
			sdkMembers, ok := sdk.Members(desc.SDKType)
			if !ok {
				t.Fatalf("the SDK has no struct %s, which this descriptor declares as its type "+
					"argument; every SDK comparison below would be vacuous", desc.SDKType)
			}

			// _id and site are the Spec's own ID and Site closures rather than
			// Fields entries, so they are not expected in the slice.
			expected := map[string]mappingField{}
			for _, f := range mapping {
				if f.Disposition != "managed" {
					continue
				}
				if f.TerraformName == "id" || f.TerraformName == "site" {
					continue
				}
				expected[f.StructuralName] = f
			}

			got := map[string]descriptorField{}
			for _, f := range desc.Fields {
				got[f.Wire] = f
			}

			for wire := range got {
				if _, ok := expected[wire]; !ok {
					t.Errorf("the descriptor carries wire %q, which is not a managed field of "+
						"%s.mapping.json; a wire name that matches nothing is sent to the "+
						"controller and silently ignored", wire, name)
				}
			}
			// A MANAGED FIELD CAN BE CARRIED BY EITHER MECHANISM, and requiring
			// a Fields entry was wrong. site_to_site_vpn's x_ipsec_pre_shared_key
			// is a write-only secret set by a BeforeSend hook and deliberately
			// never read back; port_profile's excluded_networkconf_ids is derived
			// from tagged_networkconf_ids because the provider translates one to
			// the other. Both are declared in AlwaysWire, both say so in their own
			// comments, and both would have been reported here as attributes that
			// do not round-trip.
			//
			// What is still caught is a field carried by neither, which is the
			// case that actually stops round-tripping.
			for wire, want := range expected {
				if _, ok := got[wire]; ok {
					continue
				}
				if desc.AlwaysWire[wire] {
					continue
				}
				t.Errorf("%s.mapping.json declares %q managed (terraform name %q) and no "+
					"descriptor field carries it, nor is it in AlwaysWire, so that attribute "+
					"does not round-trip", name, wire, want.TerraformName)
			}

			for wire, f := range got {
				want, ok := expected[wire]
				if !ok {
					continue
				}
				// The kind rarely fires, and that is worth knowing rather than
				// mistaking for thoroughness: the Model and SDK closures are typed,
				// so most wrong kinds fail to compile. What survives compilation is
				// a kind that is internally consistent and still disagrees with the
				// mapping, which is what this catches.
				if kind := derivableKind(want); kind != "" && !kindAgrees(f.Kind, kind) {
					t.Errorf("%s: the mapping says %s -> %s, which is a %s, and the descriptor "+
						"uses %s", wire, want.StructuralType, want.TerraformType, kind, f.Kind)
				}

				// THE PAIRING, NOT JUST THE SET. Two same-typed fields with their
				// wires swapped satisfies every check above -- both wires exist,
				// both kinds agree -- and is exactly the failure that let a
				// mutation to src_mac_address keep fifty-one packages green.
				// Requiring the SDK identifier to be the conventional rendering of
				// the wire name ties each entry to its own field.
				member, ok := sdkMembers[wire]
				if !ok {
					t.Errorf("%s: the SDK struct %s has no JSON member of this name, so this "+
						"entry names a field the controller never reads", wire, desc.SDKType)
					continue
				}
				if f.SDK != member.GoName {
					t.Errorf("%s: the entry names SDK field %s, and %s's member %q is carried "+
						"by %s. Either this entry is paired with another field, or the SDK "+
						"renamed it", wire, f.SDK, desc.SDKType, wire, member.GoName)
				}
				if member.Pointer != kindCarriesAPointer(f.Kind) {
					t.Errorf("%s: the SDK field is pointer=%v and the descriptor uses %s; a "+
						"pointer distinguishes absent from zero and the two must agree",
						wire, member.Pointer, f.Kind)
				}
				if tag, ok := desc.ModelTags[f.Model]; !ok {
					t.Errorf("%s: the entry names model field %s, which the model struct does "+
						"not declare", wire, f.Model)
				} else if tag != want.TerraformName {
					t.Errorf("%s: the entry's model field %s is tfsdk:%q and the mapping's "+
						"terraform name is %q", wire, f.Model, tag, want.TerraformName)
				}
			}
		})
	}
}

// derivableKind is the field kind the mapping alone implies, or "" where the
// mapping cannot say.
//
// It returns the NON-POINTER kind always. Pointer-ness is not in the mapping --
// it is a property of the SDK struct -- so the comparison below treats Int64Field
// and Int64PtrField as agreeing. That is the gap being measured, not a hole in
// the check: see the derivability report for how many fields it covers.
func derivableKind(f mappingField) string {
	switch {
	case f.StructuralType == "int64" && f.TerraformType == "string":
		return "DurationField"
	case f.StructuralType == "array<string>" && f.TerraformType == "set":
		return "StringSetField"
	case f.StructuralType == "array<string>" && f.TerraformType == "list":
		return "StringListField"
	case f.StructuralType == "string" && f.TerraformType == "string":
		return "StringField"
	case f.StructuralType == "bool" && f.TerraformType == "bool":
		return "BoolField"
	case f.StructuralType == "int64" && f.TerraformType == "int64":
		return "Int64Field"
	}
	return ""
}

// kindAgrees compares the kinds the mapping can actually distinguish.
//
// TWO DISTINCTIONS ARE DROPPED, AND BOTH ARE THE MAPPING'S LIMIT RATHER THAN A
// SOFTENED CHECK. Pointer-ness is not in the mapping at all, and is compared
// against the resolved SDK field a few lines above instead, so nothing is lost
// by ignoring it here. StringLikeField maps a string-backed custom type to a
// plain SDK string, which the mapping records as string -> string exactly as it
// records a plain types.String; the artifact carries no way to tell them apart,
// so asserting on it would be asserting on a fact no input supplies.
//
// Nine descriptors use nine kinds, three of which the first four had no instance
// of, so this strips decorations rather than enumerating the pairs it has seen.
func kindAgrees(got, want string) bool {
	base := strings.Replace(strings.Replace(got, "Like", "", 1), "Ptr", "", 1)
	return base == want
}

// TestDescriptorDerivabilityIsReported is the measurement half, and it asserts
// nothing about the numbers on purpose.
//
// The question it answers -- how much of a descriptor a generator could emit --
// is not a property the tree should be held to. Asserting a percentage would
// make adding a descriptor with an unusual name fail a check about generators,
// which is a gate whose remedy has nothing to do with the change in front of
// whoever trips it. The numbers are logged so a person deciding whether to write
// the generator can read them.
func TestDescriptorDerivabilityIsReported(t *testing.T) {
	descriptors := loadDescriptors(t)
	if len(descriptors) == 0 {
		t.Fatal("no descriptors were parsed, so the report below would describe nothing")
	}

	sdk, err := loadSDK()
	if err != nil {
		t.Fatalf("resolving %s: %v", goUnifiPackage, err)
	}

	var total, wireOK, kindOK, pointerNeedsSDK, identNeedsSDK int
	var needsSDK []string

	for _, name := range sortedDescriptorNames(descriptors) {
		desc := descriptors[name]
		byWire := map[string]mappingField{}
		for _, f := range loadMapping(t, name) {
			byWire[f.StructuralName] = f
		}
		members, _ := sdk.Members(desc.SDKType)
		for _, f := range desc.Fields {
			total++
			want, ok := byWire[f.Wire]
			if !ok {
				continue
			}
			wireOK++
			if k := derivableKind(want); k != "" && kindAgrees(f.Kind, k) {
				kindOK++
			}
			member, ok := members[f.Wire]
			if !ok {
				continue
			}
			if member.Pointer {
				pointerNeedsSDK++
				needsSDK = append(needsSDK, fmt.Sprintf(
					"%s.%s is *T in the SDK and %s in the mapping", name, f.Wire, want.StructuralType))
			}
			// The Go identifier is a fact about the struct, not a rendering of
			// the wire name. Count where the two differ by more than case and
			// underscores, because that is what no generator can infer.
			// Strip both separators. The wire names use underscores and, for
			// static_route, hyphens -- "static-route_distance" is StaticRouteDistance
			// in Go, which is a case-fold once both are gone. Counting it as a
			// divergence overstated this by five of seventeen on the first run.
			flattened := strings.NewReplacer("_", "", "-", "").Replace(f.Wire)
			if !strings.EqualFold(flattened, member.GoName) {
				identNeedsSDK++
				needsSDK = append(needsSDK, fmt.Sprintf(
					"%s.%s is %s in the SDK", name, f.Wire, member.GoName))
			}
		}
	}

	sort.Strings(needsSDK)
	t.Logf("%d descriptor(s), %d field(s)", len(descriptors), total)
	t.Logf("  wire name from structural_name       %d/%d", wireOK, total)
	t.Logf("  field kind from the type pair        %d/%d  (pointer-ness excluded)", kindOK, total)
	t.Logf("  pointer-ness the mapping cannot say  %d/%d", pointerNeedsSDK, total)
	t.Logf("  Go identifier not a case-fold of the wire  %d/%d", identNeedsSDK, total)
	if len(needsSDK) > 0 {
		t.Logf("  facts only the SDK struct carries:\n    %s", strings.Join(needsSDK, "\n    "))
	}
}

func sortedDescriptorNames(m map[string]descriptor) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func loadMapping(t *testing.T, surface string) []mappingField {
	t.Helper()
	path := filepath.Join("..", "provider-codegen", "generated", surface+".mapping.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the mapping for %s: %v", surface, err)
	}
	var doc struct {
		Fields []mappingField `json:"fields"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	if len(doc.Fields) == 0 {
		t.Fatalf("%s carries no fields, so a comparison against it would assert nothing", path)
	}
	return doc.Fields
}

// loadDescriptors parses every *_descriptor.go in this package and returns the
// Fields entries keyed by the Spec's TypeName.
//
// It FAILS on an element shape it does not recognise rather than skipping it.
// Skipping is how the first version of this reported firewall_zone as missing
// two managed fields: they were wrapped in resourcekit.ReadOnly, the extractor
// saw a CallExpr where it wanted a composite literal, and the under-count read
// as a defect in the descriptor rather than in the reader.
func loadDescriptors(t *testing.T) map[string]descriptor {
	t.Helper()
	paths, err := filepath.Glob("*_descriptor.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]descriptor{}
	for _, path := range paths {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		modelTags := modelTagsIn(file)
		helpers := helpersIn(file)
		aliases := aliasesIn(file)
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if exprName(lit.Type) != "Spec" {
				return true
			}
			desc := descriptor{ModelTags: map[string]string{}, AlwaysWire: map[string]bool{}}
			if args, ok := lit.Type.(*ast.IndexListExpr); ok && len(args.Indices) == 2 {
				desc.SDKType = resolveAlias(aliases, exprName(args.Indices[1]))
				desc.ModelTags = modelTags[resolveAlias(aliases, exprName(args.Indices[0]))]
			}
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, _ := kv.Key.(*ast.Ident)
				if key == nil {
					continue
				}
				switch key.Name {
				case "TypeName":
					if bl, ok := kv.Value.(*ast.BasicLit); ok {
						desc.TypeName, _ = strconv.Unquote(bl.Value)
					}
				case "AlwaysWire":
					lit, ok := kv.Value.(*ast.CompositeLit)
					if !ok {
						t.Fatalf("%s: AlwaysWire is %T, not a composite literal", path, kv.Value)
					}
					for _, item := range lit.Elts {
						bl, ok := item.(*ast.BasicLit)
						if !ok {
							t.Fatalf("%s: an AlwaysWire entry is %T, not a string literal; "+
								"skipping it would let a dropped field look accounted for",
								path, item)
						}
						wire, _ := strconv.Unquote(bl.Value)
						desc.AlwaysWire[wire] = true
					}
				case "Fields":
					slice, ok := kv.Value.(*ast.CompositeLit)
					if !ok {
						t.Fatalf("%s: Fields is %T, not a composite literal", path, kv.Value)
					}
					for _, item := range slice.Elts {
						desc.Fields = append(desc.Fields, parseField(t, path, item, helpers))
					}
				}
			}
			if desc.TypeName != "" {
				if desc.SDKType == "" {
					t.Fatalf("%s: could not read the Spec's SDK type argument", path)
				}
				out[desc.TypeName] = desc
			}
			return true
		})
	}
	return out
}

// modelTagsIn maps each struct type in the file to its Go field -> tfsdk tag.
// The model identifiers are read here rather than inferred, which is what
// removes the fitted naming convention this check used to score against.
func modelTagsIn(file *ast.File) map[string]map[string]string {
	out := map[string]map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := spec.Type.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}
		tags := map[string]string{}
		for _, f := range st.Fields.List {
			if f.Tag == nil || len(f.Names) == 0 {
				continue
			}
			raw, err := strconv.Unquote(f.Tag.Value)
			if err != nil {
				continue
			}
			if tag := reflect.StructTag(raw).Get("tfsdk"); tag != "" {
				tags[f.Names[0].Name] = tag
			}
		}
		out[spec.Name.Name] = tags
		return true
	})
	return out
}

// helperSpec is a per-file constructor such as
//
//	str := func(wire string, model func(*M) *types.String,
//		sdk func(*S) *string, elide resourcekit.ElideZero,
//	) resourcekit.StringField[M, S] {
//		return resourcekit.StringField[M, S]{Wire: wire, Model: model, SDK: sdk, Elide: elide}
//	}
//
// THE ARGUMENT POSITIONS ARE READ OFF THE HELPER, NOT ASSUMED. Nine descriptors
// now use seven of these across sixty-seven call sites, with three arities, and
// a reader that hardcoded "wire is argument one" would be guessing -- which is
// the thing this reader refuses to do. Each helper's own body says which
// parameter becomes which field, so the mapping is derived per helper and a
// constructor that does anything more interesting than forward its parameters
// is not recognised, and fails.
type helperSpec struct {
	Kind  string
	Wire  int
	Model int
	SDK   int
}

// helpersIn finds the field constructors declared in a file, whether they are
// package-level functions or closures assigned inside the spec function.
func helpersIn(file *ast.File) map[string]helperSpec {
	out := map[string]helperSpec{}
	consider := func(name string, params *ast.FieldList, body *ast.BlockStmt) {
		if params == nil || body == nil || len(body.List) != 1 {
			return
		}
		ret, ok := body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return
		}
		lit, ok := ret.Results[0].(*ast.CompositeLit)
		if !ok {
			return
		}
		kind := exprName(lit.Type)
		if !strings.HasSuffix(kind, "Field") {
			return
		}
		index := map[string]int{}
		position := 0
		for _, group := range params.List {
			for _, ident := range group.Names {
				index[ident.Name] = position
				position++
			}
		}
		spec := helperSpec{Kind: kind, Wire: -1, Model: -1, SDK: -1}
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, _ := kv.Key.(*ast.Ident)
			value, _ := kv.Value.(*ast.Ident)
			if key == nil || value == nil {
				continue
			}
			at, ok := index[value.Name]
			if !ok {
				continue
			}
			switch key.Name {
			case "Wire":
				spec.Wire = at
			case "Model":
				spec.Model = at
			case "SDK":
				spec.SDK = at
			}
		}
		if spec.Wire >= 0 && spec.Model >= 0 && spec.SDK >= 0 {
			out[name] = spec
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.FuncDecl:
			consider(decl.Name.Name, decl.Type.Params, decl.Body)
		case *ast.AssignStmt:
			if len(decl.Lhs) != 1 || len(decl.Rhs) != 1 {
				return true
			}
			name, ok := decl.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}
			if fn, ok := decl.Rhs[0].(*ast.FuncLit); ok {
				consider(name.Name, fn.Type.Params, fn.Body)
			}
		}
		return true
	})
	return out
}

// aliasesIn resolves `type ppSDK = ui.PortProfile`, which two descriptors use to
// keep their generic instantiations readable.
func aliasesIn(file *ast.File) map[string]string {
	out := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if ok && spec.Assign.IsValid() {
			out[spec.Name.Name] = exprName(spec.Type)
		}
		return true
	})
	return out
}

func resolveAlias(aliases map[string]string, name string) string {
	for range 8 {
		next, ok := aliases[name]
		if !ok {
			return name
		}
		name = next
	}
	return name
}

func parseField(t *testing.T, path string, el ast.Expr, helpers map[string]helperSpec) descriptorField {
	t.Helper()
	wrapper := ""
	for {
		call, ok := el.(*ast.CallExpr)
		if !ok {
			break
		}
		name := exprName(call.Fun)
		if spec, ok := helpers[name]; ok {
			return fieldFromHelper(t, path, name, spec, call, wrapper)
		}
		if len(call.Args) != 1 {
			t.Fatalf("%s: %s(...) takes %d arguments and is not a recognised field "+
				"constructor -- its body must return a resourcekit field literal whose Wire, "+
				"Model and SDK are its own parameters. Guessing the positions would "+
				"mis-attribute every field it builds", path, name, len(call.Args))
		}
		wrapper = name
		el = call.Args[0]
	}
	lit, ok := el.(*ast.CompositeLit)
	if !ok {
		t.Fatalf("%s: a Fields entry is %T, which this reader does not understand; it must fail "+
			"rather than skip, because a skipped entry reads as a missing field", path, el)
	}
	field := descriptorField{Kind: exprName(lit.Type), Wrapper: wrapper}
	for _, item := range lit.Elts {
		kv, ok := item.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, _ := kv.Key.(*ast.Ident)
		if key == nil {
			continue
		}
		switch key.Name {
		case "Wire":
			bl, ok := kv.Value.(*ast.BasicLit)
			if !ok {
				t.Fatalf("%s: a Wire value is %T, not a string literal", path, kv.Value)
			}
			field.Wire, _ = strconv.Unquote(bl.Value)
		case "Model":
			field.Model = returnedSelector(kv.Value)
		case "SDK":
			field.SDK = returnedSelector(kv.Value)
		}
	}
	if field.Wire == "" {
		t.Fatalf("%s: a %s entry has no Wire", path, field.Kind)
	}
	return field
}

// fieldFromHelper reads a call to a per-file constructor using the argument
// positions derived from that constructor's own signature.
func fieldFromHelper(
	t *testing.T, path, name string, spec helperSpec, call *ast.CallExpr, wrapper string,
) descriptorField {
	t.Helper()
	if spec.Wire >= len(call.Args) || spec.Model >= len(call.Args) || spec.SDK >= len(call.Args) {
		t.Fatalf("%s: %s takes arguments this call does not supply", path, name)
	}
	field := descriptorField{Kind: spec.Kind, Wrapper: wrapper}
	bl, ok := call.Args[spec.Wire].(*ast.BasicLit)
	if !ok {
		t.Fatalf("%s: %s's wire argument is %T, not a string literal", path, name, call.Args[spec.Wire])
	}
	field.Wire, _ = strconv.Unquote(bl.Value)
	field.Model = returnedSelector(call.Args[spec.Model])
	field.SDK = returnedSelector(call.Args[spec.SDK])
	return field
}

// returnedSelector reads the struct field a `func(m *M) *T { return &m.X }`
// closure names.
func returnedSelector(e ast.Expr) string {
	fn, ok := e.(*ast.FuncLit)
	if !ok || fn.Body == nil {
		return ""
	}
	var out string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && out == "" {
			out = sel.Sel.Name
		}
		return out == ""
	})
	return out
}

func exprName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.IndexListExpr:
		return exprName(t.X)
	case *ast.IndexExpr:
		return exprName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return exprName(t.X)
	}
	return ""
}

// kindCarriesAPointer reports whether a field kind's SDK accessor addresses a
// pointer, which is what decides whether it can express "absent" as distinct
// from "the zero value".
//
// IT WAS A SUBSTRING TEST ON THE KIND'S NAME, and that held only while every
// pointer-carrying kind happened to be spelled with Ptr in it. ObjectField's
// accessor is func(*S) **E -- it is pointer-carrying by construction, and it
// nils the SDK pointer when the model object is null, which is precisely the
// absent-versus-zero distinction the rule is about. Its name says nothing about
// that, so the substring test rejected the first ObjectField over a pointer
// member and would have been read as the descriptor being wrong.
//
// ObjectListField is deliberately absent: its accessor is func(*S) *[]E over a
// slice member, so the JSON member is not a pointer and the two already agree.
func kindCarriesAPointer(kind string) bool {
	if kind == "ObjectField" {
		return true
	}
	return strings.Contains(kind, "Ptr")
}
