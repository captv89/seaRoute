package route

import (
	"slices"
	"sync"
	"testing"
)

// Port positions from dataset/port-list.sqlite.
var (
	bilbao      = Position{-3.0469894370947661, 43.339978415980845}
	gdynia      = Position{18.546982540197764, 54.538336522499797}
	rotterdam   = Position{4.4424468168432654, 51.904383352111097}
	richardsBay = Position{32.083520507812, -28.771113820687}
	septIles    = Position{-66.36136067705, 50.202675418655}
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

func testGraph(t *testing.T) *Graph {
	t.Helper()
	graphOnce.Do(func() { graph, graphErr = Load("../dataset/marnet_densified_v2.geojson") })
	if graphErr != nil {
		t.Fatal(graphErr)
	}
	return graph
}

// Boxes around passages the scene routes must avoid, as [minLon, minLat, maxLon, maxLat].
var (
	kielCanal = [4]float64{9.1, 53.85, 10.2, 54.4}   // too shallow for a laden Handysize
	theSound  = [4]float64{12.4, 55.3, 13.0, 56.1}   // Drogden draught limit
	belleIsle = [4]float64{-58.0, 51.0, -55.0, 52.5} // ice-bound in winter
)

func passes(r Route, box [4]float64) bool {
	return slices.ContainsFunc(r.Path, func(p Position) bool {
		return p[0] > box[0] && p[1] > box[1] && p[0] < box[2] && p[1] < box[3]
	})
}

func TestRouteDistances(t *testing.T) {
	g := testGraph(t)
	// Wide bounds: these catch unit and direction bugs only. MARNET is coarse,
	// so totals are not checked against published distance tables here.
	cases := []struct {
		name   string
		points []Position
		minNM  float64
		maxNM  float64
	}{
		{"Richards Bay to Rotterdam", []Position{richardsBay, rotterdam}, 5500, 8000},
		{"Rotterdam to Sept-Iles via Cabot Strait", []Position{rotterdam, cabot, septIles}, 2500, 3600},
		{"Bilbao to Gdynia via Skaw and Great Belt", []Position{bilbao, skaw, greatBelt, gdynia}, 1300, 2000},
	}
	for _, c := range cases {
		r, err := g.Route(c.points...)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		nm := r.DistanceNM()
		t.Logf("%s: %.0f nm, %d points", c.name, nm, len(r.Path))
		if nm < c.minNM || nm > c.maxNM {
			t.Errorf("%s: %.0f nm, want %.0f-%.0f", c.name, nm, c.minNM, c.maxNM)
		}
	}
}

func TestViaPointsAvoidPassages(t *testing.T) {
	g := testGraph(t)
	ivelle, err := g.Route(bilbao, skaw, greatBelt, gdynia)
	if err != nil {
		t.Fatal(err)
	}
	if passes(ivelle, kielCanal) {
		t.Error("Bilbao to Gdynia passes the Kiel Canal")
	}
	if passes(ivelle, theSound) {
		t.Error("Bilbao to Gdynia passes the Sound")
	}

	tormar, err := g.Route(rotterdam, cabot, septIles)
	if err != nil {
		t.Fatal(err)
	}
	if passes(tormar, belleIsle) {
		t.Error("Rotterdam to Sept-Iles passes the Strait of Belle Isle")
	}
}

func TestViaPointDoesNotDoubleBack(t *testing.T) {
	r, err := testGraph(t).Route(bilbao, greatBelt, gdynia)
	if err != nil {
		t.Fatal(err)
	}
	for i := 2; i < len(r.Path); i++ {
		if r.Path[i] == r.Path[i-2] {
			t.Fatalf("path doubles back at %v", r.Path[i-1])
		}
	}
}

func TestRouteEndpointsAndOrder(t *testing.T) {
	g := testGraph(t)
	r, err := g.Route(bilbao, skaw, greatBelt, gdynia)
	if err != nil {
		t.Fatal(err)
	}
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
	if sum != r.DistanceM {
		t.Errorf("legs sum to %.0f m, route says %.0f m", sum, r.DistanceM)
	}
}

func TestRouteNeedsTwoPoints(t *testing.T) {
	if _, err := testGraph(t).Route(bilbao); err == nil {
		t.Error("want error for a single point")
	}
}
