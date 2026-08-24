# Unifi Terraform Provider (terraform-provider-unifi)

[![Acceptance Tests](https://github.com/ubiquiti-community/terraform-provider-unifi/actions/workflows/acctest.yaml/badge.svg)](https://github.com/ubiquiti-community/terraform-provider-unifi/actions/workflows/acctest.yaml) [![codecov](https://codecov.io/github/ubiquiti-community/terraform-provider-unifi/graph/badge.svg?token=KVP7FS41IG)](https://codecov.io/github/ubiquiti-community/terraform-provider-unifi)

> **Note**: You can't (for obvious reasons) configure your network while connected to something that may disconnect (like the WiFi). Use a hard-wired connection to your controller to use this provider.

Functionality first needs to be added to the [go-unifi](https://github.com/ubiquiti-community/go-unifi) SDK.

This fork tracks [jamesbraid/go-unifi](https://github.com/jamesbraid/go-unifi), which carries fixes not yet upstream. Its releases are tagged on the fork, but the module still declares the upstream path. So `go.mod` redirects them with a `replace`, and `go get -u` will not move the SDK. To change SDK version:

```
go mod edit -replace github.com/ubiquiti-community/go-unifi=github.com/jamesbraid/go-unifi@vX.Y.Z
go mod tidy
```

## How this provider is built

Managed resources are served by a shared resource engine
(`internal/resourcekit`), configured per resource by a descriptor and backed
by Go schema packages generated from the go-unifi SDK and a per-resource
policy file. [development/architecture.md](development/architecture.md)
explains the design; [development/new-controller.md](development/new-controller.md)
is the playbook for adding support for a new controller release. Building,
generating, and testing the provider locally is covered in
[CONTRIBUTING.md](CONTRIBUTING.md).

## Documentation

You can browse documentation on the [Terraform provider registry](https://registry.terraform.io/providers/ubiquiti-community/unifi/latest/docs).

## Supported Unifi Controller Versions

Acceptance tests run against UniFi Network 10.4.57. That is the version the SDK's field definitions are generated from, and the one to assume when a resource's behaviour is in question.

Version 6 is the floor, from [v0.34](https://github.com/ubiquiti-community/terraform-provider-unifi/releases/tag/v0.34.0) onwards. Pin an older provider release if you need v5.

Some attributes need more than the floor. UniFi Network 10.x moved geo IP filtering and IPS suppression out of the `usg` and `ips` settings into objects of their own. `unifi_setting`'s `usg.geo_ip_filtering_*` and `ips.suppression_*` therefore need a controller that exposes those objects, and report an error against one that does not. A 10.0 controller is not enough. 10.4.57 has them.

The docker, UDM, and UDM-Pro versions are slightly different (the API is proxied a little differently) but for the most part should all be supported. Individual patch versions of the controller are generally not tested for compatibility, just the latest stable versions.

## Using the Provider

### Terraform 1.0 and above

Use the provider from its canonical Terraform Registry address,
[`ubiquiti-community/unifi`](https://registry.terraform.io/providers/ubiquiti-community/unifi).

Existing state that records the legacy `paultyng/unifi` address does not move
implicitly when the configuration changes. Review the state backup and plan
for the affected workspace, then perform the address migration explicitly:

```shell
terraform state replace-provider \
  registry.terraform.io/paultyng/unifi \
  registry.terraform.io/ubiquiti-community/unifi
```

This is a state migration, not a provider compatibility promise. Test it with
the Terraform CLI and provider versions used by the workspace before applying
any infrastructure changes.
