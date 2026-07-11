package unifi

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func Test_rawValueHelpers(t *testing.T) {
	data := map[string]any{
		"str":       "value",
		"empty_str": "",
		"flag":      true,
		"num":       float64(480), // encoding/json decodes numbers to float64
		"num_str":   "42",
		"langs":     []any{"en", "de"},
	}

	if got := rawString(data, "str"); got.ValueString() != "value" {
		t.Fatalf("rawString(str) = %v", got)
	}
	if !rawString(data, "empty_str").IsNull() {
		t.Fatal("rawString should map empty string to null")
	}
	if !rawString(data, "missing").IsNull() {
		t.Fatal("rawString should map missing key to null")
	}
	if got := rawBool(data, "flag"); !got.ValueBool() {
		t.Fatalf("rawBool(flag) = %v", got)
	}
	if !rawBool(data, "missing").IsNull() {
		t.Fatal("rawBool should map missing key to null")
	}
	if got := rawInt(data, "num"); got.ValueInt64() != 480 {
		t.Fatalf("rawInt(num) = %v", got)
	}
	if got := rawInt(data, "num_str"); got.ValueInt64() != 42 {
		t.Fatalf("rawInt(num_str) = %v", got)
	}
	if !rawInt(data, "missing").IsNull() {
		t.Fatal("rawInt should map missing key to null")
	}
	langs := rawStringList(data, "langs")
	if langs.IsNull() || len(langs.Elements()) != 2 {
		t.Fatalf("rawStringList(langs) = %v", langs)
	}
	if !rawStringList(data, "missing").IsNull() {
		t.Fatal("rawStringList should map missing key to null")
	}
	if !anyRawKey(data, "nope", "flag") || anyRawKey(data, "nope") {
		t.Fatal("anyRawKey wrong")
	}
}

func Test_setRawHelpers(t *testing.T) {
	data := map[string]any{"keep": "keep"}

	setRawString(data, "s", types.StringValue("v"))
	setRawString(data, "s_null", types.StringNull())
	setRawBool(data, "b", types.BoolValue(true))
	setRawBool(data, "b_null", types.BoolNull())
	setRawInt(data, "i", types.Int64Value(7))
	setRawInt(data, "i_null", types.Int64Null())

	if data["s"] != "v" || data["b"] != true || data["i"] != int64(7) {
		t.Fatalf("set values wrong: %v", data)
	}
	for _, k := range []string{"s_null", "b_null", "i_null"} {
		if _, present := data[k]; present {
			t.Fatalf("null value wrote key %q", k)
		}
	}
	if data["keep"] != "keep" {
		t.Fatal("unrelated key clobbered")
	}
}
