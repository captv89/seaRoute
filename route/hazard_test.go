package route

import (
	"reflect"
	"slices"
	"testing"
)

func TestHazardAground(t *testing.T) {
	g := NewWith(chainSegs(Position{0, 0}, Position{1, 0}), landOf(rect(0.4, -0.1, 0.6, 0.1)), nil)
	r := routeOn(t, g, Options{}, Position{0, 0}, Position{1, 0})
	want := []Hazard{{From: 0, To: 1, Start: Position{0, 0}, End: Position{1, 0}, Severity: "aground", Kind: "marnet", Fixes: []string{}}}
	if !reflect.DeepEqual(r.Hazards, want) {
		t.Errorf("hazards %+v", r.Hazards)
	}
}

func TestHazardClose(t *testing.T) {
	g := NewWith(chainSegs(Position{0, 0}, Position{1, 0}), landOf(rect(0.4, passLat, 0.6, 0.1)), nil)
	r := routeOn(t, g, Options{ClearanceNM: 2}, Position{0, 0}, Position{1, 0})
	if len(r.Hazards) != 1 || r.Hazards[0].Severity != "close" || r.Hazards[0].MinClearanceNM != 1.5 {
		t.Errorf("hazards %+v", r.Hazards)
	}
	if r = routeOn(t, g, Options{ClearanceNM: 1}, Position{0, 0}, Position{1, 0}); len(r.Hazards) != 0 {
		t.Errorf("1 NM clearance: hazards %+v", r.Hazards)
	}
}

func TestHazardsMerge(t *testing.T) {
	g := NewWith(chainSegs(Position{0, 0}, Position{0.5, 0}, Position{1, 0}), landOf(rect(0.2, -0.05, 0.8, 0.05)), nil)
	r := routeOn(t, g, Options{}, Position{0, 0}, Position{1, 0})
	if len(r.Hazards) != 1 || r.Hazards[0].From != 0 || r.Hazards[0].To != 2 {
		t.Errorf("hazards %+v", r.Hazards)
	}
}

func TestHazardsEmptyWithoutLand(t *testing.T) {
	g := NewWith(chainSegs(Position{0, 0}, Position{1, 0}), nil, nil)
	if r := routeOn(t, g, Options{}, Position{0, 0}, Position{1, 0}); r.Hazards == nil || len(r.Hazards) != 0 {
		t.Errorf("hazards %#v", r.Hazards)
	}
}

func TestHazardFixLinksProposal(t *testing.T) {
	// Base path A-B-C crosses an island; the chord A-C passes it at 1.5 NM.
	a, b, c := Position{0, 0}, Position{0.1, 0.05}, Position{0.2, 0}
	g := NewWith(chainSegs(a, b, c), landOf(rect(0.05, passLat, 0.15, 0.1)), coverTSS())
	r := routeOn(t, g, Options{Kinks: true, ClearanceNM: 2}, a, c)
	if len(r.Proposals) != 1 || len(r.Hazards) != 1 {
		t.Fatalf("proposals %+v hazards %+v", r.Proposals, r.Hazards)
	}
	if h := r.Hazards[0]; h.Severity != "aground" || !slices.Equal(h.Fixes, []string{r.Proposals[0].ID}) {
		t.Errorf("hazard %+v", h)
	}
	r = routeOn(t, g, Options{Kinks: true, ClearanceNM: 2, Accept: []string{r.Proposals[0].ID}}, a, c)
	if len(r.Hazards) != 1 || r.Hazards[0].Severity != "close" || r.Hazards[0].Kind != "user-accepted" {
		t.Errorf("after accept: hazards %+v", r.Hazards)
	}
}

func TestHazardIndicesAcrossLegs(t *testing.T) {
	// Two legs via (1, 0); land only on the second leg.
	g := NewWith(chainSegs(Position{0, 0}, Position{1, 0}, Position{2, 0}), landOf(rect(1.4, -0.1, 1.6, 0.1)), nil)
	r := routeOn(t, g, Options{}, Position{0, 0}, Position{1, 0}, Position{2, 0})
	if len(r.Hazards) != 1 || r.Hazards[0].From != 1 || r.Hazards[0].To != 2 || r.Hazards[0].Start != (Position{1, 0}) {
		t.Errorf("hazards %+v path %v", r.Hazards, r.Path)
	}
}

func TestRealHazards(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	g := testTSSGraph(t)
	first := routeOn(t, g, Options{Kinks: true}, rotterdam, singapore)
	var ids []string
	for _, p := range first.Proposals {
		ids = append(ids, p.ID)
	}
	r := routeOn(t, g, Options{Kinks: true, Accept: ids}, rotterdam, singapore)
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
