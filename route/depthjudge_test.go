package route

import (
	"math"
	"testing"
)

// rowTile is a deep tile (0,0) with the cells (i0..i1, row 20) set to v.
func rowTile(v uint8, i0, i1 int) DepthTile {
	m := deepCells()
	for i := i0; i <= i1; i++ {
		m[20*depthCellsPerTile+i] = v
	}
	return DepthTile{CX: 0, CY: 0, M: m}
}

func rowLeg() (Position, Position) { return cellCentre(0, 0, 2, 20), cellCentre(0, 0, 60, 20) }

func TestSegmentDepthRunLength(t *testing.T) {
	a, b := rowLeg()
	for _, tc := range []struct {
		cells, run int
		want       float64
	}{
		{1, runMarnet, 255}, {2, runMarnet, 255}, {3, runMarnet, 7},
		{4, runTSS, 255}, {5, runTSS, 7},
	} {
		g := &Graph{depth: NewDepth([]DepthTile{rowTile(7, 10, 9+tc.cells)}, "t")}
		m, known := g.segmentDepth(a, b, tc.run, 30)
		if !known || m != tc.want {
			t.Errorf("%d shallow cells, run %d: got %v, %v, want %v", tc.cells, tc.run, m, known, tc.want)
		}
	}
}

func TestMinDepthIgnoresIsolatedShoal(t *testing.T) {
	a, b := rowLeg()
	build := func(cells int) float32 {
		g := newGraph([]Segment{{A: a, B: b}}, nil, nil, NewDepth([]DepthTile{rowTile(7, 10, 9+cells)}, "t"), "t")
		return g.minDepth[0]
	}
	if m := build(2); m != 255 {
		t.Errorf("2-cell shoal: minDepth %v, want 255", m)
	}
	if m := build(3); m != 7 {
		t.Errorf("3-cell shoal: minDepth %v, want 7", m)
	}
}

func TestSegmentDepthZeroCellsFarFromLandAreUnknown(t *testing.T) {
	a, b := rowLeg()
	d := NewDepth([]DepthTile{rowTile(0, 10, 20)}, "t")
	far := cellCentre(0, 0, 30, 100) // a coast about 0.4 degrees north, well over 0.5 NM away
	ring := []Position{far, {far[0] + 0.01, far[1]}, {far[0] + 0.01, far[1] + 0.01}, {far[0], far[1] + 0.01}, far}
	g := &Graph{depth: d, land: NewLand([][]Position{ring}, 50, "t")}
	if m, known := g.segmentDepth(a, b, runMarnet, 30); !known || m != 255 {
		t.Errorf("zero cells far from land: got %v, %v, want unknown (255)", m, known)
	}
	if m, _ := (&Graph{depth: d}).segmentDepth(a, b, runMarnet, 30); m != 0 {
		t.Errorf("without land data zero cells stay 0, got %v", m)
	}
}

func TestSegmentDepthNearLandNotJudged(t *testing.T) {
	a, b := rowLeg()
	d := NewDepth([]DepthTile{rowTile(7, 10, 20)}, "t")
	w, e := cellCentre(0, 0, 6, 21), cellCentre(0, 0, 26, 21) // a coast 0.25 NM north of the shoal
	ring := []Position{w, e, {e[0], e[1] + 0.01}, {w[0], w[1] + 0.01}, w}
	near := &Graph{depth: d, land: NewLand([][]Position{ring}, 50, "t")}
	if m, _ := near.segmentDepth(a, b, runMarnet, 30); m != 255 {
		t.Errorf("shallow cells near land: got %v, want 255 (land clearance judges them)", m)
	}
	ring = []Position{{1.5, 1.5}, {1.51, 1.5}, {1.51, 1.51}, {1.5, 1.51}, {1.5, 1.5}}
	far := &Graph{depth: d, land: NewLand([][]Position{ring}, 50, "t")}
	if m, _ := far.segmentDepth(a, b, runMarnet, 30); m != 7 {
		t.Errorf("shallow cells far from land: got %v, want 7", m)
	}
}

func TestPassageApproachNotDepthClosed(t *testing.T) {
	// A suez edge, an edge next to it and an edge 80 NM away, all in 10 m water.
	segs := []Segment{
		{A: Position{0, 0.25}, B: Position{0.2, 0.25}, Pass: "suez"},
		{A: Position{0.2, 0.25}, B: Position{0.4, 0.25}},
		{A: Position{1.5, 0.25}, B: Position{1.9, 0.25}},
	}
	g := newGraph(segs, nil, nil, NewDepth(depthTiles([2]int32{0, 0}, [2]int32{3, 0}), "t"), "t")
	for e := range g.to {
		a, b := g.nodes[g.to[g.twin[e]]], g.nodes[g.to[e]]
		got := float64(g.minDepth[e])
		switch {
		case a[0] < 0.5 && b[0] < 0.5:
			if !math.IsInf(got, 1) {
				t.Errorf("edge %v-%v within the approach: minDepth %v, want +Inf", a, b, got)
			}
		case a[0] > 1 && b[0] > 1:
			if got != 10 {
				t.Errorf("distant edge %v-%v: minDepth %v, want 10", a, b, got)
			}
		}
	}
}
