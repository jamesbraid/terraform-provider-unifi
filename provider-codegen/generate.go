package providercodegen

//go:generate go run ../cmd/provider-spec-compiler -catalog catalog/go-unifi-v1.102.0-dns-record.catalog.json -policy policy/dns_record.json -baseline ../build/m0/provider-schema-digests.json -output-dir generated
//go:generate go tool tfplugingen-framework generate resources --input generated/dns_record.provider-code-spec.json --output ../internal/generated/resource_dns_record --package resource_dns_record
//go:generate gofmt -w ../internal/generated/resource_dns_record/dns_record_resource_gen.go
