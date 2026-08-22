package unifi

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// droppedOnWrite reports every populated field of an object that the object's
// own encoder will not send, as one warning per field.
//
// WHY THIS EXISTS. go-unifi's Network serialises through one of seven
// hand-maintained alias structs chosen by Purpose, and any field the chosen one
// omits is dropped with no diagnostic at any layer. Measured on the provider's
// attributes, 62 of them can be set and never reach the controller, and a
// vlan-only network discards 44 of the 51 it exposes. The plan is clean, the
// apply succeeds, and the controller receives seven of them.
//
// ONLY WHAT THE PRACTITIONER SET. Warning about all 44 would be 44 warnings on
// every plan that nobody can act on, which is how a diagnostic becomes noise
// people filter out -- the same failure as a gate that is always red. A
// practitioner who never touches dhcpd_dns_1 on a vlan-only network sees
// nothing; one who sets it sees exactly one warning naming it.
//
// IT ASKS THE ENCODER RATHER THAN A TABLE, which is what keeps it correct
// without a second copy of go-unifi's per-purpose knowledge. Marshal the object
// the provider is about to send, read back which json names survived, and
// report the populated fields that did not. That needs no mapping file, no
// per-purpose list, and no walker -- and it catches BOTH ways a field goes
// missing: absent from the alias struct entirely, and present but tagged
// omitempty so a set-but-zero value vanishes.
//
// THE 62 AND THE 44 ABOVE ARE PINNED IN internal/blastradius, by name and not by
// count, so a figure quoted here cannot drift from the one a test measures.
//
// A ZERO FIELD IS NOT REPORTED, deliberately. The encoder legitimately omits
// zero values everywhere, and the provider cannot tell an attribute the
// practitioner set to zero from one they never mentioned once it has reached
// the SDK struct. Reporting those would put the noise back.
func droppedOnWrite(subject string, object any) diag.Diagnostics {
	var diags diag.Diagnostics

	raw, err := json.Marshal(object)
	if err != nil {
		// A Network with no Purpose cannot encode at all. That is a real
		// problem but not this one's, and the write will report it.
		return diags
	}
	var emitted map[string]json.RawMessage
	if err := json.Unmarshal(raw, &emitted); err != nil {
		return diags
	}

	value := reflect.ValueOf(object)
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return diags
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return diags
	}

	var missing []string
	structType := value.Type()
	for i := range structType.NumField() {
		field := structType.Field(i)
		if !field.IsExported() {
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || value.Field(i).IsZero() {
			continue
		}
		if _, sent := emitted[name]; !sent {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)

	for _, name := range missing {
		diags.AddWarning(
			"Value will not reach the controller",
			fmt.Sprintf(
				"This configuration sets %s, but the UniFi API does not accept it for a %s. "+
					"The apply will succeed and the value will be silently discarded; the "+
					"controller keeps whatever it had. Remove the attribute, or use a "+
					"configuration the controller supports for this kind of network.",
				name, subject),
		)
	}
	return diags
}
