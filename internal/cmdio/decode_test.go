package cmdio

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type probeDocument struct {
	Gate   string `json:"gate"`
	Result string `json:"result"`
}

func writeProbe(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "receipt.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestDecodeReturnsTheDigestOfTheBytesItRead pins that the digest is of the
// file, not of the re-encoded value: a digest taken after decoding would
// normalise field order and drop unknown keys, identifying something else.
func TestDecodeReturnsTheDigestOfTheBytesItRead(t *testing.T) {
	body := `{"gate":"g","result":"pass"}`
	path := writeProbe(t, body)

	var doc probeDocument
	digest, err := DecodeStrictFile(path, &doc)
	if err != nil {
		t.Fatalf("DecodeStrictFile() error = %v", err)
	}
	if doc.Gate != "g" || doc.Result != "pass" {
		t.Errorf("decoded %+v, want the document's values", doc)
	}
	sum := sha256.Sum256([]byte(body))
	if want := hex.EncodeToString(sum[:]); digest != want {
		t.Errorf("digest = %s, want the sha256 of the file's bytes %s", digest, want)
	}
}

// TestUnknownFieldIsRefused is the strictness property. A producer that adds a
// field the consumer's type does not declare must be an error here, because the
// alternative is the value being silently dropped and the pipeline continuing
// on a receipt that means something other than what was written.
func TestUnknownFieldIsRefused(t *testing.T) {
	path := writeProbe(t, `{"gate":"g","result":"pass","tree_state":{"status":"clean"}}`)
	var doc probeDocument
	if _, err := DecodeStrictFile(path, &doc); err == nil {
		t.Fatal("a document carrying a field the type does not declare was accepted; " +
			"that is exactly how a producer and a consumer drift apart unnoticed")
	}
}

// TestASecondJSONValueIsRefused covers the trailing-value check. Decode stops at
// the end of the first value, so without this a file holding two documents
// decodes the first and reports success.
func TestASecondJSONValueIsRefused(t *testing.T) {
	path := writeProbe(t, `{"gate":"g","result":"pass"}{"gate":"other","result":"fail"}`)
	var doc probeDocument
	_, err := DecodeStrictFile(path, &doc)
	if err == nil {
		t.Fatal("a file holding two JSON documents was accepted, and only the first was read")
	}
	if !strings.Contains(err.Error(), "multiple JSON values") {
		t.Errorf("refused, but not for the reason under test: %v", err)
	}
}

// TestTruncatedDocumentIsRefused is the control for the case above: the check
// must distinguish a second value from an incomplete first one.
func TestTruncatedDocumentIsRefused(t *testing.T) {
	path := writeProbe(t, `{"gate":"g","result":`)
	var doc probeDocument
	if _, err := DecodeStrictFile(path, &doc); err == nil {
		t.Fatal("a truncated document was accepted")
	}
}

// TestMissingFileIsAnError, because a gate reading a receipt that was never
// produced must not read as an empty document that happens to satisfy
// nothing.
func TestMissingFileIsAnError(t *testing.T) {
	var doc probeDocument
	if _, err := DecodeStrictFile(filepath.Join(t.TempDir(), "absent.json"), &doc); err == nil {
		t.Fatal("a missing file was accepted")
	}
}
