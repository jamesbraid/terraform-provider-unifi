package unifi

// guestAccessSecret and guestAccessPlain partition every field of
// settings.GuestAccess (go-unifi's generated struct for the guest_access
// settings section) into exactly two sets: secret, meaning the wire field
// carries the SDK's x_ prefix, and plain, meaning it does not. Task 0
// (.superpowers/sdd/plan-r2b-guest-access/task-0-report.md) measured all 18
// x_-prefixed fields live against the pinned controller and found every one
// echoes back verbatim on read -- no mask, no hash, no empty string, no
// absence -- and the 6 identifier-shaped fields among them (e.g.
// x_paypal_username) behave identically to the 12 genuine credentials. So
// there is no further split within the 18: secret-vs-plain is the only
// distinction this section's schema needs, and it tracks the x_ prefix
// exactly.
//
// This is the section's only field list. Nothing else may hardcode one --
// TestGuestAccessFieldSplitCoversTheStruct (setting_guest_access_fieldsplit_test.go)
// asserts these two maps partition settings.GuestAccess totally, so a field
// the SDK adds or renames fails loudly here instead of silently missing the
// schema.
var guestAccessSecret = map[string]bool{
	"AuthorizeLoginid":             true, // x_authorize_loginid
	"AuthorizeTransactionkey":      true, // x_authorize_transactionkey
	"FacebookAppSecret":            true, // x_facebook_app_secret
	"GoogleClientSecret":           true, // x_google_client_secret
	"IPpayTerminalid":              true, // x_ippay_terminalid
	"MerchantwarriorApikey":        true, // x_merchantwarrior_apikey
	"MerchantwarriorApipassphrase": true, // x_merchantwarrior_apipassphrase
	"MerchantwarriorMerchantuuid":  true, // x_merchantwarrior_merchantuuid
	"Password":                     true, // x_password
	"PaypalPassword":               true, // x_paypal_password
	"PaypalSignature":              true, // x_paypal_signature
	"PaypalUsername":               true, // x_paypal_username
	"QuickpayAgreementid":          true, // x_quickpay_agreementid
	"QuickpayApikey":               true, // x_quickpay_apikey
	"QuickpayMerchantid":           true, // x_quickpay_merchantid
	"StripeApiKey":                 true, // x_stripe_api_key
	"WechatAppSecret":              true, // x_wechat_app_secret
	"WechatSecretKey":              true, // x_wechat_secret_key
}

// guestAccessPlain is every settings.GuestAccess field not in
// guestAccessSecret. See guestAccessSecret's comment.
var guestAccessPlain = map[string]bool{
	"AllowedSubnet":                          true,
	"Auth":                                   true,
	"AuthUrl":                                true,
	"AuthorizeUseSandbox":                    true,
	"CustomIP":                               true,
	"EcEnabled":                              true,
	"Expire":                                 true,
	"ExpireNumber":                           true,
	"ExpireUnit":                             true,
	"FacebookAppID":                          true,
	"FacebookEnabled":                        true,
	"FacebookScopeEmail":                     true,
	"Gateway":                                true,
	"GoogleClientID":                         true,
	"GoogleDomain":                           true,
	"GoogleEnabled":                          true,
	"GoogleScopeEmail":                       true,
	"IPpayUseSandbox":                        true,
	"MerchantwarriorUseSandbox":              true,
	"PasswordEnabled":                        true,
	"PaymentEnabled":                         true,
	"PaypalUseSandbox":                       true,
	"PortalCustomized":                       true,
	"PortalCustomizedAuthenticationText":     true,
	"PortalCustomizedBgColor":                true,
	"PortalCustomizedBgImageEnabled":         true,
	"PortalCustomizedBgImageFilename":        true,
	"PortalCustomizedBgImageTile":            true,
	"PortalCustomizedBgType":                 true,
	"PortalCustomizedBoxColor":               true,
	"PortalCustomizedBoxLinkColor":           true,
	"PortalCustomizedBoxOpacity":             true,
	"PortalCustomizedBoxRADIUS":              true,
	"PortalCustomizedBoxTextColor":           true,
	"PortalCustomizedButtonColor":            true,
	"PortalCustomizedButtonText":             true,
	"PortalCustomizedButtonTextColor":        true,
	"PortalCustomizedLanguages":              true,
	"PortalCustomizedLinkColor":              true,
	"PortalCustomizedLogoEnabled":            true,
	"PortalCustomizedLogoFilename":           true,
	"PortalCustomizedLogoPosition":           true,
	"PortalCustomizedLogoSize":               true,
	"PortalCustomizedSuccessText":            true,
	"PortalCustomizedTextColor":              true,
	"PortalCustomizedTitle":                  true,
	"PortalCustomizedTos":                    true,
	"PortalCustomizedTosEnabled":             true,
	"PortalCustomizedUnsplashAuthorName":     true,
	"PortalCustomizedUnsplashAuthorUsername": true,
	"PortalCustomizedWelcomeText":            true,
	"PortalCustomizedWelcomeTextEnabled":     true,
	"PortalCustomizedWelcomeTextPosition":    true,
	"PortalEnabled":                          true,
	"PortalHostname":                         true,
	"PortalUseHostname":                      true,
	"QuickpayTestmode":                       true,
	"RADIUSAuthType":                         true,
	"RADIUSDisconnectEnabled":                true,
	"RADIUSDisconnectPort":                   true,
	"RADIUSEnabled":                          true,
	"RADIUSProfileID":                        true,
	"RedirectEnabled":                        true,
	"RedirectHttps":                          true,
	"RedirectToHttps":                        true,
	"RedirectUrl":                            true,
	"RestrictedDNSEnabled":                   true,
	"RestrictedDNSServers":                   true,
	"RestrictedSubnet":                       true,
	"VoucherCustomized":                      true,
	"VoucherEnabled":                         true,
	"WechatAppID":                            true,
	"WechatEnabled":                          true,
	"WechatShopID":                           true,
}
