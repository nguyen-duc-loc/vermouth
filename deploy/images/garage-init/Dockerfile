# syntax=docker/dockerfile:1.12@sha256:93bfd3b68c109427185cd78b4779fc82b484b0b7618e36d0f104d4d801e66d25

ARG GARAGE_IMAGE=dxflrs/garage:v2.2.0@sha256:45a61ce3f7c9c24fc23d9ed2b09b27ed560ab87b34605d175d5c588f539c24e4
ARG ALPINE_IMAGE=alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

FROM ${GARAGE_IMAGE} AS garage

FROM ${ALPINE_IMAGE} AS runtime
COPY --from=garage /garage /usr/local/bin/garage
COPY deploy/images/garage-init/reconcile.sh /usr/local/bin/reconcile-garage
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/reconcile-garage"]
