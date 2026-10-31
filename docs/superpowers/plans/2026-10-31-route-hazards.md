# Route Hazards and Reviewable Proposals Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Flag every route segment that touches or nears land, stop shortcuts where the coastline data is missing, allow TSS-legal shortcuts in scheme areas, keep proposals local, and make proposals and hazards reviewable on the map.

**Architecture:** All checks live in package `route`. `classify` (route/smooth.go) gains a corridor test, a TSS-rule test in place of the blanket scheme-area ban, and a 20 NM proposal cap. A new `route/hazard.go` checks each segment of the final path after the legs are built and links pending proposals that cover it. REST (`pasgen.go`) and MCP (`mcpserver/`) pass `hazards` through. The web UI draws hazards, makes hazard and proposal lines clickable, and keeps map and side pane selection in sync.

**Tech Stack:** Go 1.26 (gin, MCP go-sdk), Leaflet 1.9.4, Tailwind v3 prebuilt (`web/static/tailwind.css`, rebuild with `cd web/tailwind && npm ci && npm run build`).

**Spec:** `docs/superpowers/specs/2026-10-25-route-hazards-design.md` (builds on `2026-10-23-route-kink-removal-design.md`).

## Global Constraints

- Hazard `aground`: segment clearance minus `Land.Tolerance` is 0 or less. `close`: clear of land but closer than the requested clearance (`clearance_nm`, default 2).
- Adjacent hazard segments with the same severity, kind and passage merge into one entry.
- Hazards are computed whenever land data is loaded, with or without `kinks`. Without land data `hazards` is `[]`, never omitted.
- A shortcut partly outside the land-import corridor is rejected whole.
- Proposal length cap: 20 NM (rhumb distance of the replacement leg). Automatic shortcuts are not capped. Proposals saving under 0.1 NM stay dropped.
- Shortcuts never replace lane or join edges (`skipSeg` unchanged).
- `joinOK` must add `Land.Tolerance` to its land test.
- Route calculation stays under 1 s for any route (`TestRoutePerformance`).
- Map colours: one colour per severity everywhere (red aground, amber close), canals and connectors included. Use existing theme tokens (`text-error`, `text-warning`, `#dc2626`/`#ffb4ab` for error, `#f59e0b` for warning).
- No inline event handlers (nonce CSP): bind all clicks with `addEventListener` / delegation.
- UI is tested by the owner in the browser. Agents run static checks and give a checklist.
- Before any UI code: invoke `impeccable`, `ui-ux-pro-max`, `redesign-existing-projects`, `high-end-visual-design`.

## Review Focus

1. **Accepting a below-clearance fix.** An aground hazard fixed by a below-clearance proposal becomes a `close` hazard of kind `user-accepted` after accept, not "no hazard". Pinned in Task 5 `TestHazardFixLinksProposal`.
2. **Via-points and multi-leg routes.** Hazard and proposal indices must be offsets into `Route.Path`, not into the leg. Pinned in Task 5 `TestHazardIndicesAcrossLegs`.
3. **Rejected proposals in `fixes`.** The server does not know UI rejects; the UI must not show "Fix available" for a proposal the user rejected. Pinned in Task 7 checklist item.
4. **Open-ocean shortcuts lost to the corridor rule.** If MARNET open-ocean vertices are sparse, the corridor test may strip most auto shortcuts. `TestRealKinksReduced` (existing, must still pass) catches it; log the auto shortcut count before and after in Task 1.
5. **Stale selection after recompute.** Hazards are selected by index; a recompute must clear the selection so index 3 of the old list never highlights index 3 of the new one. Pinned in Task 7 checklist item.

---

### Task 1: Reject shortcuts outside the land corridor

**Files:**
- Modify: `route/land.go` (`Land` struct, `readLand` header)
- Create: `route/corridor.go`
- Modify: `route/route.go` (`Graph` field, build the index in `newGraph`)
- Modify: `route/smooth.go:153-179` (`classify`)
- Test: `route/smooth_test.go`, `route/land_test.go`

