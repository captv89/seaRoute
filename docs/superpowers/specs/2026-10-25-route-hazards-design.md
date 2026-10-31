# Route hazards and reviewable proposals: design

Date: 2026-10-25
Status: approved by owner 2026-10-31
Builds on: `2026-10-23-route-kink-removal-design.md` (all of its rules
still hold unless changed here)

## Intent

The owner tested the kink-removal build (branch `feat/kink-removal`) in
the browser on Rotterdam (51.95 N, 4.05 E) to Singapore (1.26 N,
103.82 E) and found four problems:

1. Proposals cannot be reviewed on the map: a proposal line cannot be
   clicked to accept or reject it, and clicking a proposal in the side
   pane does not move the map to it.
2. The MARNET route runs aground in places (Suez Canal, among others)
   and nothing says so.
3. Obvious kinks remain that were never offered as corrections (Dover
   Strait, Cabo São Vicente, Bab-el-Mandeb).
4. An accepted correction crossed land (north Sumatra), although the
   design promised a land clearance of 2 NM plus the data error buffer.

What the owner wants, in their words: the tool should **highlight the
danger first**; if it can be avoided with an alternative, **suggest
that**; if there is no way to correct it, **at least show the user that
this area needs attention**. Later, the user will edit the track by hand
to make it safe, and the hazard should then clear. "If we know it's going
on land, highlighting that itself is a great achievement."

A navigator must be able to trust the drawn track: a line that silently
crosses land is the worst outcome this tool can produce.

## Root causes (measured 2026-10-24, Rotterdam to Singapore, 2 NM)

Measured with a throwaway probe against the shipped datasets
(`land_osm.bin.gz` tolerance 50 m, 16.4 MB).

- **Sumatra crossing: a hole in the land data.** `cmd/land-import`
  keeps only land rings in 0.5° cells within 30 NM of a MARNET vertex.
  North Sumatra is not in the dataset: at Lhokseumawe (97.15 E, 5.18 N)
  the nearest land is reported as more than 50 NM away. The accepted
  proposal `k-4f6e41653f46` ran 470 NM from (94.0 E, 6.7 N) to
  (100.64 E, 3.17 N), left the covered corridor, saw no land and was
  reported "no TSS data, 2.0 NM clearance". Any long shortcut has the
  same blind spot; proposals on this route spanned up to 40 vertices
  (e.g. 4.6 E to 37.4 E across the Mediterranean, 50.9 E to 80.1 E across
  the Arabian Sea).
- **Groundings never flagged: the base route is never checked.** Only
  shortcut candidates get a land check. Checking every segment of the
  final path against land (clearance minus tolerance ≤ 0) finds 13
  aground segments: 9 in the Suez Canal, 1 in the Brest approach
  (-4.89 E, 48.39 N), 1 TSS join at Bab-el-Mandeb (42.49 E, 14.12 N to
  43.01 E, 13.21 N), and both port connectors. Another 20 segments are
  under the 2 NM clearance. In Suez the canal (about 300 m wide) is
  closed in the simplified land data, so every canal segment reads as
  land.
- **Bab-el-Mandeb join aground: join land test ignores the tolerance.**
  `joinOK` accepts a join when `SegmentClearance(a, b, 1) > 0`, without
  adding `Land.Tolerance`, so a join can pass within the data error
  buffer of land.
- **Dover kinks: TSS areas ban all shortcuts.** In the Dover Strait the
  route leaves the SW lane by a join at 2.08 E and then follows plain
  MARNET edges (path points 23 to 44) that turn 15° to 56° every 1 to
  4 NM. `classify` rejects every shortcut that touches a TSS scheme area
  (`SegmentTouchesArea`), so none are offered, even though the router
  itself is allowed to use those MARNET edges.
- **Cabo São Vicente and Bab-el-Mandeb jogs: lane and join edges.** These
  kinks lie on OSM lane or join edges, which `skipSeg` excludes from
  shortcuts by design.
- **Proposals are hard to review.** Spans of hundreds of NM that save
  0.1 to 0.2 NM cannot be seen on the map even when zoomed to.

## Goals

1. **Hazard check on the final route.** Every segment of the returned
   path (MARNET, lane, join, connector, smoothed, user-accepted) is
   checked against land. Hazards are reported in the API and MCP and
   highlighted on the map.
2. **No shortcut is ever judged where the coastline data is missing.**
3. **TSS-aware shortcuts:** shortcuts may run inside a TSS scheme area
   when they obey the TSS rules.
4. **Local proposals:** each proposal is a short, visible fix.
5. **Reviewable on the map:** proposals and hazards can be clicked on
   the map and in the side pane, and the two stay in sync.
