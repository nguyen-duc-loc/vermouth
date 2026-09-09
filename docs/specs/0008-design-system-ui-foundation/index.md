# 0008. Design system and UI foundation

**Date**: 2026-08-27
**Status**: Accepted

## Summary

Vermouth will use a calm calendar workspace shaped by the supplied schedule screenshot, with
Montserrat, clear blue actions, quiet surfaces, and color coded classes. A semantic token system
(names based on purpose) will support light and dark themes plus seven user selected accent colors.
Accessible shadcn components and a development gallery will make later screens assembly work rather
than a new design exercise.

## Requirements

**User stories**:

1. As a tutor, I want a calm interface that works well on my phone so I can mark attendance while
   standing up and still understand dense schedules and invoices on a larger screen.
2. As a tutor, I want to choose light or dark mode and an accent color so the workspace feels
   comfortable without changing the meaning of warnings or class colors.
3. As a builder, I want documented tokens, components, states, and responsive rules so every later
   screen reuses one visual language.

**Acceptance criteria**:

1. **AC-1**: `web/design.md` records the visual direction, Montserrat type scale, semantic color
   tokens, the seven accent families, spacing, radius, elevation, motion, icon, responsive, writing,
   and class color rules for both light and dark themes.
2. **AC-2**: The app supports `light`, `dark`, and `system` theme choices plus `red`, `rose`,
   `orange`, `green`, `blue`, `yellow`, and `violet` accent choices. It defaults to `system` and
   `blue`, applies a valid choice before first paint, stores it in browser storage, and synchronizes
   it across open tabs.
3. **AC-3**: Missing, blocked, malformed, old, or unknown appearance storage never prevents the app
   from rendering. The app falls back to the system theme and blue accent, and the controls still
   work for the current tab.
4. **AC-4**: The base component set includes the app shell, navigation, button, icon button, text
   field, select, checkbox, radio group, switch, form field, card, semantic table, badge, tabs,
   dialog, menu, tooltip, sheet, toast, skeleton, empty state, and error state. Every component has
   the applicable default, hover, active, focus, disabled, loading, empty, and error states.
5. **AC-5**: Every component meets WCAG 2.2 AA. It has visible keyboard focus, correct semantics and
   accessible names, at least a 44 by 44 CSS pixel target for normal controls, sufficient contrast,
   a logical focus order, and no color only meaning. Content remains usable at 200 percent zoom.
6. **AC-6**: The shell uses a compact top bar and bottom navigation on phones, then a narrow icon
   rail with an optional contextual panel on wide screens. Callers supply typed destinations, at
   most five appear in phone navigation, and remaining actions stay reachable through labeled
   menus. Wide data tables have a caller supplied phone card presentation, while true grids such as
   calendars may scroll horizontally.
7. **AC-7**: Components accept all visible text from callers and remain usable with long Vietnamese
   labels, Unicode content, and locale aware dates, times, numbers, and money. Feature 6 does not
   install translation state or ship language switching.
8. **AC-8**: A development only `/design-system` gallery demonstrates every component, applicable
   state, theme, accent, phone layout, and long Vietnamese label case. A production build cannot
   route to or import the gallery.
9. **AC-9**: Ordinary component feedback uses short CSS transitions. One restrained page entrance
   pattern may use GSAP through `useGSAP`, with scoped targets and automatic cleanup. Reduced motion
   removes nonessential motion without hiding content or delaying interaction.
10. **AC-10**: The foundation exposes a curated class color contract using the seven accent
    families. A stable suggestion uses 32 bit FNV 1a over the UTF 8 `class_id`, a tutor may override
    it in Feature 8, and class displays use a pale surface plus a stronger marker and normal high
    contrast text.

## Decision

**Chosen option**: A product owned semantic design system built from shadcn components on Radix UI.

Use the supplied screenshot as inspiration for a calm schedule workspace, not as a page to copy.
Keep Vermouth rules in tokens and composed product components, while generated shadcn files remain
small editable primitives.

**Implementation skills**: `frontend-design` (`anthropics/skills`, `.agents/skills/frontend-design/`) · `tailwindcss-fundamentals-v4` (`josiahsiegel/claude-plugin-marketplace`, `.agents/skills/tailwindcss-fundamentals-v4/`) · `tailwindcss-accessibility` (`josiahsiegel/claude-plugin-marketplace`, `.agents/skills/tailwindcss-accessibility/`) · `shadcn-ui` (`jezweb/claude-skills`, `.agents/skills/shadcn-ui/`) · `gsap-core` (`greensock/gsap-skills`, `.agents/skills/gsap-core/`) · `gsap-react` (`greensock/gsap-skills`, `.agents/skills/gsap-react/`) · `pnpm` (`antfu/skills`, `.agents/skills/pnpm/`) · `vite` (`antfu/skills`, `.agents/skills/vite/`) · `tanstack-router-best-practices` (`deckardger/tanstack-agent-skills`, `.agents/skills/tanstack-router-best-practices/`)

