package route

import (
	"math"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
)

const depthData = "../dataset/depth_gebco.bin.gz"

var (
	depthGraphOnce sync.Once
	depthGraphG    *Graph
	depthGraphErr  error
)

// testDepthGraph loads the full graph with the GEBCO depth grid once; it skips
// the test while the dataset is not built.
func testDepthGraph(t testing.TB) *Graph {
	t.Helper()
	if _, err := os.Stat(depthData); err != nil {
		t.Skip("depth dataset not built yet")
	}
	depthGraphOnce.Do(func() {
		depthGraphG, depthGraphErr = LoadAll(Datasets{Network: network, Land: landData, TSS: tssData, Depth: depthData})
	})
	if depthGraphErr != nil {
		t.Fatal(depthGraphErr)
	}
	return depthGraphG
}

func inBox(path []Position, minLon, maxLon, minLat, maxLat float64) bool {
	for _, p := range path {
		if p[0] >= minLon && p[0] <= maxLon && p[1] >= minLat && p[1] <= maxLat {
			return true
		}
	}
	return false
}

func reroutes(r Route) []Notice {
	var out []Notice
	for _, n := range r.Notices {
		if n.Kind == "draught-reroute" {
			out = append(out, n)
		}
	}
	return out
}

var (
	rasTannurah = Position{50.16, 26.64}
	qingdao     = Position{120.32, 36.07}
)

func TestDraughtReroutesAroundSingapore(t *testing.T) {
	g := testDepthGraph(t)
	// To the open Yellow Sea, not Qingdao: the grid reads 18-24 m on every
	// network edge into Qingdao, under the 25.8 m this tanker needs.
	r := routeOn(t, g, Options{Vessel: Vessel{DraughtM: 22, SpeedKn: 14, Type: Tanker}}, rasTannurah, Position{123.0, 34.5})
	if inBox(r.Path, 103.4, 104.6, 1.0, 1.5) {
		t.Error("22 m tanker passes Singapore")
	}
	if !inBox(r.Path, 115.5, 116.0, -9.0, -8.2) {
		t.Error("22 m tanker does not pass Lombok")
	}
	// The Malacca table sends the 22 m tanker round; the Gulf and the Yellow
	// Sea are shallow for it too and may add their own reroute notices.
	var named []Notice
	for _, n := range reroutes(r) {
		if slices.Contains(n.Passages, "malacca") || strings.Contains(n.Text, "malacca") || strings.Contains(n.Text, "Singapore") {
			named = append(named, n)
		}
	}
	if len(named) != 1 {
		t.Fatalf("got %d notices naming malacca or Singapore, want 1: %+v", len(named), r.Notices)
	}
	n := named[0]
	if n.ExtraNM <= 100 {
		t.Errorf("extra %.0f NM, want > 100", n.ExtraNM)
	}
	if !slices.Contains(n.Passages, "malacca") && !strings.Contains(n.Text, "Singapore") {
		t.Errorf("notice names neither malacca nor Singapore: %+v", n)
	}
	r = routeOn(t, g, Options{Vessel: Vessel{DraughtM: 12, SpeedKn: 14, Type: Tanker}}, rasTannurah, qingdao)
	if !inBox(r.Path, 103.4, 104.6, 1.0, 1.5) {
		t.Error("12 m tanker avoids Singapore")
	}
	for _, n := range r.Notices { // the Gulf and the Yellow Sea may still add their own
		if slices.Contains(n.Passages, "malacca") || strings.Contains(n.Text, "malacca") || strings.Contains(n.Text, "Singapore") {
			t.Errorf("12 m: unexpected notice %+v", n)
		}
	}
}

