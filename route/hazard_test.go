package route

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

// coarseHazards runs the hazard check on the network path alone, as Route
// builds it before the taut stage reshapes it, so the checks see land the
// network path touches.
func coarseHazards(t *testing.T, g *Graph, opts Options, pts ...Position) (hazards []Hazard, path []Position) {
	t.Helper()
	blocked, _ := g.blockedPasses(nil, nil)
	sc := searchCtx{blocked: blocked}
	snapped := make([]int32, len(pts))
	for i, p := range pts {
		v, ok := g.nearest(p, sc)
		if !ok {
			t.Fatal("no usable node")
		}
		snapped[i] = v
	}
	var legs []Leg
	for i := 0; i+1 < len(pts); i++ {
		from, to := pts[i], pts[i+1]
		if i > 0 {
			from = g.nodes[snapped[i]]
		}
		if i+2 < len(pts) {
			to = g.nodes[snapped[i+1]]
		}
		l, err := g.leg(from, to, snapped[i], snapped[i+1], sc, AStar)
		if err != nil {
			t.Fatal(err)
		}
		legs = append(legs, l)
		if i == 0 {
			path = append(path, l.Path...)
		} else {
			path = append(path, l.Path[1:]...)
		}
	}
	return g.hazards(legs, path, opts, &pathCtx{ends: pts}), path
}

func TestHazardAground(t *testing.T) {
	g := NewWith(chainSegs(Position{0, 0}, Position{1, 0}), landOf(rect(0.4, -0.1, 0.6, 0.1)), nil)
	hz, _ := coarseHazards(t, g, Options{}, Position{0, 0}, Position{1, 0})
	want := []Hazard{{From: 0, To: 1, Start: Position{0, 0}, End: Position{1, 0}, Severity: "aground", Kind: "marnet"}}
	if !reflect.DeepEqual(hz, want) {
		t.Errorf("hazards %+v", hz)
	}
}

func TestHazardClose(t *testing.T) {
	g := NewWith(chainSegs(Position{0, 0}, Position{1, 0}), landOf(rect(0.4, passLat, 0.6, 0.1)), nil)
	hz, _ := coarseHazards(t, g, Options{ClearanceNM: 2}, Position{0, 0}, Position{1, 0})
	if len(hz) != 1 || hz[0].Severity != "close" || hz[0].MinClearanceNM != 1.5 {
		t.Errorf("hazards %+v", hz)
	}
	if hz, _ = coarseHazards(t, g, Options{ClearanceNM: 1}, Position{0, 0}, Position{1, 0}); len(hz) != 0 {
		t.Errorf("1 NM clearance: hazards %+v", hz)
	}
}

func TestHazardsMerge(t *testing.T) {
	g := NewWith(chainSegs(Position{0, 0}, Position{0.5, 0}, Position{1, 0}), landOf(rect(0.2, -0.05, 0.8, 0.05)), nil)
	hz, _ := coarseHazards(t, g, Options{}, Position{0, 0}, Position{1, 0})
	if len(hz) != 1 || hz[0].From != 0 || hz[0].To != 2 {
		t.Errorf("hazards %+v", hz)
	}
}

func TestHazardsEmptyWithoutLand(t *testing.T) {
	g := NewWith(chainSegs(Position{0, 0}, Position{1, 0}), nil, nil)
	if r := routeOn(t, g, Options{}, Position{0, 0}, Position{1, 0}); r.Hazards == nil || len(r.Hazards) != 0 {
		t.Errorf("hazards %#v", r.Hazards)
	}
}

func TestHazardIndicesAcrossLegs(t *testing.T) {
	// Two legs via (1, 0); land only on the second leg.
	g := NewWith(chainSegs(Position{0, 0}, Position{1, 0}, Position{2, 0}), landOf(rect(1.4, -0.1, 1.6, 0.1)), nil)
	hz, path := coarseHazards(t, g, Options{}, Position{0, 0}, Position{1, 0}, Position{2, 0})
	if len(hz) != 1 || hz[0].From != 1 || hz[0].To != 2 || hz[0].Start != (Position{1, 0}) {
		t.Errorf("hazards %+v path %v", hz, path)
	}
}

