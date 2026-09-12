package main

import "strings"

// initialisms is how a tfsdk tag part is cased inside a model member name.
// One canonical spelling per part, applied uniformly: where a hand model
// spelled the same part two ways (RADIUSMacAuthEnabled beside
// RadiusProfileID), migration renames the outlier to the derived form.
var initialisms = map[string]string{
	"2g": "2G", "5g": "5G", "6e": "6E", "6g": "6G",
	"ap": "AP", "arp": "ARP", "as": "AS", "asn": "ASN",
	"dhcp": "DHCP", "dhcpd": "DHCPD", "dhcpv6": "DHCPv6", "dns": "DNS",
	"esp": "ESP", "geo": "Geo", "http": "HTTP", "https": "HTTPS",
	"icmp": "ICMP", "id": "ID", "ids": "IDs", "igmp": "IGMP",
	"ike": "IKE", "ip": "IP", "ips": "IPs", "ipsec": "IPsec",
	"ipv4": "IPv4", "ipv6": "IPv6", "l2tp": "L2TP", "lan": "LAN",
	"mac": "MAC", "mtu": "MTU", "nat": "NAT", "ntp": "NTP",
	"oui": "OUI", "pd": "PD", "pfs": "PFS", "pmf": "PMF",
	"qos": "QoS", "ra": "RA", "radius": "RADIUS", "snmp": "SNMP",
	"ssh": "SSH", "ssid": "SSID", "ssl": "SSL", "stp": "STP",
	"tcp": "TCP", "tftp": "TFTP", "tls": "TLS", "ttl": "TTL",
	"udp": "UDP", "upnp": "UPnP", "url": "URL", "utc": "UTC",
	"vlan": "VLAN", "vpn": "VPN", "wan": "WAN", "wlan": "WLAN",
	"wo": "WO", "wpa": "WPA", "wpa3": "WPA3",
}

func tagParts(tag string) []string {
	return strings.FieldsFunc(tag, func(r rune) bool { return r == '_' || r == '-' })
}

// camel derives a model member name from a tfsdk tag: initialisms cased per
// the table, everything else title-cased.
func camel(tag string) string {
	var b strings.Builder
	for _, part := range tagParts(tag) {
		if up, ok := initialisms[part]; ok {
			b.WriteString(up)
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

// camelNaive title-cases every part with no initialism table: the casing
// go-unifi's settings package uses for its own struct names (Mdns, RadioAi,
// GuestAccess), and the casing this package's type and function names use
// (dhcpOptionKitModel, wlanGroupGenFields).
func camelNaive(tag string) string {
	var b strings.Builder
	for _, part := range tagParts(tag) {
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

// lowerCamelNaive is camelNaive with the first part left lower-case.
func lowerCamelNaive(tag string) string {
	parts := tagParts(tag)
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(parts[0]) + camelNaive(strings.Join(parts[1:], "_"))
}
