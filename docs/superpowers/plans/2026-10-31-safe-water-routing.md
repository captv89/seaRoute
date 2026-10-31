# Safe-Water Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Compute every route inside safe water for the vessel's draught: a taut, kink-free chain of rhumb legs inside a corridor around the MARNET route, rerouting the long way (and saying so) when the draught does not fit.

**Architecture:** The existing MARNET+TSS graph search stays as the coarse stage, with per-edge controlling depths that close shallow edges per request. Each free (untagged MARNET) run of the coarse path is then replaced by a Theta* path over a lateral-offset lattice around it, checked with one `safeLeg` predicate (land clearance, depth limit, TSS rules); TSS lanes, joins, tagged passages and connectors are pinned as drawn. Depth comes from a new GEBCO-derived dataset; the kink-removal smoother and proposals are deleted.

**Tech Stack:** Go 1.26 (stdlib only for new code), gin, existing `route` package, vanilla JS + Leaflet UI, Tailwind prebuilt CSS.

**Spec:** `docs/superpowers/specs/2026-10-31-safe-water-routing-design.md`

## Global Constraints

- Legs are rhumb lines (`route.Distance`, `rhumbSamples`); never great-circle legs.
- Defaults: `draught_m` 10, `speed_kn` 14 (API/MCP; UI sends its slider), `vessel_type` `bulk`, `clearance_nm` 2.
- Block coefficients: `tanker` 0.80, `bulk` 0.80, `general` 0.70, `container` 0.60.
- Squat (Barrass): open `Cb × V² / 100`, confined `Cb × V² / 50` (metres, V in knots).
- Data margin `max(2 m, 0.10 × draught)`. Depth limit `draught + squat + margin`.
- Port approach exemption: along track from each requested endpoint until the track first reaches safe water, capped at 15 NM.
- Reroute notice threshold: a divergent stretch adding ≥ 1 NM, or changing the passages used.
- Performance: < 1 s per route on the VPS (4 cores) including the unconstrained comparison; total app memory < 1.5 GB.
- Dataset files: each < 200 MB compressed. The raw GEBCO grid is never committed.
- No new third-party Go dependencies.
- Disclaimer copy (README and About): "Depths come from GEBCO 2025 and land from OpenStreetMap. Both are approximate and not for navigation; check the official chart."
- GEBCO credit (README, About, dataset README): "GEBCO Compilation Group (2025) GEBCO 2025 Grid (doi:10.5285/37c52e96-24ea-67ce-e063-7086abc05f29)".

## Review Focus

1. **Endpoints in shallow or inland water** (Pub. 151 Rotterdam lies up the Nieuwe Waterweg, ~20 NM from sea). Expect: a route is still returned, the approach is flagged, no error. Test added in Task 5 (`TestRouteFromInlandPort`).
2. **A route crossing the antimeridian** (San Francisco–Busan). Expect: the lattice and `safeLeg` work across ±180 and the distance stays within 2%. Covered by the Pub. 151 set (Task 9) plus `TestTautAcrossAntimeridian` in Task 5.
3. **Draught at the validation edges** (1 m, 25 m, 25.1 m, speed 0). Expect: 1 and 25 accepted, others rejected with the exact message. Test added in Task 2 (`TestCheckVesselLimits`).
4. **Via-points / multi-leg MCP `route`.** Expect: each leg is made taut separately, and the notices and hazards index the joined path. Test added in Task 5 (`TestTautMultiLeg`).
5. **No depth dataset loaded** (tests and dev runs with only MARNET). Expect: routing works, the depth limit is reported, no edge is closed by depth, and no `shallow` hazards. Test added in Task 4 (`TestNoDepthDataNoClosure`).

---

### Task 1: Remove the proposals feature

