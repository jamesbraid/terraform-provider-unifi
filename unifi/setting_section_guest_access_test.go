package unifi

import (
	"reflect"
	"testing"

	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// TestGuestAccessFieldSplit pins the spec's modeled/secret/preserved split
// against the live go-unifi struct so a future go-unifi bump that adds,
// removes, or renames a GuestAccess field fails here first, not as a
// silent schema gap. See
// docs/superpowers/specs/2026-07-13-pr-b4-guest-access-design.md
// (rev. 2: 56 modeled / 18 secret / 41 preserved).
func TestGuestAccessFieldSplit(t *testing.T) {
	typ := reflect.TypeOf(settings.GuestAccess{})
	all := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Anonymous { // BaseSetting
			continue
		}
		all[f.Name] = true
	}
	if len(all) != 97 {
		t.Fatalf("settings.GuestAccess has %d fields, want 97 — go-unifi drifted; reconcile the spec before proceeding", len(all))
	}

	// wantModeled: the 56 Go field names from the spec's rev. 2 "Modeled"
	// table, including the four rev. 2 promotions (AuthUrl, EcEnabled,
	// CustomIP, RedirectHttps).
	wantModeled := map[string]bool{
		"Auth":                         true,
		"AuthUrl":                      true,
		"PortalEnabled":                true,
		"PortalUseHostname":            true,
		"PortalHostname":               true,
		"CustomIP":                     true,
		"EcEnabled":                    true,
		"Expire":                       true,
		"ExpireNumber":                 true,
		"ExpireUnit":                   true,
		"RedirectEnabled":              true,
		"RedirectUrl":                  true,
		"RedirectToHttps":              true,
		"RedirectHttps":                true,
		"AllowedSubnet":                true,
		"RestrictedSubnet":             true,
		"RestrictedDNSEnabled":         true,
		"RestrictedDNSServers":         true,
		"PasswordEnabled":              true,
		"Password":                     true,
		"VoucherEnabled":               true,
		"RADIUSEnabled":                true,
		"RADIUSProfileID":              true,
		"RADIUSAuthType":               true,
		"RADIUSDisconnectEnabled":      true,
		"RADIUSDisconnectPort":         true,
		"FacebookEnabled":              true,
		"FacebookAppID":                true,
		"FacebookAppSecret":            true,
		"GoogleEnabled":                true,
		"GoogleClientID":               true,
		"GoogleClientSecret":           true,
		"WechatEnabled":                true,
		"WechatAppID":                  true,
		"WechatAppSecret":              true,
		"WechatSecretKey":              true,
		"PaymentEnabled":               true,
		"Gateway":                      true,
		"PaypalUsername":               true,
		"PaypalPassword":               true,
		"PaypalSignature":              true,
		"PaypalUseSandbox":             true,
		"StripeApiKey":                 true,
		"AuthorizeLoginid":             true,
		"AuthorizeTransactionkey":      true,
		"AuthorizeUseSandbox":          true,
		"QuickpayMerchantid":           true,
		"QuickpayApikey":               true,
		"QuickpayAgreementid":          true,
		"QuickpayTestmode":             true,
		"MerchantwarriorMerchantuuid":  true,
		"MerchantwarriorApikey":        true,
		"MerchantwarriorApipassphrase": true,
		"MerchantwarriorUseSandbox":    true,
		"IPpayTerminalid":              true,
		"IPpayUseSandbox":              true,
	}

	// wantSecret: the 18 modeled leaves that are WriteOnlySecret-class
	// (Optional+Computed+Sensitive), a subset of wantModeled.
	wantSecret := map[string]bool{
		"Password":                     true,
		"FacebookAppSecret":            true,
		"GoogleClientSecret":           true,
		"WechatAppSecret":              true,
		"WechatSecretKey":              true,
		"PaypalUsername":               true,
		"PaypalPassword":               true,
		"PaypalSignature":              true,
		"StripeApiKey":                 true,
		"AuthorizeLoginid":             true,
		"AuthorizeTransactionkey":      true,
		"QuickpayMerchantid":           true,
		"QuickpayApikey":               true,
		"QuickpayAgreementid":          true,
		"MerchantwarriorMerchantuuid":  true,
		"MerchantwarriorApikey":        true,
		"MerchantwarriorApipassphrase": true,
		"IPpayTerminalid":              true,
	}

	if len(wantModeled) != 56 {
		t.Fatalf("wantModeled has %d entries, want 56", len(wantModeled))
	}
	if len(wantSecret) != 18 {
		t.Fatalf("wantSecret has %d entries, want 18", len(wantSecret))
	}
	for name := range wantSecret {
		if !wantModeled[name] {
			t.Errorf("wantSecret contains %q which is not in wantModeled", name)
		}
	}
	for name := range wantModeled {
		if !all[name] {
			t.Errorf("wantModeled contains %q which is not a settings.GuestAccess field", name)
		}
	}
	preserved := 0
	for name := range all {
		if !wantModeled[name] {
			preserved++
		}
	}
	if preserved != 41 {
		t.Errorf("preserved field count = %d, want 41", preserved)
	}

	// FacebookWifiGwSecret is the 19th credential-like field, deliberately
	// preserved (not modeled) — see spec Key Decision 1. Assert it explicitly
	// so a future accidental promotion/demotion is caught here too.
	if wantModeled["FacebookWifiGwSecret"] {
		t.Error("FacebookWifiGwSecret must stay preserved, not modeled (spec Key Decision 1)")
	}
	if wantSecret["FacebookWifiGwSecret"] {
		t.Error("FacebookWifiGwSecret must not be in wantSecret")
	}
}