### Visual direction

| Part | Contract |
|---|---|
| Character | Calm editorial workspace, focused and credible rather than playful or financial |
| Reference | White calendar workspace, fine borders, clear blue actions, compact navigation, and colored schedule blocks from the supplied screenshot |
| Signature | Class identity appears as a quiet tint with a strong edge marker, repeated consistently in schedules, lists, and detail views |
| Type | Bundled Montserrat variable font for interface text, medium and semibold for controls and headings, regular for body copy |
| Utility type | System monospace only for times, money, identifiers, and aligned numeric data |
| Shape | Medium corner radius, about 10 to 12 pixels for cards and controls, pills only for status and segmented controls |
| Density | Comfortable by default, compact only where schedules, calendars, tables, or invoices gain meaning from density |
| Elevation | Fine borders first, quiet shadows only for floating overlays and selected actions |
| Copy | Active voice, sentence case, caller supplied text, the same action name before and after completion |

### Token architecture

`web/src/styles.css` is the source for Tailwind v4 CSS first configuration. Tokens use OKLCH where
practical and have three levels:

1. Primitive values hold the neutral and color scales.
2. Semantic tokens name purpose, such as `background`, `foreground`, `primary`, `muted`, `border`,
   `focus`, `success`, `warning`, `destructive`, and `class-color`.
3. Components consume semantic tokens only. Raw palette utilities do not appear in reusable
   components.

Light and dark selectors map the semantic layer to primitives. Accent selectors map `primary`,
`primary-foreground`, links, selected navigation, focus treatment, and component emphasis to one of
`red`, `rose`, `orange`, `green`, `blue`, `yellow`, or `violet`. Success, warning, destructive, and
class identity remain separate from the chosen app accent.

Each class family defines accessible values for a pale surface, strong marker, and optional solid
swatch in both themes. Text on class surfaces uses the normal foreground token. Color is always
paired with the class name and, where state is involved, text or an icon.

#### Canonical shadcn base preset

The neutral foundation, default blue accent, charts, sidebar roles, and base radius come from the
Tailwind v4 code returned by `https://v3.shadcn.com/themes` for **Blue** with radius **0.75**. These
values are the canonical source. Vermouth may add semantic aliases such as `surface`, `focus`,
status, class color, and hover tokens, but those additions must derive from or remain compatible
with this preset rather than replace its declared values.