6. **Joins respect the land tolerance.**

## Non-goals

- Manual track editing (the owner's later phase). This design only
  leaves room for it: hazards are computed from the final path, so an
  edited path can be re-checked by the same code.
- Automatic rerouting around a hazard on MARNET's own path (for example
  finding a different canal line). A hazard is reported; a fix is
  suggested only when an existing proposal resolves it (see "Hazards and
  proposals").
- Depth, draught, or chart-grade accuracy.
- Changing lane or join geometry (Cabo São Vicente, Bab-el-Mandeb lane
  jogs stay as OSM draws them).

## Anti-goals (a failure even if it technically works)

- A segment that touches land and is not highlighted.
- A smoothed or proposed segment that crosses land, including land the
  dataset does not cover.
- A TSS-area shortcut that crosses a separation zone or line, or runs
  against a lane.
- So many hazard markers on normal routes that users stop reading them.
  Port connectors and canals are expected to be aground or close; they
  are still shown but must not drown out the rest.
- Map and side pane disagreeing about what is selected, accepted or
  rejected.

## Decisions made with the owner

### Hazard severities

- **Aground (red):** the segment's clearance minus the land tolerance is
  0 or less (it touches land or the data error buffer).
- **Close (amber):** the segment is clear of land but closer than the
  requested clearance.
- One colour per severity everywhere, canals included. A canal or other
  passage segment (`pass` tag such as `suez`, `kiel`, `panama`) that is
  aground is **red like any other**, with a short note in its popup, for
  example: "Canal narrower than the land data resolves; follow the canal
  and pilotage." Mariners know why; the note is for everyone else.
- Builder's call (default): connectors get the same severities, with a
  popup note "Port approach: connects the port to the network".

### Hazard noise (owner's decision)

- Adjacent hazard segments with the same severity, kind and passage
  merge into **one entry**: all of Suez is one red entry, e.g. "Suez
  Canal: 9 legs aground".
- On the map every hazard segment is drawn in its colour.
- In the side pane, **red entries are always expanded**; **amber
  entries collapse** under one summary line, e.g. "20 close passes
  (under 2 NM)", which expands on click.
- Canal and connector hazards count as hazards like any other (red,
  listed, "needs attention" unless a proposal fixes them).

### Fix, or flag "needs attention"

- If a pending proposal replaces a hazard segment and is itself clear
  of land, the hazard entry links to it ("Fix available: accept
  shortcut"). Accepting it removes the hazard on the next compute.
- Claude's call: a merged hazard links every pending proposal that
  overlaps any of its segments (`fixes`, a list). Parts not covered by
  a proposal stay "needs attention" after the accept, and show up as a
  new, smaller hazard on the next compute.
- Claude's call: hazards have no stable ID. The UI selects them by
  their index in the current response; every recompute redraws the
  list, so there is nothing to keep in sync across computes.
- Otherwise the hazard stays listed as **needs attention**. It never
  disappears unless the path changes and the check passes.

### Close the land-data hole

A shortcut may only be classified where the coastline data is complete.
Builder's call between:

- **(a)** reject (`classNever`) any shortcut that leaves the land-import
  corridor. The corridor must be recorded in a way the router can test
  (the header already has `corridor_nm`; the cell set or a distance-to-
  network test is the builder's choice), with a margin of at least the
  clearance limit so land just outside the corridor is not missed; or
- **(b)** rebuild `land_osm.bin.gz` with a wider corridor or full
  coverage, if it stays near the existing dataset sizes (largest today:
  EEZ at 45 MB gzipped) and loads in a few seconds.

(a) is required in any case for land outside whatever corridor is
chosen; (b) is optional. Default: (a) only.

Claude's call: a shortcut partly outside the corridor is rejected
whole, not clipped. The greedy scan already falls back to shorter
spans from the same vertex, so the route still gets the in-corridor
shortcuts. With the 20 NM proposal cap, long ocean-crossing proposals
are gone anyway.

The hazard check on the final path does not have this problem for
MARNET, lane and join segments (they are inside the corridor by
construction), but `smoothed` and `user-accepted` segments must also be
covered by the same corridor rule.

### TSS-aware shortcuts (replaces "Shortcuts and schemes" in the 2026-10-23 design)

A shortcut that touches a TSS scheme area is no longer automatically
never-offered. It is allowed when, for every scheme it touches, it would
pass the rules the router applies to MARNET edges (`schemeReasons`):
it does not cross a separation zone or line, and does not run against a
lane. Shortcuts still never replace lane or join edges (`skipSeg`
unchanged). Test: the Dover Strait SW-bound stretch (points 23 to 44 in
the measured route) is straightened, both directions, with no TSS
violation.

Claude's call: reuse `schemeReasons` as is. It already checks a rhumb
leg of any length against zones, lines and lane direction, the same
way it checks MARNET edges and lane data.

The owner accepted that kinks on lane and join edges themselves
(Cabo São Vicente, Bab-el-Mandeb) stay as OSM draws them; only the
MARNET edges inside a scheme area become shortcut candidates.

Builder's call: whether such shortcuts are `classAuto` (inside OSM TSS
coverage, the rules are known) or proposals. Default: auto, which is
consistent with the coverage rule.

### Local proposals

- A **proposal** may be at most **20 NM** long. Claude's call:
  automatic shortcuts are not capped. They need no review, and the
  corridor rule already protects them; capping them would add vertices
  to every open-ocean route. The greedy scan then offers several
  consecutive proposals where one long one would have been.
- Proposals that save less than **0.1 NM** are dropped (current rule,
  unchanged).
- Builder's call: whether the cap is in NM or in path vertices; the
  intent is "one visible fix per proposal".

### Map and side pane interaction

- **Side pane list:** proposals and hazards are listed (one list or two
  sections, builder's call, following the existing ECDIS panel style).
  Clicking an entry **fits the map** to that segment (with padding,
  sensible max zoom) and highlights it.
- **Map:** proposal lines (dashed amber) and hazard segments (red or
  amber, drawn over the route) are **clickable**. Clicking opens a popup
  with details (reason, min clearance NM, NM saved or how far onto
  land, the canal or connector note) and, for proposals, **Accept** and
  **Reject** buttons; for hazards with a fix, a link to the proposal.
- Selecting on the map highlights the matching side pane entry and
  scrolls it into view; selecting in the pane highlights it on the map.
- Accept and reject behave as today (server applies accepted IDs; a
  reject only hides the proposal for this route).

### Joins

`joinOK` adds `Land.Tolerance` to its land test, as every other land
check does. If this disconnects a lane, the existing `tss-unresolved`
fallback applies.

## API surface

- `Route` gains `Hazards []Hazard`. Builder's call on exact fields;
  minimum: `from`, `to` (indices into `Route.Path`, like `Stretch`),
  `severity` (`aground` or `close`), `min_clearance_nm` (0 when aground),
  `kind` (the segment's provenance: `marnet`, `osm-tss`, `connector`,
  `smoothed`, `user-accepted`, ...), `passage` (tag or empty), and
  `fixes` (proposal IDs, may be empty).
- Adjacent segments with the same severity, kind and passage merge into one
  hazard, as stretches do.
- Hazards are computed whether or not `kinks` is on, whenever land data
  is loaded. Claude's call: without land data, `hazards` is an empty
  array, never omitted (like `proposals`).
- Accepted IDs that no longer come up (for example a proposal now over
  20 NM or outside the corridor) are ignored and reported in
  `ignored_accept`, as today. Clearance for the `close` severity is `clearance_nm`
  (default 2).
- REST `/waypoints` and MCP `route` return `hazards`; MCP
  `distance_to_go` returns them too (builder's call if that output gets
  too large: a count plus the aground ones).

## Performance

Route calculation stays under 1 s for any route. The hazard check is one
`SegmentClearance` per path segment (about 250 for Rotterdam to
Singapore), so it is cheap; measure it.

## Docs

- README and about page Limitations: the land data is simplified (50 m)
  and covers a corridor around MARNET; canals narrower than that read as
  aground; MARNET itself is not a surveyed track.

## Tests (minimum)

- Synthetic: a land ring outside the land corridor and a shortcut over
  it: never offered.
- Synthetic: a MARNET segment over land is reported `aground`; one
  within the clearance is `close`.
- Real data (behind the existing large-dataset test guard, if any):
  Rotterdam to Singapore: no smoothed or proposed segment crosses
  Sumatra; Suez canal segments are reported aground with passage
  `suez`; Dover SW-bound stretch straightened with no TSS violation;
  no join aground at Bab-el-Mandeb.
- UI: owner checks in the browser (agents run static checks and give a
  checklist).

## Left to the builder

- Corridor test structure and margin, and whether to rebuild the land
  data (see above).
- Exact `Hazard` field names, consistent across REST and MCP.
- Whether TSS-area shortcuts are auto or proposals (default auto).
- 20 NM cap in NM or vertices.
- Popup and list layout, colours from the existing theme tokens (run
  the UI skills first).
