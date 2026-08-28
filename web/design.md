---
name: calm-calendar-workspace
source: spec-0008-and-supplied-schedule-reference
character: "A calm editorial workspace for a tutor who is often standing with a phone in hand. Quiet neutral surfaces keep dense teaching facts legible, while one strong class edge marker carries identity across schedules, lists, and details."
tokens: "Real values live in src/styles.css. Read them there and never duplicate them here."
contrast: "Text, controls, focus, status, accents, and class colors are checked in both themes against WCAG 2.2 AA."
---

# Vermouth design system

## Build mandate

You are designing the tutor's working surface, not a generic admin template. Every screen should
feel complete, credible, and ready for a real teaching day. Use the full shell, clear context,
honest product copy, and useful loading, empty, and error states. Keep controls close to the fact
they change. Avoid decorative dashboard metrics, oversized empty heroes, and floating forms with no
surrounding context.

## Character and direction

The visual language takes its rhythm from a paper timetable placed inside a precise digital frame.
Montserrat gives headings and controls a steady geometric voice. System monospace is reserved for
times, money, identifiers, and aligned numbers. Fine borders do most of the separating. Shadows are
quiet and appear only where an overlay or raised action needs depth.

The signature move is class identity. A class appears on a pale family surface with a stronger edge
marker and ordinary high contrast text. This device should recur in calendars, lists, and details.
It must never compete with success, warning, or destructive status colors.

## Type

Montserrat Variable is the only interface family. Regular weight carries body copy, medium carries
labels and controls, and semibold carries headings. The scale begins at a readable 16 pixel body and
grows in a minor third rhythm. Line height stays generous for Vietnamese diacritics. Long headings
wrap naturally and never rely on truncation for meaning.

## Color

Components use purpose based tokens from `src/styles.css`, never palette utility names. The neutral
foundation uses shadcn New York Zinc, with stronger semantic border steps where Vermouth needs the
44 pixel phone controls to remain visually distinct. Light and dark themes map the same background,
surface, foreground, muted, border, focus, success, warning, and destructive roles. The seven app
accents may change actions, links, selection, and focus only. They never change status meaning. The
seven class color families use their own surface and marker contract in both themes.

## Space, shape, and elevation

Spacing follows a four pixel base and an eight pixel working rhythm. Related controls sit close,
groups have a clear gap, and page sections breathe more than cards. Controls and small cards use a
medium corner, large surfaces use one larger corner, and pills are reserved for status or segmented
choices. Borders come before shadows. Floating menus, dialogs, sheets, and toasts may use the quiet
overlay shadow defined in CSS.

## Motion

Ordinary feedback uses short color, opacity, and small transform transitions. `PageEntrance` is the
only foundation pattern that may coordinate an entrance with GSAP. It scopes every target, cleans up
automatically, and renders the final state immediately when reduced motion is requested.

## Composition patterns

Phone screens use a compact top bar, one clear page title, content in a single reading flow, and a
bottom navigation with no more than five frequent destinations. Wide screens replace that bar with
a narrow icon rail and may add a contextual panel. Forms use labelled sections and persistent help.
Data tables always have a caller designed phone card view. True grids such as calendars may scroll
when both axes carry meaning.

Sign in uses a complete two part welcome surface on wide screens and one calm column on phones. The
working area uses `AppShell`, a focused page header, and grouped facts. Empty states invite the next
useful action. Errors say what failed and what the tutor can do next.

## Component and usage rules

The foundation includes app shell and navigation, button and icon button, text field, select,
checkbox, radio group, switch, form field, card, semantic table, badge, tabs, dialog, menu, tooltip,
sheet, toast, skeleton, empty state, and error state. Primitive source lives in
`src/components/ui`. Product patterns live in `src/components`.

Aceternity UI is an additional source owned component registry, configured as `@aceternity` in
`components.json`. Add its components only when a product surface needs their expressive motion or
layout, place reviewed source in `src/components/aceternity`, and replace every registry palette
value with Vermouth semantic tokens. Motion powers those components. GSAP remains the one page
entrance pattern. Both paths must honor reduced motion, caller supplied copy, and the same accessible
interaction contract.

Use the primary button once per local decision point. Use secondary or quiet variants for nearby
alternatives. Icon buttons always receive an accessible action name from their caller. Visible text
comes from callers. Components do not hide English defaults. Loading preserves the action name and
adds progress. Disabled controls remain legible and expose their state.

## Responsive and accessibility direction

Phone styles come first. Normal controls have at least a 44 by 44 CSS pixel target. Focus uses a
visible two pixel ring with offset in both themes. Keyboard order follows reading order. Dialogs and
sheets trap focus and return it to their trigger. Dynamic success uses a polite live region, while a
blocking failure uses an alert. Content must remain usable at 200 percent zoom and with long
Vietnamese labels. Color is always paired with text, an icon, or a stable shape.

## Writing

Use active voice and sentence case. Name actions by their result, then use the same words in the
result message. Labels label, hints explain, and errors give a recovery path. Vietnamese is the
default product language, while this foundation accepts either Vietnamese or English copy from its
callers so translation state can arrive later.
