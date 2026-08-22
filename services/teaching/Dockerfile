# syntax=docker/dockerfile:1
#
# Build this image with the repository root as the context, never this
# directory (STK-24):
#
#     docker build -f services/teaching/Dockerfile -t vermouth/teaching:dev .
#     task image -- teaching
#
# The shared module carries no published version, so the build needs both
# modules present. This is the first thing that breaks a FROM scratch build,
# which is why the spec fixes it rather than leaving it to be discovered.

FROM golang:1.27-alpine AS build
WORKDIR /src

# Dependencies first, so a code change does not re download the module cache.
COPY pkg/vermouth/go.mod pkg/vermouth/go.sum ./pkg/vermouth/
COPY services/teaching/go.mod services/teaching/go.sum ./services/teaching/
RUN cd services/teaching && go mod download

COPY pkg/vermouth ./pkg/vermouth
COPY services/teaching ./services/teaching

# CGO off and a static binary, which is what makes FROM scratch possible
# (STK-6). The zone database is embedded in the binary, because a scratch image
# carries no system tzdata.
RUN cd services/teaching && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/teaching ./cmd/teaching

# Nothing but the binary. No shell, no package manager, nothing to patch.
#
# Note for later: a scratch image also carries no CA certificates. The first
# outbound TLS call, which is the email provider in feature 17 and object
# storage in feature 14, needs ca-certificates copied in from the build stage.
FROM scratch
COPY --from=build /out/teaching /teaching
USER 65534:65534
ENTRYPOINT ["/teaching"]
