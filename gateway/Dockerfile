# syntax=docker/dockerfile:1.12@sha256:93bfd3b68c109427185cd78b4779fc82b484b0b7618e36d0f104d4d801e66d25

ARG GO_IMAGE=golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

COPY pkg/vermouth/go.mod pkg/vermouth/go.sum ./pkg/vermouth/
COPY gateway/go.mod gateway/go.sum ./gateway/
RUN --mount=type=cache,target=/go/pkg/mod \
    cd gateway && go mod download

COPY pkg/vermouth ./pkg/vermouth
COPY gateway ./gateway
RUN --mount=type=cache,target=/root/.cache/go-build \
    cd gateway && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/gateway

FROM scratch AS runtime
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/gateway /gateway
USER 65534:65534
ENTRYPOINT ["/gateway"]
