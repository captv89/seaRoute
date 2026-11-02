package route

import (
	"math/rand"
	"testing"
)

func TestDeepTilesNeverMissesClippedCorner(t *testing.T) {
	// Shallow tile (cx=4, cy=0) covers lon 2..2.5, lat 0..0.5. This 6.7 NM leg
	// clips its north-west corner by about 0.3 NM; no 5 NM sampling sees it.
	d := NewDepthMask(nil, maskWith(map[[2]int]uint8{{4, 0}: 30}), "x/gebco2026")
	a, b := Position{1.98, 0.45}, Position{2.03, 0.55}
	if d.DeepTiles(a, b, 60) {
		t.Error("leg clipping the shallow tile corner reported as deep")
	}
	if !d.DeepTiles(Position{1.98, 0.55}, Position{2.03, 0.65}, 60) {
		t.Error("leg clear of the shallow tile reported as shallow")
	}
}

func TestDeepTilesGateNeverSkipsShallowCell(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	var tiles []DepthTile
	shallow := map[[2]int]uint8{}
	for cx := int32(0); cx < 4; cx++ {
		for cy := int32(0); cy < 2; cy++ {
			tl := DepthTile{CX: cx, CY: cy, M: deepCells()}
			if rng.Intn(2) == 0 {
				for range 1 + rng.Intn(3) {
					tl.M[rng.Intn(len(tl.M))] = uint8(5 + rng.Intn(40))
				}
				mn := uint8(255)
				for _, v := range tl.M {
					mn = min(mn, v)
				}
				shallow[[2]int{int(cx), int(cy)}] = mn
			}
			tiles = append(tiles, tl)
		}
	}
	d := NewDepthMask(tiles, maskWith(shallow), "x/gebco2026")
	const lim = 60.0
	deep, rejected := 0, 0
	for range 3000 {
		a := Position{rng.Float64() * 2, rng.Float64()}
		b := Position{rng.Float64() * 2, rng.Float64()}
		if d.DeepTiles(a, b, lim) {
			deep++
			if m, _ := d.SegmentMin(a, b); m < lim {
				t.Fatalf("gate skipped %v -> %v which has a %v m cell", a, b, m)
			}
		} else {
			rejected++
		}
	}
	if deep == 0 || rejected == 0 {
		t.Errorf("degenerate test: %d deep, %d rejected", deep, rejected)
	}
}

// TestRouterSafeLegTooShallowAgree: the router (edge depth), safeLeg (taut)
// and tooShallow (hazards) judge the same synthetic shoal alike.
func TestRouterSafeLegTooShallowAgree(t *testing.T) {
	a, b := rowLeg()
	for cells := 1; cells <= 5; cells++ {
		wantShallow := cells >= runMarnet
		d := NewDepth([]DepthTile{rowTile(7, 10, 9+cells)}, "t")
		g := newGraph([]Segment{{A: a, B: b}}, nil, nil, d, "t")
		c := &pathCtx{limitM: 12, limitConfinedM: 12}
		if got := !g.safeLeg(a, b, c); got != wantShallow {
			t.Errorf("%d cells: safeLeg unsafe=%v, want %v", cells, got, wantShallow)
		}
		if got := g.tooShallow(a, b, segTaut, "", c); got != wantShallow {
			t.Errorf("%d cells: tooShallow=%v, want %v", cells, got, wantShallow)
		}
		r := routeOn(t, g, Options{Vessel: Vessel{DraughtM: 5}}, a, b)
		got := false
		for _, h := range r.Hazards {
			got = got || h.Severity == "shallow"
		}
		if got != wantShallow {
			t.Errorf("%d cells: router hazards shallow=%v, want %v (%v)", cells, got, wantShallow, r.Hazards)
		}
	}
}

