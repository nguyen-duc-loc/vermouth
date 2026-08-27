# syntax=docker/dockerfile:1.12@sha256:93bfd3b68c109427185cd78b4779fc82b484b0b7618e36d0f104d4d801e66d25

ARG ALPINE_IMAGE=alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
ARG GO_IMAGE=golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS helper-build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY pkg/vermouth/go.mod pkg/vermouth/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY pkg/vermouth/cmd/s3probe ./cmd/s3probe
COPY pkg/vermouth/cmd/garageinit ./cmd/garageinit
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/s3probe ./cmd/s3probe && \
    go build -trimpath -ldflags="-s -w" -o /out/garageinit ./cmd/garageinit

FROM ${ALPINE_IMAGE} AS runtime
COPY --from=helper-build /out/s3probe /usr/local/bin/s3probe
COPY --from=helper-build /out/garageinit /usr/local/bin/garageinit
COPY deploy/images/garage-init/reconcile.sh /usr/local/bin/reconcile-garage
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/reconcile-garage"]
