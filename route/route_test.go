package route

import (
	"math"
	"slices"
	"sync"
	"testing"
)

const network = "../dataset/marnet_plus_5km.geojson"

// Port positions from dataset/port-list.sqlite.
var (
	bilbao      = Position{-3.0469894370947661, 43.339978415980845}
	gdynia      = Position{18.546982540197764, 54.538336522499797}
	rotterdam   = Position{4.4424468168432654, 51.904383352111097}
	richardsBay = Position{32.083520507812, -28.771113820687}
	septIles    = Position{-66.36136067705, 50.202675418655}
	singapore   = Position{103.85, 1.26}
	// Via-points. Positions are approximate; routing snaps them to the network.
	skaw      = Position{10.80, 57.80}  // off the Skaw, north of Grenen
	greatBelt = Position{11.04, 55.335} // Great Belt East Bridge span
	cabot     = Position{-59.9, 47.3}   // Cabot Strait
)

var (
	graphOnce sync.Once
	graph     *Graph
	graphErr  error
)

func testGraph(tb testing.TB) *Graph {
	tb.Helper()
	graphOnce.Do(func() { graph, graphErr = Load(network) })
	if graphErr != nil {
		tb.Fatal(graphErr)
	}
	return graph
}

// Boxes around passages the scene routes must avoid, as [minLon, minLat, maxLon, maxLat].
var (
	kielCanal = [4]float64{9.1, 53.85, 10.2, 54.4}     // draught limit too low for a laden Handysize
	theSound  = [4]float64{12.45, 55.45, 12.95, 56.08} // Drogden draught limit
	belleIsle = [4]float64{-57.3, 51.0, -55.6, 51.9}   // ice-bound in winter
)

func passes(r Route, box [4]float64) bool {
	return slices.ContainsFunc(r.Path, func(p Position) bool {
		return p[0] > box[0] && p[1] > box[1] && p[0] < box[2] && p[1] < box[3]
	})
}