**Files:**
- Delete: `route/smooth.go`, `route/smooth_test.go`
- Create: `route/provenance.go` (moved from `smooth.go`: `Stretch`, `segConnector`, `segFlags`, `stretches`, `segFlag`, `segMid`, `pathLength`, `(*Leg).measure`, `unknownAt`)
- Modify: `route/route.go` (`Options`, `Route`, `Graph.Route`), `route/hazard.go` (drop `Fixes`, `props`), `route/options.go`, `route/options_test.go`, `route/hazard_test.go`, `route/tssgraph_test.go`
- Modify: `main.go:52-57,443-450,477-497`, `pasgen.go:36-43,62,147-153`, `main_test.go`
- Modify: `mcpserver/mcpserver.go` (inputs `kinks`/`accept`, outputs `proposals`/`ignored_accept`, `kinkOptions`, `proposalsOut`, tool descriptions), `mcpserver/mcpserver_test.go`
- Modify: `web/templates/page/home.gohtml:232-313` (smoothing card's "Remove kinks" toggle and proposals card; keep the clearance stepper, moved into the hazards section header area for now), `web/static/script.js` (all proposal code: `renderProposals`, accept/reject state, proposal layers, `kinks`/`accept` request fields)
- Modify: `README.md`, `web/templates/page/about.gohtml` (drop kink-removal text)

**Interfaces:**
- Produces: `Options{Avoid, Allow []string; Algorithm Algorithm; NoTSS bool; ClearanceNM float64}`; `Route` without `Proposals`/`IgnoredAccept`; `Hazard` without `Fixes`; `func (g *Graph) hazards(legs []Leg, path []Position, opts Options) []Hazard`; `func CheckClearance(clearanceNM float64) error` (replaces `CheckKinkOptions`, same message "clearance_nm must be between 0.5 and 10"); segment code `segConnector` only (no `segSmoothed`/`segAccepted`).

- [ ] **Step 1: Update tests first.** Delete `route/smooth_test.go`. Move into `route/hazard_test.go` any `stretches`/`segFlag` assertions from it that do not involve proposals or smoothing. In `main_test.go` and `mcpserver_test.go`, remove proposal and accept tests. Add `TestWaypointsRejectsNoKinkFields`: a POST with `"accept":["k-1"]` still returns 200 (unknown JSON fields are ignored), and the response has no `proposals` key.
- [ ] **Step 2: Run** `go test ./...` — Expected: FAIL to compile (references to removed symbols).
- [ ] **Step 3: Implement the removal.** Move the kept helpers to `route/provenance.go` unchanged except: `segFlag` has no `segAccepted`/`segSmoothed` cases, and a new code `segTaut` (Task 5) maps to `"marnet"`. Remove `Kinks`, `Accept`, `Proposals`, `IgnoredAccept`, `Fixes`, `ignoredAccept`, `newSmoother`. UI: remove the proposals card and code paths; keep the hazard list and the clearance stepper; the request no longer sends `kinks`/`accept`.
- [ ] **Step 4: Run** `go vet ./... && go test ./...` — Expected: PASS. Run `grep -rn -i "proposal\|kink\|accept" route main.go pasgen.go mcpserver web/static/script.js web/templates` — Expected: no hits except HTTP `Accept-Encoding`.
- [ ] **Step 5: Commit** `refactor(route,api,ui): remove kink smoothing and proposals`.

---

### Task 2: Vessel model, squat and passage limits

**Files:**
- Create: `route/vessel.go`, `route/vessel_test.go`, `route/passages.go`, `route/passages_test.go`
- Modify: `route/options.go`, `route/options_test.go`

**Interfaces:**
- Produces:
  ```go
  type VesselType string // "tanker", "bulk", "general", "container"
  type Vessel struct { DraughtM, SpeedKn float64; Type VesselType }
  const (DefaultDraughtM = 10.0; DefaultSpeedKn = 14.0; DefaultVesselType VesselType = "bulk")
  func (v Vessel) WithDefaults() Vessel            // zero fields -> defaults
  func BlockCoefficient(t VesselType) (float64, bool)
  func Squat(cb, speedKn float64, confined bool) float64
  func DataMargin(draughtM float64) float64
  type DepthLimit struct {
      DraughtM float64 `json:"draught_m"`; SquatM float64 `json:"squat_m"`
      MarginM float64 `json:"margin_m"`; LimitM float64 `json:"depth_limit_m"`
  }
  func (v Vessel) DepthLimit(confined bool) DepthLimit
  type PassageLimit struct { MaxDraughtM float64; Confined bool; Source string } // MaxDraughtM 0: no fixed limit
  var PassageLimits map[string]PassageLimit
  func CheckVessel(v Vessel) error // after WithDefaults
  ```
- `Options` gains `Vessel Vessel` (zero value = defaults).

- [ ] **Step 1: Write the failing tests.**
  - `TestSquat`: `Squat(0.80, 14, false)` = 1.568 ±1e-9; `Squat(0.80, 14, true)` = 3.136; `Squat(0.60, 20, false)` = 2.4.
  - `TestDataMargin`: 10 m → 2.0; 25 m → 2.5; 15 m → 2.0.
  - `TestDepthLimit`: `Vessel{10, 14, "tanker"}.DepthLimit(false)` → `{10, 1.568, 2, 13.568}`.
  - `TestWithDefaults`: `Vessel{}.WithDefaults()` == `Vessel{10, 14, "bulk"}`.
  - `TestCheckVesselLimits`: draught 1 and 25 OK; 0.9 and 25.1 → "draught_m must be between 1 and 25"; speed 0.9 and 30.1 → "speed_kn must be between 1 and 30"; type "ferry" → "vessel_type must be one of tanker, bulk, general, container".
  - `TestPassageLimits`: `suez` 20.1 confined, `panama` 15.24 confined, `kiel` 9.5 confined, `corinth` confined, `malacca` confined with MaxDraughtM 0, `dover` confined with MaxDraughtM 0; every entry has a non-empty `Source`; every key is a passage tag in `dataset/marnet_plus_5km.geojson` (load with `Load` and check `Graph.Passages()`).
- [ ] **Step 2: Run** `go test ./route -run 'Squat|Margin|DepthLimit|Defaults|CheckVessel|PassageLimits' -v` — Expected: FAIL (undefined).
- [ ] **Step 3: Implement.** Values from Global Constraints. In `passages.go`, cite each row's source in `Source` (Suez Canal Authority 66 ft; Panama Canal Authority Neopanamax 50 ft TFW; Kiel Canal (WSV) 9.5 m; Corinth Canal operator, verify the current value). If a source gives a different number than this plan, use the source and say so in the commit message.
- [ ] **Step 4: Run** the same command — Expected: PASS.
- [ ] **Step 5: Commit** `feat(route): vessel model with Barrass squat and passage draught limits`.

---

### Task 3: Depth dataset and importer

**Files:**
- Create: `internal/corridor/corridor.go` (move `corridorCells` from `cmd/land-import/main.go`, exported as `Cells(networkPath string, radiusNM, cellDeg float64) (map[[2]int32]bool, error)`), `internal/corridor/corridor_test.go`
- Modify: `cmd/land-import/main.go` (use `internal/corridor`)
- Create: `cmd/depth-import/main.go`, `cmd/depth-import/main_test.go`
- Create: `route/depth.go`, `route/depth_test.go`
- Create: `dataset/depth_gebco.bin.gz` (generated), modify `dataset/README.md`
- Modify: `route/route.go` (`Datasets` gains `Depth string`; `LoadAll` loads it; `Graph` keeps `depth *Depth`; version string includes `depthVersion`)

**Interfaces:**
- Produces:
  ```go
  const depthTileDeg = 0.5; const depthCellsPerTile = 120 // 15 arc-seconds
  type DepthTile struct { CX, CY int32; Deep bool; M []uint8 } // M: 120*120 row-major from the SW corner; nil when Deep
  type Depth struct { Version string /* "<date>/gebco2025" */ }
  func NewDepth(tiles []DepthTile, version string) *Depth
  func LoadDepth(path string) (*Depth, error)
  func (d *Depth) At(p Position) (m float64, known bool)
  func (d *Depth) SegmentMin(a, b Position) (m float64, known bool)
  ```
  Cell values: 0 = land or drying (GEBCO elevation ≥ 0), 1–254 = depth in whole metres rounded **down** (conservative), 255 = 255 m or deeper. A tile whose every cell is 255 is stored `Deep` with no cells. `known` is false outside the stored tiles. `SegmentMin` samples the rhumb leg every 200 m (ends included), skips a whole `Deep` tile at once, and returns the minimum; `known` is false if any sample is outside coverage (the minimum over the known samples is still returned).
- File format: gzip of `"SRDEPTH1\n"` + JSON header line `{"source":"gebco2025","date":...,"tile_deg":0.5,"arcsec":15,"tiles":N}` + per tile: zigzag-varint `CX`, `CY`, one byte (1 = deep, 0 = cells), then 14400 bytes when not deep.

- [ ] **Step 1: Write the failing tests.**
  - `internal/corridor`: `TestCellsCoverNetworkVertex` on a 2-segment GeoJSON fixture: the vertex's cell and its neighbours within the radius are true.
  - `route/depth_test.go`: `TestDepthAt` (one tile with cell (10,20) = 7 and the rest 255: `At` at that cell's centre returns 7, known; at a deep tile 255; outside coverage known=false); `TestSegmentMinCrossesShoal` (a leg crossing the 7 m cell returns 7; a parallel leg 2 cells away returns 255); `TestSegmentMinAntimeridian` (tiles at CX -360 and 359, i.e. either side of ±180; a leg from 179.9 E to 179.9 W is known); `TestLoadDepthRoundTrip` (write with the importer's writer, read with `LoadDepth`, `At` matches).
  - `cmd/depth-import`: `TestReadEsriASCII` on a 4×3 fixture with `NODATA_value` and negative values (−12.7 → 12; +3 → 0; −300 → 255).
- [ ] **Step 2: Run** `go test ./internal/... ./cmd/depth-import ./route -run 'Cells|Depth|SegmentMin|EsriASCII' -v` — Expected: FAIL.
- [ ] **Step 3: Implement** `route/depth.go`, `internal/corridor`, and `cmd/depth-import`. Usage: `go run ./cmd/depth-import -asc 'gebco/*.asc' -network dataset/marnet_plus_5km.geojson -corridor 30 -date 2025-06-01 -out dataset/depth_gebco.bin.gz`. Input is the GEBCO_2025 grid as **Esri ASCII** tiles (gebco.net → Gridded bathymetry data → GEBCO_2025 Grid → Esri ASCII, 8 tiles), read one line at a time without holding a whole tile in memory. Keep only the tiles in `corridor.Cells(network, 30, 0.5)`, which is the same coverage as the land data.
- [ ] **Step 4: Run** the same command — Expected: PASS.
- [ ] **Step 5: Build the dataset.** Download the 8 Esri ASCII tiles to `~/data/gebco_2025/` (outside the repo). Run the importer. Record in `dataset/README.md`: source, licence (public domain, attribution requested), the credit line from Global Constraints, the command, the tile count, the file size, and the load time. Expected: file < 200 MB. If it is larger, keep tiles only where some cell is < 60 m and treat the rest as Deep, then re-measure.
- [ ] **Step 6: Wire it.** `Datasets{..., Depth: "dataset/depth_gebco.bin.gz"}` in `main.go` and in the full-graph test helpers (`main_test.go`, `mcpserver_test.go`, `route/tssgraph_test.go`). Add `TestLoadAllWithDepth` (the graph loads; `g.Version()` contains `gebco2025`).
- [ ] **Step 7: Run** `go vet ./... && go test ./...` — Expected: PASS.
- [ ] **Step 8: Commit** `feat(data): GEBCO 2025 depth dataset, importer and depth queries` (dataset file included; use Git LFS only if it exceeds 100 MB, GitHub's hard limit).

---

### Task 4: Draught-aware coarse search, reroute notices, no-route fallback

**Files:**
- Create: `route/draught.go`, `route/draught_test.go`
- Modify: `route/search.go` (`open`, `search`), `route/route.go` (`nearest`, `hasOpenEdge`, `leg`, `Route`), `route/tssgraph.go` (`reach`, `liftFor`)

**Interfaces:**
- Consumes: `Vessel`, `DepthLimit`, `PassageLimits` (Task 2); `Depth.SegmentMin` (Task 3).
- Produces:
  ```go
  // searchCtx replaces the (blocked, lifted []bool, noTSS bool) parameters of open, search,
  // nearest, hasOpenEdge, leg, liftFor and reach.
  type searchCtx struct { blocked, lifted []bool; noTSS bool; shallow []bool } // shallow[e]: edge closed by depth; nil = none
  // Graph.minDepth []float32 per CSR edge, filled at build: Depth.SegmentMin of the edge,
  // +Inf when unknown, when there is no depth data, or when the edge's passage has a PassageLimits entry.
  func (g *Graph) shallowEdges(lim, limConfined float64, ends []Position) []bool
  type Notice struct {
      Kind string `json:"kind"` // "draught-reroute" | "draught-no-route"
      Text string `json:"text"`
      ExtraNM float64 `json:"extra_nm"`
      Avoided []Position `json:"avoided"`
      Passages []string `json:"passages"` // passage tags on the avoided stretch
  }
  // Route gains: Depth DepthLimit `json:"depth"`; ConfinedDepthM float64 `json:"confined_depth_limit_m"`; Notices []Notice `json:"notices"`
  ```
- `shallowEdges` closes edge e when `minDepth[e] < limit` (the confined limit if the edge's passage is `Confined`, else the open one), except edges with an endpoint within 15 NM (rhumb) of any requested point. Passages whose `MaxDraughtM > 0 && Vessel.DraughtM > MaxDraughtM` are added to `blocked`.
- `Graph.Route` runs the coarse search twice per leg: once constrained, and once with no depth or passage-draught closure (all other options kept). It compares the MARNET node sequences and turns each divergence into a notice under the threshold in Global Constraints. If the constrained search finds no path, it uses the unconstrained one and adds one `draught-no-route` notice.

Notice text (exact formats):
- Depth reroute: `"Draught %.1f m: %s too shallow (needs %.1f m incl. squat and margin). Routed via %s, +%.0f NM / +%.1f h."`. The first `%s` is the avoided passage tags joined with ", ", or `"shallow water near %.2f°, %.2f°"` (lat, lon of the shallowest closed edge's midpoint). The second `%s` is the constrained stretch's passage tags, or `"deeper water"`. Hours = extra NM / speed.
- Passage draught closure: `"Draught %.1f m exceeds the %s limit of %.1f m. Routed via %s, +%.0f NM / +%.1f h."`.
- No route: `"Draught %.1f m: no route deep enough (needs %.1f m). Showing the shortest route; shallow stretches are flagged."`

- [ ] **Step 1: Write the failing tests** (full graph with depth, built once per package via `sync.Once`):
  - `TestDraughtReroutesAroundSingapore`: Ras Tannurah (26.64 N, 50.16 E) to Qingdao (36.07 N, 120.32 E), `Vessel{DraughtM: 22, SpeedKn: 14, Type: "tanker"}`: the path has no point in box lon 103.4–104.6, lat 1.0–1.5; some point lies in the Lombok box lon 115.5–116.0, lat −9.0 to −8.2; there is one `draught-reroute` notice with `ExtraNM > 100` whose `Passages` contains `malacca` or whose text contains "Singapore". At `DraughtM: 12` the path does pass the Singapore box, with no notice.
  - `TestDraughtClosesSuez`: Rotterdam to Singapore at 21 m: `Passages` has no `suez`, and a notice's text contains "suez limit of 20.1 m". At 15 m, `suez` is used.
  - `TestDraughtClosesPanama`: New York (40.70 N, 74.02 W) to Los Angeles (33.72 N, 118.27 W) at 16 m: no `panama`. At 12 m: `panama` used.
  - `TestDraughtNoRoute`: a 25 m draught between two points in the Baltic (Gdańsk 54.40 N, 18.67 E to Stockholm 59.32 N, 18.08 E): a route is returned with one `draught-no-route` notice.
  - `TestNoDepthDataNoClosure`: on the graph without depth, Ras Tannurah–Qingdao at 22 m passes Singapore; `Notices` is empty; `Depth.LimitM` is 22 + squat + margin.
  - `TestNoticeThreshold`: a unit test of the divergence function on synthetic node sequences: a 0.5 NM detour with the same passages gives no notice; 1.0 NM gives one; a 0.2 NM detour that swaps a passage gives one.
- [ ] **Step 2: Run** `go test ./route -run 'Draught|NoDepth|Notice' -v` — Expected: FAIL.
- [ ] **Step 3: Implement** `searchCtx`, `minDepth` (filled in `newGraph` after the CSR arrays, computed in parallel over edges with `runtime.NumCPU()` workers; log the build time with the existing TSS stats), `shallowEdges`, the double search, the divergence walk, and the notices.
- [ ] **Step 4: Run** the same command, then `go test ./...` — Expected: PASS. The existing TSS tests must pass unchanged.
- [ ] **Step 5: Commit** `feat(route): draught closes shallow edges and passages; reroute notices`.

---

### Task 5: Taut safe-water path inside the corridor

**Files:**
- Create: `route/taut.go`, `route/taut_test.go`
- Modify: `route/route.go` (`leg` returns the coarse leg; `Route` calls `g.tautLeg` per leg), `route/provenance.go` (`segTaut`)

**Interfaces:**
- Consumes: `searchCtx` and the coarse `Leg` with `segs` (Task 4); `Land.SegmentClearance`, `Land.Tolerance`, `Graph.inCorridor` (existing); `Depth.SegmentMin`, `Depth.At` (Task 3); `TSS.breaksRules` (existing).
- Produces:
  ```go
  const segTaut int32 = -2
  type pathCtx struct { clearM, limitM, limitConfinedM float64; ends []Position } // ends: requested points (approach exemption)
  func (g *Graph) safeLeg(a, b Position, c *pathCtx) bool
  func (g *Graph) tautLeg(l *Leg, c *pathCtx)        // rewrites l.Path and l.segs in place, then l.measure()
  func (g *Graph) tautRun(pts []Position, c *pathCtx) ([]Position, bool) // ends fixed; false = no safe path
  ```
- `safeLeg(a, b)` is true when all of these hold:
  - land: `SegmentClearance(a, b, clearM+tol) ≥ clearM + tol`;
  - depth: `SegmentMin` is unknown or ≥ the limit (the confined limit inside a `Confined` passage area);
  - TSS: `tss == nil || !tss.breaksRules(a, b)`;
  - coverage: `inCorridor(a, b, 4 NM)`.
- Within 15 NM (rhumb) of any point in `ends`, the clearance and depth checks are relaxed to "does not touch land": `SegmentClearance > tol`.

Algorithm (the plan fixes it; implement as written):
1. Split the coarse leg's segments into **pinned** and **free** runs.
   - Pinned: `segConnector`; edges with `edgeLane|edgeJoin` flags; edges with a passage tag (`g.pass[e] != 0`); edges whose rhumb leg crosses the antimeridian.
   - Free: everything else.
   - Pinned segments are kept as drawn. Each maximal free run is passed to `tautRun`.
2. `tautRun`: place stations along the run every 5 NM of arc length. Ends are stations, with no offsets. At each inner station, add lattice points at lateral offsets {0, ±1, ±2, ±4, ±8, ±16} NM, perpendicular to the local run bearing in Mercator. Keep a point only when `safeLeg(p, p)` holds (a point check).
3. Run Theta* over the lattice.
   - Neighbours of station i are all kept points of station i+1.
   - On relaxing m from n: if `safeLeg(parent(n), m)`, take parent(m) = parent(n); else if `safeLeg(n, m)`, take parent(m) = n.
   - Cost is rhumb length; the heuristic is `Distance(m, end)`.
   - Ties break on lower offset |k|, then lower k, so the result is deterministic.
4. Tighten: repeat until nothing changes. For each inner vertex v (in order), drop v when `safeLeg(prev, next)`.
5. If Theta* finds no path, retry once with offsets scaled ×1.5 (max 24 NM). If that also fails, keep the run's coarse points (hazards will flag it) and return false.
6. Taut segments get code `segTaut`, which reads as provenance `"marnet"`.

- [ ] **Step 1: Write the failing tests** (full graph with land, TSS and depth):
  - `TestTautNoGrounding`: these routes at the default vessel, each with every segment that is not an approach, connector or pinned piece satisfying `safeLeg`:
    - Rotterdam (51.95 N, 4.05 E) to Singapore (1.26 N, 103.82 E)
    - Dover Strait in both directions (51.0 N, 1.2 E to 50.6 N, 0.6 E and back)
    - Gibraltar (36.0 N, −6.0 E to 36.1 N, −4.8 E)
    - Singapore Strait (1.2 N, 103.6 E to 1.3 N, 104.3 E)
  - `TestTautIsTaut`: for each free inner vertex of those routes, `safeLeg(prev, next)` is false.
  - `TestTautFewerTurnsThanMARNET`: Rotterdam–Singapore has at most half the path vertices of the coarse leg, and no two consecutive free legs turn less than 0.5° (no near-collinear zigzag).
  - `TestTautStaysNearMARNET`: every taut vertex is within 24 NM of the coarse path.
  - `TestTautAcrossAntimeridian`: San Francisco (37.81 N, 122.40 W) to Busan (35.10 N, 129.04 E) returns a route; no segment spans more than 180° of longitude unsplit; the distance is within 2% of 4,914 NM.
  - `TestTautMultiLeg`: Rotterdam → Port Said (31.26 N, 32.31 E) → Singapore through `Route` with 3 points: two legs, each with free runs replaced, and `Hazards[].From/To` within `len(Path)`.
  - `TestRouteFromInlandPort`: from Pub. 151 Rotterdam (51.90 N, 4.4667 E) to Singapore: no error, and the first hazards are approach hazards (Task 6 adds the severity; until then, assert only no error and a path that starts at the given point).
  - `BenchmarkRouteRotterdamSingapore` plus `TestRouteUnderOneSecond`: the median of 5 runs < 1 s (skip under `-short`).
- [ ] **Step 2: Run** `go test ./route -run 'Taut|InlandPort|UnderOneSecond' -v` — Expected: FAIL.
- [ ] **Step 3: Implement** `route/taut.go` per the algorithm above. Run free runs of one leg concurrently, bounded by `runtime.NumCPU()`.
- [ ] **Step 4: Run** the same command, then `go test ./...` — Expected: PASS. If `TestRouteUnderOneSecond` fails, profile with `go test -run XXX -bench RouteRotterdam -cpuprofile cpu.out ./route` and optimise `safeLeg`, for example by caching point checks or doing a cheap `Land.Within` early-out. Do not loosen the test.
- [ ] **Step 5: Commit** `feat(route): taut safe-water path over a corridor lattice (Theta*)`.

---

### Task 6: Hazards for depth and port approaches

**Files:**
- Modify: `route/hazard.go`, `route/hazard_test.go`, `route/route.go` (pass the limits to `hazards`)

**Interfaces:**
- Consumes: `pathCtx` (Task 5), `Depth.SegmentMin` (Task 3).
- Produces: `func (g *Graph) hazards(legs []Leg, path []Position, opts Options, c *pathCtx) []Hazard`. New severities:
  - `"shallow"` (red): depth below the limit, outside the approach.
  - `"approach"` (amber): any aground, close or shallow finding inside the port approach. Note: `"Port approach: check chart and pilotage."`
  - The existing `aground`, `close` and `unchecked` severities keep their meaning. Severity precedence per segment: aground > shallow > close.
- The approach runs along `path` from each requested endpoint until the first vertex that passes the full (non-relaxed) point check, capped at 15 NM of track.
- Pinned passage segments keep their passage note.

- [ ] **Step 1: Write the failing tests.**
  - `TestHazardShallow`: a synthetic graph and depth (one 8 m tile across a straight MARNET line, limit 13.6 m) reports one `shallow` run with `Kind` `marnet`.
  - `TestHazardApproach`: from Pub. 151 Rotterdam (51.90 N, 4.4667 E), the first hazard has severity `approach` and starts at index 0, and no `aground` or `shallow` hazard ends within 15 NM of track from the start.
  - `TestHazardSuezCanalNote`: Rotterdam–Singapore at 15 m has a hazard with `Passage` `suez` and the passage note, unchanged from today.
  - `TestHazardsQuietOnOpenRoute`: New York (40.70 N, 74.02 W) to Cape Town (33.91 S, 18.43 E): every hazard is `approach` or `unchecked`.
- [ ] **Step 2: Run** `go test ./route -run Hazard -v` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the same command, then `go test ./...` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(route): shallow and port-approach hazards`.

---

### Task 7: REST and MCP inputs and outputs

**Files:**
- Modify: `main.go` (`WaypointRequest`, handler), `pasgen.go` (`Output`, `generateOutput`), `main_test.go`
- Modify: `mcpserver/mcpserver.go` (`distanceIn`, `distanceOut`, `routeIn`, `routeOut`, the options helper, tool descriptions), `mcpserver/mcpserver_test.go`
- Modify: `README.md` (API section)

**Interfaces:**
- Consumes: `Vessel`, `CheckVessel`, `CheckClearance`, `Route.Depth`, `Route.ConfinedDepthM`, `Route.Notices` (Tasks 1, 2, 4).
- Request (REST and MCP): `draught_m *float64`, `speed_kn *float64`, `vessel_type string`, `clearance_nm *float64`. Absent means the default.
  - MCP jsonschema descriptions: `"vessel draught in metres, 1 to 25; default 10"`, `"speed in knots for squat, 1 to 30; default 14"`, `"tanker, bulk, general or container; default bulk"`.
- Response: `depth` (`DepthLimit`), `confined_depth_limit_m`, and `notices` (an empty array when none) on `/waypoints` properties, MCP `route` and MCP `distance_to_go`.
- Both MCP tool descriptions gain: `"The route stays in water deeper than draught + squat + margin where depth data exists; when the draught does not fit, it goes the long way and notices say why."`

- [ ] **Step 1: Write the failing tests.**
  - `TestWaypointsVesselDefaults`: a POST without vessel fields gives `depth.draught_m` 10 and `depth.depth_limit_m` 10 + 1.568 (bulk Cb 0.80 at 14 kn, open) + 2 = 13.568 ±1e-6, and `notices` is `[]`.
  - `TestWaypointsVesselValidation`: `draught_m: 30` → 400 with `"draught_m must be between 1 and 25"`.
  - `TestMCPRouteDraughtNotice`: MCP `route` Ras Tannurah → Qingdao with `draught_m: 22` returns a `draught-reroute` notice.
  - Add `"notices"` to the array-key loop in `main_test.go`.
- [ ] **Step 2: Run** `go test . ./mcpserver -v -run 'Vessel|Draught|Waypoints'` — Expected: FAIL.
- [ ] **Step 3: Implement.** Document the fields in `README.md` next to `clearance_nm`.
- [ ] **Step 4: Run** `go vet ./... && go test ./...` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(api,mcp): vessel inputs, depth limit and draught notices`.

---

### Task 8: UI — vessel inputs, notices, avoided stretch, new hazard kinds

**Files:**
- Modify: `web/templates/page/home.gohtml` (route panel), `web/static/script.js`, `web/tailwind/` sources if new classes, `web/static/tailwind.css` (rebuild: `cd web/tailwind && npm ci && npm run build`)

**Interfaces:**
- Consumes: `/waypoints` response fields from Task 7.

- [ ] **Step 1: Invoke the UI skills first** (CLAUDE.md): `impeccable`, `ui-ux-pro-max`, `redesign-existing-projects`, `high-end-visual-design`. Follow the existing "Oceanic Tactical ECDIS" panel style.
- [ ] **Step 2: Build the UI.**
  - **Vessel card** where the smoothing card was:
    - draught stepper (0.5 m steps, 1–25, default 10);
    - vessel type segmented control (Tanker / Bulk / General / Container, default Bulk);
    - the land clearance stepper (moved here);
    - a one-line readout: `Depth limit 14.6 m = 10.0 draught + 2.6 squat + 2.0 margin` (bulk at the 18 kn slider default).
  - **Speed:** the existing Transit Speed slider value is sent as `speed_kn`. Changing draught, type or speed re-requests the route (reuse the existing stale-response guard).
  - **Notices card** above the hazards list, one row per notice: an icon, the text, and `+X NM` as a chip. Clicking a row fits the map to its `avoided` path.
  - **Avoided stretches:** drawn as a faint dashed line (opacity 0.35), not clickable, under the route.
  - **Hazards:** `shallow` is red (same red as aground) and `approach` is amber (same amber as close). Legend and list labels: "Shallow", "Port approach".
- [ ] **Step 3: Static checks.** Run `node --check web/static/script.js`. Rebuild Tailwind. Run `go test ./...` (template parse tests).
- [ ] **Step 4: Browser checklist for the owner** (agents do not browser-test). Put it in the commit body:
  1. Rotterdam–Singapore with defaults: no zigzags, approach hazards only at both ends plus Suez.
  2. Ras Tannurah–Qingdao at 22 m: a notice appears, the route goes via Lombok, and the faint Malacca stretch is shown.
  3. Draught 21: the Suez notice appears and the route goes via the Cape.
  4. The phone-width layout has no horizontal scroll.
- [ ] **Step 5: Commit** `feat(ui): vessel inputs, draught notices and avoided stretches`.

---

### Task 9: Pub. 151 distance benchmark, docs, finish

**Files:**
- Create: `route/testdata/pub151.json`, `route/pub151_test.go`
- Modify: `README.md` (Limitations, data credits), `web/templates/page/about.gohtml`, `dataset/README.md`

**Interfaces:**
- Fixture row: `{"from":{"name":"","lat":0,"lon":0},"to":{...},"nm":0,"page":0,"note":""}`. Positions are each port's own Pub. 151 header position (e.g. Singapore 1°16'00"N 103°50'00"E). Source: `/Users/scorpion/Downloads/pub151-distances-btw-ports.pdf` (NGA Pub. 151, 2001, public domain). Find rows with `pdftotext` (no `-layout`) and confirm each against the page.

- [ ] **Step 1: Build the fixture.** Already read from the book:
  - Singapore–Colombo 1,581
  - Singapore–Ras Tannurah 3,701
  - Singapore–Durban 4,872
  - Shanghai (31°14'00"N 121°29'30"E)–Singapore 2,207
  - New York (40°42'00"N 74°01'00"W)–Cape Town 6,786
  - San Francisco (37°48'30"N 122°24'00"W)–Honolulu 2,091
  - San Francisco–Busan 4,914
  - San Francisco–Panama 3,245
  - New York–Colombo 8,604 (via Gibraltar and Port Said, from the preface)
  - King's Lynn (52°45'30"N 0°23'28"E)–Amsterdam 203

  Add Rotterdam–Singapore (sum the junction legs as the preface describes) and one Baltic pair through Kiel, each verified on its page. Fill in every destination position from its own header. Use the `note` field for known 2001 differences (Neopanamax, TSS changes).
- [ ] **Step 2: Write** `TestPub151Distances`: for each row, route with the default vessel and require `|got − nm| / nm ≤ 0.02`. On failure, report the pair, got, want and the percentage.
- [ ] **Step 3: Run** `go test ./route -run Pub151 -v` — Expected: PASS. If a row fails, investigate it with superpowers:systematic-debugging; do not raise the tolerance. A row may get `"skip": "<reason>"` only for a documented 2001 data difference, with the owner told.
- [ ] **Step 4: Docs.**
  - `README.md` and About page Limitations: replace the kink-removal and smoothing text with three things:
    - the disclaimer copy from Global Constraints;
    - the GEBCO credit;
    - a summary of the vessel inputs and the depth limit formula.
  - `dataset/README.md`: the depth dataset entry from Task 3.
- [ ] **Step 5: Verify.** Run `go vet ./... && go test ./...` (PASS), then `codegraph sync`.
- [ ] **Step 6: Update Basic Memory.** In `projects/seaRoute.md`, note the safe-water routing branch, the datasets and the status. Add a learning in `learnings/` on corridor-lattice Theta* for kink-free sea routes. Commit and push `~/basic-memory`.
- [ ] **Step 7: Commit** `test(route): Pub. 151 distance benchmark; docs for safe-water routing`. Then run superpowers:finishing-a-development-branch.
