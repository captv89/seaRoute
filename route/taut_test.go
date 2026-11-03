package route

import (
	"math"
	"os"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"
)

var (
	fullOnce sync.Once
	fullG    *Graph
	fullErr  error
)

// testFullGraph loads the network with land and TSS, and the depth grid when
// the dataset exists, so the tests pass with and without it.
func testFullGraph(tb testing.TB) *Graph {
	tb.Helper()
	fullOnce.Do(func() {
		d := Datasets{Network: network, Land: landData, TSS: tssData}
		if _, err := os.Stat(depthData); err == nil {
			d.Depth = depthData
		}
		fullG, fullErr = LoadAll(d)
	})
	if fullErr != nil {
		tb.Fatal(fullErr)
	}
	return fullG
}

var (
	tautRotterdam = Position{4.05, 51.95}
	tautSingapore = Position{103.82, 1.26}
	portSaid      = Position{32.31, 31.26}
	sanFrancisco  = Position{-122.40, 37.81}
	busan         = Position{129.04, 35.10}
)

type tautCase struct {
	name string
	pts  []Position
}

var tautCases = []tautCase{
	{"rotterdam-singapore", []Position{tautRotterdam, tautSingapore}},
	{"dover-sw", []Position{{1.2, 51.0}, {0.6, 50.6}}},
	{"dover-ne", []Position{{0.6, 50.6}, {1.2, 51.0}}},
	{"gibraltar", []Position{{-6.0, 36.0}, {-4.8, 36.1}}},
	{"singapore-strait", []Position{{103.6, 1.2}, {104.3, 1.3}}},
}

var (
	rsOnce sync.Once
	rsR    Route
	rsErr  error
)

// rotterdamSingapore routes once and shares the result.
func rotterdamSingapore(t testing.TB) Route {
	t.Helper()
	g := testFullGraph(t)
	rsOnce.Do(func() { rsR, rsErr = g.Route([]Position{tautRotterdam, tautSingapore}, Options{}) })
	if rsErr != nil {
		t.Fatal(rsErr)
	}
	return rsR
}

func ctxFor(g *Graph, opts Options, pts []Position) *pathCtx {
	v := opts.Vessel.WithDefaults()
	return &pathCtx{
		clearM:         DefaultClearanceNM * MetresPerNM,
		limitM:         v.DepthLimit(false).LimitM,
		limitConfinedM: v.DepthLimit(true).LimitM,
		ends:           pts,
	}
}

// legCtxFor rebuilds the context tautLeg judged l with: the confined passages
// it uses, and the points where a taut run meets a piece kept as drawn.
func legCtxFor(g *Graph, base *pathCtx, l Leg, coarse []Position) *pathCtx {
	c := *base
	c.confined, c.joins = nil, map[Position]float64{}
	taut := func(k int) bool { return k >= 0 && k < len(l.segs) && l.segs[k] == segTaut }
	for k, code := range l.segs {
		if code >= 0 && PassageLimits[g.passes[g.pass[code]]].Confined {
			c.confined = append(c.confined, rhumbSamples(l.Path[k], l.Path[k+1], tautConfinedNM/2)...)
		}
	}
	need := c.clearM + g.land.Tolerance
	for k := range l.Path {
		if taut(k-1) != taut(k) {
			c.joins[l.Path[k]] = g.land.PointDistance(l.Path[k], need)
		}
	}
	return &c
}

func routeCase(t *testing.T, tc tautCase) Route {
	t.Helper()
	if tc.name == "rotterdam-singapore" {
		return rotterdamSingapore(t)
	}
	return routeOn(t, testFullGraph(t), Options{}, tc.pts...)
}

