# Verify: Local Kubernetes platform and one command startup · spec 0005 · updated 2026-08-27

_This is the repeatable release gate. It proves the real platform through fourteen workflows. Detailed fault contracts remain in the spec and regression tests. They return here only when their owning recovery code or committed input changes._

## Release gate

- [x] R1. Record `kubectl config current-context`, run `task platform:doctor`, and require the pinned host, builder, schema, matrix, capacity, identity, registry, and cold pull checks to pass without changing the selected context. → AC-1, AC-12, AC-15, AC-17
- [x] R2. Hash `.env`, run `task platform:bootstrap` twice, and require the same `.env` hash, the same profile and daemon hashes, matching cold transport proof, and no replacement of an already matching cluster. → AC-1, AC-12, AC-17
- [x] R3. Run `task platform:validate` and `task platform:status`. Require both Helm releases, six application Deployments, six StatefulSets, packaged Traefik, one Ingress, seven bound claims, eight retained volumes, immutable image references, exact workload security, and traffic row bijection. → AC-2, AC-3, AC-9, AC-10, AC-17
- [x] R4. On a cluster whose local data is explicitly disposable, reject a wrong `task platform:clean` confirmation, accept `vermouth`, prove the target absent, run confirmed `task platform:recreate`, prove no Vermouth Helm release exists, then run cold `task dev` through foundation, Garage, four migrations, application install, and final public readiness. → AC-1, AC-2, AC-3, AC-5, AC-11, AC-12, AC-16, AC-17
- [x] R5. Request `/`, `/health`, `/ready`, `/config.json`, and one `/api` path through ordinary hostname resolution and forced `127.0.0.1`. Require preserved paths, one origin, exact readiness checks, and `Cache-Control: no-store`. → AC-4, AC-14
- [x] R6. With Google disabled, use a real browser to require the disabled control, visible fixed message, and `aria-describedby`, then require auth start and callback to return `auth_unavailable`. With valid credentials, enable Google using the exact callback, require the enabled control and Google redirect, then change the callback and require failure before generated state or Helm changes. Restore `.env` byte for byte. → AC-4, AC-11, AC-14
- [x] R7. Run `task dev:token` through a secret safe parser and require exactly its five schema version 1 fields. Run `task thread` and require notifications to record the identity event through the gateway. → AC-13
- [x] R8. Record image state and Helm revisions, reject an unknown `task dev:redeploy` target without mutation, then redeploy one database owning service and require only its service and migration paths, one migration Job, and a ready rollout. → AC-7, AC-11, AC-12
- [x] R9. Run `task images:multi`. Require all eleven deployable artifacts to stage before the first push, exact `linux/arm64` and `linux/amd64` indexes, both web smoke checks, no development token artifact, and zero unused host images carrying `vermouth.dev/workload` after success. → AC-8, AC-11, AC-17
- [x] R10. Record shared builder cache, reject a wrong `task platform:cache:clean` confirmation, accept `vermouth`, require zero reclaimable BuildKit cache, and confirm the live platform remains ready. → AC-8, AC-12, AC-17
- [x] R11. Stop Kubernetes, run `task infra:up` and `task test` against Compose, then stop Compose and run `task dev`. Require tests to stay cluster independent and the Kubernetes platform to return ready. → AC-5, AC-6
- [x] R12. Record all four database migration versions, one identity row, one notification projection, one Redpanda event, and the Garage bucket plus billing key identities. Replace all six stateful Pods, stop and restart the platform, and require every recorded value to remain. → AC-5, AC-16
- [x] R13. Start one mutating platform command, require a concurrent command to refuse the live owner, let the owner complete, then run `task platform:measure` and require the committed memory and CPU formula to pass. → AC-11, AC-12, AC-15
- [x] R14. Run `task platform:locks:verify`, `task platform:doctor`, `task platform:status`, and `task thread`. Require every external lock, final topology, public readiness, event thread, original `.env` hash, and original unrelated Kubernetes context to match. → AC-1, AC-2, AC-3, AC-4, AC-12, AC-13, AC-17

## Qualification boundary

The following details are durable implementation contracts, not repeated destructive release exercises: interrupted writes, failed native and multiple architecture builds, partial pushes, migration failure retention, Helm rollback, stale lock recovery, Garage conflict refusal, port collision, low memory, full volume, schema field removal, and immutable drift. Their focused regression tests are mandatory. The related release gate workflow must be repeated whenever its recovery implementation or committed platform input changes.

## Acceptance criteria coverage

- AC-1: R1, R2, R4, R14
- AC-2: R3, R4, R14
- AC-3: R3, R4, R14
- AC-4: R5, R6, R14
- AC-5: R4, R11, R12
- AC-6: R11
- AC-7: R8
- AC-8: R9, R10
- AC-9: R3
- AC-10: R3
- AC-11: R4, R6, R8, R9, R13
- AC-12: R1, R2, R4, R8, R10, R13, R14
- AC-13: R7, R14
- AC-14: R5, R6
- AC-15: R1, R13
- AC-16: R4, R12
- AC-17: R1, R2, R3, R4, R9, R10, R14
