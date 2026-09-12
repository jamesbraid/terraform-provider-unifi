package unifi

// The SDK's constraint tables and the served schemas must agree: every
// value-set, numeric-range, length or pattern fact the SDK exports for a
// wire that policy serves as an observed member must appear as the derived
// validator in the schema snapshot, and no hand validator entry in policy
// may restate a fact of a kind the SDK exports for its wire. The compiler
// derives the validators at generation time and refuses same-kind hand
// entries there; this closes the loop from the other end, over committed
// artifacts in their own terms -- the mapping reports (which record each
// surface's lead struct), the policy files (which relate wire names to
// snapshot paths) and the schema snapshot -- rather than by asking the
// compiler, which would agree with itself by construction. The constraint
// tables are read from the pinned SDK, the same module the bootstraps are
// derived from; nested struct types are resolved by reflecting over the
// SDK's own types, so the (Go type, wire) key each lookup uses is the
// SDK's, not a guessed naming convention.
//
// Claims, invented members and collapsed elements are hand-mapped surface
// the compiler deliberately does not derive into. A constrained wire a
// claim consumes is recorded in claimConsumedConstrainedWires rather than
// silently skipped, and a hand validator there that restates a same-kind
// SDK fact must be recorded in validatorOpinions -- both ledgers are
// checked for staleness in both directions.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/controllerregex"
)

// unifiStructTypes resolves the Go type names the unifi-package surfaces
// use -- each mapping report's sdk_struct, plus the structural_source
// names non-settings policies qualify members with. Checked in both
// directions below: every mapping report's sdk_struct must resolve here,
// and every entry must be some surface's sdk_struct or source, so the
// table cannot go stale in either direction.
var unifiStructTypes = map[string]reflect.Type{
	"APGroup":         reflect.TypeOf(ui.APGroup{}),
	"Account":         reflect.TypeOf(ui.Account{}),
	"BGPConfig":       reflect.TypeOf(ui.BGPConfig{}),
	"Client":          reflect.TypeOf(ui.Client{}),
	"ClientGroup":     reflect.TypeOf(ui.ClientGroup{}),
	"ClientInfo":      reflect.TypeOf(ui.ClientInfo{}),
	"DHCPOption":      reflect.TypeOf(ui.DHCPOption{}),
	"DNSRecord":       reflect.TypeOf(ui.DNSRecord{}),
	"Device":          reflect.TypeOf(ui.Device{}),
	"DpiApp":          reflect.TypeOf(ui.DpiApp{}),
	"DpiGroup":        reflect.TypeOf(ui.DpiGroup{}),
	"DynamicDNS":      reflect.TypeOf(ui.DynamicDNS{}),
	"FirewallGroup":   reflect.TypeOf(ui.FirewallGroup{}),
	"FirewallPolicy":  reflect.TypeOf(ui.FirewallPolicy{}),
	"FirewallRule":    reflect.TypeOf(ui.FirewallRule{}),
	"FirewallZone":    reflect.TypeOf(ui.FirewallZone{}),
	"HotspotOp":       reflect.TypeOf(ui.HotspotOp{}),
	"Network":         reflect.TypeOf(ui.Network{}),
	"PortForward":     reflect.TypeOf(ui.PortForward{}),
	"PortProfile":     reflect.TypeOf(ui.PortProfile{}),
	"PowerSupervisor": reflect.TypeOf(ui.PowerSupervisor{}),
	"RADIUSProfile":   reflect.TypeOf(ui.RADIUSProfile{}),
	"Routing":         reflect.TypeOf(ui.Routing{}),
	"ScheduleTask":    reflect.TypeOf(ui.ScheduleTask{}),
	"Site":            reflect.TypeOf(ui.Site{}),
	"TrafficRoute":    reflect.TypeOf(ui.TrafficRoute{}),
	"WLAN":            reflect.TypeOf(ui.WLAN{}),
	"WLANGroup":       reflect.TypeOf(ui.WLANGroup{}),
	"WireGuardPeer":   reflect.TypeOf(ui.WireGuardPeer{}),
}

// settingsStructTypes is the same resolution for the settings package:
// the unifi_setting surface's lead struct and every companion its policy
// names as a structural_source.
var settingsStructTypes = map[string]reflect.Type{
	"AutoSpeedtest":            reflect.TypeOf(settings.AutoSpeedtest{}),
	"Connectivity":             reflect.TypeOf(settings.Connectivity{}),
	"Country":                  reflect.TypeOf(settings.Country{}),
	"Dashboard":                reflect.TypeOf(settings.Dashboard{}),
	"DeviceSupervision":        reflect.TypeOf(settings.DeviceSupervision{}),
	"Doh":                      reflect.TypeOf(settings.Doh{}),
	"Dpi":                      reflect.TypeOf(settings.Dpi{}),
	"EtherLighting":            reflect.TypeOf(settings.EtherLighting{}),
	"GlobalAp":                 reflect.TypeOf(settings.GlobalAp{}),
	"GlobalNat":                reflect.TypeOf(settings.GlobalNat{}),
	"GlobalNetwork":            reflect.TypeOf(settings.GlobalNetwork{}),
	"GlobalSwitch":             reflect.TypeOf(settings.GlobalSwitch{}),
	"GuestAccess":              reflect.TypeOf(settings.GuestAccess{}),
	"IgmpSnooping":             reflect.TypeOf(settings.IgmpSnooping{}),
	"Ips":                      reflect.TypeOf(settings.Ips{}),
	"IpsSuppression":           reflect.TypeOf(settings.IpsSuppression{}),
	"Ipsec":                    reflect.TypeOf(settings.Ipsec{}),
	"Lcm":                      reflect.TypeOf(settings.Lcm{}),
	"Locale":                   reflect.TypeOf(settings.Locale{}),
	"MagicSiteToSiteVpn":       reflect.TypeOf(settings.MagicSiteToSiteVpn{}),
	"Mdns":                     reflect.TypeOf(settings.Mdns{}),
	"Mgmt":                     reflect.TypeOf(settings.Mgmt{}),
	"Netflow":                  reflect.TypeOf(settings.Netflow{}),
	"NetworkOptimization":      reflect.TypeOf(settings.NetworkOptimization{}),
	"Ntp":                      reflect.TypeOf(settings.Ntp{}),
	"RadioAi":                  reflect.TypeOf(settings.RadioAi{}),
	"Radius":                   reflect.TypeOf(settings.Radius{}),
	"Rsyslogd":                 reflect.TypeOf(settings.Rsyslogd{}),
	"SettingUsgGeoIPFiltering": reflect.TypeOf(settings.SettingUsgGeoIPFiltering{}),
	"Snmp":                     reflect.TypeOf(settings.Snmp{}),
	"SslInspection":            reflect.TypeOf(settings.SslInspection{}),
	"Teleport":                 reflect.TypeOf(settings.Teleport{}),
	"TrafficFlow":              reflect.TypeOf(settings.TrafficFlow{}),
	"Usg":                      reflect.TypeOf(settings.Usg{}),
	"Usw":                      reflect.TypeOf(settings.Usw{}),
}

