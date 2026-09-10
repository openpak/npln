#!/bin/sh
# Regenerates the Go bindings from the .proto schema in this directory (run from the repo root).
# Needs protoc, protoc-gen-go and protoc-gen-go-grpc on PATH.
set -e
find proto -name '*.pb.go' -delete
protoc -I . -I proto/third_party \
  --go_out=. --go_opt=module=openpak/splatoon-3 \
  --go-grpc_out=. --go-grpc_opt=module=openpak/splatoon-3 \
  $(find proto -name '*.proto' -not -path 'proto/third_party/*')