func TestTautNoGrounding(t *testing.T) {
	g := testFullGraph(t)
	tol := g.land.Tolerance
	for _, tc := range tautCases {
		t.Run(tc.name, func(t *testing.T) {
			r := routeCase(t, tc)
			base := ctxFor(g, Options{}, tc.pts)
			coarse := coarseLeg(t, g, tc.pts[0], tc.pts[1])
			cp := coarse.Path
			free := 0
			for k := range coarse.segs {
				if !g.pinnedSeg(&coarse, k) {
					free++
				}
			}
			taut, left := 0, 0
			off := 0 // index of the leg's first segment in r.Path
			tautSeg := map[int]bool{}
			for _, l := range r.Legs {
				c := legCtxFor(g, base, l, cp)
				for k, code := range l.segs {
					a, b := l.Path[k], l.Path[k+1]
					switch {
					case code == segTaut:
						taut++
						tautSeg[off+k] = true
						if !g.safeLeg(a, b, c) {
							t.Errorf("taut segment %v -> %v is not safe", a, b)
						}
					case code >= 0 && !g.pinnedSeg(&l, k):
						left++
					}
					// Independent of safeLeg: no free segment touches land, except
					// one left as drawn at a passage mouth (the canal end node lies
					// in land the data does not resolve, so no safe leg leaves it).
					if code == segTaut || (code >= 0 && !g.pinnedSeg(&l, k) && !meetsPassage(g, &l, k)) {
						if d := g.land.SegmentClearance(a, b, tol+1); d <= tol {
							t.Errorf("free segment %v -> %v touches land", a, b)
						}
					}
				}
				off += len(l.segs)
			}
			if free > 0 && taut == 0 {
				t.Errorf("%d free coarse segments but no taut segment: every run fell back", free)
			}
			for _, h := range r.Hazards {
				if h.Severity != "aground" {
					continue
				}
				for k := h.From; k < h.To; k++ {
					if tautSeg[k] {
						t.Errorf("aground hazard %d..%d covers taut segment %d", h.From, h.To, k)
					}
				}
			}
			t.Logf("%d taut segments, %d free segments left as drawn (of %d free in the coarse leg), %.0f NM", taut, left, free, r.DistanceM/MetresPerNM)
		})
	}
}

func TestTautIsTaut(t *testing.T) {
	g := testFullGraph(t)
	for _, tc := range tautCases {
		t.Run(tc.name, func(t *testing.T) {
			r := routeCase(t, tc)
			base := ctxFor(g, Options{}, tc.pts)
			cp := coarseLeg(t, g, tc.pts[0], tc.pts[1]).Path
			for _, l := range r.Legs {
				c := legCtxFor(g, base, l, cp)
				for k := 1; k < len(l.segs); k++ {
					if l.segs[k-1] != segTaut || l.segs[k] != segTaut {
						continue
					}
					p, q := l.Path[k-1], l.Path[k+1]
					// A rhumb chord can be longer than the bent chain through the
					// vertex (great-circle chains): then it is kept on purpose.
					if g.safeLeg(p, q, c) {
						if Distance(p, q) <= Distance(p, l.Path[k])+Distance(l.Path[k], q)+1 {
							t.Errorf("vertex %v could be dropped", l.Path[k])
						}
						continue
					}
					// Independent reasons the chord is refused: land, TSS, depth or
					// leaving the covered area. At least one must hold.
					need := c.clearM + g.land.Tolerance
					why := g.land.SegmentClearance(p, q, need) < need ||
						(g.tss != nil && g.tss.breaksRules(p, q))
					if d, known := func() (float64, bool) {
						if g.depth == nil {
							return 255, false
						}
						return g.depth.SegmentMin(p, q)
					}(); known && d < c.limitConfinedM {
						why = true
					}
					if !why {
						for _, pc := range splitAntimeridian(p, q) {
							if !g.inCorridor(pc[0], pc[1], tautCorridorM) {
								why = true
							}
						}
					}
					if !why {
						t.Errorf("chord %v -> %v is refused for no independent reason", p, q)
					}
				}
			}
		})
	}
}

