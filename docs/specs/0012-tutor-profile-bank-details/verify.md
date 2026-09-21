# Verification plan for tutor profile and bank details

Run these checks after the build. Each result should retain its request id where a boundary call is
involved.

1. Start with no profile row. Call `GET /api/invoice-profile` twice and confirm one empty revision
   zero row, `is_complete: false`, and the same ordered missing field codes. This verifies **AC-2**,
   **AC-4**.
2. Save only legal name with revision zero. Confirm normalization, revision one, persistence after
   reload, and incomplete status in the page and account panel. This verifies **AC-1**, **AC-3**,
   **AC-6**.
3. Complete every field through a bank selection. Confirm billing stores the catalog short name,
   never a request supplied bank name, and the page becomes complete. Confirm the committed catalog
   records its official endpoint, capture instant, source checksum, exact source field mapping, and
   code ordering. This verifies **AC-3** to **AC-6**.
4. Try every length boundary, control character, line break, blank value, account character class,
   unknown bank, and inactive bank. Confirm `422` has stable field and reason codes with no entered
   value. This verifies **AC-6**, **AC-8**, **AC-9**.
5. Commit a save and drop its response. Retry the old revision and identical values. Confirm `200`
   returns the current row and revision increases only once. This verifies **AC-7**.
6. Submit two different saves from one revision concurrently. Confirm one changes the row and one
   receives `409 profile_conflict` with only the current revision. This verifies **AC-7**, **AC-11**.
7. Retire the saved bank in a test catalog. Confirm it remains readable, is absent from choices, and
   blocks a changed save until another active bank is selected. This verifies **AC-5**, **AC-8**.
8. Load a migrated row with a free text bank name and no code, then a row with an unknown code.
   Confirm the first shows a read only previous name hint and the second shows inactive. Preserve
   both until an active selection replaces them. This verifies **AC-4**, **AC-8**.
9. Change a catalog short name without changing its code. Confirm an unrelated save preserves the
   stored name and revision behavior. Select another code and confirm only that selection derives a
   new name. This verifies **AC-3**, **AC-7**, **AC-8**.
10. Replay `identity.tutor.registered` after a complete profile exists. Confirm every profile field,
   timestamp, and revision remains unchanged. This verifies **AC-13**.
11. Use tutor B's token after tutor A saves a profile. Confirm B sees a separate empty row and cannot
   name A in a request. Inspect logs and broker topics for absence of profile values. This verifies
   **AC-9**, **AC-13**.
12. Call `PUT` before `GET`, submit unknown JSON properties and omitted keys, submit a current
    revision no op, and run concurrent identical saves. Confirm seeding, `400` boundaries, and one
    revision change follow the contract. This verifies **AC-2**, **AC-6**, **AC-7**.
13. Fail the profile read, bank read, and save separately. Confirm the three recovery states preserve
    or hide the form exactly as specified. This verifies **AC-11**.
14. Make the form dirty, trigger a background refetch, and exercise app navigation, reload, and
    close. Confirm the draft remains. Then clear one complete field
    and inspect the confirmation copy and focus return. This verifies **AC-12**.
15. Start a save and confirm editing is disabled until it settles. Sign out with profile and catalog
    requests in flight, then sign in as another tutor and confirm no old result or editor value
    appears. This verifies **AC-11**, **AC-14**.
16. Use keyboard only at phone width and 200 percent zoom in both themes. Search by official name,
    short name, code, and Vietnamese text without accents. Confirm visible focus, correct announcements,
    `đ` matching, composed and decomposed accent matching, 44 pixel targets, long copy wrapping, and
    no obscured control. This verifies **AC-5**, **AC-10**.
17. Inspect response headers and browser query memory. Confirm profile responses are `no-store`, the
    bank catalog has a private one day cache, and sign out removes both queries. This verifies
    **AC-14**.
18. Run the billing migration down and up, and record that the down step deliberately loses the new
    code and revision columns rather than promising a data preserving round trip. Regenerate Go and
    browser types, run the focused Go and Vitest suites, then run `task check`, `task test`,
    `task web:build`, and the extended `task thread`. This verifies **AC-2** to **AC-14**.
