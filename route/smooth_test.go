package route

import (
	"math"
	"slices"
	"testing"
	"time"
)

var (
	yokohama   = Position{139.65, 35.45}
	losAngeles = Position{-118.27, 33.73}
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

// coverTSS is a thin zone along lat 0.2 whose 20 NM coverage reaches lat 0 but
// whose area (1 NM) does not.
func coverTSS() *TSS {
	return NewTSS([]TSSFeature{{Type: "zone", Scheme: "tss-cover", OSMID: 1, Coords: rect(-0.6, 0.19, 3.1, 0.21)}}, "v")
}

func landOf(rings ...[]Position) *Land { return NewLand(rings, 0, "test") }

var (
	kA = Position{0, 0}
	kB = Position{0.5, -0.3}
	kC = Position{1, 0}
)

// kinkGraph: a kink A-B-C whose shortcut A-C passes 1.5 NM from a small island.
func kinkGraph(withTSS bool) *Graph {
	var tss *TSS
	if withTSS {
		tss = coverTSS()
	}
	return NewWith(chainSegs(kA, kB, kC), landOf(rect(0.3, passLat, 0.7, 0.1)), tss)
}

func kinkRoute(t *testing.T, g *Graph, c float64, accept ...string) Route {
	t.Helper()
	return routeOn(t, g, Options{Kinks: true, ClearanceNM: c, Accept: accept}, kA, kC)
}

func flagsOf(r Route) []string {
	var out []string
	for _, s := range r.Stretches {
		out = append(out, s.Flag)
	}
	return out
}

func TestZigZagSmoothed(t *testing.T) {
	segs := chainSegs(Position{0, 0}, Position{0.5, 0.1}, Position{1, 0}, Position{1.5, 0.1}, Position{2, 0})
	g := NewWith(segs, landOf(rect(0, 10, 1, 11)), nil)
	plain := routeOn(t, g, Options{}, Position{0, 0}, Position{2, 0})
	r := routeOn(t, g, Options{Kinks: true}, Position{0, 0}, Position{2, 0})
	if len(r.Path) != 2 {
		t.Fatalf("path %v", r.Path)
	}
	if want := []Stretch{{From: 0, To: 1, Flag: "smoothed", Severity: "label"}}; !slices.Equal(r.Stretches, want) {
		t.Errorf("stretches %v", r.Stretches)
	}
	if r.DistanceM >= plain.DistanceM || math.Abs(r.DistanceM-Distance(Position{0, 0}, Position{2, 0})) > 1e-6 {
		t.Errorf("distance %v plain %v", r.DistanceM, plain.DistanceM)
	}
	if len(r.Proposals) != 0 || len(r.IgnoredAccept) != 0 {
		t.Errorf("proposals %v ignored %v", r.Proposals, r.IgnoredAccept)
	}
}

func TestBelowClearanceProposed(t *testing.T) {
	g := kinkGraph(true)
	plain := routeOn(t, g, Options{}, kA, kC)
	r := kinkRoute(t, g, 2)
	if len(r.Proposals) != 1 {
		t.Fatalf("proposals %v", r.Proposals)
	}
	p := r.Proposals[0]
	if p.Reason != "below clearance" || p.MinClearanceNM != 1.5 {
		t.Errorf("got %+v", p)
	}
	if !slices.Equal(p.Path, []Position{kA, kC}) || p.SavedNM < 0.1 {
		t.Errorf("got %+v", p)
	}
	if !slices.Equal(r.Path, plain.Path) || r.DistanceM != plain.DistanceM {
		t.Errorf("path changed: %v", r.Path)
	}
}

func TestTouchingLandNeverOffered(t *testing.T) {
	g := NewWith(chainSegs(kA, kB, kC), landOf(rect(0.3, -0.05, 0.7, 0.1)), coverTSS())
	r := kinkRoute(t, g, 2)
	if len(r.Proposals) != 0 || len(r.Path) != 3 {
		t.Fatalf("proposals %v path %v", r.Proposals, r.Path)
	}
}

func TestNoTSSDataProposed(t *testing.T) {
	g := kinkGraph(false)
	r := kinkRoute(t, g, 1)
	if len(r.Proposals) != 1 || r.Proposals[0].Reason != "no TSS data" || len(r.Path) != 3 {
		t.Fatalf("proposals %v path %v", r.Proposals, r.Path)
	}
}

func TestAcceptApplies(t *testing.T) {
	g := kinkGraph(true)
	base := kinkRoute(t, g, 2)
	p := base.Proposals[0]
	r := kinkRoute(t, g, 2, p.ID)
	if len(r.IgnoredAccept) != 0 {
		t.Errorf("ignored %v", r.IgnoredAccept)
	}
	if !slices.Equal(r.Path, []Position{kA, kC}) {
		t.Fatalf("path %v", r.Path)
	}
	if want := []Stretch{{From: 0, To: 1, Flag: "user-accepted", Severity: "label"}}; !slices.Equal(r.Stretches, want) {
		t.Errorf("stretches %v", r.Stretches)
	}
	if got := (base.DistanceM - r.DistanceM) / MetresPerNM; math.Abs(got-p.SavedNM) > 0.01 {
		t.Errorf("distance dropped %v NM, proposal says %v", got, p.SavedNM)
	}
}

func TestUnknownAcceptIgnored(t *testing.T) {
	g := kinkGraph(true)
	r := kinkRoute(t, g, 2, "k-000000000000", "nonsense", "k-000000000000")
	if !slices.Equal(r.IgnoredAccept, []string{"k-000000000000", "nonsense"}) || len(r.Path) != 3 {
		t.Errorf("ignored %v path %v", r.IgnoredAccept, r.Path)
	}
}

func TestAcceptSmoothedIDNoop(t *testing.T) {
	segs := chainSegs(Position{0, 0}, Position{0.5, 0.1}, Position{1, 0})
	g := NewWith(segs, landOf(rect(0, 10, 1, 11)), nil)
	id := proposalID(g.Version(), []Position{{0, 0}, {0.5, 0.1}, {1, 0}})
	r := routeOn(t, g, Options{Kinks: true, Accept: []string{id}}, Position{0, 0}, Position{1, 0})
	if len(r.IgnoredAccept) != 0 || len(r.Path) != 2 || !slices.Equal(flagsOf(r), []string{"smoothed"}) {
		t.Errorf("ignored %v path %v flags %v", r.IgnoredAccept, r.Path, flagsOf(r))
	}
}

func TestAcceptSurvivesClearanceChange(t *testing.T) {
	g := kinkGraph(true)
	id := kinkRoute(t, g, 2).Proposals[0].ID

	r := kinkRoute(t, g, 1.75, id)
	if len(r.Proposals) != 1 || r.Proposals[0].ID != id || !slices.Equal(flagsOf(r), []string{"user-accepted"}) || len(r.IgnoredAccept) != 0 {
		t.Errorf("C=1.75: proposals %v flags %v ignored %v", r.Proposals, flagsOf(r), r.IgnoredAccept)
	}
	r = kinkRoute(t, g, 1, id)
	if len(r.Proposals) != 0 || !slices.Equal(flagsOf(r), []string{"smoothed"}) || len(r.IgnoredAccept) != 0 {
		t.Errorf("C=1: proposals %v flags %v ignored %v", r.Proposals, flagsOf(r), r.IgnoredAccept)
	}
	// A route where the kink does not come up.
	r = routeOn(t, g, Options{Kinks: true, Accept: []string{id}}, kA, kB)
	if !slices.Equal(r.IgnoredAccept, []string{id}) {
		t.Errorf("other route: ignored %v", r.IgnoredAccept)
	}
}

// span returns the Path indices of a proposal's end vertices.
func propSpan(r Route, p Proposal) (int, int) {
	return slices.Index(r.Path, p.Path[0]), slices.Index(r.Path, p.Path[len(p.Path)-1])
}

func TestProposalsDoNotOverlap(t *testing.T) {
	a, b, c := Position{0, 0}, Position{0.5, -0.3}, Position{1, 0}
	d, e, f := Position{1.5, 0}, Position{2, -0.3}, Position{2.5, 0}
	land := landOf(rect(0.3, passLat, 0.7, 0.1), rect(2.3, passLat, 2.7, 0.1), rect(1.1, -0.4, 1.4, 0.05))
	g := NewWith(chainSegs(a, b, c, d, e, f), land, coverTSS())
	r := routeOn(t, g, Options{Kinks: true}, a, f)
	if len(r.Proposals) != 2 {
		t.Fatalf("proposals %v", r.Proposals)
	}
	end := -1
	for _, p := range r.Proposals {
		i, j := propSpan(r, p)
		if i < end || j <= i {
			t.Errorf("proposal %v overlaps the one before (ends at %d)", p, end)
		}
		end = j
	}
}

func TestKinksOffNoChanges(t *testing.T) {
	g := kinkGraph(true)
	tssOnly := NewWith(chainSegs(kA, kB, kC), nil, coverTSS())
	want := routeOn(t, tssOnly, Options{}, kA, kC)
	r := routeOn(t, g, Options{Accept: []string{"k-aaaaaaaaaaaa"}}, kA, kC)
	if !slices.Equal(r.Path, want.Path) || len(r.Proposals) != 0 || !slices.Equal(r.IgnoredAccept, []string{"k-aaaaaaaaaaaa"}) {
		t.Errorf("path %v proposals %v ignored %v", r.Path, r.Proposals, r.IgnoredAccept)
	}
	if len(r.Stretches) == 0 {
		t.Error("stretches are always computed")
	}
}

func TestSmoothingPerLegKeepsViaPoints(t *testing.T) {
	segs := chainSegs(Position{0, 0}, Position{0.5, 0.1}, Position{1, 0}, Position{1.5, 0.1}, Position{2, 0})
	g := NewWith(segs, landOf(rect(0, 10, 1, 11)), nil)
	r := routeOn(t, g, Options{Kinks: true}, Position{0, 0}, Position{1, 0.01}, Position{2, 0})
	if !slices.Equal(r.Path, []Position{{0, 0}, {1, 0}, {2, 0}}) {
		t.Errorf("path %v", r.Path)
	}
	if want := []Stretch{{From: 0, To: 2, Flag: "smoothed", Severity: "label"}}; !slices.Equal(r.Stretches, want) {
		t.Errorf("stretches %v, want %v", r.Stretches, want)
	}
	if len(r.Legs) != 2 || len(r.Legs[0].Path) != 2 || len(r.Legs[1].Path) != 2 {
		t.Errorf("legs %v", r.Legs)
	}
}

func TestLongLegIsOneSegment(t *testing.T) {
	var pts []Position
	for i := 0; i <= 20; i++ {
		pts = append(pts, Position{float64(i) * 0.25, float64(i%2) * 0.1})
	}
	g := NewWith(chainSegs(pts...), landOf(rect(0, 10, 1, 11)), nil)
	a, b := pts[0], pts[20]
	r := routeOn(t, g, Options{Kinks: true}, a, b)
	if len(r.Path) != 2 || math.Abs(r.DistanceM-Distance(a, b)) > 1e-6 {
		t.Errorf("path %d vertices, distance %v want %v", len(r.Path), r.DistanceM, Distance(a, b))
	}
}

func TestNoShortcutAcrossAntimeridian(t *testing.T) {
	g := testTSSGraph(t)
	wraps := func(p []Position) (n int) {
		for i := 0; i+1 < len(p); i++ {
			if math.Abs(p[i+1][0]-p[i][0]) > 180 {
				n++
			}
		}
		return n
	}
	plain := routeOn(t, g, Options{}, yokohama, losAngeles)
	r := routeOn(t, g, Options{Kinks: true}, yokohama, losAngeles)
	if wraps(r.Path) != wraps(plain.Path) {
		t.Errorf("antimeridian segments: %d, was %d", wraps(r.Path), wraps(plain.Path))
	}
	var sum float64
	for i := 0; i+1 < len(r.Path); i++ {
		sum += Distance(r.Path[i], r.Path[i+1])
	}
	if math.Abs(sum-r.DistanceM) > 1 {
		t.Errorf("distance %v, path sum %v", r.DistanceM, sum)
	}
	t.Logf("Yokohama-LA: %d -> %d vertices, %.1f -> %.1f nm", len(plain.Path), len(r.Path), plain.DistanceNM(), r.DistanceNM())
}

// kinks counts vertices whose local detour exceeds 3 %.
func kinks(p []Position) (n int) {
	for i := 1; i+1 < len(p); i++ {
		if (Distance(p[i-1], p[i])+Distance(p[i], p[i+1]))/Distance(p[i-1], p[i+1]) > 1.03 {
			n++
		}
	}
	return n
}

// freeKinks counts path vertices with local detour > 3 % whose two adjacent
// segments are both smoothable: flagged marnet, smoothed, user-accepted or
// tss-unknown, and not passage-tagged. Kinks on lanes, passages and connectors
// are kept on purpose.
func freeKinks(g *Graph, r Route) (n int) {
	flags := make([]string, len(r.Path)-1)
	for _, s := range r.Stretches {
		for k := s.From; k < s.To; k++ {
			flags[k] = s.Flag
		}
	}
	var segs []int32
	for _, l := range r.Legs {
		segs = append(segs, l.segs...)
	}
	free := func(k int) bool {
		switch flags[k] {
		case "marnet", "smoothed", "user-accepted", "tss-unknown":
			return segs[k] < 0 || g.pass[segs[k]] == 0
		}
		return false
	}
	for i := 1; i+1 < len(r.Path); i++ {
		if (Distance(r.Path[i-1], r.Path[i])+Distance(r.Path[i], r.Path[i+1]))/Distance(r.Path[i-1], r.Path[i+1]) > 1.03 &&
			free(i-1) && free(i) {
			n++
		}
	}
	return n
}

func TestRealKinksReduced(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	g := testTSSGraph(t)
	for _, c := range []struct {
		name     string
		from, to Position
	}{{"Rotterdam-Singapore", rotterdam, singapore}, {"Southampton-Hamburg", southampt, hamburg}} {
		base := routeOn(t, g, Options{NoTSS: true}, c.from, c.to)
		tssOnly := routeOn(t, g, Options{}, c.from, c.to)
		first := routeOn(t, g, Options{Kinks: true}, c.from, c.to)
		var ids []string
		for _, p := range first.Proposals {
			ids = append(ids, p.ID)
		}
		r := routeOn(t, g, Options{Kinks: true, Accept: ids}, c.from, c.to)
		got, want := freeKinks(g, r), freeKinks(g, tssOnly)
		t.Logf("%s: raw kinks NoTSS %d, TSS-only %d, smoothed %d; free kinks TSS-only %d, smoothed+accepted %d; %.1f -> %.1f nm; %d proposals", c.name,
			kinks(base.Path), kinks(tssOnly.Path), kinks(r.Path), want, got, tssOnly.DistanceNM(), r.DistanceNM(), len(ids))
		if got*2 > want {
			t.Errorf("%s: %d free kinks, TSS-only baseline %d", c.name, got, want)
		}
		if r.DistanceM > tssOnly.DistanceM {
			t.Errorf("%s: %.1f nm longer than TSS-only %.1f", c.name, r.DistanceNM(), tssOnly.DistanceNM())
		}
		if !slices.Equal(r.Passages, tssOnly.Passages) {
			t.Errorf("%s: passages %v, TSS-only %v", c.name, r.Passages, tssOnly.Passages)
		}
		checkSmoothedClear(t, g, r)
	}
}

func checkSmoothedClear(t *testing.T, g *Graph, r Route) {
	t.Helper()
	for _, s := range r.Stretches {
		if s.Flag != "smoothed" {
			continue
		}
		if c := g.land.SegmentClearance(r.Path[s.From], r.Path[s.To], 5*MetresPerNM); c < 2*MetresPerNM {
			t.Errorf("smoothed stretch %v clears land by %.2f nm", s, c/MetresPerNM)
		}
	}
}

func TestNarrowPassagesSafe(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	g := testTSSGraph(t)
	for _, c := range []struct {
		name     string
		pts      []Position
		passage  string
		wantPass bool
	}{
		{"Rotterdam-Gdynia via Kiel", []Position{rotterdam, {10.0, 54.35}, gdynia}, "kiel", true},
		{"Marmara-Black Sea", []Position{{28.98, 40.98}, {29.15, 41.30}}, "bosporus", false},
	} {
		tssOnly := routeOn(t, g, Options{}, c.pts...)
		r := routeOn(t, g, Options{Kinks: true}, c.pts...)
		t.Logf("%s: passages %v, %.1f -> %.1f nm, %d proposals", c.name, r.Passages, tssOnly.DistanceNM(), r.DistanceNM(), len(r.Proposals))
		if !slices.Equal(r.Passages, tssOnly.Passages) || (c.wantPass && !slices.Contains(r.Passages, c.passage)) {
			t.Errorf("%s: passages %v, TSS-only %v", c.name, r.Passages, tssOnly.Passages)
		}
		checkSmoothedClear(t, g, r)
		lifted := map[string]bool{}
		for _, l := range r.Legs {
			for _, k := range l.liftOrder {
				lifted[g.tss.Schemes[k].ID] = true
			}
		}
		if v := g.tss.violations(r.Path, lifted); len(v) > 0 {
			t.Errorf("%s: TSS violations: %v", c.name, v)
		}
		for _, s := range r.Stretches {
			if (s.Flag == "smoothed" || s.Flag == "user-accepted") && g.tss.SegmentTouchesArea(r.Path[s.From], r.Path[s.To]) {
				t.Errorf("%s: %s stretch %v touches a scheme area", c.name, s.Flag, s)
			}
		}
		tol := g.land.Tolerance
		for _, p := range r.Proposals {
			if g.land.SegmentClearance(p.Path[0], p.Path[1], tol+1) <= tol {
				t.Errorf("%s: proposal %v within land tolerance", c.name, p)
			}
		}
	}
}

func TestRoutePerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	g := testTSSGraph(t)
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
		r := routeOn(t, g, Options{Kinks: true}, c.from, c.to)
		took := time.Since(start)
		t.Logf("%s: %v, %d vertices, %d proposals, %d stretches", c.name, took, len(r.Path), len(r.Proposals), len(r.Stretches))
		if took > time.Second {
			t.Errorf("%s took %v", c.name, took)
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
	r := routeOn(t, g, Options{Kinks: true}, rotterdam, singapore)
	checkBounds(r)
	if s, ok := has(r, "connector"); !ok || s.Severity != "label" {
		t.Errorf("no connector stretch: %v", r.Stretches)
	}
	r = routeOn(t, g, Options{Kinks: true}, rotterdam, bilbao)
	checkBounds(r)
	if _, ok := has(r, "osm-tss"); !ok {
		t.Errorf("Dover transit has no osm-tss stretch: %v", r.Stretches)
	}
	r = routeOn(t, g, Options{Kinks: true}, chesapeake, newYork)
	checkBounds(r)
	if s, ok := has(r, "tss-unknown"); !ok || s.Severity != "info" {
		t.Errorf("no tss-unknown stretch: %v", r.Stretches)
	}
}