// TestPartlyUnstoredLegJudgedByReadCells: a leg that leaves the stored tiles is
// still shallow to safeLeg (taut) and tooShallow (hazards) when the cells it did
// read form a shallow run (the network keeps such edges open, see buildMinDepth); with no shallow reading it passes.
func TestPartlyUnstoredLegJudgedByReadCells(t *testing.T) {
	a := cellCentre(0, 0, 2, 20)
	b := Position{a[0] + 1.5, a[1]} // well past the stored tile
	for _, tc := range []struct {
		cells int
		want  bool
	}{{2, false}, {3, true}, {5, true}} {
		d := NewDepth([]DepthTile{rowTile(7, 10, 9+tc.cells)}, "t")
		if _, known := (&Graph{depth: d}).segmentDepth(a, b, runMarnet, 30); known {
			t.Fatal("leg should leave the stored tiles")
		}
		g := newGraph([]Segment{{A: a, B: b}}, nil, nil, d, "t")
		c := &pathCtx{limitM: 12, limitConfinedM: 12}
		if got := !g.safeLeg(a, b, c); got != tc.want {
			t.Errorf("%d cells: safeLeg unsafe=%v, want %v", tc.cells, got, tc.want)
		}
		if got := g.tooShallow(a, b, segTaut, "", c); got != tc.want {
			t.Errorf("%d cells: tooShallow=%v, want %v", tc.cells, got, tc.want)
		}
	}
}

// TestSegmentDepthRunAcrossAntimeridian: shallow cells on both sides of the
// antimeridian make one run.
func TestSegmentDepthRunAcrossAntimeridian(t *testing.T) {
	row := func(v uint8, cells ...int) []uint8 {
		m := deepCells()
		for _, i := range cells {
			m[20*depthCellsPerTile+i] = v
		}
		return m
	}
	lat := 20.5 * depthCellDeg
	a, b := Position{179.9, lat}, Position{-179.9, lat}
	for _, tc := range []struct {
		west, east []int
		want       float64
	}{
		{[]int{118, 119}, []int{0, 1}, 7}, // 4 cells in a row: shallow
		{[]int{119}, []int{0}, 255},       // 2 cells: below the run length
		{[]int{119}, []int{0, 1}, 7},      // 3 cells across the line
	} {
		d := NewDepth([]DepthTile{{CX: 359, CY: 0, M: row(7, tc.west...)}, {CX: -360, CY: 0, M: row(7, tc.east...)}}, "t")
		g := &Graph{depth: d}
		if m, known := g.segmentDepth(a, b, runMarnet, 30); !known || m != tc.want {
			t.Errorf("west %v east %v: got %v, %v, want %v", tc.west, tc.east, m, known, tc.want)
		}
	}
}

// TestTrackStaysNearMarnetOrDeep logs, and checks, that every vertex of the
// Rotterdam-Singapore and SF-Busan tracks lies where the land data holds the
// coast (within the corridor of the MARNET network, less the taut margin) or
// inside a fully deep tile. The leg's own coarse track is not the bound: the
// taut path may take another part of the network's corridor.
func TestTrackStaysNearMarnetOrDeep(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: full graph")
	}
	g := pub151Graph(t)
	if g.depth == nil {
		t.Skip("no depth dataset")
	}
	r0 := g.land.CorridorNM*MetresPerNM - tautCorridorM
	for _, tc := range []struct {
		name string
		a, b Position
	}{
		{"rotterdam-singapore", tautRotterdam, tautSingapore},
		{"sf-busan", Position{-122.4, 37.8}, Position{129.05, 35.1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := routeOn(t, g, Options{}, tc.a, tc.b)
			far, deep, near := 0, 0, 0
			for _, p := range r.Path {
				switch {
				case g.nearNetwork(p, r0):
					near++
				case g.depth.TileMinDepth(p) >= deepTileM:
					deep++
				default:
					far++
					t.Errorf("vertex %v is outside the data corridor and not in a deep tile", p)
				}
			}
			t.Logf("%d vertices: %d in the data corridor, %d in fully deep tiles, %d neither; %.0f NM", len(r.Path), near, deep, far, r.DistanceNM())
		})
	}
}

