# 0008. Design system and UI foundation rationale

## Context

The current web app intentionally has almost no visual foundation. Its Tailwind entry file imports
the framework and defers type, color, spacing, and components to Feature 6. The sign in and thread
screens prove the system pipe but are temporary. Every product screen after this feature needs forms,
dense schedules, tables, money, empty states, errors, and phone use.

The tutor often acts while standing up, so phone reach, readable density, and visible focus matter
more than decorative complexity. The product also handles schedules and invoices, which need a calm
and credible tone. Vietnamese labels can be longer than their English equivalents, and later
translation work must not require primitive component rewrites.

The supplied schedule screenshot provides a useful direction: white space, thin borders, compact
navigation, clear blue actions, and colored calendar blocks. Its desktop calendar density cannot be
copied directly onto a phone, and its solid color blocks need stronger contrast rules to become a
reusable accessible system.

## Options considered

### Option 1: Product owned semantic system on shadcn and Radix

Vermouth owns semantic tokens and composed product patterns, while shadcn supplies editable React
components built on accessible Radix primitives. (basis: `web/AGENTS.md`, the installed `shadcn-ui`
skill, and the Tailwind v4 CSS first practice)

**Pros**:

* Fits the recorded React and Tailwind stack.
* Keeps appearance decisions in the repository and makes accessibility behavior reusable.
* Adds primitives as source code, so product needs can change them without library theme overrides.

**Cons**:

* The repository owns upgrades and local changes to generated component files.
* A broad initial component set creates more theme and state combinations to verify.

### Option 2: Build all primitives directly

Build every control from native HTML, React, and Tailwind without a primitive library. (basis:
platform native semantics and the project preference for a small dependency set)

**Pros**:

* Gives complete control and the smallest dependency surface.
* Simple controls can stay very small.

**Cons**:

* Dialog, menu, select, tabs, and focus management repeat difficult accessibility work.
* Later feature teams would spend time rebuilding interaction behavior instead of product flows.

### Option 3: Adopt a complete styled component suite

Use a library that owns both component behavior and visual language. (basis: common design system
adoption practice)

**Pros**:

* Provides broad component coverage quickly.
* Central package upgrades can deliver fixes across many controls.

**Cons**:

* Its visual assumptions compete with the screenshot led Vermouth direction.
* Theme override layers make seven accents, class colors, and Tailwind ownership harder to reason
  about.

### Option 4: Keep page local Tailwind patterns

Let each screen compose utilities without a formal token or component layer. (basis: the current
temporary screen approach)

**Pros**:

* Has almost no setup cost for the next single page.
* Keeps early code close to the screen that uses it.

**Cons**:

* Focus, errors, responsive tables, and color meaning drift between screens.
* Every later feature reopens design choices, which is the exact cost Feature 6 exists to remove.

## Rationale

Option 1 is the narrowest approach that settles both visual consistency and hard interaction
behavior. The project already chose React, Tailwind v4, and shadcn for this feature. Radix carries
the keyboard and focus behavior that is expensive to reproduce, while repository owned shadcn files
keep the visual language under Vermouth control. (basis: `web/AGENTS.md`, the installed `shadcn-ui`
skill, and WCAG 2.2 keyboard and focus practices)

Montserrat gives the calm workspace a recognizable voice, but it is bundled locally so runtime font
availability is not another failure mode. Blue is the default accent because it matches the supplied
reference and keeps the seven class suggestions familiar. Letting the tutor choose the app accent is
safe because status colors remain fixed. (basis: the supplied design screenshot and shadcn theme
families)

The class suggestion uses `class_id`, not the class name. The identifier stays stable when the tutor
renames a class, so every device derives the same suggestion. Hashing the name was the runner up,
but it would unexpectedly recolor a class after a harmless wording change.

CSS remains the runner up for all motion and owns every ordinary transition. GSAP is limited to one
coordinated entrance because it is already planned for this app and gives React scoped cleanup plus
a reusable reduced motion path. Using GSAP for every control would add code without improving the
interaction. (basis: the project GSAP skills and transform based animation practice)

`date-fns` is installed now at the engineer's preference. It formats gallery examples with explicit
locales, but this spec does not let a UI foundation decide calendar arithmetic or timezone rules
that belong to the teaching workflow.

## Reference observations

The supplied screenshot shows a desktop calendar product with these useful traits:

* A narrow navigation rail and a separate contextual calendar panel.
* A white canvas, pale neutral controls, fine dividers, medium rounding, and restrained shadows.
* Blue selected states and primary actions.
* Strong green, yellow, violet, red, and blue schedule blocks.
* Dense time and day structure that carries information rather than decoration.

Vermouth adapts these traits into a phone first shell, quieter class surfaces, semantic status
tokens, visible focus, and text or icon reinforcement for every use of color.

## References

**Project sources**:

* `AGENTS.md`, Tracer Bullet delivery and web accessibility rules.
* `web/AGENTS.md`, React, Vite, Tailwind, shadcn, phone, and planned GSAP conventions.
* `web/src/styles.css`, explicit deferral of the visual foundation to Feature 6.
* `docs/specs/0002-stack-and-scaffold/index.md`, the fixed web stack.
* Installed frontend design, Tailwind v4, accessibility, shadcn, GSAP, pnpm, Vite, and TanStack
  Router skills.
* The schedule workspace screenshot supplied during the design conversation.

**Practices and standards**:

* WCAG 2.2 AA focus, contrast, keyboard, target size, zoom, and non color requirements.
* Semantic design tokens with primitive, purpose, and component layers.
* Mobile first responsive design.
* Reduced motion through `prefers-reduced-motion`.

**Links**:

* shadcn themes: https://v3.shadcn.com/themes
* Official shadcn MCP: https://ui.shadcn.com/docs/mcp
