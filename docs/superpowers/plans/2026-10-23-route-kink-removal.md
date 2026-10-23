# Route Kink Removal with TSS Awareness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Routes that follow OSM traffic separation schemes the right way, with MARNET's zig-zags straightened where land clearance allows, uncertain shortcuts offered to the user, and every stretch labelled with its provenance.

**Architecture:** All distances switch to rhumb lines (Mercator sailing). Two new offline importers build a land dataset (OSM land polygons, simplified, corridor-clipped) and a TSS dataset (OSM seamark ways, grouped into schemes). Package `route` loads both. The TSS changes the graph once at startup: it adds one-way lane edges and per-direction blocked edges. Smoothing runs per request after the search: greedy string-pulling over the found path, then a proposal pass. Each route returns proposals, stretches and warnings. REST, MCP and the UI pass these through unchanged.

**Tech Stack:** Go 1.26 (stdlib only, no new modules), gin, Leaflet and vanilla JS, prebuilt Tailwind (`web/tailwind`).

**Spec:** `docs/superpowers/specs/2026-10-23-route-kink-removal-design.md`

## Global Constraints

- No new Go module dependencies. Parse the shapefile and the Overpass JSON with the stdlib.
- Clearance default 2 NM. Allowed range 0.5–10 NM; the UI steps in 0.5 NM; the API accepts any value in range.
- "Remove kinks" defaults to on in the UI, REST (`kinks: true`) and MCP.
- TSS handling is always on in the UI, REST and MCP. Pure MARNET is reachable only via `route.Options.NoTSS` (not exposed).
- `tss-unknown` radius: 20 NM from land, outside every OSM coverage area. Open sea for auto-smoothing means farther than 20 NM from land.
- Flags, exact strings: `marnet`, `smoothed`, `user-accepted`, `osm-tss`, `tss-unknown`, `tss-unresolved`, `connector`. Severity: `tss-unresolved` = `warning`, `tss-unknown` = `info`, all others = `label`.
- Proposals: never overlap; smoothing is not re-run after an accept; unknown IDs go to `ignored_accept`; an ID does not depend on clearance; accepting a `smoothed` ID is a no-op (not ignored).
- Legs are rhumb lines everywhere: `route.Distance` becomes the rhumb-line distance; no great-circle tracks, no leg splitting.
- Route calculation stays under 1 s for any route, including TSS and smoothing.
- Land dataset: shipped gzip file ≤ 45 MB, load ≤ 5 s, heap after load ≤ 300 MB.
- OSM/ODbL attribution wherever OSM-derived data is used or shown: "© OpenStreetMap contributors, ODbL".
- Run UI skills before any UI change (global rule): `impeccable`, `ui-ux-pro-max`, `redesign-existing-projects`, `high-end-visual-design`.
- Downloaded data is untrusted. Keep it in its own directory under the scratchpad, out of the repo except the processed outputs.

## Decisions made in this plan (builder's calls from the spec)

- **Scheme area** = convex hull of all the scheme's feature vertices, each offset by **1 NM** in 16 directions. **Coverage area** = the same hull offset by **20 NM**. Coverage is wider, so "inside OSM coverage but outside every scheme area" is a real place where auto-smoothing applies.
- **Scheme grouping** happens at import: union-find over features whose bounding boxes lie within 3 NM of each other. Scheme ID = `tss-` + the first 8 hex digits of sha256(sorted OSM way IDs joined by `,`). The name is the first `seamark:name`/`name` tag found, else the ID.
- **Feature roles:** `separation_zone` and `separation_roundabout` (closed) block crossings. `separation_line` blocks crossings. `separation_lane` (open way) gives a direction. `separation_boundary` and `separation_crossing` add only to the scheme area. A closed `separation_lane` adds only to the area.
- **Against-lane rule:** a directed MARNET edge is blocked when its midpoint is within 1 NM of a lane polyline and its bearing differs from the bearing of the nearest lane segment by more than 90°. Only that direction is blocked.
- **Lane joins:** each lane start gets join edges *from* the 3 nearest MARNET nodes within 5 NM; each lane end gets join edges *to* the 3 nearest within 5 NM. A join is dropped if it touches land or crosses a zone or line.
- **A\* stays admissible:** landmarks are computed on the relaxed symmetric graph: every MARNET edge, plus lane and join edges in both directions, ignoring all TSS blocks. Every request graph is a subset of it, so the bound holds.
- **Snapping** only to MARNET nodes (index < `marnetNodes`), never to lane nodes.
- **Smoothing:** greedy, deterministic, `maxSpan = 40` vertices. Skip a range when it contains a passage-tagged edge, a lane/join edge, an antimeridian edge (|Δlon| > 180), or connector edges.
- **A\* bound stays valid with rhumb weights:** R × chord ≤ great-circle arc ≤ rhumb distance, so the existing chord estimate never overestimates. Landmarks are rebuilt from rhumb weights automatically.
- **Rhumb geometry:** a leg's land check runs in Mercator coordinates (x = lon, y = ln tan(π/4 + lat/2), in radians), where the leg is exactly straight. A Mercator gap converts to metres as `R × gap × cos(lat)` at the latitude of the closest point. A leg is one segment of any length; no densifying.
- **Antimeridian:** a rhumb leg uses the shorter longitude difference (wrapped to ±180°). For the land check, a leg crossing ±180° is split at the meridian into two Mercator pieces.
- **Proposal minimum:** `saved_nm ≥ 0.1`. Smoothed shortcuts only need to be shorter by more than 1 m.
- **Proposal ID** = `k-` + the first 12 hex digits of sha256(`v1|<dataset version>|` + replaced vertex coordinates formatted `%.6f,%.6f;`). Dataset version = `<network sha256[:8]>/<land header date+tolerance>/<tss header osm_base>`.
- **Stretch flag precedence** (per output edge): connector > user-accepted > smoothed > tss-unresolved > osm-tss > tss-unknown > marnet. `osm-tss` = a lane/join edge, or an edge whose midpoint lies in a scheme area of a scheme that is not lifted. `tss-unknown` = the midpoint or either end lies within 20 NM of land and outside every coverage area.
- **Lifting order** (no route): see Task 6.
- **API limits:** `accept` ≤ 200 IDs, each ≤ 32 chars, else HTTP 400.
- `kinks: false` disables smoothing and proposals. Any `accept` IDs then go to `ignored_accept`.