**Interfaces:**
- Produces: `Land.CorridorNM float64` (0 means full coverage, no corridor test; read from header `corridor_nm`).
- Produces: `func (g *Graph) inCorridor(a, b Position, marginM float64) bool` — true when `g.land == nil`, `g.land.CorridorNM == 0`, or every sample of the rhumb leg a-b (spacing `smoothSampleNM`, ends included) lies within `CorridorNM*MetresPerNM - marginM` of a MARNET vertex (`g.nodes[:g.marnetNodes]`). False when that radius is ≤ 0.

- [ ] **Step 1: Write the failing tests**

```go
// route/land_test.go, inside TestLoadRealLand after loading:
if l.CorridorNM != 30 {
	t.Errorf("CorridorNM = %v, want 30", l.CorridorNM)
}

// route/smooth_test.go
func zigzag() []Segment {
	return chainSegs(Position{0, 0}, Position{0.5, 0.1}, Position{1, 0}, Position{1.5, 0.1}, Position{2, 0})
}

func TestShortcutOutsideCorridorNeverOffered(t *testing.T) {
	land := landOf(rect(0, 10, 1, 11))
	land.CorridorNM = 10 // inner radius 10 - 2 - 2.5 = 5.5 NM; the shortcut runs 15 NM from vertices
	g := NewWith(zigzag(), land, nil)
	r := routeOn(t, g, Options{Kinks: true}, Position{0, 0}, Position{2, 0})
	if len(r.Path) != 5 || len(r.Proposals) != 0 {
		t.Fatalf("path %v proposals %v", r.Path, r.Proposals)
	}
}

func TestShortcutInsideCorridorSmoothed(t *testing.T) {
	land := landOf(rect(0, 10, 1, 11))
	land.CorridorNM = 30
	g := NewWith(zigzag(), land, nil)
	r := routeOn(t, g, Options{Kinks: true}, Position{0, 0}, Position{2, 0})
	if len(r.Path) != 2 {
		t.Fatalf("path %v", r.Path)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./route -run 'Corridor|TestLoadRealLand' -v`
Expected: FAIL: `land.CorridorNM undefined`.

- [ ] **Step 3: Implement**

- `Land.CorridorNM`, parsed in `readLand` (`corridor_nm`, reject negative, NaN or Inf like `tolerance_m`).
- `route/corridor.go`: a grid of MARNET vertex indices in 1° cells (`map[int64][]int32`, key `cellKey`), built once in `newGraph` when `land != nil && land.CorridorNM > 0`. `inCorridor` samples the leg the way `coveredOrOpen` does (same Mercator interpolation; factor the sampler into `rhumbSamples(a, b Position, stepNM float64) []Position` and use it in both) and for each sample scans cells within the radius (longitude span scaled by `1/cos(lat)`, clamp lat at 89°, wrap longitude at ±180) with `Distance(p, node) <= radius`.
- `classify`: after the `saved <= smoothMinSavedM` check and before the TSS check, `if !g.inCorridor(a, b, s.limit+smoothSampleNM/2*MetresPerNM) { return classNever, 0 }`.

- [ ] **Step 4: Run tests**

Run: `go test ./route -run 'Corridor|TestLoadRealLand' -v && go test ./route`
Expected: PASS, including `TestRealKinksReduced` and `TestRoutePerformance`. Log the Rotterdam–Singapore smoothed stretch count with `-v` before and after; note both in the commit body.

- [ ] **Step 5: Commit**

```bash
git add route/land.go route/corridor.go route/route.go route/smooth.go route/smooth_test.go route/land_test.go
git commit -m "feat(route): reject shortcuts outside the land data corridor"
```

---

### Task 2: Cap proposals at 20 NM

**Files:**
- Modify: `route/smooth.go` (constant, `classify`)
- Test: `route/smooth_test.go` (fixtures and new test)

**Interfaces:**
- Produces: `const proposalMaxNM = 20.0`.

- [ ] **Step 1: Rescale fixtures and write the failing test**

