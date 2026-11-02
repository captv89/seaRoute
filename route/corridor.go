package route

import (
	"math"

	"github.com/captv89/seaRoute/internal/corridor"
)

// The land and depth datasets keep only data within Land.CorridorNM of the
// MARNET network (see internal/corridor: every edge, densified). A shortcut
// judged against land the dataset does not hold could cross an island or a
// coast it cannot see, so the router only judges legs whose every point lies
// well inside that corridor. The network's own geometry is never trusted:
// some MARNET edges cut across land.

// corridorCellDeg is the cell size of the network sample grid, in degrees.
const corridorCellDeg = 0.5

// buildCorridor indexes the points of every MARNET edge, densified exactly as
// the importers densify them (corridor.Densify), by corridorCellDeg cell.
func (g *Graph) buildCorridor(edges []edge) {
	g.corridor = map[int64][]Position{}
	add := func(lon, lat float64) {
		p := Position{wrapLon(lon), lat}
		k := cellKey(int32(math.Floor(p[0]/corridorCellDeg)), int32(math.Floor(p[1]/corridorCellDeg)))
		g.corridor[k] = append(g.corridor[k], p)
	}
	for i := 0; i < len(edges); i += 2 { // edges come in forward/reverse pairs
		e := edges[i]
		a, b := g.nodes[e.from], g.nodes[e.to]
		for _, q := range corridor.Densify(a[0], a[1], b[0], b[1]) {
			add(q[0], q[1])
		}
	}
}

// inCorridor reports whether every sample of the rhumb leg a-b (every
// corridorSampleNM, ends included) lies within CorridorNM - marginM of a MARNET
// vertex. It is true when there is no land data or the data has no corridor.
func (g *Graph) inCorridor(a, b Position, marginM float64) bool {
	if g.land == nil || g.land.CorridorNM == 0 {
		return true
	}
	r := g.land.CorridorNM*MetresPerNM - marginM
	if r <= 0 {
		return false
	}
	for _, p := range rhumbSamples(a, b, corridorSampleNM) {
		if !g.nearNetwork(p, r) {
			return false
		}
	}
	return true
}

// nearNetwork reports whether a sample of the MARNET network lies within rM
// metres of p (less 2%, see below). The samples are the points the importers kept data around, so
// with rM below CorridorNM the data holds the land near p.
func (g *Graph) nearNetwork(p Position, rM float64) bool {
	const degM = earthRadiusM * math.Pi / 180
	ey := rM / degM
	ex := math.Min(360, rM/(degM*math.Max(0.01, math.Cos(math.Min(89, math.Abs(p[1])+ey)*math.Pi/180))))
	cy0, cy1 := int32(math.Floor((p[1]-ey)/corridorCellDeg)), int32(math.Floor((p[1]+ey)/corridorCellDeg))
	cx0, cx1 := int32(math.Floor((p[0]-ex)/corridorCellDeg)), int32(math.Floor((p[0]+ex)/corridorCellDeg))
	cells := int32(math.Round(360 / corridorCellDeg))
	// Flat-earth distance in degrees of latitude, with the longitude scale at
	// p. Within the corridor (tens of NM) its error is under 2%, so the radius
	// is cut by 2% to stay on the safe side.
	cosP := math.Cos(p[1] * math.Pi / 180)
	rr := ey * 0.98 * ey * 0.98
	for cx := cx0; cx <= cx1 && cx-cx0 < cells; cx++ {
		wx := cx
		if lo := int32(math.Floor(-180 / corridorCellDeg)); wx < lo {
			wx += cells
		} else if wx >= lo+cells {
			wx -= cells
		}
		for cy := cy0; cy <= cy1; cy++ {
			for _, q := range g.corridor[cellKey(wx, cy)] {
				dl := math.Abs(q[0] - p[0])
				if dl > 180 {
					dl = 360 - dl
				}
				if x, y := dl*cosP, q[1]-p[1]; x*x+y*y <= rr {
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

// corridorSampleNM is the spacing of the samples along a leg in inCorridor.
const corridorSampleNM = 5.0
