package route

import "math"

// The land dataset keeps only coastline within Land.CorridorNM of a MARNET
// vertex (see cmd/land-import). A shortcut judged against land the dataset
// does not hold could cross an island or a coast it cannot see, so smoothing
// only judges shortcuts whose every point lies well inside that corridor.

// corridorCellDeg is the cell size of the MARNET vertex grid, in degrees.
const corridorCellDeg = 1.0

// buildCorridor indexes the MARNET vertices by corridorCellDeg cell.
func (g *Graph) buildCorridor() {
	g.corridor = map[int64][]int32{}
	for v := range g.marnetNodes {
		p := g.nodes[v]
		k := cellKey(int32(math.Floor(p[0]/corridorCellDeg)), int32(math.Floor(p[1]/corridorCellDeg)))
		g.corridor[k] = append(g.corridor[k], int32(v))
	}
}

// inCorridor reports whether every sample of the rhumb leg a-b (every
// smoothSampleNM, ends included) lies within CorridorNM - marginM of a MARNET
// vertex. It is true when there is no land data or the data has no corridor.
func (g *Graph) inCorridor(a, b Position, marginM float64) bool {
	if g.land == nil || g.land.CorridorNM == 0 {
		return true
	}
	r := g.land.CorridorNM*MetresPerNM - marginM
	if r <= 0 {
		return false
	}
	for _, p := range rhumbSamples(a, b, smoothSampleNM) {
		if !g.nearVertex(p, r) {
			return false
		}
	}
	return true
}

// nearVertex reports whether a MARNET vertex lies within rM metres of p.
func (g *Graph) nearVertex(p Position, rM float64) bool {
	const degM = earthRadiusM * math.Pi / 180
	ey := rM / degM
	ex := math.Min(360, rM/(degM*math.Max(0.01, math.Cos(math.Min(89, math.Abs(p[1])+ey)*math.Pi/180))))
	cy0, cy1 := int32(math.Floor((p[1]-ey)/corridorCellDeg)), int32(math.Floor((p[1]+ey)/corridorCellDeg))
	cx0, cx1 := int32(math.Floor((p[0]-ex)/corridorCellDeg)), int32(math.Floor((p[0]+ex)/corridorCellDeg))
	cells := int32(math.Round(360 / corridorCellDeg))
	for cx := cx0; cx <= cx1 && cx-cx0 < cells; cx++ {
		wx := cx
		if lo := int32(math.Floor(-180 / corridorCellDeg)); wx < lo {
			wx += cells
		} else if wx >= lo+cells {
			wx -= cells
		}
		for cy := cy0; cy <= cy1; cy++ {
			for _, v := range g.corridor[cellKey(wx, cy)] {
				if Distance(p, g.nodes[v]) <= rM {
					return true
				}
			}
		}
	}
	return false
}

// rhumbSamples returns points along the rhumb leg a-b, at most stepNM apart,
// including both ends.
func rhumbSamples(a, b Position, stepNM float64) []Position {
	ma, mb := Mercator(a), Mercator(b)
	n := max(1, int(math.Ceil(Distance(a, b)/(stepNM*MetresPerNM))))
	out := make([]Position, 0, n+1)
	out = append(out, a)
	for k := 1; k < n; k++ {
		t := float64(k) / float64(n)
		out = append(out, Position{
			(ma[0] + t*(mb[0]-ma[0])) * 180 / math.Pi,
			invMercatorLat(ma[1] + t*(mb[1]-ma[1])),
		})
	}
	return append(out, b)
}
