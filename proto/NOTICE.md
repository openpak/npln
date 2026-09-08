`*.proto` here is the NPLN service schema (package `nn.npln.*`) as the Switch client speaks it:
message and service definitions reconstructed from the protocol's own descriptors for
interoperability, with no vendor options, comments or code. The `*.pb.go` files are generated from
them by stock `protoc` (`proto/generate.sh`) and are AGPL-3.0-only like the rest of this repository.
`third_party/google/rpc/status.proto` is Google's, Apache-2.0, kept verbatim so generation is
self-contained.