func mustRoute(t *testing.T, opts Options, points ...Position) Route {
	t.Helper()
	r, err := testGraph(t).Route(points, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPassagesLoaded(t *testing.T) {
	got := testGraph(t).Passages()
	for _, want := range []string{"kiel", "suez", "panama", "dover", "gibraltar", "corinth"} {
		if !slices.Contains(got, want) {
			t.Errorf("passage %q missing; got %v", want, got)
		}
	}
}

func TestRouteDistances(t *testing.T) {
	// Wide bounds: these catch unit and direction bugs only. Totals are
	// not checked against published distance tables here.
	cases := []struct {
		name   string
		points []Position
		minNM  float64
		maxNM  float64
	}{
		{"Richards Bay to Rotterdam", []Position{richardsBay, rotterdam}, 6000, 7500},
		{"Rotterdam to Sept-Iles via Cabot Strait", []Position{rotterdam, cabot, septIles}, 2800, 3400},
		{"Bilbao to Gdynia via Skaw and Great Belt", []Position{bilbao, skaw, greatBelt, gdynia}, 1300, 1900},
	}
	for _, c := range cases {
		r := mustRoute(t, Options{}, c.points...)
		nm := r.DistanceNM()
		t.Logf("%s: %.0f nm, %d points, passages %v", c.name, nm, len(r.Path), r.Passages)
		if nm < c.minNM || nm > c.maxNM {
			t.Errorf("%s: %.0f nm, want %.0f-%.0f", c.name, nm, c.minNM, c.maxNM)
		}
	}
}

func TestScenePassagesAvoided(t *testing.T) {
	// IVELLE STAR: laden Handysize, no Kiel Canal, no Sound (Drogden).
	ivelle := mustRoute(t, Options{Avoid: []string{"kiel", "oresund"}}, bilbao, gdynia)
	t.Logf("Bilbao to Gdynia avoiding Kiel and the Sound: %.0f nm %v", ivelle.DistanceNM(), ivelle.Passages)
	if passes(ivelle, kielCanal) || passes(ivelle, theSound) {
		t.Errorf("route uses Kiel or the Sound: %v", ivelle.Passages)
	}
	// TORMAR SPRING in January: no Strait of Belle Isle.
	tormar := mustRoute(t, Options{Avoid: []string{"belle_isle"}}, rotterdam, septIles)
	t.Logf("Rotterdam to Sept-Iles avoiding Belle Isle: %.0f nm %v", tormar.DistanceNM(), tormar.Passages)
	if passes(tormar, belleIsle) {
		t.Error("route passes the Strait of Belle Isle")
	}
}

func TestExtraPassagesTagged(t *testing.T) {
	sound := mustRoute(t, Options{Avoid: []string{"kiel"}}, bilbao, gdynia)
	if passes(sound, theSound) && !slices.Contains(sound.Passages, "oresund") {
		t.Errorf("route through the Sound is not tagged oresund: %v", sound.Passages)
	}
	belle := mustRoute(t, Options{}, rotterdam, septIles)
	if passes(belle, belleIsle) && !slices.Contains(belle.Passages, "belle_isle") {
		t.Errorf("route through Belle Isle is not tagged belle_isle: %v", belle.Passages)
	}
}

func TestArcticClosedByDefault(t *testing.T) {
	r := mustRoute(t, Options{Avoid: []string{"suez"}}, rotterdam, singapore)
	if slices.Contains(r.Passages, "northeast") || slices.Contains(r.Passages, "northwest") {
		t.Errorf("default route uses an Arctic passage: %v", r.Passages)
	}
	open := mustRoute(t, Options{Avoid: []string{"suez"}, Allow: []string{"northeast"}}, rotterdam, singapore)
	t.Logf("Rotterdam to Singapore avoiding Suez: %.0f nm %v; with Northeast Passage open: %.0f nm %v", r.DistanceNM(), r.Passages, open.DistanceNM(), open.Passages)
}

func TestAvoidKiel(t *testing.T) {
	direct := mustRoute(t, Options{}, rotterdam, gdynia)
	if !slices.Contains(direct.Passages, "kiel") {
		t.Skipf("shortest Rotterdam to Gdynia does not use Kiel (passages %v); nothing to avoid", direct.Passages)
	}
	avoided := mustRoute(t, Options{Avoid: []string{"kiel"}}, rotterdam, gdynia)
	if slices.Contains(avoided.Passages, "kiel") || passes(avoided, kielCanal) {
		t.Fatalf("route still uses Kiel: %v", avoided.Passages)
	}
	t.Logf("Rotterdam to Gdynia: %.0f nm via Kiel, %.0f nm avoiding it", direct.DistanceNM(), avoided.DistanceNM())
	if avoided.DistanceM <= direct.DistanceM {
		t.Error("avoiding Kiel should be longer")
	}
}

func TestAvoidSuez(t *testing.T) {
	via := mustRoute(t, Options{}, rotterdam, singapore)
	round := mustRoute(t, Options{Avoid: []string{"suez"}}, rotterdam, singapore)
	t.Logf("Rotterdam to Singapore: %.0f nm %v; avoiding Suez %.0f nm %v", via.DistanceNM(), via.Passages, round.DistanceNM(), round.Passages)
	if !slices.Contains(via.Passages, "suez") {
		t.Errorf("shortest route should use Suez, got %v", via.Passages)
	}
	if slices.Contains(round.Passages, "suez") {
		t.Error("route avoiding Suez still uses it")
	}
	if round.DistanceNM() < via.DistanceNM()+2000 {
		t.Errorf("Cape route %.0f nm is not clearly longer than Suez %.0f nm", round.DistanceNM(), via.DistanceNM())
	}
}

func TestAvoidUnknownPassage(t *testing.T) {
	if _, err := testGraph(t).Route([]Position{rotterdam, gdynia}, Options{Avoid: []string{"kiell"}}); err == nil {
		t.Error("want error for a misspelt passage")
	}
}

func TestAStarMatchesDijkstra(t *testing.T) {
	pairs := [][2]Position{{rotterdam, gdynia}, {richardsBay, rotterdam}, {rotterdam, septIles}, {rotterdam, singapore}, {bilbao, gdynia}}
	for _, p := range pairs {
		a := mustRoute(t, Options{Algorithm: AStar}, p[0], p[1])
		d := mustRoute(t, Options{Algorithm: Dijkstra}, p[0], p[1])
		if math.Abs(a.DistanceM-d.DistanceM) > 1e-6 {
			t.Errorf("%v to %v: A* %.3f m, Dijkstra %.3f m", p[0], p[1], a.DistanceM, d.DistanceM)
		}
	}
}

func TestAntimeridianJoined(t *testing.T) {
	// Yokohama to Seattle: the short way crosses 180°.
	r := mustRoute(t, Options{}, Position{139.64, 35.44}, Position{-122.34, 47.6})
	t.Logf("Yokohama to Seattle: %.0f nm", r.DistanceNM())
	if r.DistanceNM() > 6000 {
		t.Errorf("%.0f nm: route did not cross the antimeridian", r.DistanceNM())
	}
}

func TestViaPointDoesNotDoubleBack(t *testing.T) {
	r := mustRoute(t, Options{}, bilbao, greatBelt, gdynia)
	for i := 2; i < len(r.Path); i++ {
		if r.Path[i] == r.Path[i-2] {
			t.Fatalf("path doubles back at %v", r.Path[i-1])
		}
	}
}

func TestRouteEndpointsAndOrder(t *testing.T) {
	r := mustRoute(t, Options{}, bilbao, skaw, greatBelt, gdynia)
	if r.Path[0] != bilbao || r.Path[len(r.Path)-1] != gdynia {
		t.Errorf("path runs %v to %v, want Bilbao to Gdynia", r.Path[0], r.Path[len(r.Path)-1])
	}
	if len(r.Legs) != 3 {
		t.Fatalf("got %d legs, want 3", len(r.Legs))
	}
	var sum float64
	for _, l := range r.Legs {
		sum += l.DistanceM
	}
	if math.Abs(sum-r.DistanceM) > 1e-6 {
		t.Errorf("legs sum to %.0f m, route says %.0f m", sum, r.DistanceM)
	}
}

func TestRouteNeedsTwoPoints(t *testing.T) {
	if _, err := testGraph(t).Route([]Position{bilbao}, Options{}); err == nil {
		t.Error("want error for a single point")
	}
}

func benchRoute(b *testing.B, alg Algorithm, from, to Position) {
	g := testGraph(b)
	b.ResetTimer()
	for range b.N {
		if _, err := g.Route([]Position{from, to}, Options{Algorithm: alg}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAStarShort(b *testing.B)    { benchRoute(b, AStar, rotterdam, gdynia) }
func BenchmarkDijkstraShort(b *testing.B) { benchRoute(b, Dijkstra, rotterdam, gdynia) }
func BenchmarkAStarLong(b *testing.B)     { benchRoute(b, AStar, rotterdam, singapore) }
func BenchmarkDijkstraLong(b *testing.B)  { benchRoute(b, Dijkstra, rotterdam, singapore) }
