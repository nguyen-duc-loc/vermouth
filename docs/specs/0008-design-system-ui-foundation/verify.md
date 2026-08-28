# Verify the design system and UI foundation

Use this guide with `$check verify design system & UI foundation`. Record evidence against the
acceptance criteria in `index.md`.

## Static checks

1. Run `task web:check`, `task web:typecheck`, and `task web:build`.
2. Confirm `web/design.md` covers every topic named in **AC-1**.
3. Confirm reusable components use semantic tokens and named exports, with no raw palette classes,
   hidden English defaults, arbitrary stored colors, or dynamic Lucide namespace imports.
4. Confirm the gallery uses a lazy import inside `import.meta.env.DEV` in the existing manual route
   tree, and the production route tree and output do not import it.

## Appearance matrix

Check every combination in the gallery:

| Dimension | Values |
|---|---|
| Theme | light, dark, system light, system dark |
| Accent | red, rose, orange, green, blue, yellow, violet |
| Width | 320 CSS pixels, 390 CSS pixels, wide desktop |
| Zoom | 100 percent, 200 percent |
| Input | pointer, keyboard only |
| Motion | normal, reduced motion |
| Copy | short English, long Vietnamese |

For each combination, verify readable contrast, visible focus, no clipping, no hidden action, and
no semantic status color changed by the accent.

## Preference failures

1. Start with no appearance key and confirm system theme plus blue.
2. Store malformed JSON, unknown theme, unknown accent, and an old key version separately. Confirm
   safe fallback without a blank screen or error toast.
3. Block browser storage. Change theme and accent. Confirm the current tab updates and remains usable.
4. Open two tabs with storage available. Change each preference and confirm the other tab follows.
5. Use system theme, then change the operating system preference and confirm the app follows.

## Keyboard and assistive behavior

1. Reach every gallery control in logical order without a pointer.
2. Open and close the menu, select, dialog, tooltip, sheet, tabs, and radio group with their expected
   keyboard controls.
3. Confirm dialogs and sheets trap focus while open and restore it to the trigger when closed.
4. Confirm icon buttons have accessible names, loading changes are announced politely, and blocking
   errors are announced as alerts.
5. Confirm ordinary targets are at least 44 by 44 CSS pixels and focused controls are not obscured.

## Responsive behavior

1. At phone width, confirm the top bar and bottom navigation remain reachable and the icon rail is
   absent.
2. Supply more than five primary destinations. Confirm only five appear in bottom navigation and
   the remainder stay reachable through a labeled menu.
3. At wide width, confirm the icon rail and optional contextual panel do not cover page content.
4. Confirm a semantic table becomes its caller supplied card form on a phone.
5. Confirm the true grid example scrolls without losing its labels or keyboard access.

## Motion and class color

1. Confirm the page entrance uses scoped `useGSAP` setup and leaves no inline styles or active
   animation after unmount.
2. Enable reduced motion and confirm content appears immediately in its final state.
3. Check the documented FNV 1a test vectors, then call `suggestClassColor` repeatedly with the same
   UUID and confirm the same result. Rename the displayed class and confirm its suggestion does not
   change.
4. Check all seven class surfaces in both themes. Confirm text remains readable and class identity
   includes its name rather than color alone.

## Build derived replay steps · updated 2026-08-27

These steps exercise every value source named in spec 0008. They capture the checks that found the
button slot and control border contrast defects during the build, so later verification can replay
the same edges independently.

### UI and manual

- [x] Clear `vermouth.appearance.v1`, set the operating system to light and then dark, and reload.
  Confirm the stored choice defaults to `system`, the resolved document theme follows
  `prefers-color-scheme`, and the accent defaults to `blue`. This checks all three boot appearance
  sources and **AC-2**.
- [x] Choose dark mode and violet from their labeled controls, then open a second tab. Confirm both
  tabs show the choices, the second tab follows another change, and the values survive reload. This
  checks the change appearance source and **AC-2**.
- [x] Test malformed JSON, unknown theme, unknown accent, missing storage, and a `setItem` call that
  throws. Confirm the page still renders with finite fallbacks and the current tab controls still
  change its appearance. This checks **AC-3**.
- [x] At 393 CSS pixels and at a wide desktop size, inspect every gallery section in light and dark.
  Use keyboard only, 200 percent zoom, and the long Vietnamese labels. Confirm no horizontal page
  overflow, clipped action, hidden focus, or control target below 44 by 44 CSS pixels. Open and close
  every overlay and confirm focus returns to its trigger. This checks **AC-4**, **AC-5**, **AC-6**,
  and **AC-7**.
- [x] Change every app accent while viewing success, warning, destructive, and all seven class
  surfaces. Confirm app accent does not change status meaning or class identity. Confirm text and a
  stable edge marker accompany every class color. This checks **AC-2**, **AC-5**, and **AC-10**.
- [x] Supply six primary shell destinations and secondary destinations. Confirm the current path
  determines active state, at most five primary destinations appear in the phone bar, overflow stays
  in a labeled menu, and the wide rail plus contextual panel remain usable. This checks the shell
  navigation sources and **AC-6**.
- [x] Give `ResponsiveTable` the same rows at phone and desktop widths. Confirm caller supplied cards
  appear on the phone, the semantic table appears on desktop, and an empty row set uses the caller
  supplied empty state. This checks the responsive table source and **AC-6**.
- [x] Render the fixed gallery date with explicit Vietnamese and English `date-fns` locales. Confirm
  each string follows its supplied locale and no timezone or recurrence rule is inferred. This
  checks the gallery date source and **AC-7**.
- [x] Call `suggestClassColor` with an empty identifier, repeated UUIDs, and different UUIDs. Confirm
  empty input returns `blue`, identical UTF 8 input stays stable, and the unsigned 32 bit FNV 1a
  result selects from the ordered seven color palette. Render a caller class name with the chosen or
  suggested identifier and confirm its tint and marker follow that identifier. This checks both
  class color sources and **AC-10**.
- [x] Enable reduced motion and reload the gallery. Confirm every entrance item is immediately in its
  final visible state with no delayed interaction. Disable reduced motion and confirm the one scoped
  page entrance cleans up after unmount. This checks **AC-9**.
- [x] Open `/signin` and the populated thread screen. Confirm visible component copy comes from each
  screen, the sign in link renders its icon and text as one control, and loading, projection, error,
  and account appearance states use the foundation. This checks the caller copy source and **AC-4**,
  **AC-5**, and **AC-7**.

### Commands

- [x] `task web:check` completes with no Biome diagnostic. This checks **AC-1**, **AC-4**, and
  **AC-5**.
- [x] `task web:typecheck` completes with strict TypeScript and named component exports. This checks
  **AC-4**, **AC-6**, and **AC-10**.
- [x] `task web:build` completes, then search `web/dist` for `DesignSystemPage`, `/design-system`,
  `Một ngôn ngữ chung cho ngày dạy học`, and `Chỉ có trong môi trường phát triển`. Confirm the
  search has no match and there is no separate gallery chunk. This checks **AC-8**.

### Acceptance criteria coverage

**AC-1** is covered by the static design and token checks plus the web checks. **AC-2** and **AC-3**
are covered by appearance boot, storage, controls, system changes, and tab synchronization. **AC-4**
and **AC-5** are covered by the gallery state matrix, keyboard work, focus return, target size, and
contrast. **AC-6** is covered by the shell and responsive table checks. **AC-7** is covered by caller
copy, Vietnamese labels, and explicit locales. **AC-8** is covered by the production search. **AC-9**
is covered by the normal and reduced motion paths. **AC-10** is covered by FNV 1a and the class color
render contract.
