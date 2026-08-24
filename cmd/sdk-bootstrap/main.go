// Command sdk-bootstrap derives a structural bootstrap from an SDK struct.
//
// Checkable only because `go generate` runs this and CI requires a clean tree
// afterwards: a hand-edited field or digest is overwritten and shows up as a diff.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
)

// A struct, not a map, so field order in the output stays deterministic.
type bootstrapDocument struct {
	FormatVersion int               `json:"format_version"`
	Source        bootstrapSource   `json:"source"`
	Resource      bootstrapResource `json:"resource"`
	// Companions are further SDK structs a surface projects, in the order named;
	// the lead stays in Resource, not companions[0], since identity follows it alone.
	Companions []bootstrapCompanion `json:"companions,omitempty"`
}

// bootstrapCompanion is one further struct, named by its Go type rather than a
// resource name -- the policy qualifies a field by this name.
type bootstrapCompanion struct {
	Struct string  `json:"struct"`
	Fields []field `json:"fields"`
}

type bootstrapSource struct {
	Repository          string `json:"repository"`
	Commit              string `json:"commit"`
	SpecificationSHA256 string `json:"specification_sha256"`
}

type bootstrapResource struct {
	Name   string  `json:"name"`
	Fields []field `json:"fields"`
}

type field struct {
	Name   string  `json:"name"`
	Type   string  `json:"type"`
	Fields []field `json:"fields,omitempty"`
	// SecretCandidate flags a field named with UniFi's x_ wire prefix; it's a
	// candidate, not a verdict -- the policy decides omit/mask, and the compiler
	// refuses anything that does neither.
	SecretCandidate bool `json:"secret_candidate,omitempty"`

	// GoName and Pointer are recorded here because this is the only step that
	// reads the Go struct -- downstream sees JSON, which has neither. GoName
	// doesn't follow from the wire name by any rule (key->Key, ttl->Ttl,
	// record_type->RecordType); Pointer isn't derivable from the type or from
	// `omitempty` either.
	GoName  string `json:"go_name,omitempty"`
	Pointer bool   `json:"pointer,omitempty"`
}

// stringList collects a flag given more than once, in the order given, because
// which struct leads is a fact the order carries.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(value string) error {
	*l = append(*l, value)
	return nil
}