Scale the existing kink fixtures to 0.2° so their shortcuts are 12 NM: `kA = {0, 0}`, `kB = {0.1, -0.06}`, `kC = {0.2, 0}`, island `rect(0.06, passLat, 0.14, 0.1)`. In `TestProposalsDoNotOverlap` divide every longitude by 5 (`a..f` and the three land rects); latitudes unchanged. All existing assertions stay as they are.

```go
func TestLongProposalNotOffered(t *testing.T) {
	// 60 NM shortcut, 1.5 NM from land: would be a proposal without the cap.
	a, b, c := Position{0, 0}, Position{0.5, -0.3}, Position{1, 0}
	g := NewWith(chainSegs(a, b, c), landOf(rect(0.3, passLat, 0.7, 0.1)), coverTSS())
	r := routeOn(t, g, Options{Kinks: true, ClearanceNM: 2}, a, c)
	if len(r.Proposals) != 0 || len(r.Path) != 3 {
		t.Fatalf("proposals %v path %v", r.Proposals, r.Path)
	}
}
```

- [ ] **Step 2: Run to verify the new test fails and the rescaled ones pass**

Run: `go test ./route -run 'Proposal|Accept|Clearance|Touching|NoTSSData' -v`
Expected: only `TestLongProposalNotOffered` FAILS (1 proposal).

- [ ] **Step 3: Implement** — in `classify`, just before `return classProposal, ...`: `if Distance(a, b) > proposalMaxNM*MetresPerNM { return classNever, 0 }`. Update the package comment at the top of smooth.go to state the cap.

- [ ] **Step 4: Run** `go test ./route` — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add route/smooth.go route/smooth_test.go
git commit -m "feat(route): cap kink proposals at 20 NM"
```

---

### Task 3: TSS-aware shortcuts

**Files:**
- Modify: `route/smooth.go` (`classify`)
- Modify: `route/tssgraph.go` (helper next to `schemeReasons`)
- Test: `route/tssgraph_test.go`, `route/smooth_test.go` (`TestNarrowPassagesSafe`)

**Interfaces:**
- Produces: `func (t *TSS) breaksRules(a, b Position) bool` — true when `schemeReasons` reports `cross` or `againstLane` for any scheme.

- [ ] **Step 1: Write the failing tests**

```go
// route/tssgraph_test.go
func TestShortcutInsideSchemeObeysRules(t *testing.T) {
	// Zigzag inside a scheme area (the hull of its features; a boundary
	// sets the extent) with no zone or line in the way: smoothed.
	bound := TSSFeature{Type: "boundary", Scheme: "tss-a", OSMID: 1, Coords: rect(-0.1, -0.2, 2.1, 0.3)}
	g := NewWith(zigzag(), landOf(rect(0, 10, 1, 11)), NewTSS([]TSSFeature{bound}, "v"))
	if r := routeOn(t, g, Options{Kinks: true}, Position{0, 0}, Position{2, 0}); len(r.Path) != 2 {
		t.Errorf("free shortcut in scheme area not taken: %v", r.Path)
	}
	// A small zone on the chord at lon 0.5, clear of the zigzag (lat 0.08+
	// there): no shortcut may pass it.
	zone := TSSFeature{Type: "zone", Scheme: "tss-a", OSMID: 2, Coords: rect(0.4, -0.01, 0.6, 0.01)}
	g = NewWith(zigzag(), landOf(rect(0, 10, 1, 11)), NewTSS([]TSSFeature{bound, zone}, "v"))
	r := routeOn(t, g, Options{Kinks: true}, Position{0, 0}, Position{2, 0})
	if len(r.Path) == 2 {
		t.Errorf("shortcut through the zone taken: %v", r.Path)
	}
	if v := g.tss.violations(r.Path, nil); len(v) > 0 {
		t.Errorf("shortcut breaks TSS: %v", v)
	}
}

