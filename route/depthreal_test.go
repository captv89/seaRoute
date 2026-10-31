package route

import (
	"strings"
	"testing"
)

func TestRealRotterdamHamburgDefaultDraught(t *testing.T) {
	g := testDepthGraph(t)
	r := routeOn(t, g, Options{}, Position{4.05, 51.95}, Position{9.97, 53.54})
	for _, n := range r.Notices {
		if n.Kind == "draught-no-route" {
			t.Errorf("Rotterdam-Hamburg at the default draught: %+v", n)
		}
	}
}

func TestRealTorresStraitClosesAt12m(t *testing.T) {
	g := testDepthGraph(t)
	r := routeOn(t, g, Options{Vessel: Vessel{DraughtM: 12, SpeedKn: 14, Type: Bulk}}, Position{145.78, -16.92}, Position{130.84, -12.46})
	if inBox(r.Path, 141.0, 143.5, -10.9, -9.8) {
		t.Error("12 m vessel passes Torres Strait")
	}
	found := false
	for _, n := range reroutes(r) {
		found = found || n.ExtraNM > 500
	}
	if !found {
		t.Errorf("no Torres reroute notice: %+v", r.Notices)
	}
}

func TestRealGreatBeltClosesAt20m(t *testing.T) {
	g := testDepthGraph(t)
	r := routeOn(t, g, Options{Vessel: Vessel{DraughtM: 20, SpeedKn: 14, Type: Bulk}}, Position{11.97, 57.70}, Position{18.55, 54.52})
	for _, n := range r.Notices {
		if n.Kind == "draught-no-route" && strings.Contains(n.Text, "no route deep enough") {
			return
		}
	}
	t.Errorf("20 m Gothenburg-Gdynia has no no-route notice: %+v", r.Notices)
}
