package catalogparity

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// MarshalReceipt renders a receipt as the bytes that go on disk: two-space
// indent, keys sorted at every level, one trailing newline, no HTML escaping.
//
// SORTED RATHER THAN DECLARATION ORDER, and the reason is not cosmetic.
// encoding/json emits struct fields in the order they are declared and map keys
// in sorted order. Only one of those is stable under editing. Several gates
// compare receipts byte for byte, so with declaration order a field moved for
// readability changes the bytes of a release artifact, and the diff that caused
// it shows a reordering rather than a comparison about to fail. Sorting severs
// the artifact from the source layout: the receipt is then a function of what
// the run measured and nothing else.
//
// The round trip through a generic value is what does the sorting -- there is
// no way to ask encoding/json to sort struct fields directly. UseNumber keeps
// integers as the literals they arrived as; without it a format_version of 1
// comes back as a float64 and re-renders as 1, which is right by luck and stops
// being right for anything large enough to lose precision.
//
// HTML escaping is off because jq does not escape, and a receipt is read by
// jq, cmp and Go -- never by a browser. It costs nothing here (none of the
// frozen receipts contain <, > or &) and it removes a difference that would
// otherwise appear the first time a finding string quoted a comparison
// operator.
func MarshalReceipt(v any) ([]byte, error) {
	direct, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal receipt: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(direct))
	decoder.UseNumber()
	var generic any
	if err := decoder.Decode(&generic); err != nil {
		return nil, fmt.Errorf("re-read receipt for canonical ordering: %w", err)
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(generic); err != nil {
		return nil, fmt.Errorf("render canonical receipt: %w", err)
	}
	return out.Bytes(), nil
}
