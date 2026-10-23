# Route kink removal with TSS awareness: design

Date: 2026-10-23
Status: approved

## Intent

seaRoute finds the shortest route over the Eurostat MARNET 5 km network.
The path must go node to node, so where MARNET nodes sit off the ideal
line the route bends for no reason (e.g. the zig-zag off Dover in the
2026-10-22 screenshot). A navigator would never draw that track.

The goal is a route that looks like a track a navigator would draw:
straight where it safely can be, following traffic separation schemes
(TSS) the correct way, and honest about where the data is too thin to be
sure. Users must be able to trust it, so every change to MARNET's path is
labelled with why it was made, and anything uncertain is left to the user.

Measured today (MARNET only, A*): routes take 1–4 ms; Rotterdam–Singapore
has 19 path vertices that add more than 3 % locally, Southampton–Hamburg
19, New York–Lisbon 5.

## Goals

1. **Kink removal** replaces runs of MARNET nodes with straight segments
   where that is safe, and reports each change.
2. **TSS awareness** from OpenStreetMap (OpenSeaMap tags): the route never
   crosses a separation zone or line, and inside a lane runs with the
   lane's direction. Where OSM has a scheme and MARNET runs the wrong lane,
   the router moves to the correct lane.
3. **User proposals** for every shortcut the system finds but is not
   confident about. The user accepts or rejects each one.
4. **Honesty:** every stretch of the route carries a provenance flag, and
   the README and about page carry a Limitations section.
5. **Hooks for crowd learning** (later phase, see below): stable proposal
   IDs so acceptances can be counted later.

## Non-goals

- Weather routing, and the weather-along-route feature (separate design,
  built after this one, on the smoothed route).
- Under-keel clearance, draught, depth contours, or chart-grade accuracy.
- Replacing MARNET with a different network.
- A full ENC (S-57/S-101) integration. Licensed ENC data is documented as
  the authoritative alternative, not implemented.

## Anti-goals (a failure even if it technically works)

- Any applied or proposed segment that crosses land.
- Any segment that crosses a known TSS separation zone or separation line,
  or runs against a known lane. The only exception is the flagged
  `tss-unresolved` stretch (see "When TSS rules leave no route").
- A shortcut that silently changes which passages the route uses (Suez,
  Kiel, Panama, ...), or makes the route longer.
- Uncertain changes applied without telling the user.
- Distances, ETA, manifest, simulator or export that disagree with each
  other or with the line drawn on the map.
- Crowd acceptances that change everyone's route without the owner's
  approval.

## Decisions made with the owner

### Kink removal toggle and clearance

- A **"Remove kinks"** checkbox in the route panel, with a **clearance**
  setting next to it (NM, default **2 NM**, user adjustable).
- Checkbox off: no shortcuts applied, nothing suggested. TSS handling
  still applies (see "TSS is always on").
- Checkbox on: confident shortcuts are applied, uncertain ones are offered.
- Builder's call (default): checkbox **on** by default; clearance range
  0.5–10 NM in 0.5 NM steps.

### Three classes of shortcut

A candidate shortcut replaces path vertices `i..j` with one straight
segment `P[i] -> P[j]`. It is classified as:

1. **Applied automatically** (`smoothed`) when all hold:
   - the segment keeps at least the clearance from land everywhere;
   - it lies in an area with OSM TSS coverage (see "TSS coverage areas")
     or in open sea (farther than 20 NM from land, the same line as the
     `tss-unknown` rule), and does not touch any TSS scheme area (see
     "Shortcuts and schemes");
   - it keeps the same passage sequence as the MARNET path it replaces;
   - it is shorter than the replaced vertices.
2. **Proposed to the user** (`proposal`) when it does not touch land and
   keeps the passage sequence, but:
   - it passes closer to land than the clearance; or
   - it meets the clearance but lies in a `tss-unknown` stretch (see
     "Unknown TSS"), so an unknown scheme may exist.
3. **Never offered** when it touches land (including the land-data error
   buffer below), crosses a known separation zone or line, runs against a
   known lane, changes the passages, or is not shorter.

"Touches land" includes a buffer equal to the coastline data's own
maximum simplification error, so a segment cannot pass over a rock the
simplified data smoothed away.

The first and last legs (origin to network, network to destination) are
left as MARNET produces them: a port sits on the coast and cannot meet the
clearance. They are flagged `connector`.

### TSS is always on (Claude's call)

Following a known scheme is a correctness rule, not cosmetic, so TSS
handling (one-way lane edges, blocked edges) is always applied, in the UI,
REST and MCP, independent of the "Remove kinks" checkbox. One graph is
built at startup. Pure MARNET stays reachable internally (an `Options`
field not exposed in the API) as a baseline for tests and comparisons.
Note: ShoreWatch's `distance_to_go` will then return TSS-aware distances.

