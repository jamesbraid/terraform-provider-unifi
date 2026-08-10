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
//go:generate go run ../cmd/sdk-bootstrap -package github.com/ubiquiti-community/go-unifi/unifi -struct APGroup -resource unifi_ap_group -commit a58839fe296859bbb0e91bd57efe54f9e954fe4e -output bootstrap/go-unifi-v1.103.0-ap-group.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-ap-group.json -policy policy/ap_group.json -baseline ../build/m0/provider-schema-digests.json -ledger generated/catalog-parity-ledger.json -artifact-prefix ap_group -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/ap_group.provider-code-spec.json --output ../internal/generated/resource_ap_group --package resource_ap_group
//go:generate gofmt -w ../internal/generated/resource_ap_group/ap_group_resource_gen.go
//go:generate go run ../cmd/sdk-bootstrap -package github.com/ubiquiti-community/go-unifi/unifi -struct FirewallGroup -resource unifi_firewall_group -commit a58839fe296859bbb0e91bd57efe54f9e954fe4e -output bootstrap/go-unifi-v1.103.0-firewall-group.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-firewall-group.json -policy policy/firewall_group.json -baseline ../build/m0/provider-schema-digests.json -ledger generated/catalog-parity-ledger.json -artifact-prefix firewall_group -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/firewall_group.provider-code-spec.json --output ../internal/generated/resource_firewall_group --package resource_firewall_group
//go:generate gofmt -w ../internal/generated/resource_firewall_group/firewall_group_resource_gen.go
//go:generate go run ../cmd/sdk-bootstrap -package github.com/ubiquiti-community/go-unifi/unifi -struct ClientGroup -resource unifi_client_qos_rate -commit a58839fe296859bbb0e91bd57efe54f9e954fe4e -output bootstrap/go-unifi-v1.103.0-client-qos-rate.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-client-qos-rate.json -policy policy/client_qos_rate.json -baseline ../build/m0/provider-schema-digests.json -ledger generated/catalog-parity-ledger.json -artifact-prefix client_qos_rate -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/client_qos_rate.provider-code-spec.json --output ../internal/generated/resource_client_qos_rate --package resource_client_qos_rate
//go:generate gofmt -w ../internal/generated/resource_client_qos_rate/client_qos_rate_resource_gen.go
//go:generate go run ../cmd/sdk-bootstrap -package github.com/ubiquiti-community/go-unifi/unifi -struct WireGuardPeer -resource unifi_wireguard_peer -commit a58839fe296859bbb0e91bd57efe54f9e954fe4e -output bootstrap/go-unifi-v1.103.0-wireguard-peer.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-wireguard-peer.json -policy policy/wireguard_peer.json -baseline ../build/m0/provider-schema-digests.json -ledger generated/catalog-parity-ledger.json -artifact-prefix wireguard_peer -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/wireguard_peer.provider-code-spec.json --output ../internal/generated/resource_wireguard_peer --package resource_wireguard_peer
//go:generate gofmt -w ../internal/generated/resource_wireguard_peer/wireguard_peer_resource_gen.go
