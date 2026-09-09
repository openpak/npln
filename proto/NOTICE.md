`*.proto` here is the NPLN service schema (package `nn.npln.*`) as the Switch client speaks it:
message and service definitions reconstructed from the protocol's own descriptors for
interoperability, with no vendor options, comments or code. The `*.pb.go` files are generated from
them by stock `protoc` (`proto/generate.sh`) and are AGPL-3.0-only like the rest of this repository.

This is the **subset this repository can actually answer today** — auth, friends and presence, plus
the shared value type they need. The
same schema already lives in [`servers/stardew-valley`](../../stardew-valley/proto); it is not
title-specific and belongs in one shared module. See [`docs/design.md`](../docs/design.md),
"What should be lifted".
