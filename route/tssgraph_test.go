package route

import (
	"math"
	"sync"
	"testing"
	"time"
)

const (
	landData = "../dataset/land_osm.bin.gz"
	tssData  = "../dataset/tss_osm.geojson.gz"
)

var (
	tssGraphOnce sync.Once
	tssGraphG    *Graph
	tssGraphErr  error
	tssGraphTook time.Duration
)

// testTSSGraph loads the full graph (network, land, TSS) once.
func testTSSGraph(tb testing.TB) *Graph {
	tb.Helper()
	tssGraphOnce.Do(func() {
		start := time.Now()
		tssGraphG, tssGraphErr = LoadAll(Datasets{Network: network, Land: landData, TSS: tssData})
		tssGraphTook = time.Since(start)
	})
	if tssGraphErr != nil {
		tb.Fatal(tssGraphErr)
	}
	return tssGraphG
}

// grid builds the segments of an n x n grid with spacing step degrees.
func grid(n int, step float64) []Segment {
	var segs []Segment
	p := func(i, j int) Position { return Position{float64(i) * step, float64(j) * step} }
	for i := range n {
		for j := range n {
			if i+1 < n {
				segs = append(segs, Segment{A: p(i, j), B: p(i+1, j)})
			}
			if j+1 < n {
				segs = append(segs, Segment{A: p(i, j), B: p(i, j+1)})
			}
		}
	}
	return segs
}

func square(cx, cy, r float64) []Position {
	return []Position{{cx - r, cy - r}, {cx + r, cy - r}, {cx + r, cy + r}, {cx - r, cy + r}, {cx - r, cy - r}}
}