func TestDraughtClosesSuez(t *testing.T) {
	g := testDepthGraph(t)
	jeddah, piraeus := Position{39.17, 21.48}, Position{23.64, 37.94}
	r := routeOn(t, g, Options{Vessel: Vessel{DraughtM: 21, SpeedKn: 14, Type: Tanker}}, jeddah, piraeus)
	if slices.Contains(r.Passages, "suez") {
		t.Error("21 m uses suez")
	}
	found := false
	for _, n := range r.Notices {
		found = found || strings.Contains(n.Text, "suez limit of 20.1 m")
	}
	if !found {
		t.Errorf("no suez limit notice: %+v", r.Notices)
	}
	r = routeOn(t, g, Options{Vessel: Vessel{DraughtM: 15, SpeedKn: 14, Type: Tanker}}, jeddah, piraeus)
	if !slices.Contains(r.Passages, "suez") {
		t.Errorf("15 m avoids suez: %v", r.Passages)
	}
}

func TestDraughtClosesPanama(t *testing.T) {
	g := testDepthGraph(t)
	ny, la := Position{-74.02, 40.70}, Position{-118.27, 33.72}
	r := routeOn(t, g, Options{Vessel: Vessel{DraughtM: 16, SpeedKn: 14, Type: Bulk}}, ny, la)
	if slices.Contains(r.Passages, "panama") {
		t.Error("16 m uses panama")
	}
	r = routeOn(t, g, Options{Vessel: Vessel{DraughtM: 12, SpeedKn: 14, Type: Bulk}}, ny, la)
	if !slices.Contains(r.Passages, "panama") {
		t.Errorf("12 m avoids panama: %v", r.Passages)
	}
}

func TestDraughtNoRoute(t *testing.T) {
	g := testDepthGraph(t)
	r := routeOn(t, g, Options{Vessel: Vessel{DraughtM: 25, SpeedKn: 14, Type: Tanker}}, Position{4.05, 51.95}, Position{18.67, 54.40})
	if len(r.Path) < 2 {
		t.Fatal("no route returned")
	}
	n := 0
	for _, x := range r.Notices {
		if x.Kind == "draught-no-route" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("got %d no-route notices, want 1: %+v", n, r.Notices)
	}
}

func TestNoDepthDataNoClosure(t *testing.T) {
	g := testTSSGraph(t)
	v := Vessel{DraughtM: 20, SpeedKn: 14, Type: Tanker} // under the malacca table limit, which closes without depth data too
	r := routeOn(t, g, Options{Vessel: v}, rasTannurah, qingdao)
	if !inBox(r.Path, 103.4, 104.6, 1.0, 1.5) {
		t.Error("without depth data the route should pass Singapore")
	}
	if len(r.Notices) != 0 {
		t.Errorf("unexpected notices %+v", r.Notices)
	}
	if want := 20 + 0.8*14*14/100 + 2.0; math.Abs(r.Depth.LimitM-want) > 1e-9 {
		t.Errorf("depth limit %.3f, want %.3f", r.Depth.LimitM, want)
	}
}

// --- synthetic depth ---

// twoWays is a network with two ways from s to t: a short one along lat 0.25
// (a1 -> a2 spans lon 0.5-1.0) and a longer one over lat 0.75.
//
//	b1 -- b2 -- b3
//	/              \
//	s -- a1 -- a2 -- a3 -- t
func twoWays() []Segment {
	chain := func(ps ...Position) []Segment {
		var out []Segment
		for i := 0; i+1 < len(ps); i++ {
			out = append(out, Segment{A: ps[i], B: ps[i+1]})
		}
		return out
	}
	s, tt := Position{0, 0.25}, Position{2, 0.25}
	return append(chain(s, Position{0.5, 0.25}, Position{1, 0.25}, Position{1.5, 0.25}, tt),
		chain(s, Position{0.5, 0.75}, Position{1, 0.75}, Position{1.5, 0.75}, tt)...)
}