// validatorOpinions is the ledger of hand validators that restate a
// same-kind SDK fact on a member derivation cannot reach, each with the
// reason the hand form stands. Keyed by artifact prefix then snapshot
// attribute path. An entry that stops matching such a hand validator is
// reported as stale, and a new same-kind hand validator on a non-derived
// member does not pass without being recorded here.
var validatorOpinions = map[string]map[string]string{
	"network": {
		// purpose is claim-mapped (its value is coupled to
		// third_party_gateway). The SDK exports seven values; this resource
		// deliberately serves three -- wan, vpn-client, site-vpn and
		// remote-user-vpn purposes are managed by unifi_wan,
		// unifi_vpn_client, unifi_site_to_site_vpn and unifi_vpn_server.
		"purpose": "deliberate subset: the other purposes belong to dedicated resources",
		// vlan is claim-mapped (paired with vlan_enabled). The hand entry
		// names unifi.NetworkVLANMin/Max rather than literals, so the
		// bounds move with the SDK; TestNetworkVLANClaimTracksSDKBounds
		// pins the served range to those constants.
		"vlan": "claim member; bounds are the SDK constants by reference",
	},
	"vpn_client": {
		// The claim releases the whole peer object; its port carries the
		// wireguard_client_peer_port bounds as SDK constants by reference.
		"wireguard.peer": "claim member; bounds are the SDK constants by reference",
	},
	"vpn_server": {
		// All three claim members restate facts derivation cannot reach.
		"openvpn.port":   "claim member; bounds are the SDK constants by reference",
		"wireguard.port": "claim member; bounds are the SDK constants by reference",
		// The controller's own wan[2-9]? pattern, hand-anchored with a
		// friendlier message; the claim maps it onto three observed wires.
		"wan.interface": "claim member; hand anchoring of the controller's own pattern",
	},
}

// claimConsumedConstrainedWires records, per artifact prefix, the wires
// with an SDK constraint that a claim or collapsed element consumes --
// the walk cannot follow a hand mapping onto the members it maps into,
// so each is acknowledged here instead of silently skipped. Checked in
// both directions: a listed wire must still be claim-consumed and still
// constrained, and an unlisted one refuses.
var claimConsumedConstrainedWires = map[string][]string{
	"network": {
		"dhcpd_boot_filename", "dhcpd_boot_server", "dhcpd_dns_3", "dhcpd_dns_4",
		"dhcpd_ip_1", "dhcpd_ip_2", "dhcpd_ip_3", "dhcpd_ntp_1", "dhcpd_ntp_2",
		"dhcpd_wins_1", "dhcpd_wins_2", "purpose", "vlan",
	},
	"network_ds": {
		"dhcpd_boot_filename", "dhcpd_boot_server", "dhcpd_dns_3", "dhcpd_dns_4",
		"dhcpd_ip_1", "dhcpd_ip_2", "dhcpd_ip_3", "dhcpd_ntp_1", "dhcpd_ntp_2",
		"dhcpd_wins_1", "dhcpd_wins_2", "purpose",
		"wan_dns1", "wan_dns2", "wan_dns3", "wan_dns4",
	},
	"vpn_client":    {"wireguard_client_mode", "wireguard_client_peer_port"},
	"traffic_route": {"domain"},
	"vpn_server": {
		"dhcpd_dns_3", "dhcpd_dns_4", "l2tp_interface", "l2tp_local_wan_ip",
		"local_port", "openvpn_interface", "openvpn_local_wan_ip", "wireguard_interface",
	},
}

// vaPolicyMember is the slice of a policy field, grouping member or
// flattened member this check reads.
type vaPolicyMember struct {
	StructuralName   string            `json:"structural_name"`
	StructuralSource string            `json:"structural_source"`
	TerraformName    string            `json:"terraform_name"`
	TerraformType    string            `json:"terraform_type"`
	Disposition      string            `json:"disposition"`
	ElementMember    string            `json:"element_member"`
	Invented         string            `json:"invented"`
	Attribute        vaPolicyAttribute `json:"attribute"`
	RawAttribute     json.RawMessage   `json:"-"`
	Fields           []vaPolicyMember  `json:"fields"`
}

