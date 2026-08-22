# web

The tutor's interface: React 19 on Vite 8, TypeScript, Tailwind v4. The conventions, the file map,
and the gotchas live in [AGENTS.md](./AGENTS.md); this file only lists the commands.

```bash
pnpm install          # or: task web:install, from the repository root
pnpm dev              # the Vite dev server on 5173, proxying to the gateway
pnpm build            # tsc -b then vite build
pnpm check            # Biome: formatting, lint and import order, changing nothing
pnpm check:fix        # the same pass, applying every fix Biome can make itself
pnpm typecheck        # tsc -b
pnpm generate:api     # regenerate src/api/schema.d.ts from ../api/openapi.yaml
```

Biome is the one tool over formatting and lint, TypeScript and CSS alike, configured in
[biome.jsonc](./biome.jsonc). It replaced Prettier and oxlint, so there is one config and one
binary rather than two that can disagree.

The commit hook runs `pnpm check` and `pnpm typecheck` for you through pre commit; see the root
[AGENTS.md](../AGENTS.md).
