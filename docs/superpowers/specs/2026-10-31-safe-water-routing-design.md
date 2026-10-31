# Safe-water routing with draught: design

Date: 2026-10-31
Status: approved by owner 2026-10-31
Replaces: the smoothing and proposals parts of
`2026-10-23-route-kink-removal-design.md` and
`2026-10-25-route-hazards-design.md`. Their TSS, land-index and hazard
rules still hold unless changed here.

## Intent

The owner is not happy with kink removal, nor with a track that runs
aground "happily" and only gets flagged afterwards. Today the router
searches the coarse MARNET network (which ignores the real coastline),
then tries to smooth and check the result. That order is backwards: the
track kinks where smoothing is blocked and grounds where MARNET grounds.

The goal is a **proper navigable track**: the route is computed inside
safe water from the start, so it is shortest, safe in the data we hold,
kink-free by construction, and as close to MARNET (normal shipping lanes)
as possible.

**Who it serves (owner):** someone who needs a credible distance over a
safe navigable track, with the ETA and a route picture. Not a navigator
loading the track into an ECDIS. "Safe in the data we hold and believable
to a mariner", not chart-grade.

**Industry practice we adopt (research, 2026-10-31).** Commercial voyage
planners build the vessel's safety contour from draught plus under-keel
clearance, route inside safe water, then route-check. Research routing on
obstacle data uses either a visibility graph over clearance-buffered
obstacles or an any-angle grid search (Theta*) over a land and depth
raster. Both give a "taut string": straight legs that bend only where an
obstacle forces it, so no smoothing pass is needed. Open global tools
(`searoute`, `osm-ship-routing`) are grid or network A* without draught,
made for pictures. We combine the commercial idea (safe water from
draught) with the research method (taut any-angle path), guided by
MARNET.

## Goals

1. **Safe water first.** Every leg of the returned track stays clear of
   land (with the clearance) and of water shallower than the depth limit,
   except the flagged exemptions below.
2. **Kink-free by construction.** The track turns only where land, a
   shoal, a TSS lane or a canal forces it. No smoothing, no proposals.
3. **Close to MARNET.** The track stays inside a corridor around the
   MARNET route, so it follows normal shipping lanes and does not invent
   exotic shortcuts.
4. **Draught per route** (owner), with a default. Under-keel clearance
   uses the squat formula from draught and speed (owner).
5. **Long way round, flagged** (owner): when the draught does not fit on
   the shortest way, route the long way and say why and what it costs.
6. **Credible distances:** within 2% of NGA Pub. 151 on a fixed test set.

## Non-goals

- Weather, sea state, currents, tides (owner: later).
- Chart-grade accuracy, ENC (S-57) ingestion, finer regional bathymetry
  (EMODnet, NOAA ENC): later phase. The design must let a finer depth
  source be added for a region without changing the router.
- Manual track editing (owner's later phase). Hazards are computed from
  the final path, so an edited path can still be re-checked.
- Crowd-learned shortcuts: dropped, as they depended on proposal IDs.
- Changing TSS lane or canal geometry: these stay as drawn.

## Anti-goals (a failure even if it technically works)

- A leg that crosses land or shallow water and is not flagged.
- Visible zigzags on open water or along a coast.
- A silent reroute: the user must always see when draught changed the
  route, and by how much.
- A track that leaves normal shipping lanes for a marginal saving (e.g.
  threading reef areas MARNET avoids).
- No answer at all: there is always a track, flagged where unsafe.

## Decisions made with the owner

### Remove the proposals feature (owner)

Remove smoothing (`route/smooth.go`), proposals, accept IDs,
`Options.Kinks`, `Options.Accept`, `Route.IgnoredAccept`, the MCP
`kinks`/`accept` arguments, the `/waypoints` `kinks`/`accept` fields and
the proposal UI (list, map lines, accept/reject). Keep `clearance_nm`
and the hazard list. Provenance stretches stay where still meaningful
(`marnet` becomes the new free-water kind, see below).

### Vessel inputs (owner: draught per route, sensible default)

New request inputs on `/waypoints`, MCP `route` and `distance_to_go`:

- `draught_m`: default **10**.
- `speed_kn`: the UI's existing Transit Speed slider (default 18) is now
  also sent to the server. Default 14 when absent (API, MCP).
- `vessel_type`: `tanker` / `bulk` (Cb 0.80), `general` (0.70),
  `container` (0.60). Default `bulk`.

### Depth limit and squat (owner: use the squat formula)

`depth_limit_m = draught + squat + data_margin`

- **Squat** (Barrass): open water `Cb × V² / 100`, confined water
  `Cb × V² / 50`, metres, V in knots. Confined applies only on segments
  in a known passage (canal or narrow-strait tag); everywhere else open
  water. GEBCO cannot tell confinement by itself.
- **Data margin:** `max(2 m, 10% of draught)`. GEBCO depths are
  interpolated, referenced to mean sea level not chart datum, and can be
  several metres off.
- Example: tanker, 10 m, 14 kn: squat 1.6 m, margin 2 m, limit 13.6 m.

The response returns the breakdown (`draught_m`, `squat_m`,
`margin_m`, `depth_limit_m`) so the user can see why.

### Too deep: long way round, flagged (owner)

- Route with the depth limit. If the result is longer than the route
  with no depth limit, return the constrained route plus a **notice**:
  "Draught 20.5 m: Malacca Strait too shallow (needs 23.1 m incl. squat
  and margin). Routed via Lombok Strait, +X NM / +Y h." The avoided
  stretch of the unconstrained route is returned too, and drawn faintly
  on the map.
