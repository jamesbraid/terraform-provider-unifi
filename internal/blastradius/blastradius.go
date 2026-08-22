// Package blastradius measures how many attributes a practitioner can set on a
// network-backed provider surface that the controller never receives.
//
// go-unifi serialises a Network through one of seven hand-maintained alias
// structs chosen by Purpose, and any field the chosen one omits is discarded
// with no diagnostic at any layer: the plan is clean, the apply succeeds, and
// the value is gone. This package pairs each surface's managed attributes with
// the JSON field names the encoder actually sends, so the size of that gap is a
// number a test can pin rather than a claim in a ticket.
//
// IT ASKS THE ENCODER RATHER THAN READING IT. Emitted populates every field of
// a Network with a non-zero value and marshals it, so the resulting key set is
// what that purpose sends at its maximum. Reading the alias structs out of
// go-unifi's source instead would count a field tagged omitempty as sent when a
// set-but-zero value still vanishes, and would break on any refactor that
// changed how the marshallers are written rather than what they emit.
//
// THE COUNTS ARE A FLOOR, NOT A TOTAL. Attributes returns entries with an empty
// Wire for the attributes the policy hand-maps in Go across several API fields
// -- both DNS server lists are like this -- and Dropped cannot see them. Count
// them with Unmeasurable so the blind spot stays a measured number.
package blastradius

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// Attribute is one managed, practitioner-visible attribute of a provider
// surface, paired with the single API field name it resolves to.
type Attribute struct {
	// Path is the terraform attribute path, dotted through grouping and
	// object boundaries: "dhcp.options.value".
	Path string
	// Wire is the API field name the policy resolves Path to, or "" when the
	// policy hand-maps the attribute across several fields in Go.
	Wire string
}

// Purposes are the seven network purposes, each selecting one encoder.
var Purposes = []string{
	ui.PurposeCorporate,
	ui.PurposeGuest,
	ui.PurposeVLANOnly,
	ui.PurposeWAN,
	ui.PurposeSiteVPN,
	ui.PurposeVPNClient,
	ui.PurposeUserVPN,
}

type policyNode struct {
	TerraformName  string       `json:"terraform_name"`
	StructuralName string       `json:"structural_name"`
	Disposition    string       `json:"disposition"`
	Members        []policyNode `json:"members"`
	Fields         []policyNode `json:"fields"`
}

func (n policyNode) children() []policyNode {
	if len(n.Members) > 0 {
		return n.Members
	}
	return n.Fields
}

type policyDocument struct {
	Resource  string       `json:"resource"`
	Fields    []policyNode `json:"fields"`
	Groupings []policyNode `json:"groupings"`
}

// Attributes returns every managed attribute the policy document exposes, in
// document order, including the containers of nested objects.
//
// A CONTAINER AND ITS MEMBERS ARE SEPARATE ATTRIBUTES, deliberately. The
// practitioner sets nat_outbound_ip_addresses.mode, and whether it reaches the
// controller is decided by whether the encoder sends nat_outbound_ip_addresses
// -- but the list itself is an attribute they can set too, so a purpose that
// drops the list drops five things they wrote, not four. Collapsing the members
// into the container would report a vlan-only network losing 41 attributes
// instead of 44.
//
// Groupings are entered unconditionally because they are terraform-side
// objects with no disposition and no API field of their own; only their members
// resolve to anything. Every other node must be managed, at every depth.
func Attributes(policy []byte) (resource string, attrs []Attribute, err error) {
	var document policyDocument
	if err := json.Unmarshal(policy, &document); err != nil {
		return "", nil, fmt.Errorf("parse policy: %w", err)
	}
	if document.Resource == "" {
		return "", nil, fmt.Errorf("policy names no resource")
	}
	for _, field := range document.Fields {
		if field.Disposition == "managed" {
			attrs = walk(field, "", attrs)
		}
	}
	for _, grouping := range document.Groupings {
		attrs = walk(grouping, "", attrs)
	}
	return document.Resource, attrs, nil
}