// UnmarshalJSON keeps the raw attribute bytes beside the decoded slice so
// deepHandDefinitions can walk hand-authored nested attributes.
func (m *vaPolicyMember) UnmarshalJSON(data []byte) error {
	type plain vaPolicyMember
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var envelope struct {
		Attribute json.RawMessage `json:"attribute"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	decoded.RawAttribute = envelope.Attribute
	*m = vaPolicyMember(decoded)
	return nil
}

type vaPolicyAttribute struct {
	Validators  json.RawMessage            `json:"validators"`
	ElementType map[string]json.RawMessage `json:"element_type"`
	CustomType  struct {
		Type string `json:"type"`
	} `json:"custom_type"`
}

// elementKind names a collection attribute's declared element type, empty
// when the attribute declares none.
func (a vaPolicyAttribute) elementKind() string {
	for kind := range a.ElementType {
		return kind
	}
	return ""
}

// deepHandDefinitions collects every schema_definition anywhere inside a
// raw attribute document. A claim-released object carries its nested
// attributes hand-authored inside the attribute JSON itself -- not as
// policy member fields -- so the flat reader above cannot see a validator
// on, say, wireguard.peer's port; this walk can, and the per-kind marker
// match keeps the wider net precise.
func deepHandDefinitions(raw json.RawMessage) []string {
	var definitions []string
	var walk func(node any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if definition, ok := value["schema_definition"].(string); ok {
				definitions = append(definitions, definition)
			}
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err == nil {
		walk(decoded)
	}
	return definitions
}

// handDefinitions decodes the attribute's hand validators. The "none"
// suppression marker comes back as suppressed with no definitions.
func (a vaPolicyAttribute) handDefinitions() (definitions []string, suppressed bool) {
	if len(a.Validators) == 0 {
		return nil, false
	}
	var marker string
	if err := json.Unmarshal(a.Validators, &marker); err == nil {
		return nil, marker == "none"
	}
	var entries []struct {
		Custom struct {
			SchemaDefinition string `json:"schema_definition"`
		} `json:"custom"`
	}
	if err := json.Unmarshal(a.Validators, &entries); err != nil {
		return nil, false
	}
	for _, entry := range entries {
		if entry.Custom.SchemaDefinition != "" {
			definitions = append(definitions, entry.Custom.SchemaDefinition)
		}
	}
	return definitions, false
}

// vaPolicy is the slice of one policy file this check reads.
type vaPolicy struct {
	Fields    []vaPolicyMember `json:"fields"`
	Groupings []struct {
		TerraformName string           `json:"terraform_name"`
		Members       []vaPolicyMember `json:"members"`
	} `json:"groupings"`
	Flattenings []struct {
		StructuralName   string           `json:"structural_name"`
		StructuralSource string           `json:"structural_source"`
		Members          []vaPolicyMember `json:"members"`
	} `json:"flattenings"`
	Claims []struct {
		StructuralNames  []string `json:"structural_names"`
		TerraformMembers []string `json:"terraform_members"`
	} `json:"claims"`
}

// constraintSite is one observed wire's place in the served schema with
// everything the agreement needs: the resolved SDK constraint (when the
// tables carry one), the served shape, and the hand validators policy
// still declares there.
type constraintSite struct {
	path          string // snapshot attribute path
	wire          string
	goType        string // declaring struct's Go type name
	terraformType string // string, int64, list, set, ... as served
	elementType   string // element kind for list/set of scalars
	goDuration    bool
	suppressed    bool
	served        bool
	// claimConsumed marks a field a claim maps by hand: derivation never
	// reaches it, so the served direction skips it, but a hand validator
	// there is still held against the SDK's fact for its wire.
	claimConsumed bool
	constraint    ui.FieldConstraint
	hasConstraint bool
	hand          []string
}

// releasedClaimWire is one constrained wire behind a claim-released
// attribute.
type releasedClaimWire struct {
	wire       string
	constraint ui.FieldConstraint
}

// structFieldByWire finds the struct field whose json tag names wire,
// descending into embedded structs the way encoding/json does.
func structFieldByWire(structType reflect.Type, wire string) (reflect.StructField, bool) {
	for i := range structType.NumField() {
		field := structType.Field(i)
		tag := strings.Split(field.Tag.Get("json"), ",")[0]
		if tag == wire {
			return field, true
		}
		if field.Anonymous {
			embedded := field.Type
			if embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				if found, ok := structFieldByWire(embedded, wire); ok {
					return found, true
				}
			}
		}
	}
	return reflect.StructField{}, false
}

// resolveFieldShape classifies one SDK struct field the way the bootstrap
// does: a scalar kind, a collection of scalars with its element kind, or a
// struct (possibly a slice of structs) to recurse into.
func resolveFieldShape(fieldType reflect.Type) (kind, element string, nested reflect.Type) {
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	switch fieldType.Kind() {
	case reflect.String:
		return "string", "", nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "int64", "", nil
	case reflect.Bool:
		return "bool", "", nil
	case reflect.Float32, reflect.Float64:
		return "float64", "", nil
	case reflect.Struct:
		return "object", "", fieldType
	case reflect.Slice:
		elem := fieldType.Elem()
		for elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		switch elem.Kind() {
		case reflect.String:
			return "collection", "string", nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return "collection", "int64", nil
		case reflect.Struct:
			return "array<object>", "", elem
		}
	}
	return "", "", nil
}

// lookupConstraint keys the SDK's constraint tables by Go type name and
// wire, choosing the table by the type's own package and applying the
// settings table's "Setting" key prefix fallback the same way
// cmd/sdk-bootstrap does.
func lookupConstraint(structType reflect.Type, wire string) (ui.FieldConstraint, bool) {
	name := structType.Name()
	if strings.HasSuffix(structType.PkgPath(), "/unifi/settings") {
		for _, key := range []string{name, "Setting" + name} {
			if byWire, ok := settings.FieldConstraints[key]; ok {
				if constraint, ok := byWire[wire]; ok {
					return ui.FieldConstraint(constraint), true
				}
			}
		}
		return ui.FieldConstraint{}, false
	}
	byWire, ok := ui.FieldConstraints[name]
	if !ok {
		return ui.FieldConstraint{}, false
	}
	constraint, ok := byWire[wire]
	return constraint, ok
}

// vaSurface is one compiled surface with its committed identity and the
// walk products this check needs.
type vaSurface struct {
	prefix   string
	kind     string
	resource string
	lead     reflect.Type
	sites    []constraintSite
	claimed  map[string]bool
	// claimResidue records, for each claim-consumed or collapsed wire,
	// whether the SDK constrains it -- resolved where the wire lives, so a
	// collapsed element's nested type is consulted, not the lead.
	claimResidue map[string]bool
	// claimReleased maps each attribute path a claim releases to the
	// constrained wires the claim consumes, so a hand validator on the
	// released attribute is still held against those facts.
	claimReleased map[string][]releasedClaimWire
	// unresolved names structural members reflection could not place --
	// the walk announcing what it did not measure.
	unresolved []string
}

// collectSites walks one policy's members with a type context, mirroring
// the compiler's emission rules for observed members. Claim-consumed and
// invented members produce no site (their handling is the ledgers');
// collapsed elements resolve to the element member's own field.
func collectSites(surface *vaSurface, members []vaPolicyMember, context reflect.Type, basePath string, parentServed, topLevel bool) {
	collectSitesReleased(surface, members, context, basePath, parentServed, topLevel, nil)
}

// collectSitesReleased is collectSites with the released-claim wires a
// structural-less subtree inherits: a claim that releases an object
// carries its constrained wires onto every member below it, so a hand
// validator on a nested claim member is still held against them.
func collectSitesReleased(surface *vaSurface, members []vaPolicyMember, context reflect.Type, basePath string, parentServed, topLevel bool, inherited []releasedClaimWire) {
	for _, member := range members {
		if member.Invented != "" {
			continue
		}
		served := parentServed && member.Disposition != "omitted"
		if topLevel {
			served = member.Disposition == "managed" || member.Disposition == "computed"
		}
		path := member.TerraformName
		if basePath != "" {
			path = basePath + "." + member.TerraformName
		}
		if member.StructuralName == "" {
			// A claim-released member: policy supplies its type and the
			// claim its mapping. Derivation never reaches it, but its hand
			// validators are still held against the constrained wires the
			// claim consumes -- one pseudo-site per such wire. A claim that
			// releases an object carries its wires onto the members below.
			released := surface.claimReleased[path]
			if released == nil {
				released = inherited
			}
			_, suppressed := member.Attribute.handDefinitions()
			hand := deepHandDefinitions(member.RawAttribute)
			for _, wire := range released {
				surface.sites = append(surface.sites, constraintSite{
					path:          path,
					wire:          wire.wire,
					goType:        surface.lead.Name(),
					terraformType: member.TerraformType,
					elementType:   member.Attribute.elementKind(),
					goDuration:    strings.Contains(member.Attribute.CustomType.Type, "GoDurationType"),
					suppressed:    suppressed,
					claimConsumed: true,
					constraint:    wire.constraint,
					hasConstraint: true,
					hand:          hand,
				})
			}
			collectSitesReleased(surface, member.Fields, context, path, served, false, released)
			continue
		}
		claimConsumed := topLevel && surface.claimed[member.StructuralName]
		memberContext := context
		if member.StructuralSource != "" {
			resolved, ok := sourceType(surface, member.StructuralSource)
			if !ok {
				surface.unresolved = append(surface.unresolved,
					member.StructuralSource+"."+member.StructuralName+" (unknown structural_source)")
				continue
			}
			memberContext = resolved
		}
		field, ok := structFieldByWire(memberContext, member.StructuralName)
		if !ok {
			surface.unresolved = append(surface.unresolved,
				memberContext.Name()+"."+member.StructuralName)
			continue
		}
		kind, element, nested := resolveFieldShape(field.Type)
		if member.ElementMember != "" && nested != nil {
			// A collapsed array serves one element member's values under
			// this member's own path; a constraint there is out of
			// derivation's reach, so it joins the claim residue, resolved
			// against the element's own type.
			_, constrained := lookupConstraint(nested, member.ElementMember)
			surface.claimResidue[member.ElementMember] = constrained
			continue
		}
		switch kind {
		case "object", "array<object>":
			collectSitesReleased(surface, member.Fields, nested, path, served, false, nil)
			continue
		case "":
			surface.unresolved = append(surface.unresolved,
				memberContext.Name()+"."+member.StructuralName+" (unhandled shape)")
			continue
		}
		terraformType := member.TerraformType
		if terraformType == "" {
			terraformType = kind
		}
		hand, suppressed := member.Attribute.handDefinitions()
		constraint, hasConstraint := lookupConstraint(memberContext, member.StructuralName)
		surface.sites = append(surface.sites, constraintSite{
			path:          path,
			wire:          member.StructuralName,
			goType:        memberContext.Name(),
			terraformType: terraformType,
			elementType:   element,
			goDuration:    strings.Contains(member.Attribute.CustomType.Type, "GoDurationType"),
			suppressed:    suppressed,
			served:        served,
			claimConsumed: claimConsumed,
			constraint:    constraint,
			hasConstraint: hasConstraint,
			hand:          hand,
		})
	}
}

// sourceType resolves a structural_source name against the package family
// the surface projects: the settings tables for the unifi_setting surface,
// the unifi tables for everything else.
func sourceType(surface *vaSurface, name string) (reflect.Type, bool) {
	if strings.HasSuffix(surface.lead.PkgPath(), "/unifi/settings") {
		resolved, ok := settingsStructTypes[name]
		return resolved, ok
	}
	resolved, ok := unifiStructTypes[name]
	return resolved, ok
}

// expectedDerivedValidator mirrors the compiler's derivation order over a
// site's constraint -- value set, then bounds or length, then the raw
// pattern -- and renders the validator the snapshot must carry, in the
// snapshot's own "%T: description" form. A site the deriver skips (a
// Go-duration attribute, a suppressed field, a bool) expects nothing.
func expectedDerivedValidator(ctx context.Context, site constraintSite) (string, bool) {
	if !site.hasConstraint || site.suppressed {
		return "", false
	}
	// behaviourDescription is the snapshot's own renderer, so the expected
	// string is byte-identical to what the snapshot records.
	describe := func(v any) (string, bool) {
		return behaviourDescription(ctx, v), true
	}
	constraint := site.constraint
	switch site.terraformType {
	case "int64":
		if len(constraint.Int64Values) > 0 {
			return describe(int64validator.OneOf(constraint.Int64Values...))
		}
		if constraint.HasBounds {
			return describe(int64validator.Between(constraint.Min, constraint.Max))
		}
		return "", false
	case "string":
		if site.goDuration {
			return "", false
		}
		if len(constraint.Values) > 0 {
			return describe(stringvalidator.OneOf(constraint.Values...))
		}
		if constraint.HasLength {
			return describe(stringvalidator.LengthBetween(int(constraint.MinLength), int(constraint.MaxLength)))
		}
		if constraint.Pattern != "" {
			if _, err := controllerregex.Compile(constraint.Pattern); err != nil {
				// The deriver refuses or, beside a hand validator, skips
				// with a notice; either way nothing lands in the schema.
				return "", false
			}
			return describe(controllerregex.Matches(constraint.Pattern, ""))
		}
		return "", false
	case "list", "set":
		switch site.elementType {
		case "string":
			var inner validator.String
			switch {
			case len(constraint.Values) > 0:
				inner = stringvalidator.OneOf(constraint.Values...)
			case constraint.HasLength:
				inner = stringvalidator.LengthBetween(int(constraint.MinLength), int(constraint.MaxLength))
			case constraint.Pattern != "":
				if _, err := controllerregex.Compile(constraint.Pattern); err != nil {
					return "", false
				}
				inner = controllerregex.Matches(constraint.Pattern, "")
			default:
				return "", false
			}
			if site.terraformType == "set" {
				return describe(setvalidator.ValueStringsAre(inner))
			}
			return describe(listvalidator.ValueStringsAre(inner))
		case "int64":
			var inner validator.Int64
			switch {
			case len(constraint.Int64Values) > 0:
				inner = int64validator.OneOf(constraint.Int64Values...)
			case constraint.HasBounds:
				inner = int64validator.Between(constraint.Min, constraint.Max)
			default:
				return "", false
			}
			if site.terraformType == "set" {
				return describe(setvalidator.ValueInt64sAre(inner))
			}
			return describe(listvalidator.ValueInt64sAre(inner))
		}
	}
	return "", false
}

// derivedKindMarkers mirrors the compiler's conflict markers: the Go
// expression substrings a hand validator of each derived kind always
// contains.
var derivedKindMarkers = map[string][]string{
	"OneOf":        {".OneOf("},
	"Between":      {".Between("},
	"Length":       {".Length"},
	"RegexMatches": {".RegexMatches(", "controllerregex.Matches("},
}

// constraintKinds names the derived kinds a site's constraint exports for
// its served shape -- the kinds a hand validator there must not restate.
func constraintKinds(site constraintSite) []string {
	if !site.hasConstraint {
		return nil
	}
	constraint := site.constraint
	if site.claimConsumed {
		// A claim releases whatever shape its hand mapping chose -- often
		// an object over scalar wires -- so the served type says nothing
		// about the fact's kind; read the constraint's own parsed shape.
		switch {
		case len(constraint.Values) > 0, len(constraint.Int64Values) > 0:
			return []string{"OneOf"}
		case constraint.HasBounds:
			return []string{"Between"}
		case constraint.HasLength:
			return []string{"Length"}
		case constraint.Pattern != "":
			return []string{"RegexMatches"}
		}
		return nil
	}
	elementOrSelf := site.terraformType
	if elementOrSelf == "list" || elementOrSelf == "set" {
		elementOrSelf = site.elementType
	}
	var kinds []string
	switch elementOrSelf {
	case "int64":
		if len(constraint.Int64Values) > 0 {
			kinds = append(kinds, "OneOf")
		} else if constraint.HasBounds {
			kinds = append(kinds, "Between")
		}
	case "string":
		if site.goDuration {
			return nil
		}
		switch {
		case len(constraint.Values) > 0:
			kinds = append(kinds, "OneOf")
		case constraint.HasLength:
			kinds = append(kinds, "Length")
		case constraint.Pattern != "":
			kinds = append(kinds, "RegexMatches")
		}
	}
	return kinds
}

// servedFactFindings is the first direction: every derivable fact behind a
// served site must appear in the snapshot exactly as derived. checked
// counts the sites actually verified so the caller can prove the walk saw
// something.
func servedFactFindings(
	ctx context.Context,
	surface string,
	sites []constraintSite,
	attributes map[string]schemaSnapshotFact,
) (findings []string, checked int) {
	for _, site := range sites {
		if site.claimConsumed {
			continue
		}
		expected, ok := expectedDerivedValidator(ctx, site)
		if !ok || !site.served {
			continue
		}
		checked++
		fact, present := attributes[site.path]
		if !present {
			findings = append(findings, fmt.Sprintf(
				"%s: constrained wire %s.%s maps to attribute %q, which the served schema "+
					"snapshot does not carry", surface, site.goType, site.wire, site.path))
			continue
		}
		var found bool
		for _, validator := range fact.Validators {
			if validator == expected {
				found = true
			}
		}
		if !found {
			findings = append(findings, fmt.Sprintf(
				"%s: the SDK constrains %s.%s but attribute %q does not carry the derived "+
					"validator\n  want: %s\n  have: %v",
				surface, site.goType, site.wire, site.path, expected, fact.Validators))
		}
	}
	return findings, checked
}

// handDuplicationFindings is the other direction: no hand validator entry
// in policy may restate a fact of a kind the SDK exports for its wire.
// For observed members the compiler already refuses this at generation;
// re-asserted here over committed bytes so a weakened refusal cannot pass
// silently. opinions is the surface's validatorOpinions slice; used
// reports which entries an actual conflict consumed.
func handDuplicationFindings(
	surface string,
	sites []constraintSite,
	opinions map[string]string,
	used map[string]bool,
) []string {
	var findings []string
	for _, site := range sites {
		kinds := constraintKinds(site)
		if len(kinds) == 0 || len(site.hand) == 0 {
			continue
		}
		for _, hand := range site.hand {
			for _, kind := range kinds {
				for _, marker := range derivedKindMarkers[kind] {
					if !strings.Contains(hand, marker) {
						continue
					}
					if _, acknowledged := opinions[site.path]; acknowledged {
						used[site.path] = true
						continue
					}
					findings = append(findings, fmt.Sprintf(
						"%s: attribute %q hand-transcribes a %s validator over %s.%s, a fact "+
							"the SDK exports; delete the hand entry or record it in "+
							"validatorOpinions:\n  hand: %s",
						surface, site.path, kind, site.goType, site.wire, hand))
				}
			}
		}
	}
	return findings
}

// claimResidueFindings audits the claim ledger for one surface: every
// claim-consumed constrained wire must be listed, every listed wire must
// still be claim-consumed and constrained.
func claimResidueFindings(surface string, residue map[string]bool, listed []string) []string {
	var findings []string
	listedSet := map[string]bool{}
	for _, wire := range listed {
		listedSet[wire] = true
	}
	wires := make([]string, 0, len(residue))
	for wire := range residue {
		wires = append(wires, wire)
	}
	sort.Strings(wires)
	for _, wire := range wires {
		if residue[wire] && !listedSet[wire] {
			findings = append(findings, fmt.Sprintf(
				"%s: claim-consumed wire %q carries an SDK constraint this check cannot follow "+
					"through the hand mapping; acknowledge it in claimConsumedConstrainedWires",
				surface, wire))
		}
	}
	for _, wire := range listed {
		constrained, consumed := residue[wire]
		if !consumed {
			findings = append(findings, fmt.Sprintf(
				"%s: claimConsumedConstrainedWires names %q, which no claim consumes any more "+
					"-- the entry no longer describes anything, remove it", surface, wire))
			continue
		}
		if !constrained {
			findings = append(findings, fmt.Sprintf(
				"%s: claimConsumedConstrainedWires names %q, which the SDK no longer "+
					"constrains -- the entry no longer describes anything, remove it",
				surface, wire))
		}
	}
	return findings
}

// loadValidatorAgreementSurfaces reads every committed surface mapping
// report and its policy, resolves the lead struct, and walks the policy
// into constraint sites. Per-grouping section reports describe slices of a
// surface already covered by its own report, so they are skipped.
func loadValidatorAgreementSurfaces(t *testing.T) []*vaSurface {
	t.Helper()
	reports, err := filepath.Glob(filepath.Join("..", "provider-codegen", "generated", "*.mapping.json"))
	if err != nil || len(reports) == 0 {
		t.Fatalf("no committed mapping reports found (%v); with nothing to walk, every "+
			"assertion below would pass vacuously", err)
	}
	sort.Strings(reports)
	var surfaces []*vaSurface
	leadNames := map[string]bool{}
	for _, report := range reports {
		raw, err := os.ReadFile(report)
		if err != nil {
			t.Fatalf("the mapping report is one of this check's oracles and it is unreadable: %v", err)
		}
		var header struct {
			SurfaceKind string `json:"surface_kind"`
			Resource    string `json:"resource"`
			SDKStruct   string `json:"sdk_struct"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			t.Fatalf("parse %s: %v", report, err)
		}
		if header.SurfaceKind == "managed_section" {
			continue
		}
		prefix := strings.TrimSuffix(filepath.Base(report), ".mapping.json")
		if header.SDKStruct == "" {
			t.Fatalf("%s records no sdk_struct; the check cannot key the SDK's constraint "+
				"tables for it -- regenerate the mapping reports", report)
		}
		lead, ok := unifiStructTypes[header.SDKStruct]
		if !ok {
			lead, ok = settingsStructTypes[header.SDKStruct]
		}
		if !ok {
			t.Fatalf("%s: sdk_struct %q resolves in neither struct table; add it so the walk "+
				"can see the surface", report, header.SDKStruct)
		}
		leadNames[header.SDKStruct] = true

		policyPath := filepath.Join("..", "provider-codegen", "policy", prefix+".json")
		policyRaw, err := os.ReadFile(policyPath)
		if err != nil {
			t.Fatalf("%s has a mapping report but no policy at %s: %v", prefix, policyPath, err)
		}
		var policy vaPolicy
		if err := json.Unmarshal(policyRaw, &policy); err != nil {
			t.Fatalf("parse %s: %v", policyPath, err)
		}
		surface := &vaSurface{
			prefix:        prefix,
			kind:          header.SurfaceKind,
			resource:      header.Resource,
			lead:          lead,
			claimed:       map[string]bool{},
			claimResidue:  map[string]bool{},
			claimReleased: map[string][]releasedClaimWire{},
		}
		for _, claim := range policy.Claims {
			var constrainedWires []releasedClaimWire
			for _, name := range claim.StructuralNames {
				leaf := name
				if dot := strings.LastIndex(name, "."); dot >= 0 {
					leaf = name[dot+1:]
				}
				surface.claimed[leaf] = true
				constraint, constrained := lookupConstraint(lead, leaf)
				surface.claimResidue[leaf] = constrained
				if constrained {
					constrainedWires = append(constrainedWires, releasedClaimWire{leaf, constraint})
				}
			}
			for _, member := range claim.TerraformMembers {
				surface.claimReleased[member] = append(surface.claimReleased[member], constrainedWires...)
			}
		}
		collectSites(surface, policy.Fields, lead, "", true, true)
		for _, grouping := range policy.Groupings {
			collectSites(surface, grouping.Members, lead, grouping.TerraformName, true, false)
		}
		for _, flattening := range policy.Flattenings {
			flatContext := lead
			if flattening.StructuralSource != "" {
				if resolved, ok := sourceType(surface, flattening.StructuralSource); ok {
					flatContext = resolved
				}
			}
			if field, ok := structFieldByWire(flatContext, flattening.StructuralName); ok {
				if _, _, nested := resolveFieldShape(field.Type); nested != nil {
					collectSites(surface, flattening.Members, nested, "", true, true)
					continue
				}
			}
			surface.unresolved = append(surface.unresolved,
				flatContext.Name()+"."+flattening.StructuralName+" (flattening)")
		}
		surfaces = append(surfaces, surface)
	}
	return surfaces
}

