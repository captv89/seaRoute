package route

import "testing"

// TestJoinsAreFreeLanesPinned: the taut pass may re-route every TSS join (the
// network's way into a lane end, including the pieces of a MARNET edge split
// at an access node), so a ship heads for the lane end directly. Lanes stay
// pinned.
func TestJoinsAreFreeLanesPinned(t *testing.T) {
	segs := []Segment{{A: Position{0, 0}, B: Position{1, 0}}}
	tss := NewTSS([]TSSFeature{{Type: "lane", Scheme: "tss-l", OSMID: 1, Coords: []Position{{0.4, 0.01}, {0.6, 0.01}}}}, "v")
	g := NewWith(segs, nil, tss)
	// edgeFrom finds the usable edge u->v.
	edgeFrom := func(u, v Position) int32 {
		for e := range g.to {
			if g.flags[e]&edgeWrongWay == 0 && g.nodes[g.to[g.twin[e]]] == u && g.nodes[g.to[e]] == v {
				return int32(e)
			}
		}
		t.Fatalf("no edge %v -> %v", u, v)
		return -1
	}
	var p Position // the access node joined to the lane start
	for v := g.marnetNodes; v < len(g.nodes); v++ {
		if q := g.nodes[v]; q[1] == 0 && q[0] < 0.5 {
			p = q
		}
	}
	a, s, e := Position{0, 0}, Position{0.4, 0.01}, Position{0.6, 0.01}
	l := Leg{
		Path: []Position{a, p, s, e},
		segs: []int32{edgeFrom(a, p), edgeFrom(p, s), edgeFrom(s, e)},
	}
	if f := g.flags[l.segs[0]]; f&edgeJoin == 0 {
		t.Fatalf("piece %v -> %v flags %b, want join", a, p, f)
	}
	for k, want := range []bool{false, false, true} {
		if got := g.pinnedSeg(&l, k); got != want {
			t.Errorf("segment %d (%v -> %v) pinned %v, want %v", k, l.Path[k], l.Path[k+1], got, want)
		}
	}
}

// TestRotterdamSingaporeNoPinnedAccessAground: no access piece or access join
// is drawn over land (the Hanish Islands leg south of 14 N used to be).
func TestRotterdamSingaporeNoPinnedAccessAground(t *testing.T) {
	g := testFullGraph(t)
	r := rotterdamSingapore(t)
	tol := g.land.Tolerance
	for _, l := range r.Legs {
		for k, code := range l.segs {
			if code < 0 || g.flags[code]&edgeLane != 0 || g.pass[code] != 0 {
				continue
			}
			if g.flags[code]&edgeJoin == 0 {
				continue
			}
			a, b := l.Path[k], l.Path[k+1]
			if !g.pinnedSeg(&l, k) {
				continue // judged by safeLeg in the taut pass
			}
			if d := g.land.SegmentClearance(a, b, tol+1); d <= tol {
				t.Errorf("pinned join or access piece %v -> %v (%.1f NM) touches land", a, b, Distance(a, b)/MetresPerNM)
			}
		}
	}
	for _, h := range r.Hazards {
		if h.Severity == "aground" && h.Start[1] > 11 && h.Start[1] < 16 && h.Start[0] > 40 && h.Start[0] < 45 {
			t.Errorf("aground in the southern Red Sea: %+v", h)
		}
	}
}