func walk(node policyNode, prefix string, into []Attribute) []Attribute {
	path := node.TerraformName
	if prefix != "" {
		path = prefix + "." + path
	}
	if node.StructuralName != "" || len(node.children()) == 0 {
		into = append(into, Attribute{Path: path, Wire: node.StructuralName})
	}
	for _, child := range node.children() {
		if child.Disposition != "managed" {
			continue
		}
		into = walk(child, path, into)
	}
	return into
}

// Unmeasurable counts the attributes this method cannot see: the ones the
// policy hand-maps in Go rather than resolving to a single API field.
func Unmeasurable(attrs []Attribute) []string {
	var blind []string
	for _, attr := range attrs {
		if attr.Wire == "" {
			blind = append(blind, attr.Path)
		}
	}
	sort.Strings(blind)
	return blind
}

// Emitted returns the top-level JSON field names go-unifi's Network encoder
// sends for purpose, sorted.
func Emitted(purpose string) ([]string, error) {
	var network ui.Network
	fill(reflect.ValueOf(&network).Elem())
	network.Purpose = purpose
	// The corporate and guest encoders derive DHCP range defaults from the
	// subnet and log when it will not parse. A real CIDR keeps the
	// measurement quiet without changing which keys are emitted.
	subnet := "10.0.0.0/24"
	network.IPSubnet = &subnet

	raw, err := json.Marshal(&network)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", purpose, err)
	}
	var emitted map[string]json.RawMessage
	if err := json.Unmarshal(raw, &emitted); err != nil {
		return nil, fmt.Errorf("reread %s: %w", purpose, err)
	}
	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// fill sets every field reachable from value to a non-zero value, so that no
// omitempty tag fires and the emitted key set is the encoder's maximum.
func fill(value reflect.Value) {
	switch value.Kind() {
	case reflect.String:
		value.SetString("x")
	case reflect.Bool:
		value.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(7)
	case reflect.Float32, reflect.Float64:
		value.SetFloat(7)
	case reflect.Ptr:
		value.Set(reflect.New(value.Type().Elem()))
		fill(value.Elem())
	case reflect.Slice:
		slice := reflect.MakeSlice(value.Type(), 1, 1)
		fill(slice.Index(0))
		value.Set(slice)
	case reflect.Struct:
		for i := range value.NumField() {
			if value.Type().Field(i).IsExported() {
				fill(value.Field(i))
			}
		}
	}
}

// Dropped returns the paths of the attributes whose API field the encoder does
// not send, sorted. Attributes with no resolved field are skipped: this method
// cannot see them, and reporting them as reaching the controller would be a
// guess in the safe direction.
func Dropped(attrs []Attribute, emitted []string) []string {
	sent := make(map[string]struct{}, len(emitted))
	for _, name := range emitted {
		sent[name] = struct{}{}
	}
	var dropped []string
	for _, attr := range attrs {
		if attr.Wire == "" {
			continue
		}
		if _, ok := sent[attr.Wire]; !ok {
			dropped = append(dropped, attr.Path)
		}
	}
	sort.Strings(dropped)
	return dropped
}

// AlwaysEmitted returns the JSON field names purpose's encoder sends for a
// Network nobody has touched. A key here carries no omitempty, so every write
// puts a value on the wire whether or not anything set one.
func AlwaysEmitted(purpose string) ([]string, error) {
	network := ui.Network{Purpose: purpose}
	return marshalKeys(&network, purpose)
}

// ForceEmitted returns the field names purpose's encoder sends unconditionally
// that no attribute of the surface owns -- so every unmasked write sends the Go
// zero for each of them, and the controller keeps whatever the provider does
// not model only by luck.
//
// owned is every field name the surface touches at all, managed or hand-mapped;
// it must NOT be the managed set alone. dhcpd_ip_1 through 3 are hand-mapped
// into dhcp_guarding.servers and would otherwise read as unowned.
//
// purpose itself is excluded: the encoder switches on it, so it is not a value
// anything failed to set.
func ForceEmitted(always []string, owned map[string]bool) []string {
	var forced []string
	for _, name := range always {
		if name == "purpose" || owned[name] {
			continue
		}
		forced = append(forced, name)
	}
	sort.Strings(forced)
	return forced
}

