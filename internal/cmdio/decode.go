package cmdio

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DecodeStrictFile reads a JSON document into value, refusing anything the
// type does not declare, and returns the sha256 of the bytes it read.
//
// The strictness is deliberate and not configurable: DisallowUnknownFields
// turns an unrecognized field from a silent drop into a refusal. The
// trailing-value check is load-bearing too -- Decode stops at the end of
// the first JSON value, so reading again and requiring io.EOF is what makes
// a truncated or doubled file an error rather than a partial success.
func DecodeStrictFile(path string, value any) (string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return "", err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return "", fmt.Errorf("multiple JSON values")
		}
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
