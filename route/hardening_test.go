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
// Rotterdam-Singapore and SF-Busan tracks is within 24 NM of the MARNET track
// or inside a fully deep tile.
func TestTrackStaysNearMarnetOrDeep(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: full graph")
	}
	g := pub151Graph(t)
	if g.depth == nil {
		t.Skip("no depth dataset")
	}
	for _, tc := range []struct {
		name string
		a, b Position
	}{
		{"rotterdam-singapore", tautRotterdam, tautSingapore},
		{"sf-busan", Position{-122.4, 37.8}, Position{129.05, 35.1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := routeOn(t, g, Options{}, tc.a, tc.b)
			var marnet []Position
			l := coarseLeg(t, g, tc.a, tc.b)
			for k := 0; k+1 < len(l.Path); k++ {
				for _, pc := range splitAntimeridian(l.Path[k], l.Path[k+1]) {
					marnet = append(marnet, rhumbSamples(pc[0], pc[1], 4)...)
				}
			}
			far, deep, near := 0, 0, 0
			for _, p := range r.Path {
				best := 1e18
				for _, q := range marnet {
					best = min(best, Distance(p, q))
				}
				switch {
				case best <= 24*MetresPerNM:
					near++
				case g.depth.TileMinDepth(p) >= deepTileM:
					deep++
				default:
					far++
					t.Errorf("vertex %v is %.1f NM from MARNET and not in a deep tile", p, best/MetresPerNM)
				}
			}
			t.Logf("%d vertices: %d within 24 NM of MARNET, %d in fully deep tiles, %d neither; %.0f NM", len(r.Path), near, deep, far, r.DistanceNM())
		})
	}
}