// CannotClear returns the attributes of a surface whose value the encoder
// refuses to send once it is empty, so the practitioner can set them and never
// unset them.
//
// EMPTY-BUT-SPECIFIED, NOT ABSENT. Each SDK field in turn is set to an empty
// value that is still present -- a pointer to the zero value where the field is
// a pointer, the zero itself where it is not -- and the object is remarshalled.
// A key that disappears is one omitempty drops, or one the encoder nils out on
// the way past: marshalVLANOnly runs name through nilIfEmpty, so an empty name
// vanishes even though the field is a pointer and omitempty alone would keep
// it. Reading the struct tags would miss that.
//
// NOT EVERY ENTRY IS A DEFECT. An attribute constrained to an enum or carrying
// a static default has no meaningful empty value, and one that is required or
// computed is not the practitioner's to clear. The defect is the free-form
// remainder, and the sharpest cases are the booleans -- a bool behind omitempty
// can be turned on and never off.
func CannotClear(purpose string, managed map[string]string) ([]string, error) {
	full, err := Emitted(purpose)
	if err != nil {
		return nil, err
	}
	sent := make(map[string]struct{}, len(full))
	for _, name := range full {
		sent[name] = struct{}{}
	}

	networkType := reflect.TypeOf(ui.Network{})
	seen := map[string]struct{}{}
	var stuck []string
	for i := range networkType.NumField() {
		if !networkType.Field(i).IsExported() {
			continue
		}
		var network ui.Network
		value := reflect.ValueOf(&network).Elem()
		fill(value)
		subnet := "10.0.0.0/24"
		network.IPSubnet = &subnet
		field := value.Field(i)
		if field.Kind() == reflect.Ptr {
			field.Set(reflect.New(field.Type().Elem()))
		} else {
			field.Set(reflect.Zero(field.Type()))
		}
		network.Purpose = purpose

		after, err := marshalKeys(&network, purpose)
		if err != nil {
			return nil, err
		}
		gone := make(map[string]struct{}, len(sent))
		for name := range sent {
			gone[name] = struct{}{}
		}
		for _, name := range after {
			delete(gone, name)
		}
		for name := range gone {
			attribute, owned := managed[name]
			if !owned {
				continue
			}
			if _, already := seen[attribute]; already {
				continue
			}
			seen[attribute] = struct{}{}
			stuck = append(stuck, attribute)
		}
	}
	sort.Strings(stuck)
	return stuck, nil
}

func marshalKeys(network *ui.Network, purpose string) ([]string, error) {
	raw, err := json.Marshal(network)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", purpose, err)
	}
	var emitted map[string]json.RawMessage
	if err := json.Unmarshal(raw, &emitted); err != nil {
		return nil, fmt.Errorf("reread %s: %w", purpose, err)
	}
	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// Ownership reads a surface's generated mapping and returns the API field names
// it manages, keyed by field name, and the wider set it touches at all.
//
// TOUCHED IS NOT MANAGED. A row with an empty disposition is one the provider
// hand-maps in Go -- dhcpd_dns_1 through 4 fold into dhcp_server.dns_servers
// that way. Those fields have an owner even though no single attribute names
// them, and counting them as unowned would report the provider clobbering
// values it is deliberately writing.
func Ownership(mapping []byte) (managed map[string]string, touched map[string]bool, err error) {
	var document struct {
		Fields []struct {
			StructuralName string `json:"structural_name"`
			TerraformName  string `json:"terraform_name"`
			Disposition    string `json:"disposition"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(mapping, &document); err != nil {
		return nil, nil, fmt.Errorf("parse mapping: %w", err)
	}
	if len(document.Fields) == 0 {
		return nil, nil, fmt.Errorf("mapping has no fields")
	}
	managed = map[string]string{}
	touched = map[string]bool{}
	for _, field := range document.Fields {
		if field.StructuralName == "" || field.Disposition == "omitted" {
			continue
		}
		touched[field.StructuralName] = true
		if field.Disposition == "managed" {
			managed[field.StructuralName] = field.TerraformName
		}
	}
	return managed, touched, nil
}