```css
:root {
  --radius: 0.75rem;
  --background: oklch(1 0 0);
  --foreground: oklch(0.141 0.005 285.823);
  --card: oklch(1 0 0);
  --card-foreground: oklch(0.141 0.005 285.823);
  --popover: oklch(1 0 0);
  --popover-foreground: oklch(0.141 0.005 285.823);
  --primary: oklch(0.623 0.214 259.815);
  --primary-foreground: oklch(0.97 0.014 254.604);
  --secondary: oklch(0.967 0.001 286.375);
  --secondary-foreground: oklch(0.21 0.006 285.885);
  --muted: oklch(0.967 0.001 286.375);
  --muted-foreground: oklch(0.552 0.016 285.938);
  --accent: oklch(0.967 0.001 286.375);
  --accent-foreground: oklch(0.21 0.006 285.885);
  --destructive: oklch(0.577 0.245 27.325);
  --border: oklch(0.92 0.004 286.32);
  --input: oklch(0.92 0.004 286.32);
  --ring: oklch(0.623 0.214 259.815);
  --chart-1: oklch(0.646 0.222 41.116);
  --chart-2: oklch(0.6 0.118 184.704);
  --chart-3: oklch(0.398 0.07 227.392);
  --chart-4: oklch(0.828 0.189 84.429);
  --chart-5: oklch(0.769 0.188 70.08);
  --sidebar: oklch(0.985 0 0);
  --sidebar-foreground: oklch(0.141 0.005 285.823);
  --sidebar-primary: oklch(0.623 0.214 259.815);
  --sidebar-primary-foreground: oklch(0.97 0.014 254.604);
  --sidebar-accent: oklch(0.967 0.001 286.375);
  --sidebar-accent-foreground: oklch(0.21 0.006 285.885);
  --sidebar-border: oklch(0.92 0.004 286.32);
  --sidebar-ring: oklch(0.623 0.214 259.815);
}

.dark {
  --background: oklch(0.141 0.005 285.823);
  --foreground: oklch(0.985 0 0);
  --card: oklch(0.21 0.006 285.885);
  --card-foreground: oklch(0.985 0 0);
  --popover: oklch(0.21 0.006 285.885);
  --popover-foreground: oklch(0.985 0 0);
  --primary: oklch(0.546 0.245 262.881);
  --primary-foreground: oklch(0.379 0.146 265.522);
  --secondary: oklch(0.274 0.006 286.033);
  --secondary-foreground: oklch(0.985 0 0);
  --muted: oklch(0.274 0.006 286.033);
  --muted-foreground: oklch(0.705 0.015 286.067);
  --accent: oklch(0.274 0.006 286.033);
  --accent-foreground: oklch(0.985 0 0);
  --destructive: oklch(0.704 0.191 22.216);
  --border: oklch(1 0 0 / 10%);
  --input: oklch(1 0 0 / 15%);
  --ring: oklch(0.488 0.243 264.376);
  --chart-1: oklch(0.488 0.243 264.376);
  --chart-2: oklch(0.696 0.17 162.48);
  --chart-3: oklch(0.769 0.188 70.08);
  --chart-4: oklch(0.627 0.265 303.9);
  --chart-5: oklch(0.645 0.246 16.439);
  --sidebar: oklch(0.21 0.006 285.885);
  --sidebar-foreground: oklch(0.985 0 0);
  --sidebar-primary: oklch(0.546 0.245 262.881);
  --sidebar-primary-foreground: oklch(0.379 0.146 265.522);
  --sidebar-accent: oklch(0.274 0.006 286.033);
  --sidebar-accent-foreground: oklch(0.985 0 0);
  --sidebar-border: oklch(1 0 0 / 10%);
  --sidebar-ring: oklch(0.488 0.243 264.376);
}
```

### Appearance state

The browser preference is versioned under `vermouth.appearance.v1`:

```ts
type ThemeChoice = 'light' | 'dark' | 'system'
type AccentChoice = 'red' | 'rose' | 'orange' | 'green' | 'blue' | 'yellow' | 'violet'

type AppearancePreference = {
  theme: ThemeChoice
  accent: AccentChoice
}
```

An early inline boot script validates the finite values and sets `data-theme` and `data-accent` on
the document before React mounts. A small React context owns runtime changes. It follows operating
system changes while `theme` is `system`, listens for browser storage events, and never treats
storage as trusted input. Failure falls back in memory without an error toast because appearance is
optional and no user work was lost.

The reusable appearance panel lives in the account menu. It offers light, dark, and system choices
plus labeled accent swatches. A future settings page may render the same panel.

### Components and packages

Install packages in `web/` with pnpm and commit the lockfile. Add:

* `@fontsource-variable/montserrat`
* `lucide-react`
* `class-variance-authority`, `clsx`, and `tailwind-merge`
* `gsap` and `@gsap/react`
* `date-fns`
* `sonner`
* the Radix packages selected by the shadcn component generator

Initialize shadcn with a checked in `web/components.json`, the `@/` alias, Radix primitives, CSS
variables, and `web/src/components/ui` as the generated component location. Install exactly:

* button, input, label, select, checkbox, radio group, switch
* card, table, badge, tabs, separator
* dialog, dropdown menu, tooltip, sheet
* alert, skeleton, and Sonner

Compose product patterns outside `components/ui`: `AppShell`, `FormField`, `EmptyState`,
`ErrorState`, `AppearancePanel`, `ResponsiveTable`, and `PageEntrance`. Product components have named
exports. CVA owns finite variants, and one `cn` helper merges conditional Tailwind classes. Lucide
icons use explicit named imports, never a dynamic icon namespace.

React Hook Form and Zod wait for the first real validated form. TanStack Table waits for the first
table that needs sorting, filtering, selection, or pagination. The semantic table and form field in
this feature do not pretend to own those behaviors.

### Responsive and interaction rules

Mobile base styles come first. Normal controls use a 44 pixel minimum target, and primary actions
may use 48 pixels. The phone shell has a compact top bar and bottom navigation for frequent
destinations. The shell receives typed destination objects from its caller and shows at most five
primary phone destinations. Remaining and secondary actions use labeled menus. The foundation does
not hardcode routes owned by later features. Wide screens replace the bottom navigation with a
narrow icon rail and an optional contextual panel.

