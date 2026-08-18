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

// DecodeStrictFile reads a JSON document into value, refusing anything the type
// does not declare, and returns the sha256 of the bytes it read.
//
// SEVEN COPIES, ONE BEHAVIOUR, TWO SIGNATURES. Unlike WriteAtomic, the copies of
// this did not drift: all seven read the file, decode with DisallowUnknownFields,
// refuse a second JSON value, and differ only in that six return the digest and
// one returns just an error. There is nothing to parameterise -- the single
// caller that ignored the digest can keep ignoring it.
//
// THE STRICTNESS IS THE POINT AND IT IS NOT OPTIONAL. A producer that adds a
// field a consumer does not know about is #162, and DisallowUnknownFields is
// what turns that from a silent drop into a refusal. There is deliberately no
// option to relax it.
//
// THE TRAILING-VALUE CHECK IS ALSO LOAD-BEARING. Decode stops at the end of the
// first JSON value, so a file holding two documents would decode the first and
// ignore the rest. Reading again and requiring io.EOF is what makes a truncated
// or doubled artifact an error rather than a partial success.
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