## Review Focus

1. **A route that starts or ends inside a scheme** (port inside Dover or Singapore Strait coverage). Expect a valid route. The connector is flagged `connector`, and there is no lifting unless the route is really disconnected. Test: `TestPortInsideScheme` (Task 6).
2. **A via-point route** (3+ points). Smoothing works per leg, and no shortcut crosses a via-point. Test: `TestSmoothingPerLegKeepsViaPoints` (Task 7).
3. **Accept IDs replayed after a clearance change.** The same ID still applies when the kink still classifies as a proposal; IDs that no longer come up go to `ignored_accept`, with no error. Test: `TestAcceptSurvivesClearanceChange` (Task 7).
4. **Hostile `accept` input** (10k IDs, 1 MB strings, non-string entries). Expect HTTP 400 fast, not slow processing. Test: `TestWaypointsAcceptLimits` (Task 8).
5. **A route across the antimeridian** (Pacific). No shortcut wraps the globe, and the totals match the drawn path. Test: `TestNoShortcutAcrossAntimeridian` (Task 7).

---

### Task 1: Rhumb-line distances everywhere

**Files:**
- Modify: `route/route.go` (`Distance`)
- Test: `route/route_test.go`

**Interfaces:**
- Produces: `func Distance(a, b Position) float64`, the rhumb-line distance in metres (same name and signature, so every caller in `route`, `main.go` and `pasgen.go` switches at once). Also `func Mercator(p Position) [2]float64` returns (x, y) in radians, for Task 3.
- Formula: Δφ = φ2−φ1; Δψ = ln(tan(π/4+φ2/2)/tan(π/4+φ1/2)); q = Δφ/Δψ, or cos φ1 when |Δψ| < 1e-12; Δλ wrapped to [−π, π]; d = R × √(Δφ² + q²Δλ²), with R = `earthRadiusM`.

- [ ] **Step 1: Write failing tests.**
  - `TestRhumbDistanceMeridian`: (0,0) to (0,1) equals 60.0 NM ± 0.1 (R × π/180).
  - `TestRhumbDistanceParallel`: (0,60) to (1,60) equals 30.0 NM ± 0.1 (departure = Δλ cos φ).
  - `TestRhumbLongerThanGreatCircle`: Rotterdam to Singapore direct, rhumb > haversine (keep the old formula as a test helper `haversine`).
  - `TestRhumbAntimeridian`: (179.5, 10) to (−179.5, 10) is about 59 NM, not about 21,000 NM.
  - `TestMercatorEquator`: `Mercator({0,0})` is (0,0).
- [ ] **Step 2: Run** `go test ./route/ -run 'Rhumb|Mercator'` and expect FAIL.
- [ ] **Step 3: Implement** `Distance` with the formula above, and `Mercator`. Update the `Distance` doc comment.
- [ ] **Step 4: Run** `go test ./...` and expect PASS. `TestRouteDistances` has wide bounds and must still pass unchanged. Log Rotterdam–Singapore before and after in the commit body; the difference must be < 1 NM.
- [ ] **Step 5: Fix the UI copy.** The A* explanation string in `web/static/script.js` (search "haversine") says "rhumb-line distance" instead.
- [ ] **Step 6: Commit** `feat(route): rhumb-line distances for every leg`.

### Task 2: Land dataset importer and measurement

**Files:**
- Create: `cmd/land-import/main.go`, `cmd/land-import/shp.go`, `cmd/land-import/main_test.go`
- Create: `dataset/land_osm.bin.gz` (generated)
- Modify: `dataset/README.md` (land section with source, licence, tolerance, measured size and load time)

**Interfaces:**
- Produces the file format read by Task 3:
  - Line 1 is magic `SRLAND1\n`.
  - Line 2 is a JSON header `{"source":"osm-land-polygons-split-4326","date":"YYYY-MM-DD","tolerance_m":<float>,"corridor_nm":30,"rings":<int>}` plus `\n`.
  - Then each ring: uvarint vertex count, then zigzag-varint deltas of lon and lat in units of 1e-6°. The first vertex is a delta from (0,0).
  - The whole file is gzip.
- Usage: `go run ./cmd/land-import -shp <land_polygons.shp> -network dataset/marnet_plus_5km.geojson -tolerance 50 -out dataset/land_osm.bin.gz`