func TestDoverStraightenedBothWays(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	g := testTSSGraph(t)
	box := [4]float64{0.9, 50.6, 2.3, 51.4}
	for _, dir := range [][2]Position{{rotterdam, bilbao}, {bilbao, rotterdam}} {
		plain := routeOn(t, g, Options{}, dir[0], dir[1])
		r := routeOn(t, g, Options{Kinks: true}, dir[0], dir[1])
		before, after := kinksIn(g, plain, box), kinksIn(g, r, box)
		t.Logf("%v->%v Dover free kinks %d -> %d", dir[0], dir[1], before, after)
		if after*2 > before {
			t.Errorf("%v->%v: Dover free kinks %d, TSS-only %d", dir[0], dir[1], after, before)
		}
		if v := g.tss.violations(r.Path, nil); len(v) > 0 {
			t.Errorf("%v->%v: violations %v", dir[0], dir[1], v)
		}
	}
}
```

`kinksIn(g, r, box)` is `freeKinks` restricted to vertices `r.Path[i]` inside box (lon0, lat0, lon1, lat1); refactor `freeKinks` to take an optional filter rather than copying it. Note: `freeKinks` counts only `marnet`/`smoothed`/`user-accepted`/`tss-unknown` segments; Dover's MARNET edges inside the area are flagged `osm-tss` by `segFlag`, so `kinksIn` must also count `osm-tss` segments whose code is a MARNET edge (`segs[k] >= 0 && g.flags[segs[k]]&(edgeLane|edgeJoin) == 0`).

In `TestNarrowPassagesSafe` delete the loop that errors when a smoothed or user-accepted stretch touches a scheme area (now allowed); the `violations` check above it stays.

- [ ] **Step 2: Run** `go test ./route -run 'SchemeObeys|Dover' -v` — Expected: FAIL (path length 5; Dover kinks unchanged).

- [ ] **Step 3: Implement** — `breaksRules` in tssgraph.go; in `classify` replace `g.tss.SegmentTouchesArea(a, b)` with `g.tss.breaksRules(a, b)`. Auto vs proposal stays decided by `coveredOrOpen` (inside OSM coverage means auto). Update the smooth.go package comment.

- [ ] **Step 4: Run** `go test ./route` — Expected: PASS, including `TestNarrowPassagesSafe`, `TestDoverBothWaysNoViolations`, `TestRoutePerformance`.

- [ ] **Step 5: Commit**

```bash
git add route/smooth.go route/tssgraph.go route/tssgraph_test.go route/smooth_test.go
git commit -m "feat(route): allow shortcuts in TSS areas that obey the scheme rules"
```

---

### Task 4: Joins respect the land tolerance

**Files:**
- Modify: `route/tssgraph.go:429-436` (`joinOK`)
- Test: `route/tssgraph_test.go`

- [ ] **Step 1: Write the failing test** — copy `TestJoinAvoidsLand` as `TestJoinKeepsTolerance`: same lane and `channels()`, but land `NewLand([][]Position{square(0.025, 0.1+0.0045, 0.0035)}, 200, "test/200m")` (island bottom edge about 110 m north of the join leg from (0, 0.1) to (0.05, 0.1), inside the 200 m tolerance). Same assertion: no join edge from (0, 0.1) to the lane start.

- [ ] **Step 2: Run** `go test ./route -run JoinKeepsTolerance -v` — Expected: FAIL (join accepted).

- [ ] **Step 3: Implement** — `return g.land == nil || g.land.SegmentClearance(a, b, g.land.Tolerance+1) > g.land.Tolerance`.

- [ ] **Step 4: Run** `go test ./route` — Expected: PASS, including `TestRotterdamSingaporeTSS` (if a lane is disconnected the `tss-unresolved` fallback applies; the test still requires no violations, which is correct).

- [ ] **Step 5: Commit**

```bash
git add route/tssgraph.go route/tssgraph_test.go
git commit -m "fix(route): join land test adds the land tolerance"
```

---

### Task 5: Hazard check on the final route

**Files:**
- Create: `route/hazard.go`, `route/hazard_test.go`
- Modify: `route/smooth.go` (`Proposal` gets unexported path indices; `smoothLeg` sets them)
- Modify: `route/route.go` (`Route.Hazards`, call in `Route`)

**Interfaces:**
- Produces:

```go
// Hazard is a run of Route.Path segments that touch land (aground) or pass
// closer than the requested clearance (close). From and To index Route.Path.
type Hazard struct {
	From           int        `json:"from"`
	To             int        `json:"to"`
	Start          Position   `json:"start"` // Path[From]
	End            Position   `json:"end"`   // Path[To]
	Severity       string     `json:"severity"` // "aground" or "close"
	MinClearanceNM float64    `json:"min_clearance_nm"` // beyond the land tolerance, 0 when aground, rounded to 0.1
	Kind           string     `json:"kind"`    // the segment flag, as Stretch.Flag
	Passage        string     `json:"passage,omitempty"`
	Note           string     `json:"note,omitempty"`
	Fixes          []string   `json:"fixes"` // pending proposal IDs overlapping the run; never nil
}
```

- `Route.Hazards []Hazard `json:"hazards"`` (always non-nil).
- `Proposal.from, Proposal.to int` (unexported): indices into `Route.Path` of the proposal's end vertices; only meaningful for proposals not accepted.
- `func (g *Graph) hazards(legs []Leg, path []Position, props []Proposal, accepted map[string]bool, clearanceNM float64) []Hazard`.

Rules:
- Per segment: `c := g.land.SegmentClearance(a, b, C+T)` with `C = clearanceNM*MetresPerNM` (`defaultClearanceNM` when 0), `T = g.land.Tolerance`. `c <= T` → aground; `c < C+T` → close; else none.
- `Kind` from `segFlag` (iterate legs exactly as `stretches` does, with the same `unknown` cache). `Passage` is `g.passes[g.pass[code]]` for `code >= 0`, else "".
- `Note`: passage set → `"Passage narrower than the land data resolves; follow the canal or fairway and pilotage."`; kind `connector` → `"Port approach: connects the port to the network."`.
- Merge adjacent runs (`To == next From`) with equal Severity, Kind, Passage; keep the smallest clearance.
- `Fixes`: IDs of proposals not in `accepted` whose `[from, to]` overlaps `[From, To]` (`p.from < h.To && p.to > h.From`), in proposal order.
- Proposal indices: in `smoothLeg`, a non-taken proposal at span `i..j` in the post-auto path ends up at `i - shift`, `j - shift` where `shift` is the sum of `(sp.j - sp.i - 1)` over taken spans before it. `Route` adds the leg's base offset (the same `base` that `stretches` uses: sum of `len(leg.Path)-1` over earlier legs).
- `g.land == nil` → `[]Hazard{}`. Computed regardless of `opts.Kinks`.

- [ ] **Step 1: Write the failing tests** (`route/hazard_test.go`)

```go
func TestHazardAground(t *testing.T) {
	g := NewWith(chainSegs(Position{0, 0}, Position{1, 0}), landOf(rect(0.4, -0.1, 0.6, 0.1)), nil)
	r := routeOn(t, g, Options{}, Position{0, 0}, Position{1, 0})
	want := []Hazard{{From: 0, To: 1, Start: Position{0, 0}, End: Position{1, 0}, Severity: "aground", Kind: "marnet", Fixes: []string{}}}
	if !reflect.DeepEqual(r.Hazards, want) {
		t.Errorf("hazards %+v", r.Hazards)
	}
}