### Shortcuts and schemes (Claude's call)

Each OSM scheme gets a **scheme area**: the union of its zones plus a
polygon built around its lanes, boundaries, lines, roundabouts and
crossings (builder: buffered hull of all features of the scheme, buffer a
few NM so the lanes' full width is covered). A shortcut that touches any
scheme area is **never offered**. Inside a scheme the TSS-aware search
alone decides the path; that removes the need to classify crossing a lane
"with the traffic" or at right angles.

### Proposal rules (Claude's call)

- Proposals are computed in one deterministic pass over the path after
  automatic smoothing, and **never overlap**; each covers a distinct
  vertex range.
- Accepting a proposal replaces its range; smoothing is **not re-run**
  afterwards, so accepted results replay exactly.
- An `accept` ID that does not come up for this request (route, clearance
  or dataset changed) is **ignored and reported** in the response
  (`ignored_accept`), not an error.
- A proposal ID identifies the kink by the replaced MARNET node
  coordinates and dataset versions, **not by the clearance**. Accepting an
  ID that is currently classed `smoothed` is a no-op; a never-offered
  shortcut cannot be accepted whatever the ID.

### Proposals: UI and API

- Each proposal is drawn as a **dashed amber line** beside MARNET's path,
  with a label: reason, closest distance to land (NM), NM saved.
- The user **accepts or rejects** each proposal, or accepts all.
- The **server applies** accepted proposals: the request carries the
  accepted proposal IDs, so the API, MCP tools and UI always compute the
  same distances. Accepting recomputes distance, ETA, manifest, simulator
  and export.
- Accepted proposals are flagged `user-accepted` in the result and in
  every export.
- **MCP tools** return proposals but never apply them on their own; an
  agent must pass accepted IDs explicitly.

### Proposal IDs

Stable: the same kink always gets the same ID regardless of the request,
derived from the replaced MARNET node coordinates (not array indices),
the network version and the land/TSS dataset versions. Builder chooses
the hash. Same input, same ID; different datasets, different ID.

### Provenance flags

Every stretch of the returned path carries one flag:
`marnet` (unchanged), `smoothed`, `user-accepted`, `osm-tss` (routed along
an OSM lane), `tss-unknown` (coastal stretch with no OSM TSS coverage; a
scheme may exist that seaRoute does not know), `tss-unresolved` (TSS rules
lifted for one scheme because its data left no route; see below),
`connector`.

Severity in the UI and API: `tss-unresolved` is a **warning** (shown
prominently, e.g. "OSM scheme data inconsistent, check the chart");
`tss-unknown` is a quiet **info** note. The others are informational
labels.

Note in the docs: MARNET's own segments may still pass closer than the
clearance; the clearance is only guaranteed on `smoothed` segments.

### TSS from OSM

- Source: OSM ways with `seamark:type` in `separation_lane`,
  `separation_zone`, `separation_line`, `separation_boundary`,
  `separation_roundabout`, `separation_crossing`. Recon on 2026-10-22
  (OSM base 2026-05-31): 2,357 ways: 956 lanes (948 lines, way direction =
  traffic direction, verified on Dover), 365 zones (361 closed areas),
  150 lines, 878 boundaries.
- Coverage present: Dover, Ushant, Finisterre, Cabo São Vicente,
  Gibraltar, Bosporus, Bab-el-Mandeb, Hormuz, Malacca, Singapore Strait,
  Suez approaches, Netherlands coast, German Bight, Skagen, Panama, Uraga,
  Bass Strait. Thin: Dardanelles (no lanes). Missing in sample: Chesapeake
  approaches, Cape Town.
- Graph changes:
  - lane lines become **one-way edges**, joined at both ends to nearby
    MARNET nodes;
  - MARNET edges that cross a separation zone or line are **blocked**;
  - MARNET edges inside a scheme that run **against** a lane are blocked.
  - The search then picks the correct lane itself.
- **TSS coverage areas:** each OSM scheme gets a coverage polygon (builder:
  the convex hull of its features buffered by a few NM). Inside one, OSM
  data is trusted.
- **Unknown TSS (owner's decision, rule-based):** no free list of all
  IMO-adopted schemes exists, so seaRoute cannot tell "no scheme here"
  from "scheme missing in OSM". Every stretch within **20 NM of land**
  that lies outside all OSM coverage areas is flagged `tss-unknown` (info
  severity). No hand-curated scheme list is built. Connector legs keep
  their `connector` flag. This same rule decides the "no TSS data" case
  for shortcut classification above.
- **When TSS rules leave no route (owner's decision):** if blocking makes
  origin and destination disconnected (lane drawn the wrong way, lanes
  not joined to MARNET, port inside a scheme), the router does **not**
  return an error and does **not** drop TSS handling for the whole route.
  It lifts the TSS rules for the **one scheme** responsible (builder:
  retry with schemes unblocked one at a time, nearest the break first,
  until a route exists; deterministic order) and keeps them everywhere
  else. The stretch inside the lifted scheme is flagged `tss-unresolved`
  with warning severity, and the response names the scheme. If lifting
  one scheme is not enough, lift further schemes the same way, each
  flagged. A test fixture with a deliberately reversed lane covers this.
- Optional, builder's call: NOAA "Shipping Lanes and Zones" (public
  domain) to fill US waters.

### Coastline

- Source: OSM land polygons (osmdata.openstreetmap.de, ODbL), preprocessed
  offline: clipped to a corridor around MARNET, simplified, stored compact
  with a spatial index like the other datasets.
- The simplification tolerance is recorded with the dataset and added to
  every clearance check ("land-data error buffer").
- Fallback if the result is too large to ship: GSHHG full resolution.
  Natural Earth (3.3 MB zipped) is too coarse for a 2 NM margin and is not
  acceptable.
- **First plan task:** download, process and measure. Target: the shipped
  file stays within the existing dataset sizes (largest today is the EEZ
  file at ~45 MB gzipped) and loads in a few seconds at startup.

### Honesty and docs

- README and about page get a **Limitations** section: TSS data is
  community-maintained (OpenSeaMap/OSM) with incomplete coverage; a
  licensed ENC (S-57/S-101 from a hydrographic office or chart
  distributor) is the authoritative source; seaRoute is not for
  navigation and a passage plan still needs official charts.
- OSM/ODbL attribution added wherever OSM-derived data is used or shown.
- A **TSS overlay** layer alongside EEZ and ECA in the layers panel, so
  users can see the lanes the route follows.

### API surface

- `POST /waypoints` request gains: `kinks` (bool), `clearance_nm`
  (number), `accept` (array of proposal IDs). Response gains
  `proposals[]` (id, geometry, reason, min_clearance_nm, saved_nm) and
  per-stretch provenance flags.
- MCP `route` and `distance_to_go` gain the same three inputs. Builder's
  call on exact field names, consistent across REST and MCP.
- Defaults when omitted: REST and MCP use `kinks: true`,
  `clearance_nm: 2`, `accept: []`.

### Legs are rhumb lines (owner's decision)

seaRoute suggests a navigable route, not a great-circle track. Every leg
between two waypoints is a **rhumb line** (constant course, a straight
line on a Mercator chart), as a navigator lays it off.

- All distances use the rhumb-line (Mercator sailing) formula: MARNET's own
  segments, connectors, straightened legs, and every total. This replaces
  the great-circle (haversine) distance used today. On MARNET's 5 km
  segments the difference is negligible (well under 1 NM on
  Rotterdam–Singapore), so ShoreWatch's `distance_to_go` shifts only
  slightly.
- A straightened leg is one rhumb line of any length: no intermediate
  points, one waypoint pair. It is only applied if its rhumb distance is
  shorter than the MARNET stretch it replaces.
- Land clearance is checked on the Mercator chart, where a rhumb line is
  exactly straight.
- The map draws legs as straight lines on the Mercator map, which is the
  rhumb line, so the drawn track, distances, ETA, simulator and export
  agree.

### Performance

Route calculation including TSS graph and smoothing stays under 1 s for
any route (today 1–4 ms). Startup may grow by the dataset load time.

## Later phase: crowd-learned shortcuts

Not built in this phase; this phase only provides stable proposal IDs.

- Acceptances are counted per proposal ID from **distinct anonymous
  clients**; no personal data stored.
- Once a threshold is reached the shortcut is marked
  **community-confirmed** for the owner to review.
- **The owner approves** before it applies to everyone. Approved
  shortcuts become `smoothed` with a `community-approved` source.
- Rejected or stale (dataset changed) entries are dropped.
- Abuse resistance: only system-generated proposals can be accepted, so
  the worst case is bounded to shortcuts that already pass the
  never-offered rules.

## Left to the builder

- Smoothing algorithm (greedy line-of-sight / string pulling, or
  any-angle search), as long as the classes above hold and results are
  deterministic.
- Spatial index structure and dataset file format.
- How lane endpoints join MARNET nodes (distance threshold, number of
  joins).
- UI layout of the checkbox, clearance control and proposal list,
  following the existing ECDIS panel style (run the UI skills first).
- Test routes and fixtures; must include Dover both directions,
  Gibraltar, Singapore Strait, a no-OSM-TSS area, a narrow passage
  (Kiel, Bosporus), and a reversed-lane fixture for `tss-unresolved`.