- [ ] **Step 1: Write failing tests.**
  - `TestReadShpPolygons` writes a 2-ring polygon shapefile (type 5) into `t.TempDir()` by hand and asserts the rings and vertices come back exactly.
  - `TestSimplifyKeepsWithinTolerance`: Douglas–Peucker in metres on a 1,000-vertex noisy coastline. Every removed vertex lies ≤ tolerance from the kept polyline, and the first and last vertices are kept.
  - `TestCorridorKeepsNearRings`: one ring 10 NM from a network node is kept; one ring 100 NM away is dropped.
  - `TestEncodeRoundTrip` decodes its own output (copy the Task 3 decoder signature into the test as a helper) and gets coordinates back within 1e-6°.
- [ ] **Step 2: Run** `go test ./cmd/land-import/` and expect FAIL (undefined functions).
- [ ] **Step 3: Implement** in `shp.go`: `readShp(path string) ([][][2]float64, error)`, handling polygon type 5 only (each part is a ring). In `main.go`: `simplify(ring [][2]float64, tolM float64) [][2]float64`, `corridorCells(networkPath string, radiusNM float64) map[[2]int32]bool` (0.5° cells around MARNET vertices), and `encode(w io.Writer, hdr header, rings [][][2]float64) error`. Keep a ring if its bbox touches a marked cell. Drop rings left with fewer than 4 vertices after simplifying.
- [ ] **Step 4: Run** `go test ./cmd/land-import/` and expect PASS.
- [ ] **Step 5: Download and measure.**
  - Download `land-polygons-split-4326.zip` from osmdata.openstreetmap.de into a new empty scratchpad directory `land-src/`.
  - Run the importer with `-tolerance 50`. Record the file size and the ring and vertex counts.
  - If the file is > 45 MB gzipped, rerun at 100, then 200. If 200 still exceeds it, stop and report: the spec fallback is GSHHG full resolution and needs the owner.
  - Load time and heap are measured in Task 3 Step 5.
- [ ] **Step 6: Document** in `dataset/README.md`: source URL, download date, "© OpenStreetMap contributors, ODbL", tolerance, corridor 30 NM, size, and the exact command.
- [ ] **Step 7: Commit** `cmd/land-import`, `dataset/land_osm.bin.gz` and `dataset/README.md` as `feat(data): OSM land polygons importer and corridor dataset`.

### Task 3: Land index and clearance queries

**Files:**
- Create: `route/geo.go`, `route/land.go`, `route/land_test.go`

**Interfaces:**
- Consumes: the Task 2 file format.
- Produces:
  - `type Land struct` (unexported fields: vertices, 0.25° grid of edge refs, `Tolerance float64` metres, `Version string`).
  - `func LoadLand(path string) (*Land, error)`
  - `func NewLand(rings [][]Position, toleranceM float64, version string) *Land` for synthetic tests.
  - `func (l *Land) SegmentClearance(a, b Position, limitM float64) float64`: the smallest distance in metres from segment a–b to any land edge, capped at `limitM`. It returns 0 if they intersect. The leg is checked as a straight line in Mercator coordinates (see Decisions: Rhumb geometry), at any length.
  - `func (l *Land) PointDistance(p Position, limitM float64) float64`
- In `geo.go`: `func segSegDist(a, b, c, d [2]float64) (dist float64, closest [2]float64)` in Mercator units, plus `func splitAntimeridian(a, b Position) [][2]Position`. Uses `Mercator` from Task 1.

- [ ] **Step 1: Write failing tests**, all with a synthetic 1°×1° square island at (0..1, 50..51):
  - `TestSegmentClearanceCrossing` expects a segment through the island to return 0.
  - `TestSegmentClearanceParallel` uses a segment along lat 51.05 from lon 0 to 1. Assert ≈ 0.05° of latitude in metres (5,560 m ± 20).
  - `TestSegmentClearanceCapped` uses a far segment with `limitM` 1,000 and asserts exactly 1,000.
  - `TestPointDistance` checks a point 2 NM east of the island to ± 20 m.
  - `TestLongRhumbLegChecked`: a 600 NM leg along lat 50.5 from lon −10 to 5 crosses the island and returns 0; the same leg along lat 52 returns ≈ 1° of latitude in metres (± 200 m).
  - `TestSplitAntimeridian`: (179, 0) to (−179, 1) gives two pieces meeting at lon ±180.
