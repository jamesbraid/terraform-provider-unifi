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
// Network serialises through a purpose-chosen alias struct that silently
// drops what it omits; this marshals and diffs against populated fields.
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