// snapshotSurfaceAttributes finds one surface's snapshot facts by its
// kind, so data sources and actions are verified in their own sections
// rather than silently missing from resources.
func snapshotSurfaceAttributes(snapshot providerSchemaSnapshot, kind, resource string) (map[string]schemaSnapshotFact, bool) {
	var surface schemaSnapshotSurface
	var ok bool
	switch kind {
	case "managed_resource":
		surface, ok = snapshot.Resources[resource]
	case "data_source":
		surface, ok = snapshot.DataSources[resource]
	case "list_resource":
		surface, ok = snapshot.ListResources[resource]
	case "action":
		surface, ok = snapshot.Actions[resource]
	}
	return surface.Attributes, ok
}

// TestServedSchemasCarryEverySDKConstraint walks every compiled surface
// and asserts both directions of the validator agreement, plus the two
// ledgers' staleness in both directions.
func TestServedSchemasCarryEverySDKConstraint(t *testing.T) {
	if len(ui.FieldConstraints) == 0 || len(settings.FieldConstraints) == 0 {
		t.Fatal("an SDK constraint table is empty; every assertion below would pass vacuously")
	}
	ctx := context.Background()
	surfaces := loadValidatorAgreementSurfaces(t)
	snapshot := loadCommittedSchemaSnapshot(t)

	totalChecked := 0
	usedOpinions := map[string]map[string]bool{}
	for _, surface := range surfaces {
		for _, unresolved := range surface.unresolved {
			t.Errorf("%s: structural member %s did not resolve against the SDK's types; the "+
				"walk cannot see it and refuses rather than passing", surface.prefix, unresolved)
		}
		attributes, ok := snapshotSurfaceAttributes(snapshot, surface.kind, surface.resource)
		if !ok {
			t.Errorf("%s: %s %s has a mapping report and policy but no surface in the schema "+
				"snapshot; the check cannot see what it serves", surface.prefix, surface.kind, surface.resource)
			continue
		}
		findings, checked := servedFactFindings(ctx, surface.prefix, surface.sites, attributes)
		totalChecked += checked
		for _, finding := range findings {
			t.Error(finding)
		}
		used := map[string]bool{}
		usedOpinions[surface.prefix] = used
		for _, finding := range handDuplicationFindings(
			surface.prefix, surface.sites, validatorOpinions[surface.prefix], used) {
			t.Error(finding)
		}
		for _, finding := range claimResidueFindings(
			surface.prefix, surface.claimResidue, claimConsumedConstrainedWires[surface.prefix]) {
			t.Error(finding)
		}
	}

	// Ledger staleness, the other direction: every recorded opinion and
	// every acknowledged claim surface must still exist.
	surfacesByPrefix := map[string]bool{}
	for _, surface := range surfaces {
		surfacesByPrefix[surface.prefix] = true
	}
	for prefix, opinions := range validatorOpinions {
		if !surfacesByPrefix[prefix] {
			t.Errorf("validatorOpinions names surface %q, which has no mapping report", prefix)
			continue
		}
		used := usedOpinions[prefix]
		paths := make([]string, 0, len(opinions))
		for path := range opinions {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			if used[path] {
				continue
			}
			t.Errorf("validatorOpinions[%q][%q] matched no hand validator restating an SDK "+
				"fact; the entry no longer describes anything -- remove it", prefix, path)
		}
	}
	for prefix := range claimConsumedConstrainedWires {
		if !surfacesByPrefix[prefix] {
			t.Errorf("claimConsumedConstrainedWires names surface %q, which has no mapping report", prefix)
		}
	}

	if totalChecked == 0 {
		t.Fatal("zero served constraint sites were verified; the walk measured nothing")
	}
	t.Logf("verified %d served constraint sites across %d surfaces", totalChecked, len(surfaces))
}

