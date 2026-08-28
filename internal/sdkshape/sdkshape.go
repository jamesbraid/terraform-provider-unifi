// Package sdkshape answers one question about a Go SDK: for a resource's
// struct, which struct types are reachable from it by following fields, and
// what JSON members does each carry.
//
// The compiler needs this to build a catalog. A nested Terraform attribute is
// derivable only when its member list is observable in a struct the resource
// actually reaches; when it is not, the shape was invented by the provider and
// no catalog can supply it. Answering that by matching text over source files
// is unreliable — it cannot see field types, so it matches structs the resource
// never references and misses renamed members. This resolves real types.
package sdkshape

import (
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strings"
)

// Package is a resolved Go package whose struct shapes can be queried.
type Package struct {
	path    string
	structs map[string]map[string]bool // struct name -> JSON member names
	// members carries what structs cannot: for each JSON member, the Go
	// identifier it is spelled with and whether the field is a pointer. Both
	// are read off the resolved field in the same walk, and both used to be
	// discarded there.
	//
	// They are the two columns a descriptor cannot derive from the mapping
	// artifact. dns_record's ttl is TTL on the model and Ttl on the SDK, so no
	// function of the wire name produces both; port is int64 in the mapping and
	// *int64 in the struct. structs is the name-only projection of this, kept
	// because six call sites want set membership rather than detail.
	members map[string]map[string]Member
	fields  map[string][]string // struct name -> referenced struct names
	// backing maps a struct's JSON member name to the struct type that member
	// carries. It is what names an attribute's origin unambiguously: two
	// sibling shapes can have identical member sets, as firewall policy's
	// source and destination do, and only the field they hang off tells them
	// apart.
	backing map[string]map[string]string
	// operates maps an SDK client method to the struct it reads or writes.
	// A resource's root type is whatever its own Create and Get operate on,
	// which is a fact the SDK states; guessing it from the resource name is
	// wrong for any resource whose name differs from its type.
	operates map[string]string
}

// Member is what a JSON member resolves to in the Go struct that carries it.
type Member struct {
	// GoName is the field's identifier, which is not derivable from the member
	// name: the SDK spells ttl as Ttl where the provider model spells it TTL.
	GoName string
	// Pointer reports whether the field is a pointer, which distinguishes a
	// field that can be absent from one that is merely zero.
	Pointer bool
}

// Load resolves one or more packages from source into a single Package. It
// uses the standard importer rather than x/tools so the compiler gains no
// new dependency for this. extra names further packages to fold in -- the
// SDK is not one flat package: unifi_setting's per-section Specs declare
// their SDK type argument from go-unifi/unifi/settings, a subpackage of
// path, and Members would otherwise never find it.
//
// path always wins a name collision; extra only fills in names path does not
// already have. go-unifi's settings package and its root package both
// declare Dashboard, Setting and FieldConstraint (three legacy types the
// settings redesign renamed around, not shadowed) -- none of them is any
// descriptor's SDK type argument today, and resolving path first keeps every
// descriptor loaded before extra existed pointed at exactly what it always
// resolved to.
func Load(path string, extra ...string) (*Package, error) {
	resolved := &Package{
		path:     path,
		structs:  map[string]map[string]bool{},
		members:  map[string]map[string]Member{},
		fields:   map[string][]string{},
		backing:  map[string]map[string]string{},
		operates: map[string]string{},
	}
	for _, p := range append([]string{path}, extra...) {
		if err := resolved.merge(p); err != nil {
			return nil, err
		}
	}
	return resolved, nil
}

// merge imports one package and folds its exported structs into p. A struct
// name already resolved from an earlier package is left alone -- see Load's
// doc comment for why that, not an error, is the right call here.
func (p *Package) merge(path string) error {
	imported, err := importer.ForCompiler(token.NewFileSet(), "source", nil).Import(path)
	if err != nil {
		return fmt.Errorf("import %s: %w", path, err)
	}
	scope := imported.Scope()
	for _, name := range scope.Names() {
		named, ok := scope.Lookup(name).Type().(*types.Named)
		if !ok {
			continue
		}
		if structure, ok := named.Underlying().(*types.Struct); ok {
			if _, exists := p.structs[name]; !exists {
				p.record(name, structure)
			}
		}
		p.recordMethods(named)
	}
	return nil
}

