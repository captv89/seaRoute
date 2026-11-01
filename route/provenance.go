package route

import "math"

// Leg.segs codes below zero; zero and above are network edge indices.
const (
	segConnector int32 = -1
	// segTaut marks a segment of the taut safe-water path (taut.go). It reads as
	// provenance "marnet"; TSS-area checks still apply as for a MARNET edge.
	segTaut int32 = -2
)

// Stretch labels path[From..To] of Route.Path with where it came from.
type Stretch struct {
	From     int    `json:"from"`
	To       int    `json:"to"`
	Flag     string `json:"flag"`
	Severity string `json:"severity"`
	Scheme   string `json:"scheme,omitempty"`
}

// unknownAt reports whether p is within 20 NM of land and outside every OSM
// TSS coverage area: a place where the TSS rules are not known.
func (g *Graph) unknownAt(p Position) bool {
	if g.land == nil || (g.tss != nil && g.tss.InCoverage(p)) {
		return false
	}
	const r = TSSCoverageNM * MetresPerNM
	return g.land.Within(p, r)
}

// measure recomputes the leg distances from its path.
func (l *Leg) measure() {
	l.DistanceM, l.NetworkM = 0, 0
	for k, code := range l.segs {
		d := Distance(l.Path[k], l.Path[k+1])
		l.DistanceM += d
		if code != segConnector {
			l.NetworkM += d
		}
	}
}

// segFlags returns the flag and scheme of every segment of the concatenated
// route path (see segFlag).
func (g *Graph) segFlags(legs []Leg) (flags, schemes []string) {
	unknown := map[Position]bool{}
	for _, l := range legs {
		for k, code := range l.segs {
			f, sc := g.segFlag(code, l.Path[k], l.Path[k+1], l.lifted, unknown)
			flags, schemes = append(flags, f), append(schemes, sc)
		}
	}
	return flags, schemes
}

// stretches labels the segments of the concatenated route path. Flag
// precedence per segment: connector, tss-unresolved,
// osm-tss, tss-unknown, marnet. Adjacent segments with the same flag and
// scheme merge.
func stretches(flags, schemes []string) []Stretch {
	out := []Stretch{}
	for from, flag := range flags {
		scheme := schemes[from]
		if n := len(out); n > 0 && out[n-1].Flag == flag && out[n-1].Scheme == scheme && out[n-1].To == from {
			out[n-1].To = from + 1
			continue
		}
		sev := "label"
		switch flag {
		case "tss-unresolved":
			sev = "warning"
		case "tss-unknown":
			sev = "info"
		}
		out = append(out, Stretch{From: from, To: from + 1, Flag: flag, Severity: sev, Scheme: scheme})
	}
	return out
}

func (g *Graph) segFlag(code int32, a, b Position, lifted []bool, unknown map[Position]bool) (flag, scheme string) {
	switch code {
	case segConnector:
		return "connector", ""
	}
	mid := segMid(a, b)
	area, inArea := -1, false
	if g.tss != nil {
		var idx int
		if idx, inArea = g.tss.InArea(mid); inArea {
			area = idx
		}
	}
	name := func(k int) string { return g.tss.Schemes[k].ID }
	var f uint8
	k := -1
	if code >= 0 {
		f, k = g.flags[code], int(g.scheme[code])
	}
	isLifted := func(k int) bool { return k >= 0 && lifted != nil && lifted[k] }
	switch {
	case f&edgeTSSBlocked != 0 && isLifted(k):
		return "tss-unresolved", name(k)
	case inArea && isLifted(area):
		return "tss-unresolved", name(area)
	case f&(edgeLane|edgeJoin) != 0 && k >= 0:
		return "osm-tss", name(k)
	case f&(edgeLane|edgeJoin) != 0:
		return "osm-tss", ""
	case inArea:
		return "osm-tss", name(area)
	}
	for _, p := range [3]Position{a, mid, b} {
		u, ok := unknown[p]
		if !ok {
			u = g.unknownAt(p)
			unknown[p] = u
		}
		if u {
			return "tss-unknown", ""
		}
	}
	return "marnet", ""
}

// segMid is the midpoint of the rhumb leg a-b by longitude and latitude,
// taking the shorter way round the antimeridian.
func segMid(a, b Position) Position {
	lon := (a[0] + b[0]) / 2
	if math.Abs(b[0]-a[0]) > 180 {
		lon += 180
		if lon > 180 {
			lon -= 360
		}
	}
	return Position{lon, (a[1] + b[1]) / 2}
}
