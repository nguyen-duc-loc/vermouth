# web

## Overview

The tutor's whole interface, and the only client of the gateway. It ships as static files, so it adds
nothing to the runtime memory budget and there is no second backend that could quietly read a
service's database (INV-10). Its API types are generated from the same `api/openapi.yaml` the gateway
generates its Go types from, so a contract change breaks the build rather than a screen.

## Stack

- **Runtime**: TypeScript on Node 24.19, pnpm 11.22.0 (pinned by `packageManager` in the root
  `package.json`, the one place pnpm and CI both read it)
- **Type checker**: TypeScript `^7.0.2`, the native compiler, so `tsc -b` runs TypeScript 7. The
  lockfile holds the exact version, so nothing moves until you ask for it with `pnpm update`.
  `@types/node` tracks the Node major, so it stays on `^24` even though 26 is out
- **Framework**: React 19 with Vite 8, TanStack Router, TanStack Query v5
- **Styling**: Tailwind CSS v4 through `@tailwindcss/vite`, shadcn/ui New York primitives on the Zinc base, and the Aceternity registry with Motion for reviewed source owned components
- **API types**: `openapi-typescript` into `src/api/schema.d.ts`, called through `openapi-fetch`
- **Tests**: Vitest with jsdom and Testing Library, configured by `vitest.config.ts`
- **Planned, not installed yet**: `react-i18next` with Vietnamese as the default language (feature
  20), Playwright for the money path

## Key files

| File | Owns |
|---|---|
| `src/main.tsx` | Mounting, the `QueryClient`, and the router provider |
| `src/routes.tsx` | The route tree |
| `src/pages/ThreadPage.tsx` | The skeleton's one screen, which drives the end to end thread |
| `src/api/client.ts` | The single `openapi-fetch` client and the auth header |
| `src/api/schema.d.ts` | Generated. Never edit by hand; run `task web:generate` |
| `src/api/thread.ts` | Typed calls and the shared `ApiError` shape, both taken from the schema |
| `design.md` | The visual direction and component usage contract; token values remain in CSS |
| `src/styles.css` | The Tailwind v4 entry point |
| `vite.config.ts` | The dev server on port 5173 and the proxy to the gateway |

## Commands

```bash
task web:install     # or: pnpm install, from this directory
task web:dev         # the Vite dev server on 5173, proxying to the gateway
task web:build       # tsc -b then vite build
task web:generate    # regenerate src/api/schema.d.ts from ../api/openapi.yaml
pnpm check           # Biome formatting, lint and import order
pnpm typecheck       # strict TypeScript through tsc -b
pnpm exec vitest run # the web unit and component suite
pnpm lint            # Biome lint only
```

Task targets put pnpm on `PATH` themselves, because pnpm is installed only on the interactive shell
path. Running `pnpm` directly works in your own shell.

## Conventions

- Application modules use named exports only. Vite and Vitest configuration files are the tool
  required default export exceptions, covered by the Biome override or a specific inline
  suppression.
- No `any`, and no hand written type for a gateway endpoint: take it from `components['schemas'][...]`
  in the generated schema (STK-10).
- Every screen meets WCAG AA: visible focus, reachable by keyboard, and readable on a phone first,
  because attendance gets marked standing up.
- TanStack Query's cache is where eventual consistency is made honest: a projection that has not
  caught up yet is refetched rather than assumed. Invalidate after a write instead of guessing.
- Money arrives as an integer count of dong and is formatted here, at the edge, never earlier.
- Design system: build all UI to `design.md`; token values live in `src/styles.css`.

## Gotchas

- The API calls go to this app's own origin, and the dev server proxies `/api`, `/health` and
  `/ready` to the gateway (`vite.config.ts`). So `VITE_API_BASE_URL` stays unset: setting it makes
  every call cross origin, the browser preflights the JSON `POST`, and the gateway sends no CORS
  headers, so the screen fails while `curl` keeps working. If you ever do need another origin, add
  CORS at the gateway in the same change.
- The relay polls, so an event reaches a projection 500ms to 1s after the write commits. A screen
  reading a projection must tolerate that window rather than assert on it immediately.
- Vitest runs beside the source with jsdom and Testing Library. The repository wide `task test`
  remains the Go module suite, so run `pnpm exec vitest run` from `web/` for browser component tests.
