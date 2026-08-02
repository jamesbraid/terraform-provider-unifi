package unifi

import (
	"context"

	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// settingsClient is the dependency-injection seam between the settings
// engine and the transport it runs over. Production code drives a
// realSettingsClient (below), which calls straight through to the embedded
// go-unifi *ApiClient promoted onto *Client. Tests drive a fakeSettingsClient
// (unifi/setting_fake_client_test.go) that keeps its sections in memory and
// supports fault injection, so the engine can be exercised without a live
// controller.
type settingsClient interface {
	// ListSettings returns every setting section configured on the site.
	ListSettings(ctx context.Context, site string) ([]settings.RawSetting, error)

	// UpdateRawSetting PUTs a single setting section back to the site. The
	// caller is responsible for merging any preserved/unmodeled fields into
	// s.Data before calling — RawSetting.MarshalJSON merges s.Data with the
	// modeled BaseSetting fields when producing the wire body.
	UpdateRawSetting(ctx context.Context, site string, s settings.RawSetting) error
}

// realSettingsClient adapts the provider's *Client (which embeds go-unifi's
// *ApiClient) to the settingsClient seam.
type realSettingsClient struct {
	c *Client
}

// ListSettings delegates to the embedded go-unifi ApiClient.
func (r realSettingsClient) ListSettings(ctx context.Context, site string) ([]settings.RawSetting, error) {
	return r.c.ListSettings(ctx, site)
}

// UpdateRawSetting delegates to the embedded go-unifi ApiClient. It passes a
// pointer, since *settings.RawSetting is what satisfies settings.Setting
// (via BaseSetting's GetKey/SetKey) and what RawSetting.MarshalJSON is
// defined on — the merged Data map only reaches the wire through the pointer
// receiver's MarshalJSON.
func (r realSettingsClient) UpdateRawSetting(ctx context.Context, site string, s settings.RawSetting) error {
	return r.c.UpdateSetting(ctx, site, &s)
}

// Compile-time assertion that realSettingsClient satisfies settingsClient.
var _ settingsClient = realSettingsClient{}
