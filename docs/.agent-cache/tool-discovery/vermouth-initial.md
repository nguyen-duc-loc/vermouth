# Tool discovery, confirmed 2026-08-22

Run by /sync after the initial commit. Every repo below was confirmed with
`npx skills add <owner>/<repo> --list`, so the skill names really exist.
Reuse this file until 2026-09-21, after filtering out anything installed since.

## Confirmed, relevant to this repo

| Tool in repo | Repo | Skills worth taking |
|---|---|---|
| Go (all six modules) | `samber/cc-skills-golang` (46 skills, ~37K installs) | `golang-code-style`, `golang-error-handling`, `golang-testing`, `golang-concurrency`, `golang-context`, `golang-project-layout`, `golang-security`, `golang-observability`, `golang-database`, `golang-stretchr-testify`, `golang-lint` |
| goose migrations | `metalagman/agent-skills` (16 skills) | `go-goose`, `golangci-lint-strict` |
| TanStack Router + Query | `deckardger/tanstack-agent-skills` (4 skills, 10.4K/6.1K installs) | `tanstack-router-best-practices`, `tanstack-query-best-practices`, `tanstack-integration-best-practices` |
| TanStack Router + Query | `tanstack-skills/tanstack-skills` (14 skills) | `tanstack-router`, `tanstack-query` |
| Vite 8, pnpm workspace | `antfu/skills` (19 skills, 33.4K installs) | `vite`, `pnpm` |
| Tailwind v4 | `josiahsiegel/claude-plugin-marketplace` (very large, many domains) | `tailwindcss-fundamentals-v4`, `tailwindcss-accessibility`, `tailwindcss-responsive-darkmode` |
| React 19 | `github/awesome-copilot` | `react19-concurrent-patterns`, `react19-source-patterns`, `react19-test-patterns` |
| openapi-typescript | `softaworks/agent-toolkit` (44 skills) | `openapi-to-typescript` |
| Broad coverage, one repo | `oakoss/agent-skills` (130+ skills) | `openapi`, `postgres-tuning`, `tailwind`, `vite`, `pnpm-workspace`, `docker`, `typescript-patterns`, `react-patterns`, `database-security`, `tanstack-router`, `tanstack-query` |
| Kafka protocol (franz-go) | `mindrally/skills` (250+ skills) | `kafka-development`, `go`, `postgresql-best-practices` |
| oxlint | `oxc-project/oxc` (first party, 4 skills) | `performance-lint-rules`. `migrate-oxlint` is for moving off ESLint, so it does not apply here |
| Redpanda (first party) | `redpanda-data/console` (9 skills) | Scoped to their own console app, not the broker. `tanstack-router-migration` and `react-best-practices` may still read usefully |

## Phantom registry entries, do not offer

- `tanstack/router@router-core`, `@react-router`, `@router-plugin`: the registry lists these with install counts, but `--list` on the repo finds only one skill, `bundle-size-optimization`, and it is scoped to that repository. There is no first party TanStack Router skill to install.
- `kalbasit/ncps@sqlc`: the repo's 11 skills are all openspec and tdd. No sqlc skill.

## No skill exists

sqlc, oapi-codegen, franz-go specifically, Task (Taskfile), pgx specifically. Closest
matches are the generic Go database and Kafka skills above.

## MCP notes

- No MCP server is connected, globally or for this project.
- Do NOT use `@modelcontextprotocol/server-postgres`. Its read only mode was
  bypassable (`COMMIT; DROP SCHEMA public CASCADE;` escaped the read only
  transaction envelope, because the driver accepted multi statement strings).
  Datadog disclosed it in mid 2025 and the server is archived.
- Redpanda's own MCP servers (`rpk cloud mcp`, `rpk ai mcp-server`) target
  Redpanda Cloud. This repo runs a local broker in compose, so they do not fit.
  Their documentation MCP server does apply.

---

## Round 2, confirmed 2026-08-22 (engineer requested GSAP, Playwright, frontend-design)

| Tool | Repo | Skills worth taking |
|---|---|---|
| GSAP (first party, GreenSock) | `greensock/gsap-skills` (8 skills, ~48K installs) | `gsap-core`, `gsap-react`, `gsap-timeline`, `gsap-scrolltrigger`, `gsap-performance`, `gsap-plugins`, `gsap-utils`. `gsap-frameworks` is Vue/Svelte only, skipped |
| Playwright (first party, Microsoft) | `microsoft/playwright-cli` (2 skills, 127.4K installs) | `playwright-cli`. `dev` is for maintaining that repo, skipped |
| Playwright practice | `currents-dev/playwright-best-practices-skill` (1 skill, 74.3K installs) | `playwright-best-practices`, very broad: flake, POM, CI, a11y via axe-core, tags, visual |
| UI design | `anthropics/skills` (20 skills) | `frontend-design`. Also holds `webapp-testing` (Playwright driving) and `mcp-builder`, not installed |

### Not worth taking

- `microsoft/playwright` (4 skills): `playwright-dev`, `playwright-devops`, `playwright-test-results`, `playwright-triage` are all for developing Playwright itself, not for using it. Do not offer for this repo.
- `tailwindcss-responsive-darkmode`: the web app has no `dark:` class anywhere, so it is premature.
- `react19-test-patterns`: `web/package.json` has no test library at all yet. Revisit when /test sets one up.

### GSAP licensing, settled

Every GSAP plugin is free, including commercial use, since Webflow acquired GSAP. Club GSAP
is no longer a paid tier, and formerly Club only plugins (SplitText, MorphSVG, ScrollSmoother)
need no membership, license key, or auth token. Install from the public `gsap` npm package.
Never write an `.npmrc` with a GreenSock token or point at `npm.greensock.com`.

## Installed 2026-08-22, 36 skills

Installed to `.agents/skills/` (tool agnostic), symlinked into `.claude/skills/`.

Go (13): `golang-code-style`, `golang-error-handling`, `golang-testing`, `golang-concurrency`,
`golang-context`, `golang-project-layout`, `golang-security`, `golang-observability`,
`golang-database`, `golang-stretchr-testify`, `golang-lint`, `go-goose`, `golangci-lint-strict`
GSAP (7): `gsap-core`, `gsap-react`, `gsap-timeline`, `gsap-scrolltrigger`, `gsap-performance`,
`gsap-plugins`, `gsap-utils`
Web (9): `tanstack-router-best-practices`, `tanstack-query-best-practices`,
`tanstack-integration-best-practices`, `vite`, `pnpm`, `tailwindcss-fundamentals-v4`,
`tailwindcss-accessibility`, `react19-concurrent-patterns`, `react19-source-patterns`
API (2): `openapi-to-typescript`, `openapi`
Testing and design (3): `playwright-cli`, `playwright-best-practices`, `frontend-design`
Other (2): `kafka-development`, `performance-lint-rules`

Declined / no match, do not re offer: sqlc, oapi-codegen, pgx by name, Task (Taskfile),
franz-go specifically. No MCP server connected. These belong in root AGENTS.md once /audit
creates it.