func main() { os.Exit(run(os.Args[1:], os.Stderr)) }

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("sdk-bootstrap", flag.ContinueOnError)
	flags.SetOutput(stderr)
	pkgPath := flags.String("package", "", "SDK package to resolve")
	var structNames stringList
	flags.Var(&structNames, "struct",
		"SDK struct the resource operates on; repeat for a surface that projects several, "+
			"the first being the one the surface leads with")
	resource := flags.String("resource", "", "Terraform resource name")
	commit := flags.String("commit", "", "SDK commit the bootstrap is derived from")
	output := flags.String("output", "", "file to write")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *pkgPath == "" || len(structNames) == 0 || *resource == "" || *commit == "" || *output == "" {
		fmt.Fprintln(stderr, "package, struct, resource, commit and output are required")
		return 2
	}

	fset := token.NewFileSet()
	pkg, err := importer.ForCompiler(fset, "source", nil).Import(*pkgPath)
	if err != nil {
		fmt.Fprintf(stderr, "import %s: %v\n", *pkgPath, err)
		return 1
	}
	// Each struct's own declaring file, in the order named -- not a filename
	// guessed from the resource, which can differ from where the type actually lives.
	declared := make([][]byte, 0, len(structNames))
	structures := make([]*types.Struct, 0, len(structNames))
	seen := map[string]bool{}
	for _, name := range structNames {
		if seen[name] {
			fmt.Fprintf(stderr, "struct %s named twice; a field would then be observed twice\n", name)
			return 2
		}
		seen[name] = true
		object := pkg.Scope().Lookup(name)
		if object == nil {
			fmt.Fprintf(stderr, "%s defines no %s\n", *pkgPath, name)
			return 1
		}
		structure, ok := object.Type().Underlying().(*types.Struct)
		if !ok {
			fmt.Fprintf(stderr, "%s.%s is not a struct\n", *pkgPath, name)
			return 1
		}
		contents, err := os.ReadFile(fset.Position(object.Pos()).Filename)
		if err != nil {
			fmt.Fprintf(stderr, "read the file declaring %s: %v\n", name, err)
			return 1
		}
		declared = append(declared, contents)
		structures = append(structures, structure)
	}
	structure := structures[0]

	// A single struct digests exactly as before, keeping every existing
	// bootstrap's digest bound; several digest names and files together, so reordering moves it.
	sum := sha256.Sum256(declared[0])
	if len(declared) > 1 {
		hash := sha256.New()
		for index, contents := range declared {
			hash.Write([]byte(structNames[index]))
			hash.Write([]byte{0})
			hash.Write(contents)
		}
		copy(sum[:], hash.Sum(nil))
	}

	document := bootstrapDocument{
		FormatVersion: 1,
		Source: bootstrapSource{
			Repository:          "github.com/ubiquiti-community/go-unifi",
			Commit:              *commit,
			SpecificationSHA256: hex.EncodeToString(sum[:]),
		},
		Resource: bootstrapResource{Name: *resource, Fields: walk(structure)},
	}
	for index, companion := range structures[1:] {
		document.Companions = append(document.Companions, bootstrapCompanion{
			Struct: structNames[index+1],
			Fields: walk(companion),
		})
	}

	encoded := new(strings.Builder)
	encoder := json.NewEncoder(encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		fmt.Fprintf(stderr, "encode: %v\n", err)
		return 1
	}
	if err := os.WriteFile(*output, []byte(encoded.String()), 0o644); err != nil {
		fmt.Fprintf(stderr, "write %s: %v\n", *output, err)
		return 1
	}
	return 0
}

// walk records each field's wire name and shape, sorted so the output does not
// depend on declaration order.
func walk(s *types.Struct) []field {
	out := []field{}
	for index := range s.NumFields() {
		member := s.Field(index)
		name := jsonName(s.Tag(index))
		if name == "" {
			// An untagged embedded field promotes its members onto the wire
			// (what encoding/json does), so they must be walked into, not
			// skipped -- skipping would under-name fields with nothing to
			// report, since the compiler only checks names a bootstrap gives
			// it. A tagged embedded field is a named member, not a
			// promotion, and takes the ordinary path below.
			if member.Embedded() {
				if _, nested := describe(member.Type()); nested != nil {
					out = append(out, walk(nested)...)
				}
			}
			continue
		}
		shape, nested := describe(member.Type())
		// Pointer-ness is read BEFORE describe, which collapses a pointer to
		// its element -- that collapse is why the fact was being lost.
		_, isPointer := member.Type().(*types.Pointer)
		entry := field{
			Name:            name,
			Type:            shape,
			SecretCandidate: strings.HasPrefix(name, "x_"),
			GoName:          member.Name(),
			Pointer:         isPointer,
		}
		if nested != nil {
			entry.Fields = walk(nested)
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func describe(t types.Type) (string, *types.Struct) {
	switch shaped := t.(type) {
	case *types.Slice:
		inner, nested := describe(shaped.Elem())
		return "array<" + inner + ">", nested
	case *types.Pointer:
		return describe(shaped.Elem())
	case *types.Named:
		if structure, ok := shaped.Underlying().(*types.Struct); ok {
			return "object", structure
		}
		return describe(shaped.Underlying())
	case *types.Basic:
		switch shaped.Kind() {
		case types.Bool:
			return "bool", nil
		case types.String:
			return "string", nil
		case types.Float32, types.Float64:
			return "number", nil
		default:
			return "int64", nil
		}
	}
	return "unknown", nil
}

func jsonName(tag string) string {
	value := reflect.StructTag(tag).Get("json")
	if value == "" || value == "-" {
		return ""
	}
	if comma := strings.Index(value, ","); comma >= 0 {
		value = value[:comma]
	}
	return value
}
