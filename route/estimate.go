package route

import (
	"container/heap"
	"math"
)

// numLandmarks is the number of landmark nodes for the A* estimate.
const numLandmarks = 16

type vec3 [3]float64

func toUnit(p Position) vec3 {
	const rad = math.Pi / 180
	lon, lat := p[0]*rad, p[1]*rad
	return vec3{math.Cos(lat) * math.Cos(lon), math.Cos(lat) * math.Sin(lon), math.Sin(lat)}
}

// chord2 is the squared straight-line distance between two unit vectors.
func (a vec3) chord2(b vec3) float64 {
	dx, dy, dz := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return dx*dx + dy*dy + dz*dz
}

// landmarks hold network distances from a few spread-out nodes to every
// node ("ALT": A*, landmarks, triangle inequality). For any landmark L,
// |d(L,t) - d(L,v)| <= d(v,t), so the largest such gap is a lower bound on
// the remaining distance. Sea routes bend round continents, so this bound
// is much tighter than the straight great-circle line.
//
// Distances are computed with every passage open. Avoiding passages only
// removes edges, which can only make routes longer, so the bound still holds.
type landmarks struct {
	dist [][]float64 // dist[i][v]: network distance from landmark i to node v
}

// buildLandmarks picks landmarks by farthest-point selection inside the
// largest component: each new landmark is the node farthest from those chosen.
func (g *Graph) buildLandmarks(k int) landmarks {
	var lm landmarks
	start := int32(-1)
	for v, ok := range g.usable {
		if ok {
			start = int32(v)
			break
		}
	}
	if start < 0 {
		return lm
	}
	minDist := g.allDistances(start)
	for range k {
		next, far := int32(-1), -1.0
		for v, d := range minDist {
			if g.usable[v] && !math.IsInf(d, 1) && d > far {
				next, far = int32(v), d
			}
		}
		if next < 0 {
			break
		}
		d := g.allDistances(next)
		lm.dist = append(lm.dist, d)
		for v := range minDist {
			minDist[v] = min(minDist[v], d[v])
		}
	}
	return lm
}

// allDistances runs Dijkstra from s over the whole network, all passages open.
func (g *Graph) allDistances(s int32) []float64 {
	dist := make([]float64, len(g.nodes))
	for i := range dist {
		dist[i] = math.Inf(1)
	}
	dist[s] = 0
	q := &queue{{node: s}}
	for q.Len() > 0 {
		it := heap.Pop(q).(queueItem)
		v := it.node
		if it.g > dist[v] {
			continue
		}
		for e := g.first[v]; e < g.first[v+1]; e++ {
			u := g.to[e]
			if d := dist[v] + g.weight[e]; d < dist[u] {
				dist[u] = d
				heap.Push(q, queueItem{node: u, g: d, f: d})
			}
		}
	}
	return dist
}

// estimate returns a lower bound on the network distance from v to t:
// the best of the straight-line chord and the landmark bounds.
// R x chord never exceeds the arc, so the chord bound needs no trig.
func (g *Graph) estimate(v, t int32) float64 {
	h := earthRadiusM * math.Sqrt(g.unit[v].chord2(g.unit[t]))
	for _, d := range g.lm.dist {
		if gap := math.Abs(d[t] - d[v]); gap > h {
			h = gap
		}
	}
	return h
}