func routeOn(t *testing.T, g *Graph, opts Options, pts ...Position) Route {
	t.Helper()
	r, err := g.Route(pts, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestZoneBlocksCrossing(t *testing.T) {
	centre := Position{0.1, 0.1}
	tss := NewTSS([]TSSFeature{{Type: "zone", Scheme: "tss-z", OSMID: 1, Coords: square(0.1, 0.1, 0.02)}}, "v")
	g := NewWith(grid(3, 0.1), nil, tss)

	r := routeOn(t, g, Options{}, Position{0, 0.1}, Position{0.2, 0.1})
	for _, p := range r.Path {
		if p == centre {
			t.Fatalf("route enters the zone centre: %v", r.Path)
		}
	}
	if v := tss.violations(r.Path, nil); len(v) > 0 {
		t.Errorf("violations: %v", v)
	}
	// Without TSS the straight line through the centre is shortest.
	plain := routeOn(t, g, Options{NoTSS: true}, Position{0, 0.1}, Position{0.2, 0.1})
	if plain.DistanceM >= r.DistanceM {
		t.Errorf("NoTSS %.0f m should be shorter than the detour %.0f m", plain.DistanceM, r.DistanceM)
	}
}

// channels: A along lat 0.1, B along lat 0, joined at lon 0 and lon 0.3.
func channels() []Segment {
	var segs []Segment
	xs := []float64{0, 0.1, 0.2, 0.3}
	for i := range 3 {
		x0, x1 := xs[i], xs[i+1]
		segs = append(segs, Segment{A: Position{x0, 0.1}, B: Position{x1, 0.1}})
		segs = append(segs, Segment{A: Position{x0, 0}, B: Position{x1, 0}})
	}
	segs = append(segs, Segment{A: Position{0, 0}, B: Position{0, 0.1}}, Segment{A: Position{0.3, 0}, B: Position{0.3, 0.1}})
	return segs
}

func TestLaneOneWay(t *testing.T) {
	tss := NewTSS([]TSSFeature{{Type: "lane", Scheme: "tss-l", OSMID: 1, Coords: []Position{{0.05, 0.1}, {0.25, 0.1}}}}, "v")
	g := NewWith(channels(), nil, tss)
	west, east := Position{0, 0.1}, Position{0.3, 0.1}

	r := routeOn(t, g, Options{}, west, east)
	for i := 1; i < len(r.Path); i++ {
		if r.Path[i][1] != 0.1 {
			t.Fatalf("eastbound should stay in A: %v", r.Path)
		}
	}
	if v := tss.violations(r.Path, nil); len(v) > 0 {
		t.Errorf("eastbound violations: %v", v)
	}

	r = routeOn(t, g, Options{}, east, west)
	for i := 1; i < len(r.Path); i++ {
		a, b := r.Path[i-1], r.Path[i]
		if a[1] == 0.1 && b[1] == 0.1 {
			t.Fatalf("westbound uses an A edge %v-%v: %v", a, b, r.Path)
		}
	}
	if v := tss.violations(r.Path, nil); len(v) > 0 {
		t.Errorf("westbound violations: %v", v)
	}
}

func TestTwinTotal(t *testing.T) {
	tss := NewTSS([]TSSFeature{{Type: "lane", Scheme: "tss-l", OSMID: 1, Coords: []Position{{0.05, 0.1}, {0.15, 0.1}, {0.25, 0.1}}}}, "v")
	g := NewWith(channels(), nil, tss)
	if g.marnetNodes != 8 {
		t.Fatalf("MARNET nodes %d, want 8", g.marnetNodes)
	}
	from := make([]int32, len(g.to))
	for v := range len(g.nodes) {
		for e := g.first[v]; e < g.first[v+1]; e++ {
			from[e] = int32(v)
		}
	}
	lanes, wrong := 0, 0
	for e := range g.to {
		tw := g.twin[e]
		if g.twin[tw] != int32(e) || from[tw] != g.to[e] || g.to[tw] != from[e] {
			t.Fatalf("edge %d: bad twin %d", e, tw)
		}
		if g.flags[e]&edgeLane != 0 && g.flags[e]&edgeWrongWay == 0 {
			lanes++
		}
		if g.flags[e]&edgeWrongWay != 0 {
			wrong++
			if g.flags[tw]&(edgeLane|edgeJoin) == 0 || g.flags[tw]&edgeWrongWay != 0 {
				t.Errorf("wrong-way edge %d has twin flags %b", e, g.flags[tw])
			}
		}
		if g.flags[e]&(edgeLane|edgeJoin) != 0 && g.scheme[e] != 0 {
			t.Errorf("lane/join edge %d has scheme %d, want 0", e, g.scheme[e])
		}
	}
	laneNodes := map[int32]bool{}
	for e := range g.to {
		if g.flags[e]&edgeLane != 0 {
			laneNodes[g.to[e]] = true
			if g.to[e] < int32(g.marnetNodes) {
				t.Errorf("lane edge %d ends on MARNET node %d", e, g.to[e])
			}
		}
	}
	if len(laneNodes) != 3 {
		t.Errorf("lane nodes = %d, want 3", len(laneNodes))
	}
	if lanes != 2 {
		t.Errorf("forward lane edges = %d, want 2", lanes)
	}
	if wrong == 0 {
		t.Error("no wrong-way twins")
	}
	if g.Version() == "" {
		t.Error("empty version")
	}
}

func TestJoinAvoidsLand(t *testing.T) {
	// Lane start west of an island that separates it from the only MARNET
	// node within 5 NM on that side.
	tss := NewTSS([]TSSFeature{{Type: "lane", Scheme: "tss-l", OSMID: 1, Coords: []Position{{0.05, 0.1}, {0.25, 0.1}}}}, "v")
	island := NewLand([][]Position{square(0.025, 0.1, 0.005)}, 0, "test/0m")
	g := NewWith(channels(), island, tss)
	start := int32(-1)
	for v := g.marnetNodes; v < len(g.nodes); v++ {
		if g.nodes[v] == (Position{0.05, 0.1}) {
			start = int32(v)
		}
	}
	for v := range g.marnetNodes {
		for e := g.first[v]; e < g.first[v+1]; e++ {
			if g.to[e] == start && g.flags[e]&edgeJoin != 0 && g.nodes[v] == (Position{0, 0.1}) {
				t.Error("join from (0, 0.1) crosses the island")
			}
		}
	}
}

func TestAccessJoin(t *testing.T) {
	// One long MARNET edge; the lane beside it is 24 NM from either node, so
	// only a point on the edge can join it.
	segs := []Segment{{A: Position{0, 0}, B: Position{1, 0}}}
	tss := NewTSS([]TSSFeature{{Type: "lane", Scheme: "tss-l", OSMID: 1, Coords: []Position{{0.4, 0.01}, {0.6, 0.01}}}}, "v")
	g := NewWith(segs, nil, tss)
	start, end := int32(-1), int32(-1)
	for v := g.marnetNodes; v < len(g.nodes); v++ {
		switch g.nodes[v] {
		case Position{0.4, 0.01}:
			start = int32(v)
		case Position{0.6, 0.01}:
			end = int32(v)
		}
	}
	// access reports whether node v is an access node on the MARNET edge.
	access := func(v int32) bool { return v >= int32(g.marnetNodes) && g.nodes[v][1] == 0 }
	var in, out bool
	for e := range g.to {
		if g.flags[e]&edgeJoin == 0 || g.flags[e]&edgeWrongWay != 0 {
			continue
		}
		from := g.to[g.twin[e]]
		if g.to[e] == start && access(from) {
			in = true
		}
		if from == end && access(g.to[e]) {
			out = true
		}
	}
	if !in || !out {
		t.Fatalf("lane joined from an access node %v, to one %v", in, out)
	}
	plain := New(segs)
	r := routeOn(t, g, Options{NoTSS: true}, Position{0, 0}, Position{1, 0})
	if want := routeOn(t, plain, Options{}, Position{0, 0}, Position{1, 0}); r.DistanceM != want.DistanceM {
		t.Errorf("NoTSS %.3f m, plain %.3f m", r.DistanceM, want.DistanceM)
	}
}

func TestLoadAll(t *testing.T) {
	g := testTSSGraph(t)
	s := g.stats
	t.Logf("LoadAll %v (TSS build %v, min-depth build %v); nodes %d (MARNET %d); edges: MARNET %d, lane %d, join %d (far %d, chain %d, access nodes %d); blocked: zone/line %d, against-lane %d, own lane edges %d; version %s",
		tssGraphTook, s.tssBuild, s.depthBuild, len(g.nodes), g.marnetNodes, s.marnet, s.lane, s.join, s.joinFar, s.chain, s.access, s.blockedCross, s.blockedLane, s.blockedOwn, g.Version())
	if s.tssBuild > 10*time.Second {
		t.Errorf("TSS build took %v, want < 10 s", s.tssBuild)
	}
	if s.lane == 0 || s.join == 0 || s.blockedCross == 0 || s.blockedLane == 0 {
		t.Errorf("expected lane, join and blocked edges: %+v", s)
	}
	if plain, err := Load(network); err != nil {
		t.Fatal(err)
	} else if plain.Version() == g.Version() {
		t.Error("version should include land and TSS")
	}
}

func TestNoTSSMatchesMarnet(t *testing.T) {
	g := testTSSGraph(t)
	want := mustRoute(t, Options{}, rotterdam, singapore)
	got := routeOn(t, g, Options{NoTSS: true}, rotterdam, singapore)
	if math.Abs(got.DistanceM-want.DistanceM) > 1e-6 {
		t.Errorf("NoTSS %.3f m, MARNET %.3f m", got.DistanceM, want.DistanceM)
	}
}

func checkNoViolations(t *testing.T, name string, a, b Position) {
	t.Helper()
	g := testTSSGraph(t)
	for _, dir := range [][2]Position{{a, b}, {b, a}} {
		start := time.Now()
		r := routeOn(t, g, Options{}, dir[0], dir[1])
		took := time.Since(start)
		plain := routeOn(t, g, Options{NoTSS: true}, dir[0], dir[1])
		t.Logf("%s %v->%v: %.1f nm in %v (NoTSS %.1f nm, %d violations)", name, dir[0], dir[1], r.DistanceNM(), took,
			plain.DistanceNM(), len(g.tss.violations(plain.Path, nil)))
		if v := g.tss.violations(r.Path, nil); len(v) > 0 {
			t.Errorf("%s %v->%v: %d violations: %v", name, dir[0], dir[1], len(v), v)
		}
		if r.DistanceM > 1.05*plain.DistanceM {
			t.Errorf("%s %v->%v: TSS route %.1f nm is over 5%% longer than MARNET %.1f nm", name, dir[0], dir[1], r.DistanceNM(), plain.DistanceNM())
		}
	}
}

func TestRotterdamSingaporeTSS(t *testing.T) {
	// Suez south needs a join from the middle of a MARNET edge; without it the
	// route went round the Cape.
	checkNoViolations(t, "Rotterdam-Singapore", rotterdam, singapore)
}

func TestDoverBothWaysNoViolations(t *testing.T) {
	checkNoViolations(t, "Rotterdam-Bilbao", rotterdam, bilbao)
}

func TestGibraltarSingaporeNoViolations(t *testing.T) {
	checkNoViolations(t, "Gibraltar", Position{-7.0, 35.8}, Position{-3.5, 36.0})
	checkNoViolations(t, "Singapore Strait", Position{100.0, 3.5}, Position{105.5, 1.6})
}

func TestAStarMatchesDijkstraTSS(t *testing.T) {
	g := testTSSGraph(t)
	pairs := [][2]Position{{rotterdam, gdynia}, {richardsBay, rotterdam}, {rotterdam, septIles}, {rotterdam, singapore}, {bilbao, gdynia}, {rotterdam, bilbao}}
	for _, p := range pairs {
		for _, noTSS := range []bool{false, true} {
			a := routeOn(t, g, Options{Algorithm: AStar, NoTSS: noTSS}, p[0], p[1])
			d := routeOn(t, g, Options{Algorithm: Dijkstra, NoTSS: noTSS}, p[0], p[1])
			if math.Abs(a.DistanceM-d.DistanceM) > 1e-6 {
				t.Errorf("%v to %v (NoTSS %v): A* %.3f m, Dijkstra %.3f m", p[0], p[1], noTSS, a.DistanceM, d.DistanceM)
			}
		}
	}
}

func BenchmarkAStarLongTSS(b *testing.B) {
	g := testTSSGraph(b)
	b.ResetTimer()
	for range b.N {
		if _, err := g.Route([]Position{rotterdam, singapore}, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

// chain is one channel along lat 0 with nodes every 0.1 degrees up to lon 0.5.
func chain() []Segment {
	var segs []Segment
	for i := range 5 {
		segs = append(segs, Segment{A: Position{float64(i) * 0.1, 0}, B: Position{float64(i+1) * 0.1, 0}})
	}
	return segs
}

func warnedSchemes(r Route) []string {
	var ids []string
	for _, w := range r.Warnings {
		ids = append(ids, w.Scheme)
	}
	return ids
}

func TestReversedLaneLifts(t *testing.T) {
	// The lane is drawn west, but the only channel is routed east.
	lane := TSSFeature{Type: "lane", Scheme: "tss-rev", Name: "Reversed", OSMID: 1, Coords: []Position{{0.35, 0.002}, {0.15, 0.002}}}
	tss := NewTSS([]TSSFeature{lane}, "v")
	g := NewWith(chain(), nil, tss)
	r := routeOn(t, g, Options{}, Position{0, 0}, Position{0.5, 0})
	if len(r.Warnings) != 1 {
		t.Fatalf("warnings = %+v, want 1", r.Warnings)
	}
	w := r.Warnings[0]
	want := "OSM scheme data inconsistent near Reversed, TSS rules lifted here; check the chart"
	if w.Flag != "tss-unresolved" || w.Scheme != "tss-rev" || w.Name != "Reversed" || w.Message != want {
		t.Errorf("warning = %+v", w)
	}
	// The opposite direction follows the lane and needs no lift.
	back := routeOn(t, g, Options{}, Position{0.5, 0}, Position{0, 0})
	if len(back.Warnings) != 0 || back.Warnings == nil {
		t.Errorf("westbound warnings = %#v, want empty non-nil", back.Warnings)
	}
}

func TestZoneOnlyPathLifts(t *testing.T) {
	zone := TSSFeature{Type: "zone", Scheme: "tss-zone", Name: "Zone", OSMID: 1, Coords: square(0.3, 0, 0.02)}
	tss := NewTSS([]TSSFeature{zone}, "v")
	g := NewWith(chain(), nil, tss)
	r := routeOn(t, g, Options{}, Position{0, 0}, Position{0.5, 0})
	if got := warnedSchemes(r); len(got) != 1 || got[0] != "tss-zone" {
		t.Fatalf("warned schemes = %v, want [tss-zone]", got)
	}
	if v := tss.violations(r.Path, map[string]bool{"tss-zone": true}); len(v) > 0 {
		t.Errorf("violations with the zone lifted: %v", v)
	}
	if v := tss.violations(r.Path, nil); len(v) == 0 {
		t.Error("expected the route to cross the zone")
	}
}

func TestLiftOnlyResponsibleScheme(t *testing.T) {
	// An eastbound lane the route follows, then a zone that blocks the channel.
	lane := TSSFeature{Type: "lane", Scheme: "tss-ok", OSMID: 1, Coords: []Position{{0.05, 0.002}, {0.15, 0.002}}}
	zone := TSSFeature{Type: "zone", Scheme: "tss-bad", OSMID: 2, Coords: square(0.3, 0, 0.02)}
	tss := NewTSS([]TSSFeature{lane, zone}, "v")
	g := NewWith(chain(), nil, tss)
	r := routeOn(t, g, Options{}, Position{0, 0}, Position{0.5, 0})
	if got := warnedSchemes(r); len(got) != 1 || got[0] != "tss-bad" {
		t.Fatalf("warned schemes = %v, want [tss-bad]", got)
	}
	if v := tss.violations(r.Path, map[string]bool{"tss-bad": true}); len(v) > 0 {
		t.Errorf("violations outside the lifted scheme: %v", v)
	}
	for k, sc := range tss.Schemes {
		if l := r.Legs[0].lifted; len(l) != 2 || l[k] != (sc.ID == "tss-bad") {
			t.Errorf("leg lifted = %v, want only tss-bad", l)
		}
	}
}

func TestPortInsideScheme(t *testing.T) {
	g := testTSSGraph(t)
	r := routeOn(t, g, Options{}, Position{1.45, 51.0}, rotterdam)
	if len(r.Warnings) != 0 {
		t.Errorf("warnings = %+v, want none", r.Warnings)
	}
}

func TestJoinKeepsTolerance(t *testing.T) {
	// As TestJoinAvoidsLand, but the island's south edge sits about 110 m
	// north of the join leg (0, 0.1)-(0.05, 0.1): clear of the land line,
	// inside the 200 m data tolerance.
	tss := NewTSS([]TSSFeature{{Type: "lane", Scheme: "tss-l", OSMID: 1, Coords: []Position{{0.05, 0.1}, {0.25, 0.1}}}}, "v")
	island := NewLand([][]Position{square(0.025, 0.1+0.0045, 0.0035)}, 200, "test/200m")
	g := NewWith(channels(), island, tss)
	start := int32(-1)
	for v := g.marnetNodes; v < len(g.nodes); v++ {
		if g.nodes[v] == (Position{0.05, 0.1}) {
			start = int32(v)
		}
	}
	for v := range g.marnetNodes {
		for e := g.first[v]; e < g.first[v+1]; e++ {
			if g.to[e] == start && g.flags[e]&edgeJoin != 0 && g.nodes[v] == (Position{0, 0.1}) {
				t.Error("join from (0, 0.1) passes inside the land tolerance")
			}
		}
	}
}