func TestHazardClose(t *testing.T) {
	g := NewWith(chainSegs(Position{0, 0}, Position{1, 0}), landOf(rect(0.4, passLat, 0.6, 0.1)), nil)
	r := routeOn(t, g, Options{ClearanceNM: 2}, Position{0, 0}, Position{1, 0})
	if len(r.Hazards) != 1 || r.Hazards[0].Severity != "close" || r.Hazards[0].MinClearanceNM != 1.5 {
		t.Errorf("hazards %+v", r.Hazards)
	}
	if r = routeOn(t, g, Options{ClearanceNM: 1}, Position{0, 0}, Position{1, 0}); len(r.Hazards) != 0 {
		t.Errorf("1 NM clearance: hazards %+v", r.Hazards)
	}
}

func TestHazardsMerge(t *testing.T) {
	g := NewWith(chainSegs(Position{0, 0}, Position{0.5, 0}, Position{1, 0}), landOf(rect(0.2, -0.05, 0.8, 0.05)), nil)
	r := routeOn(t, g, Options{}, Position{0, 0}, Position{1, 0})
	if len(r.Hazards) != 1 || r.Hazards[0].From != 0 || r.Hazards[0].To != 2 {
		t.Errorf("hazards %+v", r.Hazards)
	}
}

func TestHazardsEmptyWithoutLand(t *testing.T) {
	g := NewWith(chainSegs(Position{0, 0}, Position{1, 0}), nil, nil)
	if r := routeOn(t, g, Options{}, Position{0, 0}, Position{1, 0}); r.Hazards == nil || len(r.Hazards) != 0 {
		t.Errorf("hazards %#v", r.Hazards)
	}
}