// recordMethods notes, for each method on a type, the struct it returns. The
// SDK's client carries GetNetwork, CreateBGPConfig and so on, and the returned
// type is the authoritative answer to "what does this resource operate on".
func (p *Package) recordMethods(named *types.Named) {
	for index := range named.NumMethods() {
		method := named.Method(index)
		if !method.Exported() {
			continue
		}
		signature, ok := method.Type().(*types.Signature)
		if !ok {
			continue
		}
		results := signature.Results()
		for result := range results.Len() {
			if carried := namedStruct(results.At(result).Type()); carried != "" {
				p.operates[method.Name()] = carried
				break
			}
		}
	}
}

// RootFor resolves a resource's SDK struct from the client methods it calls.
//
// Method names are tried in the caller's order, so a caller should offer the
// operations it trusts most first. This exists because deriving a root from the
// resource name is guesswork: unifi_bgp operates on BGPConfig, and unifi_wan,
// unifi_vpn_server and unifi_vpn_client all operate on Network, discriminated
// by a purpose field rather than by type.
func (p *Package) RootFor(methods []string) (string, bool) {
	for _, method := range methods {
		if carried, ok := p.operates[method]; ok && p.Has(carried) {
			return carried, true
		}
	}
	return "", false
}

func (p *Package) record(name string, structure *types.Struct) {
	members := map[string]bool{}
	resolved := map[string]Member{}
	references := []string{}
	backing := map[string]string{}
	for index := range structure.NumFields() {
		field := structure.Field(index)
		member := jsonMember(structure.Tag(index))
		if member != "" {
			members[member] = true
			_, pointer := field.Type().(*types.Pointer)
			resolved[member] = Member{GoName: field.Name(), Pointer: pointer}
		}
		referenced := namedStruct(field.Type())
		if referenced == "" {
			continue
		}
		references = append(references, referenced)
		if member != "" {
			backing[member] = referenced
		}
	}
	p.structs[name] = members
	p.members[name] = resolved
	p.fields[name] = references
	p.backing[name] = backing
}

// Members returns each JSON member of a struct with the Go identifier and
// pointer-ness of the field carrying it.
//
// This is the half of "resolve real types" that answers what a member is called
// in Go. Matching text over source cannot supply it -- that is the failure this
// package exists to avoid -- and neither can any naming convention, because the
// SDK and the provider model spell the same field differently.
func (p *Package) Members(root string) (map[string]Member, bool) {
	resolved, ok := p.members[root]
	return resolved, ok
}

// Backing returns the struct carried by root's member of this name, which is
// the attribute's origin when the SDK exposes one.
func (p *Package) Backing(root, member string) (string, bool) {
	carried, ok := p.backing[root][member]
	return carried, ok
}

// Origin resolves where a nested attribute's member list comes from.
//
// It prefers the struct hanging off the SDK field of the same name, because
// that is the actual correspondence and it is unambiguous. Only when the SDK
// exposes no such field does it fall back to searching reachable structs by
// member set, which is a guess and is reported as one.
func (p *Package) Origin(root, attribute string, want []string) (Candidate, bool) {
	if carried, ok := p.Backing(root, attribute); ok {
		return p.assess(carried, want, false), true
	}
	candidate, found := p.Best(root, want)
	candidate.Inferred = true
	return candidate, found
}

func (p *Package) assess(structure string, want []string, inferred bool) Candidate {
	members := p.structs[structure]
	missing := []string{}
	for _, member := range want {
		if !members[member] {
			missing = append(missing, member)
		}
	}
	return Candidate{
		Struct:   structure,
		Missing:  missing,
		Extra:    len(members) - (len(want) - len(missing)),
		Inferred: inferred,
	}
}