// TestServedFactFindingsDetectEveryDisagreement is the served direction's
// positive control: a missing attribute and a missing derived validator go
// red, while an agreeing site, an unserved one and a claim-consumed one
// stay silent.
func TestServedFactFindingsDetectEveryDisagreement(t *testing.T) {
	ctx := context.Background()
	bounds := ui.FieldConstraint{Min: 1, Max: 9, HasBounds: true}
	agreeing := behaviourDescription(ctx, int64validator.Between(1, 9))
	sites := []constraintSite{
		{path: "vanished", wire: "vanished", goType: "Probe", terraformType: "int64", served: true, constraint: bounds, hasConstraint: true},
		{path: "bare", wire: "bare", goType: "Probe", terraformType: "int64", served: true, constraint: bounds, hasConstraint: true},
		{path: "agreeing", wire: "agreeing", goType: "Probe", terraformType: "int64", served: true, constraint: bounds, hasConstraint: true},
		{path: "unserved", wire: "unserved", goType: "Probe", terraformType: "int64", served: false, constraint: bounds, hasConstraint: true},
		{path: "claimed", wire: "claimed", goType: "Probe", terraformType: "int64", served: true, claimConsumed: true, constraint: bounds, hasConstraint: true},
	}
	attributes := map[string]schemaSnapshotFact{
		"bare":     {Kind: "attribute"},
		"agreeing": {Kind: "attribute", Validators: []string{agreeing}},
	}
	findings, checked := servedFactFindings(ctx, "unifi_probe", sites, attributes)
	if len(findings) != 2 || checked != 3 {
		t.Fatalf("findings = %v (checked %d), want exactly the vanished attribute and the "+
			"bare one, with three sites checked", findings, checked)
	}
	for _, want := range []string{`"vanished"`, `"bare"`} {
		var named bool
		for _, finding := range findings {
			if strings.Contains(finding, want) && strings.Contains(finding, "unifi_probe") {
				named = true
			}
		}
		if !named {
			t.Errorf("no finding names both unifi_probe and %s", want)
		}
	}
}