func TestRealHazards(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	g := testTSSGraph(t)
	r := routeOn(t, g, Options{}, rotterdam, singapore)
	suez := false
	for _, h := range r.Hazards {
		t.Logf("hazard %+v", h)
		if h.Passage == "suez" && h.Severity == "aground" {
			suez = true
		}
	}
	if !suez {
		t.Error("no aground hazard with passage suez")
	}
	// No lane-end join touches the land data tolerance (Bab-el-Mandeb was
	// one). Joins made by addOneWay have a wrong-way twin; the split MARNET
	// edges of addAccess also carry edgeJoin but follow MARNET's own line.
	k := 0
	for _, l := range r.Legs {
		for _, code := range l.segs {
			if code >= 0 && g.flags[code]&edgeJoin != 0 && g.flags[g.twin[code]]&edgeWrongWay != 0 {
				if c := g.land.SegmentClearance(r.Path[k], r.Path[k+1], g.land.Tolerance+1); c <= g.land.Tolerance {
					t.Errorf("join %d %v -> %v aground", k, r.Path[k], r.Path[k+1])
				}
			}
			k++
		}
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

func TestConnectorOutsideCorridorUnchecked(t *testing.T) {
	land := landOf(rect(5, 5, 6, 6))
	land.CorridorNM = 10
	g := NewWith(chainSegs(Position{0, 0}, Position{1, 0}), land, nil)
	r := routeOn(t, g, Options{}, Position{0, 1}, Position{1, 0})
	if len(r.Hazards) != 1 || r.Hazards[0].Severity != "unchecked" || r.Hazards[0].Kind != "connector" || r.Hazards[0].Note == "" {
		t.Fatalf("hazards %+v", r.Hazards)
	}
	land.CorridorNM = 0
	g = NewWith(chainSegs(Position{0, 0}, Position{1, 0}), land, nil)
	if r = routeOn(t, g, Options{}, Position{0, 1}, Position{1, 0}); len(r.Hazards) != 0 {
		t.Errorf("full coverage: hazards %+v", r.Hazards)
	}
}

var (
	southampt  = Position{-1.40, 50.89}
	hamburg    = Position{9.97, 53.54}
	chesapeake = Position{-75.8, 36.9}
	newYork    = Position{-74.0, 40.6}
)

// passLat is the latitude of the land edge that sits 1.5 NM from the line lat 0.
var passLat = 1.5 * MetresPerNM / degLatM

func chainSegs(pts ...Position) []Segment {
	var segs []Segment
	for i := 0; i+1 < len(pts); i++ {
		segs = append(segs, Segment{A: pts[i], B: pts[i+1]})
	}
	return segs
}

func rect(lon0, lat0, lon1, lat1 float64) []Position {
	return []Position{{lon0, lat0}, {lon1, lat0}, {lon1, lat1}, {lon0, lat1}, {lon0, lat0}}
}

func landOf(rings ...[]Position) *Land { return NewLand(rings, 0, "test") }

func TestRoutePerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	g := testTSSGraph(t)
	limit := perfBudget(time.Second)
	for _, c := range []struct {
		name     string
		from, to Position
	}{
		{"Rotterdam-Singapore", rotterdam, singapore},
		{"Southampton-Hamburg", southampt, hamburg},
		{"Rotterdam-Gdynia", rotterdam, gdynia},
		{"Bilbao-Gdynia", bilbao, gdynia},
		{"RichardsBay-SeptIles", richardsBay, septIles},
	} {
		start := time.Now()
		r := routeOn(t, g, Options{}, c.from, c.to)
		took := time.Since(start)
		t.Logf("%s: %v, %d vertices, %d stretches, %d hazards", c.name, took, len(r.Path), len(r.Stretches), len(r.Hazards))
		if took > limit {
			t.Errorf("%s took %v, limit %v", c.name, took, limit)
		}
	}
}

func TestStretchFlags(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	g := testTSSGraph(t)
	has := func(r Route, flag string) (Stretch, bool) {
		for _, s := range r.Stretches {
			if s.Flag == flag {
				return s, true
			}
		}
		return Stretch{}, false
	}
	checkBounds := func(r Route) {
		for _, s := range r.Stretches {
			if s.From < 0 || s.To <= s.From || s.To >= len(r.Path) {
				t.Errorf("bad stretch %v for %d vertices", s, len(r.Path))
			}
		}
	}
	r := routeOn(t, g, Options{}, rotterdam, singapore)
	checkBounds(r)
	if s, ok := has(r, "connector"); !ok || s.Severity != "label" {
		t.Errorf("no connector stretch: %v", r.Stretches)
	}
	r = routeOn(t, g, Options{}, rotterdam, bilbao)
	checkBounds(r)
	if _, ok := has(r, "osm-tss"); !ok {
		t.Errorf("Dover transit has no osm-tss stretch: %v", r.Stretches)
	}
	r = routeOn(t, g, Options{}, chesapeake, newYork)
	checkBounds(r)
	if s, ok := has(r, "tss-unknown"); !ok || s.Severity != "info" {
		t.Errorf("no tss-unknown stretch: %v", r.Stretches)
	}
}

// hazardGraph is the full graph with depth when the dataset exists, and the
// land and TSS graph (shared with the other tests) when it does not.
func hazardGraph(t *testing.T) *Graph {
	t.Helper()
	if _, err := os.Stat(depthData); err == nil {
		return testFullGraph(t)
	}
	return testTSSGraph(t)
}

func TestHazardShallow(t *testing.T) {
	// One 8 m tile across a straight MARNET line; the limit is 10 + squat + margin.
	m := deepCells()
	for i := range m {
		m[i] = 8
	}
	depth := NewDepth([]DepthTile{{CX: 0, CY: 0, M: m}}, "x/gebco2026")
	a, b := cellCentre(0, 0, 10, 20), cellCentre(0, 0, 100, 20)
	g := newGraph(chainSegs(a, b), nil, nil, depth, "test")
	var r Route
	blocked, _ := g.blockedPasses(nil, nil)
	sc := searchCtx{blocked: blocked}
	va, _ := g.nearest(a, sc)
	vb, _ := g.nearest(b, sc)
	leg, err := g.leg(a, b, va, vb, sc, AStar)
	if err != nil {
		t.Fatal(err)
	}
	r.Legs, r.Path = []Leg{leg}, leg.Path
	pc := &pathCtx{limitM: 13.6, limitConfinedM: 13.6, ends: []Position{{0, 40}, {0, 41}}}
	got := g.hazards(r.Legs, r.Path, Options{}, pc)
	if len(got) != 1 || got[0].Severity != "shallow" || got[0].Kind != "marnet" {
		t.Fatalf("hazards %+v", got)
	}
	pc.limitM = 0
	if got = g.hazards(r.Legs, r.Path, Options{}, pc); len(got) != 0 {
		t.Errorf("no limit: hazards %+v", got)
	}
}

func TestHazardApproach(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	g := hazardGraph(t)
	// The land data has no river at Pub. 151 Rotterdam (51.90 N, 4.4667 E), so
	// that point passes the full check itself and a first finding is not at
	// index 0 there. tautRotterdam is on the coast, in the approach.
	r := routeOn(t, g, Options{}, tautRotterdam, singapore)
	if len(r.Hazards) == 0 || r.Hazards[0].Severity != "approach" || r.Hazards[0].From != 0 {
		t.Fatalf("hazards %+v", r.Hazards)
	}
	if r.Hazards[0].Note != noteApproach {
		t.Errorf("note %q", r.Hazards[0].Note)
	}
	r = routeOn(t, g, Options{}, Position{4.4667, 51.90}, singapore)
	for _, h := range r.Hazards {
		if h.Severity != "aground" && h.Severity != "shallow" {
			continue
		}
		track := 0.0
		for k := 0; k < h.To; k++ {
			track += Distance(r.Path[k], r.Path[k+1])
		}
		if track <= endpointExemptNM*MetresPerNM {
			t.Errorf("%s hazard %d..%d ends within the approach (%.1f NM)", h.Severity, h.From, h.To, track/MetresPerNM)
		}
	}
}

func TestHazardSuezCanalNote(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	g := hazardGraph(t)
	r := routeOn(t, g, Options{Vessel: Vessel{DraughtM: 15}}, rotterdam, singapore)
	for _, h := range r.Hazards {
		if h.Passage == "suez" && h.Note == notePassage {
			return
		}
	}
	t.Errorf("no suez hazard with the passage note: %+v", r.Hazards)
}

func TestHazardsQuietOnOpenRoute(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	g := hazardGraph(t)
	r := routeOn(t, g, Options{}, Position{-74.02, 40.70}, Position{18.43, -33.91})
	for _, h := range r.Hazards {
		if h.Severity != "approach" && h.Severity != "unchecked" {
			t.Errorf("hazard %+v", h)
		}
	}
}

// TestShallowHazardWithoutLandMarshals: with depth but no land data a shallow
// hazard has a finite clearance, so the route encodes as JSON.
func TestShallowHazardWithoutLandMarshals(t *testing.T) {
	a, b := rowLeg()
	d := NewDepth([]DepthTile{rowTile(7, 10, 14)}, "t")
	g := newGraph([]Segment{{A: a, B: b}}, nil, nil, d, "t")
	r := routeOn(t, g, Options{Vessel: Vessel{DraughtM: 5}}, a, b)
	shallow := false
	for _, h := range r.Hazards {
		shallow = shallow || h.Severity == "shallow"
	}
	if !shallow {
		t.Fatalf("no shallow hazard: %v", r.Hazards)
	}
	if _, err := json.Marshal(r); err != nil {
		t.Fatalf("marshal: %v", err)
	}
}