// depthTiles covers lon 0-2.5, lat -0.5-1.5 with deep water except the tiles in
// shoal, which are 10 m everywhere.
func depthTiles(shoal ...[2]int32) []DepthTile {
	var tiles []DepthTile
	for cx := int32(0); cx < 5; cx++ {
		for cy := int32(-1); cy < 3; cy++ {
			tile := DepthTile{CX: cx, CY: cy, Deep: true}
			if slices.Contains(shoal, [2]int32{cx, cy}) {
				m := make([]uint8, depthCellsPerTile*depthCellsPerTile)
				for i := range m {
					m[i] = 10
				}
				tile = DepthTile{CX: cx, CY: cy, M: m}
			}
			tiles = append(tiles, tile)
		}
	}
	return tiles
}

func twoWaysGraph(shoal ...[2]int32) *Graph {
	return newGraph(twoWays(), nil, nil, NewDepth(depthTiles(shoal...), "test/gebco2026"), "test")
}

var (
	wayS = Position{0, 0.25}
	wayT = Position{2, 0.25}
)

func TestSyntheticDeepVesselReroutes(t *testing.T) {
	g := twoWaysGraph([2]int32{1, 0}) // lon 0.5-1.0, lat 0-0.5: the short way
	r := routeOn(t, g, Options{Vessel: Vessel{DraughtM: 12, SpeedKn: 14, Type: Tanker}}, wayS, wayT)
	if !inBox(r.Path, 0.4, 1.6, 0.7, 0.8) || inBox(r.Path, 0.9, 1.1, 0.2, 0.3) {
		t.Errorf("deep vessel did not take the long way: %v", r.Path)
	}
	if len(r.Notices) != 1 || r.Notices[0].Kind != "draught-reroute" {
		t.Fatalf("notices: %+v", r.Notices)
	}
	n := r.Notices[0]
	if n.ExtraNM <= 0 || len(n.Avoided) == 0 {
		t.Errorf("notice: %+v", n)
	}
	if !strings.HasPrefix(n.Text, "Draught 12.0 m: shallow water near 0.25°, 0.75° too shallow (needs ") {
		t.Errorf("text: %q", n.Text)
	}
	if !strings.Contains(n.Text, "Routed via deeper water, +") {
		t.Errorf("text: %q", n.Text)
	}
}

func TestSyntheticShallowVesselStaysShort(t *testing.T) {
	g := twoWaysGraph([2]int32{1, 0})
	r := routeOn(t, g, Options{Vessel: Vessel{DraughtM: 4, SpeedKn: 14, Type: Tanker}}, wayS, wayT)
	if !inBox(r.Path, 0.9, 1.1, 0.2, 0.3) || inBox(r.Path, 0.4, 1.6, 0.7, 0.8) {
		t.Errorf("shallow vessel took the long way: %v", r.Path)
	}
	if len(r.Notices) != 0 {
		t.Errorf("notices: %+v", r.Notices)
	}
}

func TestSyntheticBothShallowNoRoute(t *testing.T) {
	g := twoWaysGraph([2]int32{1, 0}, [2]int32{1, 1})
	r := routeOn(t, g, Options{Vessel: Vessel{DraughtM: 12, SpeedKn: 14, Type: Tanker}}, wayS, wayT)
	if len(r.Notices) != 1 || r.Notices[0].Kind != "draught-no-route" {
		t.Fatalf("notices: %+v", r.Notices)
	}
	if !strings.HasPrefix(r.Notices[0].Text, "Draught 12.0 m: no route deep enough (needs ") {
		t.Errorf("text: %q", r.Notices[0].Text)
	}
	if !inBox(r.Path, 0.9, 1.1, 0.2, 0.3) {
		t.Errorf("want the shortest route, got %v", r.Path)
	}
}

func TestSyntheticEndpointExemption(t *testing.T) {
	g := twoWaysGraph([2]int32{0, 0}) // lon 0-0.5, lat 0-0.5: both first edges from s
	lim := Vessel{DraughtM: 12, SpeedKn: 14, Type: Tanker}.DepthLimit(false).LimitM
	if sh := g.shallowEdges(lim, lim, nil); sh == nil {
		t.Fatal("without endpoints the shoal should close edges")
	}
	r := routeOn(t, g, Options{Vessel: Vessel{DraughtM: 12, SpeedKn: 14, Type: Tanker}}, wayS, wayT)
	if !inBox(r.Path, 0.9, 1.1, 0.2, 0.3) {
		t.Errorf("exempt edge at the origin was closed: %v", r.Path)
	}
	if len(r.Notices) != 0 {
		t.Errorf("notices: %+v", r.Notices)
	}
}