- If no route fits at all, return the shortest unconstrained route with
  every shallow segment flagged red ("draught exceeds charted depth").

### Passages with known draught limits (Claude's call)

Canals and fixed lanes are narrower than the depth data resolves, so
they are not judged by GEBCO. A small maintained table gives each
passage tag a maximum draught, with a source cited per row, e.g. Suez
20.1 m (66 ft), Panama Neopanamax locks 15.2 m, Kiel 9.5 m. A passage
whose limit is below the draught is closed for that route (and triggers
the reroute notice). Passages without a table entry are followed as
drawn and not depth-checked.

### Port approaches (Claude's call)

The open data cannot resolve harbours. From the port position to the
first point where the track reaches safe water, the depth limit (and
land clearance, not land itself where avoidable) is not enforced. That
stretch is flagged **amber**: "Port approach: check chart and
pilotage". A cap (builder: e.g. 15 NM) stops the exemption from
swallowing a coastal leg; beyond the cap normal rules and hazards apply.

### Hazards stay as the safety net

Keep the hazard check on every final segment. Add severity **shallow**
(red): depth below the limit outside an exemption. Port approaches and
canals keep their notes. On a normal route, hazards should now appear
only at ports, canals and genuine data gaps.

## How the route is built (Claude's call, research-backed)

1. **Coarse route on the existing graph.** MARNET plus TSS lanes, with
   avoid/allow, Arctic default, TSS lifting, and draught-closed passages.
   This decides the sequence of oceans, straits and passages.
2. **Corridor.** A band around the coarse route. Wide enough to cut the
   MARNET zigzags and round headlands properly (builder: around 20–50 NM,
   narrower in straits), never wider than the area the land and depth
   data cover.
3. **Taut safe-water path inside the corridor.** Obstacles are land
   buffered by the clearance plus cells shallower than the depth limit.
   Compute the shortest any-angle path through the corridor. TSS lanes
   and passages are **pinned**: the path must enter and follow them as
   drawn (they keep their one-way and join rules), and the taut path is
   computed between pinned pieces.
