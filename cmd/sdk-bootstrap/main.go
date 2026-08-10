// Command sdk-bootstrap derives a structural bootstrap from an SDK struct.
//
// A bootstrap says which fields a resource's SDK type carries and what shape
// each one is. It used to be written by hand, which made two things possible
// that should not be: a field could be described as something it is not, and
// the specification digest — meant to tie a policy to the source it was derived
// from — could hold any value at all, because the compiler only ever compares
// it against the policy's copy of the same string.
//
// Deriving the bootstrap closes both. The shape comes from the type checker
// rather than from reading, and the digest is recomputed from the file the
// struct is actually declared in. Running this in `go generate` and requiring a
// clean tree afterwards is what makes the binding checkable: an invented token
// is overwritten and shows up as a diff.
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

// The document is a struct rather than a map so the members keep the order a
// reader expects — what it was derived from, then what was derived — instead of
// whatever order the encoder sorts map keys into.
type bootstrapDocument struct {
	FormatVersion int               `json:"format_version"`
	Source        bootstrapSource   `json:"source"`
	Resource      bootstrapResource `json:"resource"`
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
}

func main() { os.Exit(run(os.Args[1:], os.Stderr)) }

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("sdk-bootstrap", flag.ContinueOnError)
	flags.SetOutput(stderr)
	pkgPath := flags.String("package", "", "SDK package to resolve")
	rootName := flags.String("struct", "", "SDK struct the resource operates on")
	resource := flags.String("resource", "", "Terraform resource name")
	commit := flags.String("commit", "", "SDK commit the bootstrap is derived from")
	output := flags.String("output", "", "file to write")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *pkgPath == "" || *rootName == "" || *resource == "" || *commit == "" || *output == "" {
		fmt.Fprintln(stderr, "package, struct, resource, commit and output are required")
		return 2
	}

	fset := token.NewFileSet()
	pkg, err := importer.ForCompiler(fset, "source", nil).Import(*pkgPath)
	if err != nil {
		fmt.Fprintf(stderr, "import %s: %v\n", *pkgPath, err)
		return 1
	}
	object := pkg.Scope().Lookup(*rootName)
	if object == nil {
		fmt.Fprintf(stderr, "%s defines no %s\n", *pkgPath, *rootName)
		return 1
	}
	structure, ok := object.Type().Underlying().(*types.Struct)
	if !ok {
		fmt.Fprintf(stderr, "%s.%s is not a struct\n", *pkgPath, *rootName)
		return 1
	}

	// The file comes from where the struct is declared, not from a name built
	// out of the resource. Guessing produced firewall_policy.go when the type
	// lives in firewall_policy.generated.go, and a digest over the wrong file
	// is worse than none: it is a real digest of something irrelevant.
	source := fset.Position(object.Pos()).Filename
	contents, err := os.ReadFile(source)
	if err != nil {
		fmt.Fprintf(stderr, "read %s: %v\n", source, err)
		return 1
	}
	sum := sha256.Sum256(contents)

	document := bootstrapDocument{
		FormatVersion: 1,
		Source: bootstrapSource{
			Repository:          "github.com/ubiquiti-community/go-unifi",
			Commit:              *commit,
			SpecificationSHA256: hex.EncodeToString(sum[:]),
		},
		Resource: bootstrapResource{Name: *resource, Fields: walk(structure)},
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
		name := jsonName(s.Tag(index))
		if name == "" {
			continue
		}
		shape, nested := describe(s.Field(index).Type())
		entry := field{Name: name, Type: shape}
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