// TestHandDuplicationFindingsDetectEveryRestatement is the other
// direction's positive control: a same-kind hand validator goes red, an
// acknowledged one is consumed silently, and a hand validator of a kind
// the SDK does not export for the wire stays silent.
func TestHandDuplicationFindingsDetectEveryRestatement(t *testing.T) {
	bounds := ui.FieldConstraint{Min: 1, Max: 9, HasBounds: true}
	sites := []constraintSite{
		{
			path: "restated", wire: "restated", goType: "Probe", terraformType: "int64", constraint: bounds, hasConstraint: true,
			hand: []string{"int64validator.Between(1, 5)"},
		},
		{
			path: "acknowledged", wire: "acknowledged", goType: "Probe", terraformType: "int64", constraint: bounds, hasConstraint: true,
			hand: []string{"int64validator.Between(2, 9)"},
		},
		{
			path: "other_kind", wire: "other_kind", goType: "Probe", terraformType: "int64", constraint: bounds, hasConstraint: true,
			hand: []string{"int64validator.AtLeast(1)", `int64validator.ConflictsWith(path.MatchRoot("x"))`},
		},
	}
	used := map[string]bool{}
	findings := handDuplicationFindings("unifi_probe", sites,
		map[string]string{"acknowledged": "recorded reason"}, used)
	if len(findings) != 1 || !strings.Contains(findings[0], `"restated"`) {
		t.Fatalf("findings = %v, want exactly the unacknowledged restatement", findings)
	}
	if !used["acknowledged"] {
		t.Fatal("the acknowledged entry was not consumed; the staleness check would flag it")
	}
}