4. **Legs are rhumb lines** (owner's decision in the 2026-10-23 design).
   Every leg is checked with the same land and depth query that hazards
   use, so "safe" means the same thing in search and check.
5. **Fallbacks:** if the corridor holds no safe path (data gap, too
   narrow), widen it once. If that also fails, use the coarse MARNET
   piece for that stretch, flagged by the hazard check.

### How depth reaches the coarse route (Claude's call)

The coarse search must know a strait is too shallow, or it would send a
22 m tanker into Malacca and the corridor stage could only ground it.

- **Offline per-edge controlling depth.** At startup (or in the depth
  importer), every graph edge (MARNET, lane, join) gets a controlling
  depth: the shallowest GEBCO depth sampled along the edge, at the
  depth-grid resolution. Edges in a tabled passage take the table value
  instead.
- **Per request,** edges whose controlling depth is below the depth
  limit are closed, like blocked passages today. MARNET is a dense 5 km
  network, so a shoal on one edge usually leaves a nearby open edge;
  only a real bottleneck (Malacca, Singapore Strait) closes the way.
- **Unconstrained comparison:** the same search with no depth limit,
  keeping every other option (avoid/allow, Arctic default, TSS).
- **Naming the strait:** the notice names the passage tags on the
  avoided stretch (MARNET already tags `malacca`, `dover`, `gibraltar`,
  `babelmandeb` and others), otherwise "shallow water near <lat, lon>".

### Pinned lanes and canals (Claude's call)

- In the corridor search, pinned geometry (TSS lanes, joins, passage
  edges) overrides obstacles: the path follows it even through buffered
  or shallow cells.
- Pinned pieces are still subject to the per-edge depth closure above,
  so a lane shallower than the limit is closed, not followed.
- The hazard check still runs on pinned pieces. A pinned piece that is
  close or aground in the data is flagged with its passage note, so
  nothing aground goes unflagged.

### Rhumb legs over ocean distances (Claude's call)

Legs stay rhumb lines (owner's decision). The corridor runs along the
whole route, ocean crossings included, and MARNET follows near-great-
circle tracks. The taut path is therefore a chain of rhumb legs that
stays near the great circle, which is how a great circle is sailed in
practice, and keeps the 2% target. Open ocean has no obstacles, so the
search there is cheap.

### Ports (Claude's call)

- Origin and destination are user coordinates or a port's listed
  position, joined to the network by a connector as today.
- The approach exemption is measured along the track from the port,
  until the track first reaches safe water, capped at 15 NM.
- A port outside the land and depth coverage keeps today's `unchecked`
  hazard.

### When a reroute notice fires (Claude's call)

- Compare the constrained and unconstrained tracks. Where they diverge
  and rejoin, each divergent stretch is one candidate. It becomes a
  `draught-reroute` notice when it adds 1 NM or more, or when it changes
  the set of passages used.
- The avoided stretch is the unconstrained track between the divergence
  and rejoin points.
- Smaller detours around shoals are not reroutes and get no notice. The
  track is still safe, and the hazard list shows nothing for them.

### Confined water for squat (Claude's call)

The passage draught table carries a `confined` column. Canals (`suez`,
`panama`, `kiel`, `corinth`) and narrow straits chosen by the builder
(at least `malacca`/Singapore, `dover`, `bosporus` where tagged) are
confined, with a source cited per row. Everywhere else is open water.

### GEBCO source (Claude's call)

GEBCO is not fetched yet. `cmd/depth-import` reads the GEBCO_2025
15-arc-second grid (NetCDF or GeoTIFF tiles from gebco.net, several GB,
downloaded once by hand) and clips it to the land data's coverage area.
The raw grid is never committed; only the derived dataset is.

Left to the builder: visibility graph over buffered polygons vs. Theta*
on a raster with exact vector line-of-sight checks, corridor shape and
width, raster resolution, and how pinned pieces connect. The result must
be deterministic, taut (no removable vertex), and meet the performance
budget.

## Data

- **Land:** OSM land polygons (have). `cmd/land-import` today keeps only
  land within 30 NM of a MARNET vertex. The corridor must lie inside
  covered land data, so the importer's coverage must be at least the
  corridor width plus a margin.
- **Depth:** GEBCO 2025, 15 arc-second grid (~450 m), public domain,
  attribution requested (DOI 10.5285/37c52e96-24ea-67ce-e063-7086abc05f29).
  New offline importer `cmd/depth-import` turns it into a compact dataset
  covering the same area as the land data. Only shallow water matters, so
  deep water can be stored coarsely or as a single "deep" value. Depth
  values above about 30 m are never limiting and need no precision.
- **Source per region:** the depth lookup must allow a finer source to
  override GEBCO for a region later (EMODnet, NOAA ENC soundings).
- **Disclaimer:** README and About page state that GEBCO and OSM are not
  for navigation, credit GEBCO, and say depths are approximate.

## Constraints

- **Server:** OVH VPS, 4 cores, 7.6 GB RAM (6.3 GB free). The app uses
  21 MB today. Total app memory with all datasets: under 1.5 GB. Dataset
  files committed or shipped in the image: keep each under ~200 MB
  compressed (builder may use Git LFS or download at build if larger).
- **Speed:** under 1 s per route on the VPS for any port pair, including
  the unconstrained comparison run. Startup may grow by load time.
- **Go only**, existing stack. No external routing service.

## API surface

- `/waypoints` request: add `draught_m`, `speed_kn`, `vessel_type`;
  remove `kinks`, `accept`. Response: add `depth` (`draught_m`,
  `squat_m`, `margin_m`, `depth_limit_m`), `notices[]` (kind
  `draught-reroute` or `draught-no-route`, text, `extra_nm`,
  `avoided` path); remove `proposals`, `ignored_accept`.
- MCP `route` and `distance_to_go`: same inputs and outputs.
- UI: draught input and vessel type select in the route panel; speed
  slider value sent with the request; notices shown above the hazard
  list; avoided stretch drawn faintly. Run the UI skills first.

## Tests (minimum)

- **Pub. 151 distance set:** 10 port pairs from
  `/Users/scorpion/Downloads/pub151-distances-btw-ports.pdf` (NGA Pub.
  151, 2001, public domain), port positions taken from the same book,
  within 2%. Copy the needed rows into a test fixture with page
  references. Include: Rotterdam–Singapore (Suez), New York–Colombo
  (via Gibraltar and Port Said, 8,604 NM in the preface), a transpacific
  pair, a Panama pair, a Cape of Good Hope pair, a short coastal pair, a
  Baltic pair via Kiel. Known differences (Neopanamax, post-2001 TSS)
  noted per row.
- **No grounding:** every leg of every test route clear of land and of
  the depth limit outside exemptions.
- **Taut:** no vertex can be removed without breaking clearance or a
  pinned piece.
- **Draught reroute:** Ras Tanura–Qingdao at 22 m avoids the Singapore
  Strait (via Lombok) with a notice; at 12 m it goes through.
- **Draught-closed passage:** 16 m draught does not use Panama
  Neopanamax; 21 m does not use Suez.
- **Squat:** the Barrass values for open and confined water.
- **No route:** a draught deeper than any path returns the shortest
  track with shallow hazards, not an error.
- Existing TSS tests (Dover both directions, Gibraltar, Singapore,
  reversed lane) still pass.

## Left to the builder

- Path algorithm (visibility graph or Theta* + exact checks), corridor
  shape and width, raster resolution, dataset formats.
- Depth dataset layout and compression.
- Exact passage draught table values, each with a cited source.
- Port approach cap.
- UI layout within the existing ECDIS panel style.