func TestHazardFixLinksProposal(t *testing.T) {
	// Base path A-B-C crosses an island; the chord A-C passes it at 1.5 NM.
	a, b, c := Position{0, 0}, Position{0.1, 0.05}, Position{0.2, 0}
	g := NewWith(chainSegs(a, b, c), landOf(rect(0.05, passLat, 0.15, 0.1)), coverTSS())
	r := routeOn(t, g, Options{Kinks: true, ClearanceNM: 2}, a, c)
	if len(r.Proposals) != 1 || len(r.Hazards) != 1 {
		t.Fatalf("proposals %+v hazards %+v", r.Proposals, r.Hazards)
	}
	if h := r.Hazards[0]; h.Severity != "aground" || !slices.Equal(h.Fixes, []string{r.Proposals[0].ID}) {
		t.Errorf("hazard %+v", h)
	}
	r = routeOn(t, g, Options{Kinks: true, ClearanceNM: 2, Accept: []string{r.Proposals[0].ID}}, a, c)
	if len(r.Hazards) != 1 || r.Hazards[0].Severity != "close" || r.Hazards[0].Kind != "user-accepted" {
		t.Errorf("after accept: hazards %+v", r.Hazards)
	}
}

func TestHazardIndicesAcrossLegs(t *testing.T) {
	// Two legs via (1, 0); land only on the second leg.
	g := NewWith(chainSegs(Position{0, 0}, Position{1, 0}, Position{2, 0}), landOf(rect(1.4, -0.1, 1.6, 0.1)), nil)
	r := routeOn(t, g, Options{}, Position{0, 0}, Position{1, 0}, Position{2, 0})
	if len(r.Hazards) != 1 || r.Hazards[0].From != 1 || r.Hazards[0].To != 2 || r.Hazards[0].Start != (Position{1, 0}) {
		t.Errorf("hazards %+v path %v", r.Hazards, r.Path)
	}
}