// TestClaimResidueFindingsDetectStaleAndMissing is the ledger's positive
// control in both directions: an unlisted constrained wire, a listed wire
// no claim consumes, and a listed wire the SDK no longer constrains all go
// red; a listed, consumed, constrained wire stays silent.
func TestClaimResidueFindingsDetectStaleAndMissing(t *testing.T) {
	residue := map[string]bool{
		"unlisted":      true,
		"acknowledged":  true,
		"unconstrained": false,
	}
	findings := claimResidueFindings("unifi_probe", residue,
		[]string{"acknowledged", "unconstrained", "vanished"})
	if len(findings) != 3 {
		t.Fatalf("findings = %v, want the unlisted wire, the unconstrained entry and the "+
			"vanished entry", findings)
	}
	for _, want := range []string{`"unlisted"`, `"unconstrained"`, `"vanished"`} {
		var named bool
		for _, finding := range findings {
			if strings.Contains(finding, want) {
				named = true
			}
		}
		if !named {
			t.Errorf("no finding names %s", want)
		}
	}
}

// TestRadiusUserVLANBetweenSightLine is the real-data control: one derived
// range pinned end to end -- the SDK's exported Account.vlan bounds, the
// policy serving the wire, and the snapshot's Between validator built from
// those very constants.
func TestRadiusUserVLANBetweenSightLine(t *testing.T) {
	constraint, ok := ui.FieldConstraints["Account"]["vlan"]
	if !ok || !constraint.HasBounds {
		t.Fatal("the SDK no longer exports bounds for Account.vlan; this sight line is broken")
	}
	if constraint.Min != ui.AccountVLANMin || constraint.Max != ui.AccountVLANMax {
		t.Fatalf("FieldConstraints bounds (%d, %d) disagree with the exported constants (%d, %d)",
			constraint.Min, constraint.Max, ui.AccountVLANMin, ui.AccountVLANMax)
	}
	snapshot := loadCommittedSchemaSnapshot(t)
	fact, ok := snapshot.Resources["unifi_radius_user"].Attributes["vlan"]
	if !ok {
		t.Fatal("unifi_radius_user.vlan is not in the schema snapshot")
	}
	expected := behaviourDescription(context.Background(),
		int64validator.Between(ui.AccountVLANMin, ui.AccountVLANMax))
	for _, validator := range fact.Validators {
		if validator == expected {
			return
		}
	}
	t.Fatalf("unifi_radius_user.vlan validators = %v, want %q", fact.Validators, expected)
}