`ResponsiveTable` requires both a semantic table renderer and a phone card renderer from its caller.
It does not guess which columns may disappear. True two dimensional grids may scroll while keeping
labels and keyboard access intact.

Focus uses a visible two pixel ring with offset and at least 3 to 1 contrast against adjacent
colors. Dialogs and sheets trap and restore focus. Menus, tabs, radio groups, and selects follow
their Radix keyboard models. Dynamic success uses a polite live region. Blocking errors use an
alert. Toast copy is supplied by callers.

CSS handles color, opacity, and small transform transitions. `PageEntrance` is the only foundation
pattern that uses GSAP. It uses `useGSAP` with a ref scope, transform and `autoAlpha` properties,
automatic cleanup, and `gsap.matchMedia` for responsive and reduced motion behavior. Reduced motion
renders the final state immediately.

### Language and date boundary

Feature 6 uses `date-fns` for pure date formatting and locale aware gallery examples. Callers pass a
locale explicitly. This feature does not decide calendar arithmetic, tutor timezone conversion, or
session recurrence. Feature 8 owns those business rules. All visible component text is required from
the caller, so Feature 20 can add `react-i18next` without rewriting primitives.

### Class color contract

Feature 6 exports the seven color identifiers, labels, swatch tokens, and
`suggestClassColor(classId)`. The suggestion applies 32 bit FNV 1a to the UTF 8 bytes of the UUID
string, then uses the unsigned result modulo the ordered palette. It is repeatable across devices and
unaffected by a class rename. Feature 8 adds the authoritative class color field, API value, and
tutor override. A future feature may add custom colors without changing the seven identifiers
already stored.

## Feature design

**Data model sketch**:

There is no server entity, database migration, or API schema change. The only persisted value is the
versioned `AppearancePreference` JSON in browser storage. It has required `theme` and `accent` fields
from the finite unions above. Unknown fields are ignored and unknown values invalidate only that
field.

**State transitions**:

* Theme: absent or invalid becomes `system`; `system` resolves from the operating system and follows
  later operating system changes; a manual choice becomes `light` or `dark` until changed again.
* Accent: absent or invalid becomes `blue`; a valid manual choice replaces it immediately.
* Storage: writable preferences synchronize to other tabs; blocked storage leaves a valid in memory
  preference for the current tab.

**Interface surface**:

| Interface | Key inputs | Key outputs | Access | Key failures |
|---|---|---|---|---|
| `AppearanceProvider` | initial document attributes, system theme, storage | resolved theme and accent, setters | whole app | invalid or blocked storage falls back in memory |
| `AppearancePanel` | current choices, setters, caller text | accessible theme and accent controls | signed in shell | unavailable storage does not disable controls |
| `ResponsiveTable` | rows, semantic table renderer, phone card renderer | table on wide screens, cards on phones | caller controlled | empty data uses caller empty state |
| `PageEntrance` | scoped children | one coordinated entrance or immediate final state | caller controlled | reduced motion skips animation |
| `suggestClassColor` | `class_id: string` | one `AccentChoice` | pure function | empty input returns `blue` |
| `AppShell` | typed primary and secondary destinations, page content | phone and wide navigation layouts | signed in shell | more than five primary phone destinations move to the menu |
| `/design-system` | none | component and state gallery | development only | absent from production route tree and bundle |

**Value sourcing**:

| Action | Value produced or displayed | Source |
|---|---|---|
| Boot appearance | theme choice | validated `vermouth.appearance.v1.theme`, else `system` |
| Boot appearance | resolved light or dark theme | manual choice, else `prefers-color-scheme` |
| Boot appearance | accent | validated `vermouth.appearance.v1.accent`, else `blue` |
| Change appearance | next theme or accent | the labeled control selected by the tutor |
| Render component copy | labels, hints, errors, and announcements | required caller props |
| Format gallery date | text and calendar conventions | fixed example date plus explicit `date-fns` locale |
| Render shell navigation | labels, destinations, active state, and icon | typed caller supplied destination objects plus the current TanStack route |
| Suggest class color | palette identifier | unsigned 32 bit FNV 1a of UTF 8 `class_id`, modulo the ordered seven color palette |
| Render class identity | name, tint, and marker | caller class name plus chosen or suggested color identifier |

**Key invariants**:

* Components consume semantic tokens, never raw palette colors.
* Accent choice never changes semantic status meaning.
* Color never carries meaning alone.
* Appearance storage is optional, finite, versioned, and validated before use.
* Every interactive component remains keyboard usable and visible in both themes.
* Visible text comes from callers. Components contain no hidden English defaults.
* The gallery is development only and cannot enter the production import graph.
* Class color identifiers remain stable when more custom colors are added later.
* The phone shell renders at most five primary destinations and never invents feature routes.