func TestCoveredRejectsClippedShallowTileCorner(t *testing.T) {
	// Tile (cx=4, cy=0) spans lon 2..2.5, lat 0..0.5 and is shallow; every
	// other tile is deep and there is no network, so only the depth grid covers.
	g := &Graph{
		land:  &Land{CorridorNM: 30},
		depth: NewDepthMask(nil, maskWith(map[[2]int]uint8{{4, 0}: 30}), "x/gebco2026"),
	}
	c := &pathCtx{}
	// About 10 NM long, inside the shallow tile for under 1 NM, with no 5 NM
	// sample in it.
	a, b := Position{1.99, 0.44}, Position{2.03, 0.60}
	if g.covered(a, b, c) {
		t.Error("leg clipping a shallow tile corner between samples counted as covered")
	}
	if !g.covered(Position{1.9, 0.56}, Position{2.1, 0.64}, c) {
		t.Error("leg through deep tiles only should be covered")
	}
	g.depth = NewDepthMask(nil, nil, "x/gebco2026") // no mask: nothing is vouched for
	if g.covered(Position{1.9, 0.56}, Position{2.1, 0.64}, c) {
		t.Error("without a deep-ocean mask nothing is covered")
	}
}

func TestCoveredRoundsIslandInPartlyDeepTile(t *testing.T) {
	// Tile (4, 0) spans lon 2..2.5, lat 0..0.5 and is stored: 255 m except a
	// 0.05 degree island at its centre. No network, so only the grid covers.
	m := make([]uint8, depthCellsPerTile*depthCellsPerTile)
	for k := range m {
		m[k] = 255
	}
	for j := 54; j < 66; j++ {
		for i := 54; i < 66; i++ {
			m[j*depthCellsPerTile+i] = 0
		}
	}
	g := &Graph{
		land:  &Land{CorridorNM: 30},
		depth: NewDepthMask([]DepthTile{{CX: 4, CY: 0, M: m}}, maskWith(map[[2]int]uint8{{4, 0}: 0}), "x/gebco2026"),
	}
	c := &pathCtx{clearM: MetresPerNM, band: true}
	if g.covered(Position{1.9, 0.25}, Position{2.6, 0.25}, c) {
		t.Error("leg over the island counted as covered")
	}
	// 0.6 NM north of the island's edge: inside the 1 NM band.
	if g.covered(Position{1.9, 0.285}, Position{2.6, 0.285}, c) {
		t.Error("leg within the clearance of the island counted as covered")
	}
	if !g.covered(Position{1.9, 0.33}, Position{2.6, 0.33}, c) {
		t.Error("leg 3 NM clear of the island should be covered")
	}
	c.band = false
	if g.covered(Position{1.9, 0.33}, Position{2.6, 0.33}, c) {
		t.Error("without band a tile that is not deep throughout covers nothing")
	}
}

func TestNewYorkTokyoRoundsGuadalupe(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: full graph")
	}
	g := pub151Graph(t)
	if g.depth == nil {
		t.Skip("no depth dataset")
	}
	v := Vessel{DraughtM: 9, SpeedKn: 15, Type: "bulk"}
	r := routeOn(t, g, Options{ClearanceNM: 1, Vessel: v}, Position{-74.0177, 40.7055}, Position{139.7916, 35.6492})
	for k := 0; k+1 < len(r.Path); k++ {
		a, b := r.Path[k], r.Path[k+1]
		// Isla Guadalupe and the open Pacific off Baja California.
		if min(a[0], b[0]) > -116 || max(a[0], b[0]) < -121 || min(a[1], b[1]) > 31 || max(a[1], b[1]) < 28 {
			continue
		}
		if cl := g.land.SegmentClearance(a, b, MetresPerNM); cl < MetresPerNM-g.land.Tolerance {
			t.Errorf("leg %v-%v is %.2f NM from land", a, b, cl/MetresPerNM)
		}
	}
}