// --- divergence threshold ---

func edgeBetween(t *testing.T, g *Graph, a, b Position) int32 {
	t.Helper()
	v, u := nodeAt(t, g, a), nodeAt(t, g, b)
	for e := g.first[v]; e < g.first[v+1]; e++ {
		if g.to[e] == u {
			return e
		}
	}
	t.Fatalf("no edge %v -> %v", a, b)
	return 0
}

func nodeAt(t *testing.T, g *Graph, p Position) int32 {
	t.Helper()
	for v, q := range g.nodes {
		if q == p {
			return int32(v)
		}
	}
	t.Fatalf("no node at %v", p)
	return 0
}

func seq(t *testing.T, g *Graph, ps ...Position) pathSeq {
	var s pathSeq
	for i, p := range ps {
		s.nodes = append(s.nodes, nodeAt(t, g, p))
		if i > 0 {
			s.edges = append(s.edges, edgeBetween(t, g, ps[i-1], p))
		}
	}
	return s
}

func TestNoticeThreshold(t *testing.T) {
	s, tt := Position{0, 0}, Position{1, 0}
	// A detour over a mid point h degrees north adds 2*hypot(30, 60h) - 60 NM.
	detour := func(extraNM float64, directPass, detourPass string) []divergence {
		h := math.Sqrt(math.Pow((60+extraNM)/2, 2)-900) / 60
		d := Position{0.5, h}
		g := New([]Segment{
			{A: s, B: tt, Pass: directPass},
			{A: s, B: d, Pass: detourPass},
			{A: d, B: tt, Pass: detourPass},
		})
		divs := g.divergences(seq(t, g, s, d, tt), seq(t, g, s, tt))
		if len(divs) > 0 && math.Abs(divs[0].extraM/MetresPerNM-extraNM) > 0.1 {
			t.Errorf("extra %.2f NM, want about %.1f", divs[0].extraM/MetresPerNM, extraNM)
		}
		return divs
	}
	if got := detour(0.5, "a", "a"); len(got) != 0 {
		t.Errorf("0.5 NM, same passage: got %d divergences", len(got))
	}
	if got := detour(1.05, "a", "a"); len(got) != 1 {
		t.Errorf("1 NM: got %d divergences, want 1", len(got))
	}
	if got := detour(0.2, "a", "b"); len(got) != 1 {
		t.Errorf("0.2 NM swapping a passage: got %d divergences, want 1", len(got))
	}
}

func TestSyntheticPassageLimit(t *testing.T) {
	segs := twoWays()
	for i := range segs[:4] {
		segs[i].Pass = "suez"
	}
	g := newGraph(segs, nil, nil, NewDepth(depthTiles(), "test/gebco2026"), "test")
	r := routeOn(t, g, Options{Vessel: Vessel{DraughtM: 21, SpeedKn: 14, Type: Tanker}}, wayS, wayT)
	if slices.Contains(r.Passages, "suez") || len(r.Notices) != 1 || !strings.HasPrefix(r.Notices[0].Text, "Draught 21.0 m exceeds the suez limit of 20.1 m. Routed via deeper water, +") {
		t.Errorf("21 m: passages %v, notices %+v", r.Passages, r.Notices)
	}
	r = routeOn(t, g, Options{Vessel: Vessel{DraughtM: 15, SpeedKn: 14, Type: Tanker}}, wayS, wayT)
	if !slices.Contains(r.Passages, "suez") || len(r.Notices) != 0 {
		t.Errorf("15 m: passages %v, notices %+v", r.Passages, r.Notices)
	}
}
