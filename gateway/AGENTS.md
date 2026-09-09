# gateway

## Overview

The only public surface. It routes to the four services, verifies the token on every request, and
fans out read only calls so one phone first screen can ask two services at once. It holds no
business rule and no write logic of its own, and it never touches a database.

## Stack

- **Module**: `github.com/nguyen-duc-loc/vermouth/gateway`, Go 1.27
- **HTTP**: standard library `net/http` with `ServeMux` pattern routing; `errgroup` for the fan out
- **Types**: generated from `api/openapi.yaml` by `oapi-codegen` into `internal/apitypes/types.gen.go`

## Key files

| File | Owns |
|---|---|
| `cmd/gateway/main.go` | Startup: configuration, logger, upstreams, then the server |
| `internal/route/config.go` | Where the services are, read from the environment and required, never defaulted |
| `internal/route/routes.go` | The route table and the one error shape at the boundary |
| `internal/auth/auth.go` | Token verification, rejecting an unsigned or expired request |
| `internal/aggregate/upstream.go` | Calling a service on the gateway's behalf, with a timeout |
| `internal/aggregate/thread.go` | The read fan out, the skeleton's one aggregated screen |
| `internal/apitypes/types.gen.go` | Generated. Never edit by hand; run `task generate:api` |

## Commands

```bash
task generate:api   # regenerate types.gen.go from api/openapi.yaml
task build          # bin/gateway among the others
```

## Conventions

- A new endpoint starts in `api/openapi.yaml`, then `task generate:api`, then the route. The
  document is authoritative, not the code (STK-10).
- The gateway verifies the token and passes it onward unchanged, so each service verifies it again
  locally. Never forward a plain trusted header instead: anything able to reach a service could then
  impersonate any tutor.
- Every response carries `X-Request-Id`, created here when the caller sent none, and it appears on
  every log line downstream (INV-15).
- Fan out is read only. A write goes to exactly one service.
- The gateway serves `GET /health` and `GET /ready`, and `/ready` is where every service's readiness
  is asked through the gateway, which is exactly what `task status` reads.

## Gotchas

- Adding a rule here is the easiest way to break spec 0001. Business decisions belong to the owning
  service, even when the gateway is the convenient place to put them.
- A missing upstream URL stops startup on purpose: a gateway pointing at the wrong place is worse
  than one that refuses to start.

## Agent skills

The repo wide skills in the root file all apply here. These are the ones that earn their keep in this area:

- [openapi](../.agents/skills/openapi/): `oakoss/agent-skills`, keeping `api/openapi.yaml` honest
- [golang-concurrency](../.agents/skills/golang-concurrency/): `samber/cc-skills-golang`, the `errgroup` fan out
- [golang-security](../.agents/skills/golang-security/): `samber/cc-skills-golang`, input handling at the public edge

## Related specs

- [0001 service boundaries and communication](../docs/specs/0001-service-boundaries-and-communication/index.md) (the service map, identity propagation, gateway read aggregation)
- [0002 stack and scaffold](../docs/specs/0002-stack-and-scaffold/index.md) (STK-10, STK-14)

_Drafted by $audit from the repo, worth a quick human pass. Edit freely: once a line stops matching this draft, later runs treat it as curated and will flag rather than overwrite it._
