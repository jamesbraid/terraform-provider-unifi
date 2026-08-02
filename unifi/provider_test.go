package unifi

import (
	"context"
	"os"
	"testing"

	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-log/tflogtest"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/controllertest"
)

var providerFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"unifi": providerserver.NewProtocol6WithError(New()),
}

var testAccProtoV6ProviderFactories = providerFactories

var _ *unifi.ApiClient

func TestMain(m *testing.M) {
	if os.Getenv("TF_ACC") == "" {
		// short circuit non acceptance test runs
		os.Exit(m.Run())
	}

	// UNIFI_SKIP_CONTAINER bypasses docker-compose and uses pre-set UNIFI_* env vars.
	if os.Getenv("UNIFI_SKIP_CONTAINER") != "" {
		os.Exit(m.Run())
	}

	os.Exit(runAcceptanceTests(m))
}

func runAcceptanceTests(m *testing.M) int {
	// The provider's own Compose lifecycle does not want the Testcontainers
	// reaper. The herder child does, and gets it back: see herderChildEnv.
	if err := os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true"); err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(
		tflogtest.RootLogger(context.Background(), os.Stdout),
	)
	defer cancel()

	logger := NewLogger(ctx)

	controller, err := controllertest.Start(ctx, logger, "../docker-compose.yaml")
	// Stop unconditionally: Start returns a usable handle even when it fails
	// partway, and whatever it did bring up still has to come down.
	defer func() {
		if stopErr := controller.Stop(logger); stopErr != nil {
			panic(stopErr)
		}
	}()
	if err != nil {
		panic(err)
	}

	return m.Run()
}

func preCheck(t *testing.T) {
	variables := []string{
		"UNIFI_USERNAME",
		"UNIFI_PASSWORD",
		"UNIFI_API",
	}

	for _, variable := range variables {
		value := os.Getenv(variable)
		if value == "" {
			t.Fatalf("`%s` must be set for acceptance tests!", variable)
		}
	}
}

func TestClient_GetSiteName(t *testing.T) {
	tests := []struct {
		name string
		c    *Client
		want string
	}{
		{"default site", &Client{Site: "default"}, "default"},
		{"custom site", &Client{Site: "office"}, "office"},
		{"empty site", &Client{Site: ""}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.GetSiteName(); got != tt.want {
				t.Errorf("Client.GetSiteName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("New() returned nil")
	}
}

func Test_unifiProvider_Metadata(t *testing.T) {
	p := &unifiProvider{}
	resp := &fwprovider.MetadataResponse{}
	p.Metadata(context.Background(), fwprovider.MetadataRequest{}, resp)
	if resp.TypeName != "unifi" {
		t.Errorf("TypeName = %q, want %q", resp.TypeName, "unifi")
	}
}

func Test_unifiProvider_Schema(t *testing.T) {
	p := &unifiProvider{}
	resp := &fwprovider.SchemaResponse{}
	p.Schema(context.Background(), fwprovider.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("Schema() produced errors: %v", resp.Diagnostics)
	}
	for _, attr := range []string{"api_key", "username", "password", "api_url", "site", "allow_insecure"} {
		if _, ok := resp.Schema.Attributes[attr]; !ok {
			t.Errorf("missing attribute %q", attr)
		}
	}
}

func Test_unifiProvider_Resources(t *testing.T) {
	p := &unifiProvider{}
	got := p.Resources(context.Background())
	if len(got) == 0 {
		t.Error("Resources() returned empty slice")
	}
	for i, factory := range got {
		if r := factory(); r == nil {
			t.Errorf("Resources()[%d]() returned nil", i)
		}
	}
}

func Test_unifiProvider_DataSources(t *testing.T) {
	p := &unifiProvider{}
	got := p.DataSources(context.Background())
	if len(got) == 0 {
		t.Error("DataSources() returned empty slice")
	}
	for i, factory := range got {
		if ds := factory(); ds == nil {
			t.Errorf("DataSources()[%d]() returned nil", i)
		}
	}
}

func Test_unifiProvider_EphemeralResources(t *testing.T) {
	p := &unifiProvider{}
	_ = p.EphemeralResources(context.Background())
}

func Test_unifiProvider_Actions(t *testing.T) {
	p := &unifiProvider{}
	got := p.Actions(context.Background())
	if len(got) == 0 {
		t.Error("Actions() returned empty slice")
	}
	for i, factory := range got {
		if a := factory(); a == nil {
			t.Errorf("Actions()[%d]() returned nil", i)
		}
	}
}

func Test_unifiProvider_ListResources(t *testing.T) {
	p := &unifiProvider{}
	got := p.ListResources(context.Background())
	if len(got) == 0 {
		t.Error("ListResources() returned empty slice")
	}
	for i, factory := range got {
		if lr := factory(); lr == nil {
			t.Errorf("ListResources()[%d]() returned nil", i)
		}
	}
}
