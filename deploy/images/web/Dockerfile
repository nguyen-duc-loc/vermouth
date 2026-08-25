# syntax=docker/dockerfile:1.12@sha256:93bfd3b68c109427185cd78b4779fc82b484b0b7618e36d0f104d4d801e66d25

ARG NODE_IMAGE=node:24.19-alpine@sha256:d32cdf619f63fe0471182d08996dd516c6275bb5fd31ae06e55a570bd9e1ad43
ARG NGINX_IMAGE=nginx:stable-alpine3.24@sha256:97d490c12ba55b4946b01546d1c3ed324e8d41ab1c9fcb2a616aa470620e5b46

FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS build
ENV PNPM_HOME=/pnpm
ENV PATH=$PNPM_HOME:$PATH
WORKDIR /src
RUN corepack enable

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY web/package.json ./web/package.json
RUN --mount=type=cache,id=pnpm,target=/pnpm/store \
    pnpm install --filter ./web... --frozen-lockfile

COPY web ./web
RUN pnpm --dir web run build

FROM ${NGINX_IMAGE} AS runtime
COPY deploy/images/web/nginx.conf /etc/nginx/nginx.conf
COPY --from=build /src/web/dist /usr/share/nginx/html
USER 101:101
EXPOSE 8080
ENTRYPOINT ["nginx"]
CMD ["-g", "daemon off;"]
