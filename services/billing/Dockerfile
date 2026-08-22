# syntax=docker/dockerfile:1
#
# Build this image with the repository root as the context, never this
# directory (STK-24):
#
#     docker build -f services/billing/Dockerfile -t vermouth/billing:dev .
#     task image -- billing
#
# The shared module carries no published version, so the build needs both
# modules present. This is the first thing that breaks a FROM scratch build,
# which is why the spec fixes it rather than leaving it to be discovered.

FROM golang:1.27-alpine AS build
WORKDIR /src

# Dependencies first, so a code change does not re download the module cache.
COPY pkg/vermouth/go.mod pkg/vermouth/go.sum ./pkg/vermouth/
COPY services/billing/go.mod services/billing/go.sum ./services/billing/
RUN cd services/billing && go mod download

COPY pkg/vermouth ./pkg/vermouth
COPY services/billing ./services/billing

# CGO off and a static binary, which is what makes FROM scratch possible
# (STK-6). The zone database is embedded in the binary, because a scratch image
# carries no system tzdata.
RUN cd services/billing && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/billing ./cmd/billing

# Nothing but the binary. No shell, no package manager, nothing to patch.
#
# Note for later: a scratch image also carries no CA certificates. The first
# outbound TLS call, which is the email provider in feature 17 and object
# storage in feature 14, needs ca-certificates copied in from the build stage.
FROM scratch
COPY --from=build /out/billing /billing
USER 65534:65534
ENTRYPOINT ["/billing"]
