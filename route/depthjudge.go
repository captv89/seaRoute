package route

import "math"

const (
	// nearLandNM is the distance from OSM land inside which depth is not
	// judged: land clearance covers it, and the raster is unreliable there.
	nearLandNM = 0.5
	// passageApproachNM is the radius around the nodes of a passage with a
	// fixed draught limit inside which depth is not judged: the table governs.
	passageApproachNM = 25.0
	// Shallow-run lengths in 15 arc-second cells: an edge or leg is judged
	// shallow only when this many consecutive judged cells are below the limit.
	runMarnet = 3 // MARNET edges, connectors and taut legs
	runTSS    = 5 // lane and join edges
)

// depthRun is the shallow-run length that judges the edge or segment code.
func (g *Graph) depthRun(code int32) int {
	if code >= 0 && g.flags[code]&(edgeLane|edgeJoin) != 0 {
		return runTSS
	}
	return runMarnet
}

// segmentDepth is the depth the rhumb leg a-b offers, cleaned against OSM
// land and made robust to isolated cells. The leg is sampled every 200 m and
// reduced to the 15" cells it crosses (consecutive samples in one cell count
// once). A cell reading 0 is unknown (land is judged by OSM, not by the
// raster), and a cell below capM within nearLandNM of OSM land is not judged
// by depth; both count as deep. The result is the smallest value v such that
// some run of `run` consecutive cells is all at most v (a leg of fewer cells
// uses all of them), so the leg is shallow for a limit lim exactly when the
// result is below lim. It is exact below capM and at least capM otherwise.
// known is false if any sample lies outside the stored tiles; such a gap
// breaks a run, m is then the result over the cells that were read (255 when
// none was), and a result below the limit still means shallow.
func (g *Graph) segmentDepth(a, b Position, run int, capM float64) (m float64, known bool) {
	if g.depth.DeepTiles(a, b, capM) {
		return 255, true
	}
	var buf [128]float64
	cells := buf[:0]
	known, any := true, false
	var last *DepthTile
	li, lj := -1, -1
	for _, pc := range splitAntimeridian(a, b) {
		for _, p := range rhumbSamples(pc[0], pc[1], depthSampleM/MetresPerNM) {
			t, i, j := g.depth.tileAt(p)
			if t == nil {
				// An unstored tile is mostly deep ocean (the mask holds it): it
				// breaks a run rather than letting cells on either side join.
				known = false
				if last != nil || li != -2 {
					cells = append(cells, 255)
				}
				last, li, lj = nil, -2, -2
				continue
			}
			any = true
			if t.Deep {
				if t != last {
					last, li, lj = t, -1, -1
					cells = append(cells, 255)
				}
				continue
			}
			if t == last && i == li && j == lj {
				continue
			}
			last, li, lj = t, i, j
			v := t.at(i, j)
			if g.land != nil && v < capM && (v == 0 || g.land.PointDistance(p, nearLandNM*MetresPerNM) < nearLandNM*MetresPerNM) {
				v = 255
			}
			cells = append(cells, v)
		}
	}
	if !any {
		return 255, false // nothing read: no shallow reading, so it passes
	}
	n := min(run, len(cells))
	m = 255
	for s := 0; s+n <= len(cells); s++ {
		w := cells[s]
		for _, c := range cells[s+1 : s+n] {
			w = max(w, c)
		}
		m = min(m, w)
	}
	return m, known
}

// approachNode is a node of a passage with a fixed draught limit.
type approachNode struct {
	pos  Position
	pass uint8
}

// buildApproach indexes the nodes of passages with a fixed draught limit.
func (g *Graph) buildApproach() {
	g.approach = map[int64][]approachNode{}
	seen := map[int32]bool{}
	for v := range g.nodes {
		for e := g.first[v]; e < g.first[v+1]; e++ {
			p := g.pass[e]
			if PassageLimits[g.passes[p]].MaxDraughtM <= 0 {
				continue
			}
			for _, n := range [2]int32{int32(v), g.to[e]} {
				if !seen[n] {
					seen[n] = true
					k := approachKey(g.nodes[n])
					g.approach[k] = append(g.approach[k], approachNode{g.nodes[n], p})
				}
			}
		}
	}
}

func approachKey(p Position) int64 {
	return cellKey(int32(math.Floor(p[0]/depthTileDeg)), int32(math.Floor(p[1]/depthTileDeg)))
}

// approachPassage returns the tag of a fixed-limit passage with a node within
// passageApproachNM of p, or "".
func (g *Graph) approachPassage(p Position) string {
	if len(g.approach) == 0 {
		return ""
	}
	cx, cy := int32(math.Floor(p[0]/depthTileDeg)), int32(math.Floor(p[1]/depthTileDeg))
	dx := int32(min(40, math.Ceil(passageApproachNM/60/math.Max(math.Cos(p[1]*math.Pi/180), 0.05)/depthTileDeg)))
	for x := cx - dx; x <= cx+dx; x++ {
		for y := cy - 1; y <= cy+1; y++ {
			for _, n := range g.approach[cellKey(x, y)] {
				if Distance(p, n.pos) <= passageApproachNM*MetresPerNM {
					return g.passes[n.pass]
				}
			}
		}
	}
	return ""
}

// approachOf is the fixed-limit passage whose approach holds an end of a-b.
func (g *Graph) approachOf(a, b Position) string {
	if s := g.approachPassage(a); s != "" {
		return s
	}
	return g.approachPassage(b)
}