// TestNetworkVLANClaimTracksSDKBounds pins the one claim member whose hand
// validator carries a range: the policy entry must name the SDK's
// NetworkVLANMin/Max constants rather than literals, and the served range
// must be those constants' values -- so a controller change to the VLAN
// range moves this attribute on the next generate instead of going stale.
func TestNetworkVLANClaimTracksSDKBounds(t *testing.T) {
	policyRaw, err := os.ReadFile(filepath.Join("..", "provider-codegen", "policy", "network.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(policyRaw), "int64validator.Between(unifi.NetworkVLANMin, unifi.NetworkVLANMax)") {
		t.Fatal("network.json no longer pins vlan to unifi.NetworkVLANMin/Max; a literal " +
			"range here is a hand transcription waiting to go stale")
	}
	snapshot := loadCommittedSchemaSnapshot(t)
	fact, ok := snapshot.Resources["unifi_network"].Attributes["vlan"]
	if !ok {
		t.Fatal("unifi_network.vlan is not in the schema snapshot")
	}
	expected := behaviourDescription(context.Background(),
		int64validator.Between(ui.NetworkVLANMin, ui.NetworkVLANMax))
	for _, validator := range fact.Validators {
		if validator == expected {
			return
		}
	}
	t.Fatalf("unifi_network.vlan validators = %v, want %q", fact.Validators, expected)
}

// TestWLANDayOfWeekMatchesSDKValues pins the one invented member whose
// hand validator shadows an SDK value set: day_of_week presents a single
// day out of start_days_of_week (omitted, so no derivation reaches it),
// and its hand OneOf must carry exactly the SDK's exported values.
func TestWLANDayOfWeekMatchesSDKValues(t *testing.T) {
	values := ui.WLANScheduleWithDurationStartDaysOfWeekValues
	if len(values) == 0 {
		t.Fatal("the SDK no longer exports WLANScheduleWithDuration.start_days_of_week values")
	}
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = fmt.Sprintf("%q", value)
	}
	expected := fmt.Sprintf("stringvalidator.OneOf(%s)", strings.Join(quoted, ", "))
	policyRaw, err := os.ReadFile(filepath.Join("..", "provider-codegen", "policy", "wlan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var policy vaPolicy
	if err := json.Unmarshal(policyRaw, &policy); err != nil {
		t.Fatal(err)
	}
	var found bool
	var walk func(members []vaPolicyMember)
	walk = func(members []vaPolicyMember) {
		for _, member := range members {
			if member.TerraformName == "day_of_week" && member.Invented != "" {
				hand, _ := member.Attribute.handDefinitions()
				for _, definition := range hand {
					if definition == expected {
						found = true
					}
				}
				if !found {
					t.Fatalf("day_of_week hand validators = %v, want %q", hand, expected)
				}
			}
			walk(member.Fields)
		}
	}
	walk(policy.Fields)
	if !found {
		t.Fatal("wlan.json no longer carries an invented day_of_week member; this sight line is broken")
	}
}
