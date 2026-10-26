package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// upgradeClientIdentity sends a stored client identity through the provider's
// UpgradeResourceIdentity RPC, the call core makes when the stored identity
// version is older than the one the provider serves.
func upgradeClientIdentity(t *testing.T, version int64, raw string) map[string]tftypes.Value {
	t.Helper()
	ctx := context.Background()
	server, err := providerFactories["unifi"]()
	if err != nil {
		t.Fatalf("provider server: %v", err)
	}
	schemas, err := server.GetResourceIdentitySchemas(ctx, &tfprotov6.GetResourceIdentitySchemasRequest{})
	if err != nil {
		t.Fatalf("identity schemas: %v", err)
	}
	identitySchema := schemas.IdentitySchemas["unifi_client"]
	resp, err := server.UpgradeResourceIdentity(ctx, &tfprotov6.UpgradeResourceIdentityRequest{
		TypeName:    "unifi_client",
		Version:     version,
		RawIdentity: &tfprotov6.RawState{JSON: []byte(raw)},
	})
	if err != nil {
		t.Fatalf("UpgradeResourceIdentity: %v", err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("UpgradeResourceIdentity: %s: %s", d.Summary, d.Detail)
		}
	}
	objectType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}
	for _, attr := range identitySchema.IdentityAttributes {
		objectType.AttributeTypes[attr.Name] = attr.Type
	}
	value, err := resp.UpgradedIdentity.IdentityData.Unmarshal(objectType)
	if err != nil {
		t.Fatalf("decode upgraded identity: %v", err)
	}
	attrs := map[string]tftypes.Value{}
	if err := value.As(&attrs); err != nil {
		t.Fatalf("upgraded identity is not an object: %v", err)
	}
	return attrs
}

func stringOrNull(t *testing.T, v tftypes.Value) *string {
	t.Helper()
	if v.IsNull() {
		return nil
	}
	var s string
	if err := v.As(&s); err != nil {
		t.Fatalf("not a string: %v", err)
	}
	return &s
}

// 0.101.2 stored a client's identity as mac alone; later releases added id
// without bumping the identity version, so core could no longer decode the
// stored identity and failed every plan with `attribute "id" is required`.
func TestClientIdentityUpgradeAcceptsBothVersionZeroShapes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		raw    string
		wantID *string
	}{
		{"mac alone, as 0.101.2 stored it", `{"mac":"00:04:4b:f4:07:64"}`, nil},
		{
			"id and mac, as later releases stored it",
			`{"id":"6a8b3cd94c934471f6b6ff20","mac":"00:04:4b:f4:07:64"}`,
			new("6a8b3cd94c934471f6b6ff20"),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			attrs := upgradeClientIdentity(t, 0, testCase.raw)
			if mac := stringOrNull(t, attrs["mac"]); mac == nil || *mac != "00:04:4b:f4:07:64" {
				t.Errorf("mac = %v, want it carried over", mac)
			}
			id := stringOrNull(t, attrs["id"])
			switch {
			case testCase.wantID == nil && id != nil:
				t.Errorf("id = %q, want null: the upgrade has nothing to derive it from", *id)
			case testCase.wantID != nil && (id == nil || *id != *testCase.wantID):
				t.Errorf("id = %v, want %q", id, *testCase.wantID)
			}
		})
	}
}