**Security model**:

Appearance choices are private browser preferences, not authentication or authorization state. They
contain no personal or regulated data. Browser storage is untrusted input and may select only known
finite values. No component accepts raw HTML, remote theme CSS, arbitrary class names, or arbitrary
color values from storage.

No environment variables, secrets, external accounts, or backend permissions are required.

**Critical test scenarios**:

* Happy path: choose dark mode and rose, reload, open a second tab, and observe the same accessible
  appearance without a light flash, verifies **AC-2**, **AC-5**, and **AC-8**.
* Failure case: block storage and inject malformed values, then confirm the app renders with system
  theme and blue while current tab controls still work, verifies **AC-3**.
* Responsive path: inspect the shell, a long Vietnamese form, a table, a dialog, and a class card at
  phone width, wide width, 200 percent zoom, and keyboard only, verifies **AC-4**, **AC-5**,
  **AC-6**, and **AC-7**.
* Motion path: enable reduced motion and confirm every page and component renders in its final state
  with no delayed interaction, verifies **AC-9**.
* Color path: change app accent and class color independently in both themes, then confirm semantic
  statuses and text contrast do not change meaning, verifies **AC-1**, **AC-5**, and **AC-10**.
* Production path: build the web app and confirm `/design-system` and its module are absent, verifies
  **AC-8**.

## Build plan

1. Write `web/design.md`, add Montserrat, and establish the Tailwind v4 primitive and semantic
   tokens for one light theme, one dark theme, blue accent, one class color, focus, spacing, radius,
   and motion. Build the appearance boot path, one button, one form field, one class card, and the
   development gallery as the first end to end thread, satisfies **AC-1**, **AC-2**, **AC-3**,
   **AC-5**, **AC-8**, and **AC-10**.
2. Add all seven accent mappings, the remaining class color mappings, appearance context, cross tab
   synchronization, and the account menu appearance panel, satisfies **AC-2**, **AC-3**, **AC-5**,
   and **AC-10**.
3. Initialize shadcn and install the agreed primitives in dependency order. Apply semantic tokens,
   CVA variants, explicit Lucide imports, state contracts, and accessible names, satisfies **AC-4**
   and **AC-5**.
4. Compose the app shell, navigation, responsive table, empty state, error state, and feedback
   patterns. Add typed caller supplied destinations and the five item phone limit. Prove phone, wide,
   keyboard, zoom, and long Vietnamese behavior in the gallery, satisfies **AC-4**, **AC-5**,
   **AC-6**, and **AC-7**.
5. Add `date-fns` formatting examples and the `PageEntrance` GSAP pattern with scoped cleanup and a
   complete reduced motion path, satisfies **AC-7** and **AC-9**.
6. Remove temporary styling from the sign in and thread screens by assembling them from the new
   shell and components. Register the gallery through a lazy import inside an
   `import.meta.env.DEV` branch in the existing manual route tree, then assert the production route
   tree and bundle exclude it, satisfies **AC-1**, **AC-4**, **AC-5**, **AC-6**, and **AC-8**.

## Consequences

**Positive**:

* Later screens begin from an accessible visual and interaction contract.
* Theme and class colors are flexible without mixing identity with status meaning.
* The gallery makes visual drift and missing states visible before feature pages depend on them.

**Negative and tradeoffs**:

* Both themes and seven accents multiply the visual states that must be checked.
* shadcn source files are owned code and must be maintained rather than upgraded as one package.
* Montserrat is distinctive in headings but needs careful weight and line height in dense tables.
* GSAP adds bundle cost for one small pattern. It stays because later roadmap motion can reuse the
  same cleanup and reduced motion contract.
* Browser only appearance preferences do not follow the tutor to another device.

**Neutral**:

* Class color persistence remains part of Feature 8 and requires no migration here.
* Translation state remains part of Feature 20.
* React Hook Form, Zod, and TanStack Table remain deferred until their behaviors exist.

## Follow-up

* [ ] Add the installed `shadcn-ui` skill to `web/AGENTS.md` so later web work discovers it.
* [ ] Connect the official shadcn MCP in Codex user settings before implementation if live registry
  search and component retrieval are wanted.
* [ ] Feature 8 should add the authoritative class color field and API value using the stable seven
  color identifiers from this spec.
* [ ] Feature 20 should add `react-i18next`, Vietnamese as the default language, and English
  switching without adding copy defaults to primitives.

## Rationale

Reasoning and options: see [rationale.md](rationale.md).