- [ ] **Step 2: Run** `go test ./route/ -run 'Clearance|PointDistance|LongRhumb|SplitAntimeridian'` and expect FAIL.
- [ ] **Step 3: Implement.** The grid keys are `[2]int32{floor(lon*4), floor(lat*4)}`. A query covers the cells of the bbox expanded by `limitM` (convert to degrees of longitude at the piece's latitude). `SegmentClearance` returns early once it finds 0.
- [ ] **Step 4: Run** the same tests and expect PASS.
- [ ] **Step 5: Real-data test.** `TestLoadRealLand` loads `../dataset/land_osm.bin.gz` and logs the load time and `runtime.MemStats.HeapAlloc`. It fails if the load takes > 5 s or the heap is > 300 MB. It also asserts that `PointDistance` of Dover harbour entrance (1.335, 51.115) is < 1 NM and of mid-Channel (1.6, 50.9) is > 5 NM. Add the measured numbers to `dataset/README.md`.
- [ ] **Step 6: Run** `go test ./route/` and expect PASS (existing tests included).
- [ ] **Step 7: Commit** `feat(route): land index with segment clearance queries`.

### Task 4: TSS dataset importer and loader

**Files:**
- Create: `cmd/tss-import/main.go`, `cmd/tss-import/main_test.go`
- Create: `dataset/tss_osm.geojson.gz` (generated)
- Create: `route/tss.go`, `route/tss_test.go`
- Modify: `dataset/README.md` (TSS section, with the exact Overpass query and ODbL credit)

**Interfaces:**
- Importer input: Overpass JSON from `[out:json][timeout:300];way["seamark:type"~"^separation_(lane|zone|line|boundary|roundabout|crossing)$"];out tags geom;`. The recon copy is `recon/tss_geom.json` in the scratchpad; refetch for the shipped file.
- Importer output: a GeoJSON FeatureCollection. The top-level member `"osm_base": "<timestamp from osm3s.timestamp_osm_base>"`. Each feature has properties `{"type": "<seamark type without separation_>", "osm_id": <int>, "scheme": "tss-xxxxxxxx", "name": "<string>"}`. Geometry is a LineString, or a Polygon when the way is closed and the type is zone, roundabout or a closed lane.
- Usage: `go run ./cmd/tss-import -in overpass.json -out dataset/tss_osm.geojson.gz`
- Produces in `route`:
  - `type Scheme struct { ID, Name string; Area, Coverage []Position /* closed convex rings */; Zones [][]Position; Lines [][]Position; Lanes [][]Position /* direction = traffic */ }`
  - `type TSS struct { Schemes []Scheme; Version string }`
  - `func LoadTSS(path string) (*TSS, error)`
  - `func NewTSS(features []TSSFeature, version string) *TSS` with `type TSSFeature struct { Type, Scheme, Name string; OSMID int64; Coords []Position }` for synthetic tests.
  - `func (t *TSS) InArea(p Position) (schemeIdx int, ok bool)`
  - `func (t *TSS) InCoverage(p Position) bool`
  - `func (t *TSS) SegmentTouchesArea(a, b Position) bool`
  - `func (t *TSS) violations(path []Position, lifted map[string]bool) []string`: a test oracle. It lists each path segment that crosses a zone or line or runs against a lane by the Task 5 rule, as `"<scheme>: seg <i> <reason>"`.

- [ ] **Step 1: Write failing importer tests.**
  - `TestGroupSchemes`: two lane ways 1 NM apart plus one zone 100 NM away give 2 schemes, with IDs stable across input order.
  - `TestClosedZoneBecomesPolygon`
  - `TestUnknownTypeSkipped`
- [ ] **Step 2: Run** `go test ./cmd/tss-import/` and expect FAIL.
- [ ] **Step 3: Implement** the importer per the format above. Features are sorted by `osm_id` so the output is deterministic.
- [ ] **Step 4: Run** `go test ./cmd/tss-import/` and expect PASS.
- [ ] **Step 5: Write failing loader tests.**
  - `TestSchemeAreaBuffer`: a single lane line from (0,0) to (0.1,0). Points 0.9 NM north of the line are `InArea`; points 1.2 NM north are not. A point 19 NM north is `InCoverage`; 21 NM north is not.
  - `TestViolationsOracle`: a synthetic zone with a segment across it gives one violation; the same with that scheme lifted gives none. A segment running against a lane gives one violation.
  - `TestLoadRealTSS` loads `../dataset/tss_osm.geojson.gz` and checks there are ≥ 100 schemes. A Dover mid-strait point (1.45, 51.0) must be `InArea`, and (−30, 40) must not be `InCoverage`.
- [ ] **Step 6: Implement** `route/tss.go`. Buffering: for every feature vertex, add 16 points on a circle of radius r in local metres, then take the monotone-chain convex hull in lon/lat. Point-in-convex uses a cross-product sign test. Keep a bbox per scheme for quick rejection.
- [ ] **Step 7: Generate** the dataset (fresh Overpass fetch, UA `seaRoute-recon/1.0 (+https://github.com/captv89/seaRoute)`, no personal data in the request). Document it in `dataset/README.md`.
- [ ] **Step 8: Run** `go test ./route/ ./cmd/tss-import/` and expect PASS.
- [ ] **Step 9: Commit** `feat(data): OSM traffic separation schemes importer and loader`.

### Task 5: TSS-aware graph

**Files:**
- Modify: `route/route.go` (`Graph` fields, `New`, `Options`, `nearest`, `leg`), `route/search.go` (`search`), `route/estimate.go` (`allDistances` on the relaxed graph)
- Create: `route/tssgraph.go`, `route/tssgraph_test.go`

**Interfaces:**
- Consumes: `*Land` (Task 3) and `*TSS` (Task 4).
- Produces:
  - `type Datasets struct { Network, Land, TSS string }`. An empty `Land` or `TSS` path means that feature is off.
  - `func LoadAll(d Datasets) (*Graph, error)`. `Load(path)` stays and equals `LoadAll(Datasets{Network: path})`.
  - `func NewWith(segs []Segment, land *Land, tss *TSS) *Graph`. `New(segs)` = `NewWith(segs, nil, nil)`.
  - New `Graph` fields:
    - `marnetNodes int`
    - `flags []uint8` per directed edge, with bits `edgeLane`, `edgeJoin`, `edgeWrongWay`, `edgeTSSBlocked`
    - `scheme []int32` per edge (−1 for none)
    - `twin []int32` (index of the reverse edge)
    - `land *Land`, `tss *TSS`, `version string`
  - `Options.NoTSS bool`: internal. It skips lane, join and wrong-way edges and ignores blocks.
  - `func (g *Graph) Version() string` (format in Decisions).
  - `search(s, t int32, blocked []bool, lifted []bool, noTSS bool, alg Algorithm)`. `lifted` is indexed by scheme; nil means none are lifted. Edge rules:
    - wrong-way edges are always skipped;
    - TSS-blocked edges are skipped unless `noTSS` is set or `lifted[scheme[e]]` is true;
    - lane and join edges are skipped when `noTSS` is set.

- [ ] **Step 1: Write failing tests.** Synthetic graphs come from `NewWith` with a synthetic `NewTSS`:
  - `TestZoneBlocksCrossing`: a 3×3 grid with a zone over the centre node. The route avoids every edge into it.
  - `TestLaneOneWay`: two parallel channels A and B, with a lane drawn along A going east. Routing east uses A; routing west uses B, with no A edge.
  - `TestNoTSSMatchesMarnet`: the real graph with `NoTSS` gives the same distance as `Load(network)` for Rotterdam–Singapore.
  - `TestDoverBothWaysNoViolations`: real graph, Rotterdam→Bilbao and Bilbao→Rotterdam. `tss.violations(path, nil)` is empty.
  - `TestGibraltarSingaporeNoViolations` covers (−7.0, 35.8)→(−3.5, 36.0) and back, plus (100.0, 3.5)→(105.5, 1.6) and back.
  - `TestAStarMatchesDijkstra` must still pass on the TSS graph. Extend it to build with `LoadAll`.
- [ ] **Step 2: Run** `go test ./route/` and expect the new tests to FAIL.
- [ ] **Step 3: Implement** `route/tssgraph.go`:
  - `func (g *Graph) applyTSS(edges *[]edge)`: adds lane nodes and edges with their twins flagged wrong-way, adds join edges, and marks blocks per the Decisions rules.
  - `New`/`NewWith` build the CSR with the new per-edge arrays and fill `twin`.
  - `allDistances` ignores all flags (relaxed graph).
  - `markLargestComponent` also runs on the relaxed graph.
  - `nearest` only considers `v < marnetNodes`.
- [ ] **Step 4: Run** `go test ./route/` and expect PASS. Log the TSS build time in `TestLoadAll` and assert < 10 s.
- [ ] **Step 5: Commit** `feat(route): TSS-aware graph with one-way lanes and blocked crossings`.

### Task 6: Lifting a scheme when TSS leaves no route

**Files:**
- Modify: `route/route.go` (`Route`, `leg`, new `Route` fields), `route/tssgraph.go`
- Test: `route/tssgraph_test.go`

**Interfaces:**
- Produces:
  - `type Warning struct { Flag string `json:"flag"`; Scheme string `json:"scheme"`; Name string `json:"name"`; Message string `json:"message"` }`
  - The field `Route.Warnings []Warning` (`json:"warnings"`, never nil).
  - `Leg` gains `lifted []bool` (unexported) so Task 7 can flag stretches.
  - The message text: `"OSM scheme data inconsistent near <Name>, TSS rules lifted here; check the chart"`.
- Algorithm per leg, when `search` fails with TSS on:
  1. Search forward from `s` over allowed edges to get the reachable set R_s with distances d_s. Search backward from `t` (follow `twin` reversed: from u, visit v where edge v→u is allowed) to get R_t with distances d_t.
  2. Look for blocked edges v→u with v ∈ R_s and u ∈ R_t. Pick the scheme of the edge with the least d_s(v)+w+d_t(u); break ties by scheme ID. Lift it and retry the search.
  3. If no such edge exists, lift the scheme of the blocked edge v→u with u ∈ R_t, v ∉ R_t and the least d_t(u), then go to 1.
  4. Stop after lifting every scheme. If the search still fails, return the existing error.
  - Each lifted scheme adds one `Warning{Flag: "tss-unresolved"}`.

- [ ] **Step 1: Write failing tests.**
  - `TestReversedLaneLifts`: a synthetic single channel. Its only MARNET edges run west; the lane is drawn west, so eastbound edges are blocked as against-lane. Routing east succeeds and returns exactly one warning, naming the scheme.
  - `TestZoneOnlyPathLifts`: the only channel passes through a zone, giving one lift and one warning.
  - `TestLiftOnlyResponsibleScheme`: two schemes on the route, only one broken. Only that one is lifted, and the path keeps no violations in the other scheme (oracle with `lifted`).
  - `TestPortInsideScheme` (Review Focus 1): real graph, origin (1.45, 51.0) to Rotterdam. It succeeds with no warnings.
- [ ] **Step 2: Run** `go test ./route/ -run 'Lift|PortInside'` and expect FAIL.
- [ ] **Step 3: Implement** the algorithm above in `func (g *Graph) liftFor(s, t int32, blocked, lifted []bool) (int32, bool)`, which returns the scheme to lift.
- [ ] **Step 4: Run** `go test ./route/` and expect PASS.
- [ ] **Step 5: Commit** `feat(route): lift one scheme at a time when TSS data leaves no route`.

### Task 7: Smoothing, proposals, accept and stretches

**Files:**
- Create: `route/smooth.go`, `route/smooth_test.go`
- Modify: `route/route.go` (`Options`, `Route`, `Leg`, `Route()` assembly)

**Interfaces:**
- Consumes: `Land.SegmentClearance`, `Land.PointDistance`, `TSS.SegmentTouchesArea`, `TSS.InCoverage`, `TSS.InArea`, and `Graph.Version()`.
- Produces:
  - `Options` gains `Kinks bool`, `ClearanceNM float64` (0 means 2), and `Accept []string`.
  - `type Proposal struct { ID string `json:"id"`; Path []Position `json:"path"`; Reason string `json:"reason"`; MinClearanceNM float64 `json:"min_clearance_nm"`; SavedNM float64 `json:"saved_nm"` }`
    - `Reason` is exactly `"below clearance"` or `"no TSS data"`.
  - `type Stretch struct { From int `json:"from"`; To int `json:"to"`; Flag string `json:"flag"`; Severity string `json:"severity"`; Scheme string `json:"scheme,omitempty"` }`. `From` and `To` are indices into `Route.Path`; adjacent edges with the same flag and scheme merge into one stretch.
  - `Route` gains `Proposals []Proposal`, `Stretches []Stretch` and `IgnoredAccept []string` (all never nil).
  - `Leg.DistanceM`, `Leg.NetworkM` and `Route.DistanceM` are recomputed from the final path.
  - `func classify(a, b Position, replaced []Position, edges []int32) (class int, minClearM float64)` returns `classAuto`, `classProposal` or `classNever`.
  - `func proposalID(version string, replaced []Position) string`
- Classification of shortcut P[i]→P[j], given clearance C (metres) and land tolerance T:
  - **never** if any of these hold:
    - the range contains a skipped edge (see Decisions);
    - `SegmentTouchesArea`;
    - `SegmentClearance(…, C+T) ≤ T`;
    - the shortcut is not shorter by > 1 m.
  - **auto** if `SegmentClearance ≥ C+T` and the whole leg is either inside coverage or farther than 20 NM from land (sample the rhumb line every 5 NM for this test only).
  - **proposal** otherwise, with reason "below clearance" if the clearance is < C+T, else "no TSS data", and saved ≥ 0.1 NM. A proposal with a smaller saving is never offered.
  - `min_clearance_nm` = (clearance − T) / 1852, rounded to 0.1.
- Pipeline per leg (network vertices only, connector edges excluded):
  1. Run the auto pass.
  2. Compute IDs for each auto shortcut.
  3. Run the proposal pass on the result.
  4. Apply proposals whose ID is in `Accept`.
  5. Record the leftover IDs: those that are neither smoothed nor accepted go to `IgnoredAccept`.
  6. Compute stretches.
  - Greedy pass: for i, try j from min(i+40, last) down to i+2 and take the first match; then move to i=j, else i+1.

- [ ] **Step 1: Write failing tests.** Synthetic tests use `NewWith` plus a synthetic `NewLand`:
  - `TestZigZagSmoothed`: a zig-zag in open sea, more than 20 NM from synthetic land, becomes one segment flagged `smoothed`, and the distance drops.
  - `TestBelowClearanceProposed`: a 1.5 NM pass with C = 2 gives a proposal with reason "below clearance" and `min_clearance_nm` 1.5; the path is unchanged.
  - `TestTouchingLandNeverOffered`
  - `TestNoTSSDataProposed`: a shortcut within 20 NM of land with no coverage gives a proposal with reason "no TSS data".
  - `TestAcceptApplies`: the accepted ID gives an `user-accepted` stretch and the same distance as the proposal's `saved_nm` predicts (± 0.01 NM).
  - `TestUnknownAcceptIgnored`
  - `TestAcceptSmoothedIDNoop`
  - `TestAcceptSurvivesClearanceChange` (Review Focus 3), on the synthetic 1.5 NM pass from `TestBelowClearanceProposed`:
    - Take the ID returned at C = 2.
    - At C = 1.75 the kink is still a proposal: passing the ID applies it, and the stretch is `user-accepted`.
    - At C = 1.0 the kink is auto-smoothed: passing the ID is a no-op. The stretch is `smoothed`, and the ID is absent from `IgnoredAccept`.
    - On a different route where the kink never comes up, the ID is in `IgnoredAccept`, with no error.
  - `TestProposalsDoNotOverlap`
  - `TestKinksOffNoChanges`: `Kinks=false` gives the same path as the TSS-only route, no proposals, and all `Accept` IDs ignored.
  - `TestSmoothingPerLegKeepsViaPoints` (Review Focus 2): a 3-point route whose path still contains the snapped via node.
  - `TestLongLegIsOneSegment`: an open-ocean zig-zag 300 NM long becomes exactly 2 path vertices, and its distance equals `Distance(a, b)`.
  - `TestNoShortcutAcrossAntimeridian` (Review Focus 5): real graph, Yokohama (139.65, 35.45) to Los Angeles (−118.27, 33.73). The output path has no segment with |Δlon| > 180 other than the original MARNET ones, and `DistanceM` equals the path sum (± 1 m).
  - `TestRealKinksReduced`: for Rotterdam–Singapore and Southampton (−1.40, 50.89) to Hamburg (9.97, 53.54) with `Kinks` on, the count of vertices with local detour > 3 % is ≤ half the `NoTSS` baseline. The distance is ≤ the TSS-only distance. The passages equal the TSS-only passages. Every `smoothed` stretch has a land clearance ≥ 2 NM by an independent `SegmentClearance` check.
  - `TestNarrowPassagesSafe` covers Rotterdam→Gdynia via Kiel and Marmara (28.98, 40.98) to the Black Sea (29.15, 41.30) via the Bosporus, with kinks on:
    - The passages are unchanged ("kiel" kept).
    - Every `smoothed` stretch clears land by ≥ 2 NM.
    - Every proposal clears land by more than the land tolerance.
  - `TestRoutePerformance` covers 5 routes (the ones above plus Bilbao–Gdynia and Richards Bay–Sept-Îles), each < 1 s. It is skipped under `-short`.
  - `TestStretchFlags`: the connector edges are flagged `connector`. A Dover transit includes `osm-tss`. A Chesapeake approach (−75.8, 36.9) to New York (−74.0, 40.6) includes `tss-unknown` with severity `info`.
- [ ] **Step 2: Run** `go test ./route/ -run 'Smooth|Propos|Accept|Kink|Stretch|Antimeridian|ViaPoints|Performance'` and expect FAIL.
- [ ] **Step 3: Implement** `route/smooth.go` per the pipeline, and wire it into `Graph.Route` after each `leg()` (only when `g.land != nil && opts.Kinks`). Stretches are always computed.
- [ ] **Step 4: Run** `go test ./route/` and expect PASS. Run `go test -bench . ./route/` and paste the timings in the commit body.
- [ ] **Step 5: Commit** `feat(route): kink removal with proposals, accept IDs and provenance stretches`.

### Task 8: Startup wiring, REST API and TSS layer

**Files:**
- Modify: `main.go` (`App` load via `route.LoadAll`, `WaypointRequest`, `/waypoints` handler, layer map entry `"tss"`), `pasgen.go` (`FeatureProperties`, `WaypointItem`, `generateOutput`)
- Modify: `README.md` (API fields, defaults, Limitations, ODbL credit)
- Test: `main_test.go`

**Interfaces:**
- Consumes: `route.LoadAll(route.Datasets{Network: "dataset/marnet_plus_5km.geojson", Land: "dataset/land_osm.bin.gz", TSS: "dataset/tss_osm.geojson.gz"})`. If either optional file is missing, log a warning and continue with that feature off; the API still works.
- Produces: the REST contract used by Task 10.
  - `WaypointRequest` gains `Kinks *bool` (`json:"kinks,omitempty"`), `ClearanceNM *float64` (`json:"clearance_nm,omitempty"`) and `Accept []string` (`json:"accept,omitempty"`).
  - `FeatureProperties` gains `Kinks bool` (`json:"kinks"`), `ClearanceNM float64` (`json:"clearance_nm"`), `Proposals []route.Proposal` (`json:"proposals"`), `Stretches []route.Stretch` (`json:"stretches"`), `Warnings []route.Warning` (`json:"warnings"`) and `IgnoredAccept []string` (`json:"ignored_accept"`).
  - `WaypointItem` gains `Flag string` (`json:"flag"`): the flag of the stretch that arrives at the vertex; the origin gets `connector`.
  - `GET /layers/tss` serves `dataset/tss_osm.geojson.gz` through the existing layer handler.

- [ ] **Step 1: Write failing tests.**
  - `TestWaypointsKinkDefaults`: a request without the new fields echoes `kinks: true` and `clearance_nm: 2`, with `proposals`, `stretches` and `warnings` present as arrays.
  - `TestWaypointsClearanceRange`: 0.4 and 10.5 give 400; 0.5 and 10 give 200.
  - `TestWaypointsAcceptLimits` (Review Focus 4): 201 IDs give 400, a 33-char ID gives 400, and `accept: [1]` gives 400 (bind error).
  - `TestWaypointsTotalsMatchPath`: `total_dist` equals the sum of the waypoint `leg_distance_m` values (± 1 m).
  - `TestLayersTSS` returns 200 gzip.
- [ ] **Step 2: Run** `go test . -run 'Waypoints|LayersTSS'` and expect FAIL.
- [ ] **Step 3: Implement.**
  - Validate in the handler before routing.
  - The connector distances in `calculatePassageInfo` stay as they are; they read `leg.Path[1]` and `[len-2]`, which smoothing never changes.
  - Map `route.Options{..., Kinks: kinks, ClearanceNM: c, Accept: req.Accept}`.
- [ ] **Step 4: Run** `go test ./...` and expect PASS.
- [ ] **Step 5: Document** in the README:
  - the new request and response fields;
  - a **Limitations** section (wording from the spec's "Honesty and docs");
  - the ODbL credit and the dataset rebuild commands.
  - The Dockerfile already copies `dataset/`; confirm `.dockerignore` does not exclude `*.bin.gz` or `*.geojson.gz`.
- [ ] **Step 6: Commit** `feat(api): kinks, clearance and accept on /waypoints; TSS layer`.

### Task 9: MCP tools

**Files:**
- Modify: `mcpserver/mcpserver.go`
- Test: `mcpserver/mcpserver_test.go`

**Interfaces:**
- Consumes: the `route.Options` fields from Task 7.
- Produces:
  - `distanceIn` and `routeIn` gain the following, with jsonschema text stating the defaults:
    - `Kinks *bool` (`json:"kinks,omitempty"`)
    - `ClearanceNM *float64` (`json:"clearance_nm,omitempty"`)
    - `Accept []string` (`json:"accept,omitempty"`)
  - `distanceOut` and `routeOut` gain:
    - `Proposals []proposalOut` (`json:"proposals"`), where `proposalOut` is `{id, reason, min_clearance_nm, saved_nm, path?}` and `path` is set only with `include_path`;
    - `Warnings []route.Warning` (`json:"warnings"`);
    - `IgnoredAccept []string` (`json:"ignored_accept"`);
    - `Stretches []route.Stretch` (`json:"stretches,omitempty"`), set only with `include_path`.
  - The same validation as REST, returned as tool errors.
  - The tool descriptions say that proposals are never applied unless their IDs are passed in `accept`, and that distances are TSS-aware.

- [ ] **Step 1: Write failing tests.**
  - `TestDistanceToGoProposalsNotApplied`: the distance with proposals listed equals the distance without `accept`.
  - `TestDistanceToGoAcceptApplies`
  - `TestClearanceOutOfRangeToolError`
- [ ] **Step 2: Run** `go test ./mcpserver/` and expect FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./...` and expect PASS.
- [ ] **Step 5: Commit** `feat(mcp): kinks, clearance and accept on route and distance_to_go`.

### Task 10: UI

**Files:**
- Modify: `web/templates/page/home.gohtml` (route panel controls, proposals list, warnings banner, TSS layer toggle), `web/static/script.js` (request fields, rendering, accept flow, CSV flag column, TSS overlay), `web/templates/page/about.gohtml` (Limitations, ODbL credit)
- Rebuild: `web/static/tailwind.css` (`cd web/tailwind && npm ci && npm run build`)

**Interfaces:**
- Consumes: the Task 8 REST contract.

- [ ] **Step 1: Run the UI skills**: `impeccable`, `ui-ux-pro-max`, `redesign-existing-projects`, `high-end-visual-design`. Follow the existing ECDIS panel style (`avoid-pill`, `surface-container` tokens, mono 10px labels).
- [ ] **Step 2: Route panel.**
  - A "Remove kinks" checkbox (default checked) and a clearance stepper (0.5–10, step 0.5, default 2, unit "NM") below the avoid block.
  - Both go into the `/waypoints` body as `kinks` and `clearance_nm`, and into the share URL params `kinks` and `clr`.
- [ ] **Step 3: Proposals.**
  - Draw each proposal as a dashed amber polyline (`dashArray: '6 6'`, amber token) with a tooltip: reason, "min clearance X NM", "saves Y NM".
  - The list in the result panel has Accept and Reject per item, plus "Accept all".
  - Accept re-POSTs with the cumulative `accept` IDs, kept in memory for the current route and reset when origin, destination, avoid, kinks or clearance change.
  - Reject only hides the item.
  - Show `ignored_accept` as a toast.
  - All text goes through `escapeHtml()`.
- [ ] **Step 4: Stretches and warnings.**
  - Draw `tss-unresolved` stretches in the error colour with a banner listing each warning message.
  - Show `tss-unknown` as one quiet info line in the result panel ("Some coastal stretches have no TSS data; check the chart").
  - The waypoint table and the CSV export gain a `Flag` column.
- [ ] **Step 5: TSS overlay.**
  - Add a toggle `toggle-tss` in the layers panel next to ECA/EEZ that fetches `/layers/tss`.
  - Style it ECDIS magenta: lanes as lines with direction arrowheads every lane (a small rotated divIcon at the midpoint), zones as a translucent fill, lines dashed.
  - Map attribution adds "© OpenStreetMap contributors, ODbL" while the layer is on.
- [ ] **Step 6: About page.** Add a Limitations section (spec wording) and the ODbL credit.
- [ ] **Step 7: Rebuild Tailwind and verify in a browser.**
  - Run `go run .`.
  - Check these routes:
    - Rotterdam→Bilbao (Dover `osm-tss`, kinks gone);
    - one route with a proposal (accept it and watch the distance change);
    - one long open-ocean route: the straightened legs are single straight lines on the map;
    - Chesapeake→New York (`tss-unknown` note).
  - The console must show no CSP violations or JS errors.
  - The simulator, ETA and CSV must match the drawn path.
- [ ] **Step 8: Commit** `feat(ui): kink removal controls, proposals, TSS overlay and limitations`.

### Task 11: Finish

- [ ] **Step 1:** Run `go vet ./... && go test ./...` and expect PASS. Then run `codegraph sync` (or `codegraph init` if the index is stale).
- [ ] **Step 2:** Update the Basic Memory note `projects/seaRoute.md`:
  - status: deployed (ghcr image), CSP done (PR #7), kink removal and TSS in progress or done;
  - add a learning to `learnings/` for the relaxed-graph landmark trick for directed edges.
  - Commit and push `~/basic-memory`.
- [ ] **Step 3:** Use superpowers:finishing-a-development-branch (PR to `master`).
