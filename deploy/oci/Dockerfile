# stardew-nextendo server image (multi-arch: builds natively on arm64 and amd64).
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/server ./cmd/npln
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/server /server
ENTRYPOINT ["/server"]
