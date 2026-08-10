package providercodegen

//go:generate go run ../cmd/sdk-bootstrap -package github.com/ubiquiti-community/go-unifi/unifi -struct FirewallPolicy -resource unifi_firewall_policy -commit a58839fe296859bbb0e91bd57efe54f9e954fe4e -output bootstrap/go-unifi-v1.103.0-firewall-policy.json
//go:generate go run ../cmd/sdk-bootstrap -package github.com/ubiquiti-community/go-unifi/unifi -struct FirewallZone -resource unifi_firewall_zone -commit a58839fe296859bbb0e91bd57efe54f9e954fe4e -output bootstrap/go-unifi-v1.103.0-firewall-zone.json
//go:generate go run ../cmd/sdk-bootstrap -package github.com/ubiquiti-community/go-unifi/unifi -struct PowerSupervisor -resource unifi_power_supervisor -commit a58839fe296859bbb0e91bd57efe54f9e954fe4e -output bootstrap/go-unifi-v1.103.0-power-supervisor.json
//go:generate go run ../cmd/sdk-bootstrap -package github.com/ubiquiti-community/go-unifi/unifi -struct WLAN -resource unifi_wlan -commit a58839fe296859bbb0e91bd57efe54f9e954fe4e -output bootstrap/go-unifi-v1.103.0-wlan.json
//go:generate go run ../cmd/catalog-parity -baseline ../build/m0/provider-schema-digests.json -schema ../provider-contracts/schema/terraform-1.15.8.json -status parity/status.json -migration migrations/v0.101.2-to-next.json -waves policy/catalog.json -output-dir generated
//go:generate go run ../cmd/provider-spec-compiler -catalog catalog/go-unifi-v1.102.0-dns-record.catalog.json -policy policy/dns_record.json -baseline ../build/m0/provider-schema-digests.json -ledger generated/catalog-parity-ledger.json -artifact-prefix dns_record -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/dns_record.provider-code-spec.json --output ../internal/generated/resource_dns_record --package resource_dns_record
//go:generate gofmt -w ../internal/generated/resource_dns_record/dns_record_resource_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-firewall-policy.json -policy policy/firewall_policy.json -baseline ../build/m0/provider-schema-digests.json -ledger generated/catalog-parity-ledger.json -artifact-prefix firewall_policy -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/firewall_policy.provider-code-spec.json --output ../internal/generated/resource_firewall_policy --package resource_firewall_policy
//go:generate gofmt -w ../internal/generated/resource_firewall_policy/firewall_policy_resource_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-power-supervisor.json -policy policy/power_supervisor.json -baseline ../build/m0/provider-schema-digests.json -ledger generated/catalog-parity-ledger.json -artifact-prefix power_supervisor -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/power_supervisor.provider-code-spec.json --output ../internal/generated/resource_power_supervisor --package resource_power_supervisor
//go:generate gofmt -w ../internal/generated/resource_power_supervisor/power_supervisor_resource_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-firewall-zone.json -policy policy/firewall_zone.json -baseline ../build/m0/provider-schema-digests.json -ledger generated/catalog-parity-ledger.json -artifact-prefix firewall_zone -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/firewall_zone.provider-code-spec.json --output ../internal/generated/resource_firewall_zone --package resource_firewall_zone
//go:generate gofmt -w ../internal/generated/resource_firewall_zone/firewall_zone_resource_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-wlan.json -policy policy/wlan.json -baseline ../build/m0/provider-schema-digests.json -ledger generated/catalog-parity-ledger.json -artifact-prefix wlan -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/wlan.provider-code-spec.json --output ../internal/generated/resource_wlan --package resource_wlan
//go:generate gofmt -w ../internal/generated/resource_wlan/wlan_resource_gen.go
//go:generate go run ../cmd/sdk-bootstrap -package github.com/ubiquiti-community/go-unifi/unifi -struct Site -resource unifi_site -commit a58839fe296859bbb0e91bd57efe54f9e954fe4e -output bootstrap/go-unifi-v1.103.0-site.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-site.json -policy policy/site.json -baseline ../build/m0/provider-schema-digests.json -ledger generated/catalog-parity-ledger.json -artifact-prefix site -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/site.provider-code-spec.json --output ../internal/generated/resource_site --package resource_site
//go:generate gofmt -w ../internal/generated/resource_site/site_resource_gen.go