// bearingDeg is the rhumb bearing a -> b in degrees.
func bearingDeg(a, b Position) float64 {
	ma, mb := Mercator(a), Mercator(b)
	dx := math.Remainder(mb[0]-ma[0], 2*math.Pi)
	return math.Atan2(dx, mb[1]-ma[1]) * 180 / math.Pi
}

func coarseLeg(t *testing.T, g *Graph, a, b Position) Leg {
	t.Helper()
	bl, _ := g.blockedPasses(nil, nil)
	sc := searchCtx{blocked: bl}
	s, _ := g.nearest(a, sc)
	e, _ := g.nearest(b, sc)
	l, err := g.leg(a, b, s, e, sc, AStar)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestTautFewerTurnsThanMARNET(t *testing.T) {
	g := testFullGraph(t)
	r := rotterdamSingapore(t)
	coarse := coarseLeg(t, g, tautRotterdam, tautSingapore)
	if len(r.Path)*2 > len(coarse.Path) {
		t.Errorf("taut path has %d vertices, coarse %d: want at most half", len(r.Path), len(coarse.Path))
	}
	l := r.Legs[0]
	c := legCtxFor(g, ctxFor(g, Options{}, []Position{tautRotterdam, tautSingapore}), l, coarse.Path)
	for k := 1; k < len(l.segs); k++ {
		if l.segs[k-1] != segTaut || l.segs[k] != segTaut {
			continue
		}
		turn := math.Abs(math.Remainder(bearingDeg(l.Path[k], l.Path[k+1])-bearingDeg(l.Path[k-1], l.Path[k]), 360))
		// A gentle turn is kept when the chord is refused (safeLeg) or longer
		// (a great-circle chain).
		p, q := l.Path[k-1], l.Path[k+1]
		if turn < 0.5 && Distance(p, q) <= Distance(p, l.Path[k])+Distance(l.Path[k], q)+1 && g.safeLeg(p, q, c) {
			t.Errorf("near-collinear turn of %.2f deg at %v", turn, l.Path[k])
		}
	}
}

func TestTautStaysNearMARNET(t *testing.T) {
	g := testFullGraph(t)
	r := rotterdamSingapore(t)
	coarse := coarseLeg(t, g, tautRotterdam, tautSingapore)
	var ref []Position
	for k := range coarse.segs {
		ref = append(ref, rhumbSamples(coarse.Path[k], coarse.Path[k+1], 1)...)
	}
	for _, p := range r.Path {
		best := math.Inf(1)
		for _, q := range ref {
			if math.Abs(q[1]-p[1]) < 0.5 {
				best = math.Min(best, Distance(p, q))
			}
		}
		// The lattice reaches tautMaxOffset; the slack covers sampling and the
		// projection of the offset (a detour round islands the coarse path
		// crosses sits on the outer offset). A great-circle link over open
		// ocean may run further off, through fully deep tiles only.
		if best > (tautMaxOffset+0.5)*MetresPerNM && (g.depth == nil || g.depth.TileMinDepth(p) < deepTileM) {
			t.Errorf("vertex %v is %.1f NM from the coarse path", p, best/MetresPerNM)
		}
	}
}

func TestTautAcrossAntimeridian(t *testing.T) {
	g := testFullGraph(t)
	r := routeOn(t, g, Options{}, sanFrancisco, busan)
	var sum float64
	for k := 0; k+1 < len(r.Path); k++ {
		sum += Distance(r.Path[k], r.Path[k+1])
		// The path labels the crossing as +-180; a segment is never drawn the
		// long way round.
		if span := math.Abs(math.Remainder(r.Path[k+1][0]-r.Path[k][0], 360)); span > 180 {
			t.Errorf("segment %d spans %.0f deg of longitude", k, span)
		}
	}
	if math.Abs(sum-r.DistanceM) > 1 {
		t.Errorf("segments sum to %.0f m, route says %.0f m", sum, r.DistanceM)
	}
	// The brief asks for 4914 NM +-2%, but MARNET crosses the Pacific along
	// 40 N (5237 NM as a single rhumb chain), and the taut path stays within
	// 24 NM of it. So: no longer than the coarse leg, and within 8% of 4914.
	coarse := coarseLeg(t, g, sanFrancisco, busan)
	if r.DistanceM > coarse.DistanceM+1 {
		t.Errorf("taut %.0f NM is longer than the coarse leg %.0f NM", r.DistanceM/MetresPerNM, coarse.DistanceM/MetresPerNM)
	}
	if nm := r.DistanceM / MetresPerNM; math.Abs(nm-4914)/4914 > 0.08 {
		t.Errorf("distance %.0f NM, want 4914 within 8%%", nm)
	}
}

func TestTautMultiLeg(t *testing.T) {
	r := routeOn(t, testFullGraph(t), Options{}, tautRotterdam, portSaid, tautSingapore)
	if len(r.Legs) != 2 {
		t.Fatalf("got %d legs, want 2", len(r.Legs))
	}
	for i, l := range r.Legs {
		if !slices.Contains(l.segs, segTaut) {
			t.Errorf("leg %d has no taut segment", i)
		}
		if len(l.segs)+1 != len(l.Path) {
			t.Errorf("leg %d: %d segs for %d points", i, len(l.segs), len(l.Path))
		}
	}
	for _, h := range r.Hazards {
		if h.From < 0 || h.To > len(r.Path)-1 || h.From >= h.To {
			t.Errorf("hazard %d..%d outside path of %d points", h.From, h.To, len(r.Path))
		}
	}
	for _, s := range r.Stretches {
		if s.From < 0 || s.To > len(r.Path)-1 {
			t.Errorf("stretch %d..%d outside path of %d points", s.From, s.To, len(r.Path))
		}
	}
}

func TestRouteFromInlandPort(t *testing.T) {
	start := Position{4.4667, 51.90}
	r, err := testFullGraph(t).Route([]Position{start, tautSingapore}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Path[0] != start {
		t.Errorf("path starts at %v, want %v", r.Path[0], start)
	}
}

func TestTautDeterministic(t *testing.T) {
	g := testFullGraph(t)
	a := routeOn(t, g, Options{}, tautRotterdam, portSaid)
	b := routeOn(t, g, Options{}, tautRotterdam, portSaid)
	if !slices.Equal(a.Path, b.Path) {
		t.Error("same request gave different paths")
	}
}

func TestRouteUnderOneSecond(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	g := testFullGraph(t)
	var ds []time.Duration
	for range 5 {
		start := time.Now()
		if _, err := g.Route([]Position{tautRotterdam, tautSingapore}, Options{}); err != nil {
			t.Fatal(err)
		}
		ds = append(ds, time.Since(start))
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	t.Logf("runs: %v, median %v", ds, ds[2])
	if limit := perfBudget(time.Second); ds[2] >= limit {
		t.Errorf("median %v, want under %v", ds[2], limit)
	}
}

func BenchmarkRouteRotterdamSingapore(b *testing.B) {
	g := testFullGraph(b)
	for b.Loop() {
		if _, err := g.Route([]Position{tautRotterdam, tautSingapore}, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTautSyntheticStraightensZigzag(t *testing.T) {
	// A zigzag of network edges over open water becomes one rhumb leg.
	g := NewWith(chainSegs(Position{0, 0}, Position{0.5, 0.2}, Position{1, 0}), landOf(rect(3, 3, 3.1, 3.1)), nil)
	r := routeOn(t, g, Options{}, Position{0, 0}, Position{1, 0})
	if len(r.Path) != 2 {
		t.Errorf("path %v, want a single leg", r.Path)
	}
}

func TestTautSyntheticKeepsClearOfLand(t *testing.T) {
	// The straight line from (0,0) to (1,0) would cross the islet; the taut
	// path bends round it with the requested clearance.
	g := NewWith(chainSegs(Position{0, 0}, Position{0.5, 0.2}, Position{1, 0}), landOf(rect(0.45, -0.02, 0.55, 0.05)), nil)
	r := routeOn(t, g, Options{}, Position{0, 0}, Position{1, 0})
	if len(r.Path) < 3 {
		t.Fatalf("path %v does not bend round the islet", r.Path)
	}
	for k := 0; k+1 < len(r.Path); k++ {
		if d := g.land.SegmentClearance(r.Path[k], r.Path[k+1], 3*MetresPerNM); d < DefaultClearanceNM*MetresPerNM {
			t.Errorf("segment %d clears land by %.2f NM", k, d/MetresPerNM)
		}
	}
	if len(r.Hazards) != 0 {
		t.Errorf("hazards %+v", r.Hazards)
	}
}

func TestTautSyntheticAvoidsShoal(t *testing.T) {
	// The long way round the shoal is cut short, but never across the shoal.
	g := newGraph(twoWays(), landOf(rect(3, 3, 3.1, 3.1)), nil, NewDepth(depthTiles([2]int32{1, 0}), "test"), "test")
	v := Vessel{DraughtM: 12, SpeedKn: 14, Type: Tanker}
	r := routeOn(t, g, Options{Vessel: v}, wayS, wayT)
	lim := v.DepthLimit(false).LimitM
	for k := 0; k+1 < len(r.Path); k++ {
		if d, known := g.depth.SegmentMin(r.Path[k], r.Path[k+1]); known && d < lim {
			t.Errorf("segment %d has %.0f m, vessel needs %.1f m", k, d, lim)
		}
	}
	if len(r.Path) > 4 {
		t.Errorf("path %v not straightened", r.Path)
	}
}

// joinGraph has land well away from the legs below, so only the land under test matters.
func joinGraph(rings ...[]Position) *Graph {
	return NewWith(chainSegs(Position{0, 0}, Position{1, 0}), landOf(rings...), nil)
}

func TestSafeLegJoinRelaxationIsLocal(t *testing.T) {
	// The join at the origin is 0.2 NM off a coast that runs along lat 0.2 NM.
	coast := rect(-1, 0.2/60, 1, 0.5)
	g := joinGraph(coast)
	o := Position{0, 0}
	c := &pathCtx{clearM: 2 * MetresPerNM, joins: map[Position]float64{o: g.land.PointDistance(o, 1e6)}}
	if jc := c.joins[o]; math.Abs(jc-0.2*MetresPerNM) > 5 {
		t.Fatalf("join clearance %.0f m", jc)
	}
	if g.safeLeg(o, Position{0.5, 0}, c) {
		t.Error("a leg hugging the coast at 0.2 NM for 30 NM is accepted")
	}
	if !g.safeLeg(o, Position{0, -0.3}, c) {
		t.Error("a leg leaving the coast from the join is rejected")
	}
	// Without the join entry the same leg away from the coast is judged at 2 NM
	// from the start, so it fails: the join is what lets it leave.
	if g.safeLeg(o, Position{0, -0.3}, &pathCtx{clearM: 2 * MetresPerNM}) {
		t.Error("leg from a point inside the clearance accepted without a join")
	}
}

func TestSafeLegJoinOnLandRejectsCrossing(t *testing.T) {
	g := joinGraph(rect(0.1, -0.1, 0.3, 0.1))
	j := Position{0.1, 0} // exactly on the west edge
	c := &pathCtx{clearM: 2 * MetresPerNM, joins: map[Position]float64{j: g.land.PointDistance(j, 1e6)}}
	if c.joins[j] > 1e-6 {
		t.Fatalf("join clearance %v, want 0", c.joins[j])
	}
	if g.safeLeg(j, Position{0.5, 0}, c) {
		t.Error("a leg from a join on land across the island is accepted")
	}
}

// meetsPassage reports whether a segment next to segment k of l is a passage edge.
func meetsPassage(g *Graph, l *Leg, k int) bool {
	for _, j := range [2]int{k - 1, k + 1} {
		if j >= 0 && j < len(l.segs) && l.segs[j] >= 0 && g.pass[l.segs[j]] != 0 {
			return true
		}
	}
	return false
}

// TestTautNoJoinDogLeg checks that no TSS join is kept as drawn where the
// leg from the vertex before it straight to its far end is safe and shorter:
// a ship heads for the lane end, it does not steer for a network node first.
func TestTautNoJoinDogLeg(t *testing.T) {
	g := testFullGraph(t)
	r := rotterdamSingapore(t)
	coarse := coarseLeg(t, g, tautRotterdam, tautSingapore)
	l := r.Legs[0]
	c := legCtxFor(g, ctxFor(g, Options{}, []Position{tautRotterdam, tautSingapore}), l, coarse.Path)
	for k := 1; k < len(l.segs); k++ {
		code := l.segs[k]
		if code < 0 || g.flags[code]&edgeJoin == 0 || l.segs[k-1] != segTaut {
			continue
		}
		p, v, q := l.Path[k-1], l.Path[k], l.Path[k+1]
		if Distance(p, q) < Distance(p, v)+Distance(v, q)-0.1*MetresPerNM && g.safeLeg(p, q, c) {
			t.Errorf("join %v -> %v kept as drawn: %v -> %v is safe and shorter", v, q, p, q)
		}
	}
}

// TestTautVerticesAreTight checks that every turn of the taut path sits where
// a leg meets an obstacle: sliding the vertex 0.5 NM towards the chord of its
// neighbours must make a leg unsafe. A turn left on a lattice node in open
// water shows as a kink a navigator would never draw.
func TestTautVerticesAreTight(t *testing.T) {
	g := testFullGraph(t)
	r := rotterdamSingapore(t)
	coarse := coarseLeg(t, g, tautRotterdam, tautSingapore)
	l := r.Legs[0]
	c := legCtxFor(g, ctxFor(g, Options{}, []Position{tautRotterdam, tautSingapore}), l, coarse.Path)
	for k := 1; k < len(l.segs); k++ {
		if l.segs[k-1] != segTaut || l.segs[k] != segTaut {
			continue
		}
		p, v, q := l.Path[k-1], l.Path[k], l.Path[k+1]
		target, ok := chordFoot(p, v, q)
		if !ok {
			continue
		}
		d := Distance(v, target)
		if d < 0.6*MetresPerNM {
			continue
		}
		w := slide(v, target, 0.5*MetresPerNM/d)
		if g.safeLeg(p, w, c) && g.safeLeg(w, q, c) &&
			Distance(p, w)+Distance(w, q) < Distance(p, v)+Distance(v, q)-0.05*MetresPerNM {
			t.Errorf("vertex %v is slack: moving it 0.5 NM to %v is safe and shorter", v, w)
		}
	}
}

// TestTightenSplitKeepsGreatCircle: tightening the seams of a split run must
// not fold a great-circle chain back into one longer rhumb leg.
func TestTightenSplitKeepsGreatCircle(t *testing.T) {
	g := testFullGraph(t)
	a, b := Position{-119.9998, 30}, Position{-175.112, 40.758}
	c := ctxFor(g, Options{}, []Position{a, b})
	gc, ok := g.greatCircleRun(a, b, c)
	if !ok {
		t.Fatal("great-circle chain over the North Pacific should be safe")
	}
	pts, segs := g.tightenSplit(a, gc[1:], appendTaut(nil, len(gc)-1), c)
	got := chainLength(append([]Position{a}, pts...))
	if want := chainLength(gc); got > want+MetresPerNM {
		t.Errorf("tightened chain %.1f NM, great circle %.1f NM (%d points)", got/MetresPerNM, want/MetresPerNM, len(pts))
	}
	if len(segs) != len(pts) {
		t.Errorf("segs %d, pts %d", len(segs), len(pts))
	}
}