// jsonMember reads the wire name from a struct tag, ignoring options and
// fields the SDK excludes.
func jsonMember(tag string) string {
	value := reflect.StructTag(tag).Get("json")
	if value == "" || value == "-" {
		return ""
	}
	if comma := strings.Index(value, ","); comma >= 0 {
		value = value[:comma]
	}
	return value
}

// namedStruct unwraps slices, pointers and maps to find a named struct type,
// which is how a resource reaches its nested shapes.
func namedStruct(t types.Type) string {
	for {
		switch shaped := t.(type) {
		case *types.Slice:
			t = shaped.Elem()
		case *types.Array:
			t = shaped.Elem()
		case *types.Pointer:
			t = shaped.Elem()
		case *types.Map:
			t = shaped.Elem()
		case *types.Named:
			if _, ok := shaped.Underlying().(*types.Struct); ok {
				return shaped.Obj().Name()
			}
			return ""
		default:
			return ""
		}
	}
}

// Reachable returns every struct reachable from root, root included.
func (p *Package) Reachable(root string) map[string]bool {
	seen := map[string]bool{}
	var walk func(string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		if _, known := p.structs[name]; !known {
			return
		}
		seen[name] = true
		for _, next := range p.fields[name] {
			walk(next)
		}
	}
	walk(root)
	return seen
}

// Has reports whether the package defines a struct by this name.
func (p *Package) Has(name string) bool {
	_, ok := p.structs[name]
	return ok
}

// Candidate is a reachable struct considered as the origin of a member list.
type Candidate struct {
	Struct string
	// Missing are wanted members the struct does not carry. Empty means the
	// list is derivable from this struct; a short list usually means members
	// were renamed rather than invented, which policy can express; a list as
	// long as the request means the shape came from somewhere else entirely.
	Missing []string
	// Extra counts members the struct carries that were not wanted. Omitting
	// them is a policy disposition, so it does not disqualify a candidate, but
	// it does rank one candidate below a tighter fit.
	Extra int
	// Inferred marks a candidate found by searching member sets rather than by
	// following the SDK field of the same name. An inferred origin is a guess
	// and should not be treated as a correspondence.
	Inferred bool
}

// Derivable reports whether the member list is fully accounted for.
func (c Candidate) Derivable() bool { return len(c.Missing) == 0 }

// Best returns the reachable struct that best accounts for want. Selection
// is deterministic -- fewest missing members, then fewest extras, then name
// -- since an arbitrary choice among equally good candidates would produce
// attributions that swap between runs.
func (p *Package) Best(root string, want []string) (Candidate, bool) {
	if !p.Has(root) {
		return Candidate{}, false
	}
	names := make([]string, 0, len(p.structs))
	for name := range p.Reachable(root) {
		names = append(names, name)
	}
	sort.Strings(names)

	best := Candidate{Missing: append([]string(nil), want...)}
	found := false
	for _, name := range names {
		// The resource's own struct is never the origin of one of its nested
		// attributes. Allowing it produced false near-matches: a two-member
		// attribute like network's dhcp_guarding {enabled, servers} matches
		// Network itself on `enabled` alone, which reads as a rename and is
		// nothing of the kind.
		if name == root {
			continue
		}
		members := p.structs[name]
		if len(members) == 0 {
			continue
		}
		missing := []string{}
		for _, member := range want {
			if !members[member] {
				missing = append(missing, member)
			}
		}
		candidate := Candidate{Struct: name, Missing: missing, Extra: len(members) - (len(want) - len(missing))}
		if !found || better(candidate, best) {
			best, found = candidate, true
		}
	}
	return best, found
}

func better(candidate, incumbent Candidate) bool {
	if len(candidate.Missing) != len(incumbent.Missing) {
		return len(candidate.Missing) < len(incumbent.Missing)
	}
	if candidate.Extra != incumbent.Extra {
		return candidate.Extra < incumbent.Extra
	}
	return candidate.Struct < incumbent.Struct
}