func TestRealHazards(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	g := testTSSGraph(t)
	first := routeOn(t, g, Options{Kinks: true}, rotterdam, singapore)
	var ids []string
	for _, p := range first.Proposals {
		ids = append(ids, p.ID)
	}
	r := routeOn(t, g, Options{Kinks: true, Accept: ids}, rotterdam, singapore)
	suez := false
	for _, h := range r.Hazards {
		t.Logf("hazard %+v", h)
		if h.Passage == "suez" && h.Severity == "aground" {
			suez = true
		}
		mid := segMid(h.Start, h.End)
		if h.Kind == "osm-tss" && h.Severity == "aground" && mid[0] > 42.3 && mid[0] < 43.6 && mid[1] > 12.4 && mid[1] < 14.3 {
			t.Errorf("TSS segment aground at Bab-el-Mandeb: %+v", h)
		}
	}
	if !suez {
		t.Error("no aground hazard with passage suez")
	}
	// North Aceh interior (all land): no route sample may fall in it.
	for i := 0; i+1 < len(r.Path); i++ {
		for _, p := range rhumbSamples(r.Path[i], r.Path[i+1], 1) {
			if p[0] > 96.0 && p[0] < 97.5 && p[1] > 4.0 && p[1] < 5.0 {
				t.Fatalf("segment %d crosses north Sumatra at %v", i, p)
			}
		}
	}
}
```

Add a hazard count and timing line to `TestRoutePerformance`'s log.

- [ ] **Step 2: Run** `go test ./route -run Hazard -v` — Expected: FAIL (`r.Hazards undefined`).

- [ ] **Step 3: Implement** `route/hazard.go`, the proposal indices in `smoothLeg`, `Route.Hazards`, and the call at the end of `Graph.Route` (after `r.Stretches`). Build `accepted` from `opts.Accept`.

- [ ] **Step 4: Run** `go test ./route -v -run 'Hazard|Performance'` then `go test ./...` — Expected: PASS; Rotterdam–Singapore still under 1 s.

- [ ] **Step 5: Commit**

```bash
git add route/hazard.go route/hazard_test.go route/smooth.go route/route.go route/smooth_test.go
git commit -m "feat(route): hazard check on every segment of the final route"
```

---

### Task 6: Hazards in REST and MCP, docs

**Files:**
- Modify: `pasgen.go` (`Hazards []route.Hazard `json:"hazards"``, `nonNil(r.Hazards)`)
- Modify: `mcpserver/mcpserver.go` (`Hazards []route.Hazard `json:"hazards"`` on `distanceOut` and `routeOut`, filled with `orEmpty(r.Hazards)`; tool descriptions add one sentence: "Segments that touch land or pass closer than clearance_nm come back in hazards.")
- Modify: `README.md:173` (API properties), `README.md:177-181` and `web/templates/page/about.gohtml:32-36` (Limitations)
- Test: `main_test.go:591`, `mcpserver/mcpserver_test.go`

- [ ] **Step 1: Write the failing tests** — add `"hazards"` to the array-key loop in `main_test.go:591`. In `mcpserver_test.go` add `TestRouteReturnsHazards`: Rotterdam to Singapore on the full session (`fullSession`), assert `len(out.Hazards) > 0` and some hazard has `Passage == "suez"`; and `distance_to_go` returns a non-nil `Hazards`.

- [ ] **Step 2: Run** `go test . ./mcpserver -run 'Hazard|Waypoints' -v` — Expected: FAIL.

- [ ] **Step 3: Implement.** MCP returns all merged hazards for both tools (merging keeps them small). README property line adds `hazards[]` (`from`, `to`, `start`, `end`, `severity` aground/close, `min_clearance_nm`, `kind`, `passage`, `note`, `fixes[]`). Limitations bullet, same text in both files:
  "The land data is simplified (50 m) and covers a corridor around the MARNET network. Canals and channels narrower than that read as aground and are flagged as hazards. MARNET itself is not a surveyed track."

- [ ] **Step 4: Run** `go test ./...` and `go vet ./...` — Expected: PASS, no output from vet.

- [ ] **Step 5: Commit**

```bash
git add pasgen.go mcpserver/ main_test.go README.md web/templates/page/about.gohtml
git commit -m "feat(api): return route hazards on /waypoints and MCP"
```

---

### Task 7: Hazards and proposals reviewable on the map

**Files:**
- Modify: `web/static/script.js` (`renderRoute` ~2255-2300, section 9.1 ~2414-2540)
- Modify: `web/templates/page/home.gohtml:278-290` (hazards card next to proposals card)
- Modify: `web/static/style.css` (selected-entry and popup styles, if Tailwind classes do not cover them)
- Rebuild: `web/static/tailwind.css` if new Tailwind classes are used

**Interfaces:**
- Consumes: `props.hazards` (Task 6 shape), `props.proposals[].id/path`.
- Produces (module-local): `selection = null | {type: 'proposal', id} | {type: 'hazard', index}`; `selectItem(sel, {fit: boolean, scroll: boolean})`; `layerByKey: Map<string, L.Polyline>` keyed `proposal:<id>` / `hazard:<index>`.

- [ ] **Step 1: Invoke UI skills** — `impeccable`, `ui-ux-pro-max`, `redesign-existing-projects`, `high-end-visual-design`. Follow the existing Oceanic Tactical ECDIS panel style.

- [ ] **Step 2: Hazards card** in home.gohtml above the proposals card, `id="hazards-card"`, hidden when no hazards. Header: icon `warning`, "Route Hazards", count badge. Red entries (aground) always listed; amber entries collapsed under one summary button, `"<n> close passes (under <clearance> NM)"`, expanding on click. Entry text: passage tag present → `"<Passage> : <n> legs aground"` (n = `to - from`); else kind label + `"aground"` / `"<min> NM from land"`; the `note` on a second line; `fixes` filtered by `rejectedProposalIds` and `acceptedProposalIds` → `"Fix available: accept shortcut"` button (`data-hazard-fix="<id>"`, selects that proposal), otherwise `"Needs attention"` tag.

- [ ] **Step 3: Map layers.** In `renderRoute`, after the tss-unresolved overlay, draw each hazard's `latLngs.slice(from, to + 1)` solid, weight 5, red (`#dc2626` light / `#ffb4ab` dark) or amber `#f59e0b`, then proposals on top (existing dashed style). Every hazard and proposal line: `bindPopup(...)` with details (reason / severity, min clearance NM, saved NM, note) and for proposals Accept and Reject buttons carrying `data-proposal-accept` / `data-proposal-reject`; for hazards with a fix, a `data-hazard-fix` link. Bind popup buttons by delegation on the map container (`map.getContainer().addEventListener('click', ...)`), reusing the side pane handlers; no inline handlers. Line click → `selectItem(key, {fit: false, scroll: true})`.

