# syntax=docker/dockerfile:1.12@sha256:93bfd3b68c109427185cd78b4779fc82b484b0b7618e36d0f104d4d801e66d25

ARG GO_IMAGE=golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS base
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

COPY pkg/vermouth/go.mod pkg/vermouth/go.sum ./pkg/vermouth/
COPY services/billing/go.mod services/billing/go.sum ./services/billing/
RUN --mount=type=cache,target=/go/pkg/mod \
    cd services/billing && go mod download

COPY pkg/vermouth ./pkg/vermouth
COPY services/billing ./services/billing

FROM base AS runtime-build
RUN --mount=type=cache,target=/root/.cache/go-build \
    cd services/billing && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/billing ./cmd/billing
FROM base AS migration-build
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go install github.com/pressly/goose/v3/cmd/goose@v3.27.3 && \
    goose=/go/bin/goose && \
    if [ -f "/go/bin/${TARGETOS}_${TARGETARCH}/goose" ]; then goose="/go/bin/${TARGETOS}_${TARGETARCH}/goose"; fi && \
    cp "$goose" /out/goose && \
    cd pkg/vermouth && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/migrationwait ./cmd/migrationwait

FROM scratch AS runtime
COPY --from=runtime-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=runtime-build /out/billing /billing
USER 65534:65534
ENTRYPOINT ["/billing"]

FROM scratch AS migration
COPY --from=migration-build /out/goose /goose
COPY --from=migration-build /out/migrationwait /migrationwait
COPY services/billing/db/migrations /migrations
USER 65534:65534
ENV GOOSE_DRIVER=postgres GOOSE_MIGRATION_DIR=/migrations
ENTRYPOINT ["/migrationwait"]
