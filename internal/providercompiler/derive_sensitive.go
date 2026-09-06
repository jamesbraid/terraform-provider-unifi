package providercompiler

import (
	"encoding/json"
	"fmt"
)

// deriveSensitive folds the controller's own sensitivity verdict into one
// attribute body: a field cmd/sdk-bootstrap marked sensitive (its leaf wire
// name is declared for its struct's collection) is emitted with
// "sensitive": true, so the fact lives in the SDK's exported declaration and
// nowhere else. A hand-set "sensitive" on such a field is refused rather
// than silently shadowed, exactly as a hand validator of a derivable kind
// is: the duplicate would go stale the day the declaration moves. Hand
// sensitivity on a field the controller does NOT declare stays a policy
// decision and passes through untouched -- the provider may mark more than
// the controller does, never less.
//
// attribute is returned unchanged when the field carries no declaration.
func deriveSensitive(owner string, declared bool, attribute json.RawMessage) (json.RawMessage, error) {
	if !declared {
		return attribute, nil
	}
	body := map[string]json.RawMessage{}
	if len(attribute) > 0 {
		if err := json.Unmarshal(attribute, &body); err != nil {
			return nil, fmt.Errorf("attribute: %w", err)
		}
	}
	if _, present := body["sensitive"]; present {
		return nil, fmt.Errorf(
			"%s hand-sets \"sensitive\", but the controller itself declares the field "+
				"sensitive and the flag is derived from that declaration; delete the hand entry",
			owner)
	}
	body["sensitive"] = json.RawMessage("true")
	return json.Marshal(body)
}
