package providercodegen

// sdkbootstrap resolves go-unifi the way the build does -- through go.mod and
// its replace -- so a bump is one go.mod edit and nothing here can go stale.
//go:generate -command sdkbootstrap go run ../cmd/sdk-bootstrap -package github.com/ubiquiti-community/go-unifi/unifi
//go:generate sdkbootstrap -struct FirewallPolicy -resource unifi_firewall_policy -output bootstrap/go-unifi-v1.103.0-firewall-policy.json
//go:generate sdkbootstrap -struct FirewallZone -resource unifi_firewall_zone -output bootstrap/go-unifi-v1.103.0-firewall-zone.json
//go:generate sdkbootstrap -struct PowerSupervisor -resource unifi_power_supervisor -output bootstrap/go-unifi-v1.103.0-power-supervisor.json
//go:generate sdkbootstrap -struct WLAN -resource unifi_wlan -output bootstrap/go-unifi-v1.103.0-wlan.json
//go:generate sdkbootstrap -struct DNSRecord -resource unifi_dns_record -output bootstrap/go-unifi-v1.103.0-dns-record.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-dns-record.json -policy policy/dns_record.json -artifact-prefix dns_record -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/dns_record.provider-code-spec.json --output ../internal/generated/resource_dns_record --package resource_dns_record
//go:generate gofmt -w ../internal/generated/resource_dns_record/dns_record_resource_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-firewall-policy.json -policy policy/firewall_policy.json -artifact-prefix firewall_policy -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/firewall_policy.provider-code-spec.json --output ../internal/generated/resource_firewall_policy --package resource_firewall_policy
//go:generate gofmt -w ../internal/generated/resource_firewall_policy/firewall_policy_resource_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-power-supervisor.json -policy policy/power_supervisor.json -artifact-prefix power_supervisor -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/power_supervisor.provider-code-spec.json --output ../internal/generated/resource_power_supervisor --package resource_power_supervisor
//go:generate gofmt -w ../internal/generated/resource_power_supervisor/power_supervisor_resource_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-firewall-zone.json -policy policy/firewall_zone.json -artifact-prefix firewall_zone -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/firewall_zone.provider-code-spec.json --output ../internal/generated/resource_firewall_zone --package resource_firewall_zone
//go:generate gofmt -w ../internal/generated/resource_firewall_zone/firewall_zone_resource_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-wlan.json -policy policy/wlan.json -artifact-prefix wlan -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/wlan.provider-code-spec.json --output ../internal/generated/resource_wlan --package resource_wlan
//go:generate gofmt -w ../internal/generated/resource_wlan/wlan_resource_gen.go
//go:generate sdkbootstrap -struct Site -resource unifi_site -output bootstrap/go-unifi-v1.103.0-site.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-site.json -policy policy/site.json -artifact-prefix site -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/site.provider-code-spec.json --output ../internal/generated/resource_site --package resource_site
//go:generate gofmt -w ../internal/generated/resource_site/site_resource_gen.go
//go:generate sdkbootstrap -struct APGroup -resource unifi_ap_group -output bootstrap/go-unifi-v1.103.0-ap-group.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-ap-group.json -policy policy/ap_group.json -artifact-prefix ap_group -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/ap_group.provider-code-spec.json --output ../internal/generated/resource_ap_group --package resource_ap_group
//go:generate gofmt -w ../internal/generated/resource_ap_group/ap_group_resource_gen.go
//go:generate sdkbootstrap -struct FirewallGroup -resource unifi_firewall_group -output bootstrap/go-unifi-v1.103.0-firewall-group.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-firewall-group.json -policy policy/firewall_group.json -artifact-prefix firewall_group -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/firewall_group.provider-code-spec.json --output ../internal/generated/resource_firewall_group --package resource_firewall_group
//go:generate gofmt -w ../internal/generated/resource_firewall_group/firewall_group_resource_gen.go
//go:generate sdkbootstrap -struct ClientGroup -resource unifi_client_qos_rate -output bootstrap/go-unifi-v1.103.0-client-qos-rate.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-client-qos-rate.json -policy policy/client_qos_rate.json -artifact-prefix client_qos_rate -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/client_qos_rate.provider-code-spec.json --output ../internal/generated/resource_client_qos_rate --package resource_client_qos_rate
//go:generate gofmt -w ../internal/generated/resource_client_qos_rate/client_qos_rate_resource_gen.go
//go:generate sdkbootstrap -struct WireGuardPeer -resource unifi_wireguard_peer -output bootstrap/go-unifi-v1.103.0-wireguard-peer.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-wireguard-peer.json -policy policy/wireguard_peer.json -artifact-prefix wireguard_peer -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/wireguard_peer.provider-code-spec.json --output ../internal/generated/resource_wireguard_peer --package resource_wireguard_peer
//go:generate gofmt -w ../internal/generated/resource_wireguard_peer/wireguard_peer_resource_gen.go
//go:generate sdkbootstrap -struct DynamicDNS -resource unifi_dynamic_dns -output bootstrap/go-unifi-v1.103.0-dynamic-dns.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-dynamic-dns.json -policy policy/dynamic_dns.json -artifact-prefix dynamic_dns -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/dynamic_dns.provider-code-spec.json --output ../internal/generated/resource_dynamic_dns --package resource_dynamic_dns
//go:generate gofmt -w ../internal/generated/resource_dynamic_dns/dynamic_dns_resource_gen.go
//go:generate sdkbootstrap -struct Account -resource unifi_radius_user -output bootstrap/go-unifi-v1.103.0-radius-user.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-radius-user.json -policy policy/radius_user.json -artifact-prefix radius_user -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/radius_user.provider-code-spec.json --output ../internal/generated/resource_radius_user --package resource_radius_user
//go:generate gofmt -w ../internal/generated/resource_radius_user/radius_user_resource_gen.go
//go:generate sdkbootstrap -struct BGPConfig -resource unifi_bgp -output bootstrap/go-unifi-v1.103.0-bgp.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-bgp.json -policy policy/bgp.json -artifact-prefix bgp -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/bgp.provider-code-spec.json --output ../internal/generated/resource_bgp --package resource_bgp
//go:generate gofmt -w ../internal/generated/resource_bgp/bgp_resource_gen.go
//go:generate sdkbootstrap -struct Routing -resource unifi_static_route -output bootstrap/go-unifi-v1.103.0-static-route.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-static-route.json -policy policy/static_route.json -artifact-prefix static_route -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/static_route.provider-code-spec.json --output ../internal/generated/resource_static_route --package resource_static_route
//go:generate gofmt -w ../internal/generated/resource_static_route/static_route_resource_gen.go
//go:generate sdkbootstrap -struct RADIUSProfile -resource unifi_radius_profile -output bootstrap/go-unifi-v1.103.0-radius-profile.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-radius-profile.json -policy policy/radius_profile.json -artifact-prefix radius_profile -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/radius_profile.provider-code-spec.json --output ../internal/generated/resource_radius_profile --package resource_radius_profile
//go:generate gofmt -w ../internal/generated/resource_radius_profile/radius_profile_resource_gen.go
//go:generate sdkbootstrap -struct FirewallRule -resource unifi_firewall_rule -output bootstrap/go-unifi-v1.103.0-firewall-rule.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-firewall-rule.json -policy policy/firewall_rule.json -artifact-prefix firewall_rule -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/firewall_rule.provider-code-spec.json --output ../internal/generated/resource_firewall_rule --package resource_firewall_rule
//go:generate gofmt -w ../internal/generated/resource_firewall_rule/firewall_rule_resource_gen.go
//go:generate sdkbootstrap -struct PortProfile -resource unifi_port_profile -output bootstrap/go-unifi-v1.103.0-port-profile.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-port-profile.json -policy policy/port_profile.json -artifact-prefix port_profile -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/port_profile.provider-code-spec.json --output ../internal/generated/resource_port_profile --package resource_port_profile
//go:generate gofmt -w ../internal/generated/resource_port_profile/port_profile_resource_gen.go
//go:generate sdkbootstrap -struct Network -resource unifi_site_to_site_vpn -output bootstrap/go-unifi-v1.103.0-site-to-site-vpn.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-site-to-site-vpn.json -policy policy/site_to_site_vpn.json -artifact-prefix site_to_site_vpn -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/site_to_site_vpn.provider-code-spec.json --output ../internal/generated/resource_site_to_site_vpn --package resource_site_to_site_vpn
//go:generate gofmt -w ../internal/generated/resource_site_to_site_vpn/site_to_site_vpn_resource_gen.go
//go:generate sdkbootstrap -struct Network -resource unifi_wan -output bootstrap/go-unifi-v1.103.0-wan.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-wan.json -policy policy/wan.json -artifact-prefix wan -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/wan.provider-code-spec.json --output ../internal/generated/resource_wan --package resource_wan
//go:generate go run ../cmd/nested-type-dedup ../internal/generated/resource_wan/wan_resource_gen.go
//go:generate gofmt -w ../internal/generated/resource_wan/wan_resource_gen.go
//go:generate sdkbootstrap -struct APGroup -resource unifi_ap_group -output bootstrap/go-unifi-v1.103.0-ap-group-ds.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-ap-group-ds.json -policy policy/ap_group_ds.json -artifact-prefix ap_group_ds -output-dir generated
//go:generate go tool tfplugingen-framework generate data-sources --input generated/ap_group_ds.provider-code-spec.json --output ../internal/generated/datasource_ap_group --package datasource_ap_group
//go:generate gofmt -w ../internal/generated/datasource_ap_group/ap_group_ds_data_source_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-firewall-zone.json -policy policy/firewall_zone_list.json -artifact-prefix firewall_zone_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/firewall_zone_list.provider-code-spec.json --output ../internal/generated/listresource_firewall_zone --package listresource_firewall_zone
//go:generate sdkbootstrap -struct Client -struct ClientGroup -resource unifi_client -output bootstrap/go-unifi-v1.103.0-client.json
//go:generate sdkbootstrap -struct Client -struct ClientGroup -resource unifi_client -output bootstrap/go-unifi-v1.103.0-client-ds.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-client-ds.json -policy policy/client_ds.json -artifact-prefix client_ds -output-dir generated
//go:generate go tool tfplugingen-framework generate data-sources --input generated/client_ds.provider-code-spec.json --output ../internal/generated/datasource_client --package datasource_client
//go:generate gofmt -w ../internal/generated/datasource_client/client_ds_data_source_gen.go
//go:generate sdkbootstrap -struct Device -resource unifi_device -output bootstrap/go-unifi-v1.103.0-device.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-client.json -policy policy/client.json -artifact-prefix client -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/client.provider-code-spec.json --output ../internal/generated/resource_client --package resource_client
//go:generate gofmt -w ../internal/generated/resource_client/client_resource_gen.go
//go:generate sdkbootstrap -struct Network -resource unifi_network -output bootstrap/go-unifi-v1.103.0-network.json
//go:generate sdkbootstrap -struct Network -resource unifi_network -output bootstrap/go-unifi-v1.103.0-network-ds.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-network-ds.json -policy policy/network_ds.json -artifact-prefix network_ds -output-dir generated
//go:generate go tool tfplugingen-framework generate data-sources --input generated/network_ds.provider-code-spec.json --output ../internal/generated/datasource_network --package datasource_network
//go:generate gofmt -w ../internal/generated/datasource_network/network_ds_data_source_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-network.json -policy policy/network.json -artifact-prefix network -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/network.provider-code-spec.json --output ../internal/generated/resource_network --package resource_network
//go:generate gofmt -w ../internal/generated/resource_network/network_resource_gen.go
//go:generate sdkbootstrap -struct PortForward -resource unifi_port_forward -output bootstrap/go-unifi-v1.103.0-port-forward.json
//go:generate sdkbootstrap -struct TrafficRoute -resource unifi_traffic_route -output bootstrap/go-unifi-v1.103.0-traffic-route.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-traffic-route.json -policy policy/traffic_route.json -artifact-prefix traffic_route -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/traffic_route.provider-code-spec.json --output ../internal/generated/resource_traffic_route --package resource_traffic_route
//go:generate gofmt -w ../internal/generated/resource_traffic_route/traffic_route_resource_gen.go
//go:generate sdkbootstrap -struct Network -resource unifi_vpn_client -output bootstrap/go-unifi-v1.103.0-vpn-client.json
//go:generate sdkbootstrap -struct Network -resource unifi_vpn_server -output bootstrap/go-unifi-v1.103.0-vpn-server.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-vpn-server.json -policy policy/vpn_server.json -artifact-prefix vpn_server -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/vpn_server.provider-code-spec.json --output ../internal/generated/resource_vpn_server --package resource_vpn_server
//go:generate gofmt -w ../internal/generated/resource_vpn_server/vpn_server_resource_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-vpn-client.json -policy policy/vpn_client.json -artifact-prefix vpn_client -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/vpn_client.provider-code-spec.json --output ../internal/generated/resource_vpn_client --package resource_vpn_client
//go:generate gofmt -w ../internal/generated/resource_vpn_client/vpn_client_resource_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-port-forward.json -policy policy/port_forward.json -artifact-prefix port_forward -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/port_forward.provider-code-spec.json --output ../internal/generated/resource_port_forward --package resource_port_forward
//go:generate gofmt -w ../internal/generated/resource_port_forward/port_forward_resource_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-device.json -policy policy/device.json -artifact-prefix device -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/device.provider-code-spec.json --output ../internal/generated/resource_device --package resource_device
//go:generate gofmt -w ../internal/generated/resource_device/device_resource_gen.go
//go:generate sdkbootstrap -package github.com/ubiquiti-community/go-unifi/unifi/settings -struct Usg -struct Rsyslogd -struct Ips -struct Lcm -struct Radius -struct Doh -struct Dpi -struct Mgmt -struct Ntp -struct Country -struct AutoSpeedtest -struct IgmpSnooping -struct NetworkOptimization -struct IpsSuppression -struct SettingUsgGeoIPFiltering -struct Locale -struct GlobalNat -struct SslInspection -struct Ipsec -struct Dashboard -struct EtherLighting -struct GlobalNetwork -struct TrafficFlow -struct Mdns -struct Teleport -struct MagicSiteToSiteVpn -resource unifi_setting -output bootstrap/go-unifi-v1.103.0-setting.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-setting.json -policy policy/setting.json -artifact-prefix setting -output-dir generated -emit-grouping-mappings
//go:generate go tool tfplugingen-framework generate resources --input generated/setting.provider-code-spec.json --output ../internal/generated/resource_setting --package resource_setting
//go:generate gofmt -w ../internal/generated/resource_setting/setting_resource_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-ap-group.json -policy policy/ap_group_list.json -artifact-prefix ap_group_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/ap_group_list.provider-code-spec.json --output ../internal/generated/listresource_ap_group --package listresource_ap_group
//go:generate sdkbootstrap -struct Client -resource unifi_client -output bootstrap/go-unifi-v1.103.0-client-list.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-client-list.json -policy policy/client_list.json -artifact-prefix client_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/client_list.provider-code-spec.json --output ../internal/generated/listresource_client --package listresource_client
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-client-qos-rate.json -policy policy/client_qos_rate_list.json -artifact-prefix client_qos_rate_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/client_qos_rate_list.provider-code-spec.json --output ../internal/generated/listresource_client_qos_rate --package listresource_client_qos_rate
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-device.json -policy policy/device_list.json -artifact-prefix device_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/device_list.provider-code-spec.json --output ../internal/generated/listresource_device --package listresource_device
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-dynamic-dns.json -policy policy/dynamic_dns_list.json -artifact-prefix dynamic_dns_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/dynamic_dns_list.provider-code-spec.json --output ../internal/generated/listresource_dynamic_dns --package listresource_dynamic_dns
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-firewall-group.json -policy policy/firewall_group_list.json -artifact-prefix firewall_group_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/firewall_group_list.provider-code-spec.json --output ../internal/generated/listresource_firewall_group --package listresource_firewall_group
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-firewall-policy.json -policy policy/firewall_policy_list.json -artifact-prefix firewall_policy_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/firewall_policy_list.provider-code-spec.json --output ../internal/generated/listresource_firewall_policy --package listresource_firewall_policy
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-firewall-rule.json -policy policy/firewall_rule_list.json -artifact-prefix firewall_rule_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/firewall_rule_list.provider-code-spec.json --output ../internal/generated/listresource_firewall_rule --package listresource_firewall_rule
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-network.json -policy policy/network_list.json -artifact-prefix network_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/network_list.provider-code-spec.json --output ../internal/generated/listresource_network --package listresource_network
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-port-forward.json -policy policy/port_forward_list.json -artifact-prefix port_forward_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/port_forward_list.provider-code-spec.json --output ../internal/generated/listresource_port_forward --package listresource_port_forward
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-port-profile.json -policy policy/port_profile_list.json -artifact-prefix port_profile_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/port_profile_list.provider-code-spec.json --output ../internal/generated/listresource_port_profile --package listresource_port_profile
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-power-supervisor.json -policy policy/power_supervisor_list.json -artifact-prefix power_supervisor_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/power_supervisor_list.provider-code-spec.json --output ../internal/generated/listresource_power_supervisor --package listresource_power_supervisor
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-radius-profile.json -policy policy/radius_profile_list.json -artifact-prefix radius_profile_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/radius_profile_list.provider-code-spec.json --output ../internal/generated/listresource_radius_profile --package listresource_radius_profile
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-radius-user.json -policy policy/radius_user_list.json -artifact-prefix radius_user_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/radius_user_list.provider-code-spec.json --output ../internal/generated/listresource_radius_user --package listresource_radius_user
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-site.json -policy policy/site_list.json -artifact-prefix site_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/site_list.provider-code-spec.json --output ../internal/generated/listresource_site --package listresource_site
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-site-to-site-vpn.json -policy policy/site_to_site_vpn_list.json -artifact-prefix site_to_site_vpn_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/site_to_site_vpn_list.provider-code-spec.json --output ../internal/generated/listresource_site_to_site_vpn --package listresource_site_to_site_vpn
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-static-route.json -policy policy/static_route_list.json -artifact-prefix static_route_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/static_route_list.provider-code-spec.json --output ../internal/generated/listresource_static_route --package listresource_static_route
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-traffic-route.json -policy policy/traffic_route_list.json -artifact-prefix traffic_route_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/traffic_route_list.provider-code-spec.json --output ../internal/generated/listresource_traffic_route --package listresource_traffic_route
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-vpn-client.json -policy policy/vpn_client_list.json -artifact-prefix vpn_client_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/vpn_client_list.provider-code-spec.json --output ../internal/generated/listresource_vpn_client --package listresource_vpn_client
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-vpn-server.json -policy policy/vpn_server_list.json -artifact-prefix vpn_server_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/vpn_server_list.provider-code-spec.json --output ../internal/generated/listresource_vpn_server --package listresource_vpn_server
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-wan.json -policy policy/wan_list.json -artifact-prefix wan_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/wan_list.provider-code-spec.json --output ../internal/generated/listresource_wan --package listresource_wan
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-wireguard-peer.json -policy policy/wireguard_peer_list.json -artifact-prefix wireguard_peer_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/wireguard_peer_list.provider-code-spec.json --output ../internal/generated/listresource_wireguard_peer --package listresource_wireguard_peer
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-wlan.json -policy policy/wlan_list.json -artifact-prefix wlan_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/wlan_list.provider-code-spec.json --output ../internal/generated/listresource_wlan --package listresource_wlan
//go:generate sdkbootstrap -struct Device -resource unifi_port -output bootstrap/go-unifi-v1.103.0-port.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-port.json -policy policy/port_action.json -artifact-prefix port_action -output-dir generated
//go:generate go run ../cmd/action-gen --input generated/port_action.provider-code-spec.json --output ../internal/generated/action_port --package action_port
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-dns-record.json -policy policy/dns_record_list.json -artifact-prefix dns_record_list -output-dir generated
//go:generate go run ../cmd/list-resource-gen --input generated/dns_record_list.provider-code-spec.json --output ../internal/generated/listresource_dns_record --package listresource_dns_record
//go:generate sdkbootstrap -struct FirewallZone -resource unifi_firewall_zone -output bootstrap/go-unifi-v1.103.0-firewall-zone-ds.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-firewall-zone-ds.json -policy policy/firewall_zone_ds.json -artifact-prefix firewall_zone_ds -output-dir generated
//go:generate go tool tfplugingen-framework generate data-sources --input generated/firewall_zone_ds.provider-code-spec.json --output ../internal/generated/datasource_firewall_zone --package datasource_firewall_zone
//go:generate gofmt -w ../internal/generated/datasource_firewall_zone/firewall_zone_ds_data_source_gen.go
//go:generate sdkbootstrap -struct Account -resource unifi_radius_user -output bootstrap/go-unifi-v1.103.0-radius-user-ds.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-radius-user-ds.json -policy policy/radius_user_ds.json -artifact-prefix radius_user_ds -output-dir generated
//go:generate go tool tfplugingen-framework generate data-sources --input generated/radius_user_ds.provider-code-spec.json --output ../internal/generated/datasource_radius_user --package datasource_radius_user
//go:generate gofmt -w ../internal/generated/datasource_radius_user/radius_user_ds_data_source_gen.go
//go:generate sdkbootstrap -struct ClientGroup -resource unifi_client_qos_rate -output bootstrap/go-unifi-v1.103.0-client-qos-rate-ds.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-client-qos-rate-ds.json -policy policy/client_qos_rate_ds.json -artifact-prefix client_qos_rate_ds -output-dir generated
//go:generate go tool tfplugingen-framework generate data-sources --input generated/client_qos_rate_ds.provider-code-spec.json --output ../internal/generated/datasource_client_qos_rate --package datasource_client_qos_rate
//go:generate gofmt -w ../internal/generated/datasource_client_qos_rate/client_qos_rate_ds_data_source_gen.go
//go:generate sdkbootstrap -struct DNSRecord -resource unifi_dns_record -output bootstrap/go-unifi-v1.103.0-dns-record-ds.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-dns-record-ds.json -policy policy/dns_record_ds.json -artifact-prefix dns_record_ds -output-dir generated
//go:generate go tool tfplugingen-framework generate data-sources --input generated/dns_record_ds.provider-code-spec.json --output ../internal/generated/datasource_dns_record --package datasource_dns_record
//go:generate gofmt -w ../internal/generated/datasource_dns_record/dns_record_ds_data_source_gen.go
//go:generate sdkbootstrap -struct PortProfile -resource unifi_port_profile -output bootstrap/go-unifi-v1.103.0-port-profile-ds.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-port-profile-ds.json -policy policy/port_profile_ds.json -artifact-prefix port_profile_ds -output-dir generated
//go:generate go tool tfplugingen-framework generate data-sources --input generated/port_profile_ds.provider-code-spec.json --output ../internal/generated/datasource_port_profile --package datasource_port_profile
//go:generate gofmt -w ../internal/generated/datasource_port_profile/port_profile_ds_data_source_gen.go
//go:generate sdkbootstrap -struct RADIUSProfile -resource unifi_radius_profile -output bootstrap/go-unifi-v1.103.0-radius-profile-ds.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-radius-profile-ds.json -policy policy/radius_profile_ds.json -artifact-prefix radius_profile_ds -output-dir generated
//go:generate go tool tfplugingen-framework generate data-sources --input generated/radius_profile_ds.provider-code-spec.json --output ../internal/generated/datasource_radius_profile --package datasource_radius_profile
//go:generate gofmt -w ../internal/generated/datasource_radius_profile/radius_profile_ds_data_source_gen.go
//go:generate sdkbootstrap -struct ClientInfo -resource unifi_client_info -output bootstrap/go-unifi-v1.103.0-client-info-ds.json
//go:generate sdkbootstrap -struct ClientInfo -resource unifi_client_info_list -output bootstrap/go-unifi-v1.103.0-client-info-list-ds.json
//go:generate sdkbootstrap -struct Client -struct ClientInfo -resource unifi_client_list -output bootstrap/go-unifi-v1.103.0-client-list-ds.json
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-client-list-ds.json -policy policy/client_list_ds.json -artifact-prefix client_list_ds -output-dir generated
//go:generate go tool tfplugingen-framework generate data-sources --input generated/client_list_ds.provider-code-spec.json --output ../internal/generated/datasource_client_list --package datasource_client_list
//go:generate gofmt -w ../internal/generated/datasource_client_list/client_list_ds_data_source_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-client-info-list-ds.json -policy policy/client_info_list_ds.json -artifact-prefix client_info_list_ds -output-dir generated
//go:generate go tool tfplugingen-framework generate data-sources --input generated/client_info_list_ds.provider-code-spec.json --output ../internal/generated/datasource_client_info_list --package datasource_client_info_list
//go:generate gofmt -w ../internal/generated/datasource_client_info_list/client_info_list_ds_data_source_gen.go
//go:generate go run ../cmd/provider-spec-compiler -bootstrap bootstrap/go-unifi-v1.103.0-client-info-ds.json -policy policy/client_info_ds.json -artifact-prefix client_info_ds -output-dir generated
//go:generate go tool tfplugingen-framework generate data-sources --input generated/client_info_ds.provider-code-spec.json --output ../internal/generated/datasource_client_info --package datasource_client_info
//go:generate gofmt -w ../internal/generated/datasource_client_info/client_info_ds_data_source_gen.go

// Runs once, after every generated package above exists -- new sdkbootstrap/generate
// lines must go before this.
//go:generate go run ../cmd/nested-custom-type-strip ../internal/generated

// Must run after nested-custom-type-strip: that pass removes the only remaining
// references to the generated value types, so running this first would break the build.
//go:generate go run ../cmd/generated-value-strip ../internal/generated
