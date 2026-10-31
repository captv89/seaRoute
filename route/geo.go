package route

import "math"

// segSegDist returns the smallest distance between segments a-b and c-d in the
// plane they are given in (Mercator units), and the point on a-b where it is
// reached. Intersecting segments give 0. Degenerate (point) segments are fine.
func segSegDist(a, b, c, d [2]float64) (dist float64, closest [2]float64) {
	if p, ok := segIntersect(a, b, c, d); ok {
		return 0, p
	}
	dist = math.Inf(1)
	try := func(onAB [2]float64, other [2]float64) {
		if dd := math.Hypot(onAB[0]-other[0], onAB[1]-other[1]); dd < dist {
			dist, closest = dd, onAB
		}
	}
	try(a, closestOnSeg(a, c, d))
	try(b, closestOnSeg(b, c, d))
	try(closestOnSeg(c, a, b), c)
	try(closestOnSeg(d, a, b), d)
	return dist, closest
}

// closestOnSeg returns the point of segment a-b nearest to p.
func closestOnSeg(p, a, b [2]float64) [2]float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return a
	}
	t := ((p[0]-a[0])*dx + (p[1]-a[1])*dy) / l2
	t = math.Max(0, math.Min(1, t))
	return [2]float64{a[0] + t*dx, a[1] + t*dy}
}

// segIntersect reports whether a-b and c-d share a point and returns one.
func segIntersect(a, b, c, d [2]float64) ([2]float64, bool) {
	r := [2]float64{b[0] - a[0], b[1] - a[1]}
	s := [2]float64{d[0] - c[0], d[1] - c[1]}
	den := r[0]*s[1] - r[1]*s[0]
	ca := [2]float64{c[0] - a[0], c[1] - a[1]}
	if den == 0 {
		// Parallel: any overlap shows up as an endpoint lying on the other segment.
		if ca[0]*r[1]-ca[1]*r[0] != 0 {
			return [2]float64{}, false
		}
		for _, p := range [][2]float64{a, b} {
			if closestOnSeg(p, c, d) == p {
				return p, true
			}
		}
		for _, p := range [][2]float64{c, d} {
			if closestOnSeg(p, a, b) == p {
				return p, true
			}
		}
		return [2]float64{}, false
	}
	t := (ca[0]*s[1] - ca[1]*s[0]) / den
	u := (ca[0]*r[1] - ca[1]*r[0]) / den
	if t < 0 || t > 1 || u < 0 || u > 1 {
		return [2]float64{}, false
	}
	return [2]float64{a[0] + t*r[0], a[1] + t*r[1]}, true
}

// invMercatorLat converts a Mercator y (radians) back to latitude in degrees.
func invMercatorLat(y float64) float64 {
	return (2*math.Atan(math.Exp(y)) - math.Pi/2) * 180 / math.Pi
}

// splitAntimeridian splits the rhumb leg a-b at lon ±180 when its shorter
// longitude difference crosses the antimeridian. Otherwise it returns the leg
// whole. Pieces keep the ±180 convention: the piece before the crossing ends at
// the signed meridian it is heading for, the next starts at the opposite sign.
func splitAntimeridian(a, b Position) [][2]Position {
	if math.Abs(b[0]-a[0]) <= 180 {
		return [][2]Position{{a, b}}
	}
	// Unwrap b onto a's side so the leg is continuous.
	bu := b[0]
	edge, other := 180.0, -180.0
	if b[0] < a[0] {
		bu += 360
	} else {
		bu -= 360
		edge, other = -180, 180
	}
	ma, mb := Mercator(a), Mercator(Position{bu, b[1]})
	t := (edge*math.Pi/180 - ma[0]) / (mb[0] - ma[0])
	lat := invMercatorLat(ma[1] + t*(mb[1]-ma[1]))
	return [][2]Position{{a, {edge, lat}}, {{other, lat}, b}}
}
