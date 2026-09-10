# Multi-stage, cross-compiled: the build stage runs on the runner's native arch and Go emits the
# target arch, so linux/arm64 builds need no QEMU. Standalone — splatoon-3 is NPLN and imports no
# private module, so there is no netrc/GOPRIVATE block here.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The server and the rotation generator: the operator writes a rotation file with genrotation, then
# points NPLN_ROTATION at it (docs/design.md).
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/npln ./cmd/npln \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/genrotation ./cmd/genrotation

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 65532 -h /app app
WORKDIR /app
COPY --from=build /out/npln /app/npln
COPY --from=build /out/genrotation /app/genrotation

RUN mkdir -p /app/data && chown -R 65532:65532 /app
# Runs unprivileged: rootless podman maps this uid into the host's subuid range, and every listener
# (21012 tenant, 21013 health, 22210 session) is >=20000 so no capability is needed to bind.
USER 65532:65532
ENTRYPOINT ["/app/npln"]
