# Development provider contracts

This directory contains the unsigned DNS-record management contract used for
the M5 shadow integration with ubitofu. It is a release-style sidecar, but its
trust root is the adjacent operator-pinned SHA-256 file. Signing and key
rotation are later release work.

The sidecar binds one Linux/amd64 provider binary, both pinned schema CLIs,
their different complete canonical schema projections, the admitted catalog,
compiler policy and outputs, and the passing locked-controller receipt.
Consumers select the schema file for the CLI they actually use. They must stop
before contacting a controller if any digest or toolchain identity differs.

Normal Terraform and OpenTofu provider use does not read this directory.
ubitofu also remains on its legacy manifest unless every contract setting is
provided explicitly.