- TypeScript 7 ships the compiler as a binary and none of the old JavaScript compiler API:
  `ts.factory`, `ts.SyntaxKind`, and `ts.createPrinter` all read as `undefined`. `openapi-typescript`
  builds `src/api/schema.d.ts` by calling that API, so it dies on TypeScript 7. That is why the
  generator is a dependency of the **root** package, not of this one, and why `task web:generate`
  runs at the root: `typescript` is a peer dependency of the generator, a peer is satisfied by
  whichever package asks for it, so inside `web` it would always get this package's TypeScript 7 and
  the `overrides` block in `pnpm-workspace.yaml` could not stop it. At the root nothing else asks for
  TypeScript, so the override decides, the lockfile records
  `openapi-typescript@7.13.0(typescript@6.0.3)`, and `pnpm peers check` is clean. `pnpm generate:api`
  here still works; it calls the root script. Any other tool that drives the compiler, rather than
  just running it, needs the same treatment: give it to the root package.

## Agent skills

- [frontend-design](../.agents/skills/frontend-design/): `anthropics/skills`, visual direction that does not read as a template
- [shadcn-ui](../.agents/skills/shadcn-ui/): `jezweb/claude-skills`, installing and composing the owned Radix primitives
- [aceternity-ui](../.agents/skills/aceternity-ui/): `secondsky/claude-skills`, selecting and adapting Aceternity components to the Vermouth token and accessibility contracts
- [framer-motion](../.agents/skills/framer-motion/): `mindrally/skills`, Motion for React patterns used by reviewed Aceternity components
- [tailwindcss-fundamentals-v4](../.agents/skills/tailwindcss-fundamentals-v4/): `josiahsiegel/claude-plugin-marketplace`, v4's CSS first configuration and `@theme`
- [tailwindcss-accessibility](../.agents/skills/tailwindcss-accessibility/): `josiahsiegel/claude-plugin-marketplace`, the WCAG AA checklist, focus rings, and touch target sizes
- [react19-concurrent-patterns](../.agents/skills/react19-concurrent-patterns/): `github/awesome-copilot`, transitions, Suspense, and Actions
- [react19-source-patterns](../.agents/skills/react19-source-patterns/): `github/awesome-copilot`, React 19 API and ref changes
- [tanstack-router-best-practices](../.agents/skills/tanstack-router-best-practices/): `deckardger/tanstack-agent-skills`, type safe routes, loaders, and search params
- [tanstack-query-best-practices](../.agents/skills/tanstack-query-best-practices/): `deckardger/tanstack-agent-skills`, query keys, caching, and invalidation after a write
- [tanstack-integration-best-practices](../.agents/skills/tanstack-integration-best-practices/): `deckardger/tanstack-agent-skills`, router and query together
- [vite](../.agents/skills/vite/): `antfu/skills`, configuration, plugins, and the build
- [openapi-to-typescript](../.agents/skills/openapi-to-typescript/): `softaworks/agent-toolkit`, generating and consuming the schema types
- [gsap-core](../.agents/skills/gsap-core/): `greensock/gsap-skills`, tweens, easing, and `matchMedia` for reduced motion
- [gsap-react](../.agents/skills/gsap-react/): `greensock/gsap-skills`, the `useGSAP` hook and cleanup on unmount
- [gsap-timeline](../.agents/skills/gsap-timeline/): `greensock/gsap-skills`, sequencing
- [gsap-scrolltrigger](../.agents/skills/gsap-scrolltrigger/): `greensock/gsap-skills`, scroll driven animation and pinning
- [gsap-performance](../.agents/skills/gsap-performance/): `greensock/gsap-skills`, keeping animation smooth on a phone
- [gsap-plugins](../.agents/skills/gsap-plugins/): `greensock/gsap-skills`, plugin registration. Every plugin is free since Webflow acquired GSAP, so never add a GreenSock token or registry
- [gsap-utils](../.agents/skills/gsap-utils/): `greensock/gsap-skills`, `clamp`, `mapRange`, `snap`, and friends
- [playwright-cli](../.agents/skills/playwright-cli/): `microsoft/playwright-cli`, driving the browser
- [playwright-best-practices](../.agents/skills/playwright-best-practices/): `currents-dev/playwright-best-practices-skill`, flake, page objects, CI, and accessibility checks
- [performance-lint-rules](../.agents/skills/performance-lint-rules/): `oxc-project/oxc`, only relevant if you ever write an oxlint rule yourself, not part of this app's Biome setup

Declined: `tailwindcss-responsive-darkmode` (no `dark:` class in the app yet, so it is premature),
`gsap-frameworks` (Vue and Svelte only), and the four `microsoft/playwright` skills (they are for
developing Playwright itself).

## Related specs

- [0002 stack and scaffold](../docs/specs/0002-stack-and-scaffold/index.md) (the web app, styling, API contract, and testing rows)
- [0008 design system and UI foundation](../docs/specs/0008-design-system-ui-foundation/index.md) owns the component set, themes, responsive shell, and gallery

_Drafted by $audit from the repo, worth a quick human pass. Edit freely: once a line stops matching this draft, later runs treat it as curated and will flag rather than overwrite it._