- [ ] **Step 4: Selection sync.** Side pane entry click → `selectItem(key, {fit: true, scroll: false})`: `map.fitBounds(layer.getBounds(), {padding: [40, 40], maxZoom: 11})`, raise and thicken the layer (`setStyle({weight: 7})`, `bringToFront()`), open its popup, add a selected ring class to the pane entry. Map click → same highlight plus `entry.scrollIntoView({block: 'nearest', behavior: 'smooth'})`. Only one selection at a time; selecting clears the previous highlight. `renderRoute` resets `selection = null` on every new response (hazard indices are per response). Reject still re-renders from `currentRouteData` with `keepView`.

- [ ] **Step 5: Static checks**

Run: `node --check web/static/script.js && go test ./... && (cd web/tailwind && npm run build) && git diff --stat web/static/tailwind.css`
Expected: no syntax error, tests PASS, tailwind.css rebuilt (non-empty diff only if new classes were added).

- [ ] **Step 6: Commit**

```bash
git add web/
git commit -m "feat(ui): hazards on the map and clickable, synced proposals"
```

- [ ] **Step 7: Owner browser checklist** (give it to the owner; do not claim it passed):
  1. Rotterdam (51.95 N, 4.05 E) to Singapore (1.26 N, 103.82 E): Suez is one red entry, drawn red on the map, with the passage note.
  2. Amber entries collapsed under "n close passes (under 2 NM)"; click expands.
  3. Click a hazard or proposal entry: map fits to it, line highlights, popup opens.
  4. Click a proposal line on the map: popup Accept and Reject work; the pane entry highlights and scrolls into view.
  5. Reject a proposal that a hazard lists as a fix: the hazard shows "Needs attention", not "Fix available".
  6. Accept a fix: after the recompute the aground hazard is gone (a close one may remain); no entry stays highlighted from the old list.
  7. No route line crosses north Sumatra; Dover SW-bound stretch looks straight.
  8. Dark and light theme, and phone width: colours and the cards read well.
