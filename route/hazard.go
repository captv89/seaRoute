package route

import "math"

// Hazard is a run of Route.Path segments that touch land (aground) or pass
// closer than the requested clearance (close). From and To index Route.Path,
// as in Stretch. Every segment of the final path is checked, whatever its
// provenance, so a path edited later can be re-checked the same way.
type Hazard struct {
	From  int      `json:"from"`
	To    int      `json:"to"`
	Start Position `json:"start"` // Path[From]
	End   Position `json:"end"`   // Path[To]
	// Severity is "aground" (within the land data tolerance of land),
	// "shallow" (less water than the vessel needs, outside a port approach),
	// "close" (clear of land, nearer than the clearance), "approach" (any of
	// those inside a port approach) or "unchecked" (a connector that leaves
	// the area the land data covers, so its clearance is unknown).
	Severity string `json:"severity"`
	// MinClearanceNM is the smallest clearance beyond the land tolerance,
	// rounded to 0.1; 0 when aground.
	MinClearanceNM float64 `json:"min_clearance_nm"`
	Kind           string  `json:"kind"` // provenance: marnet, osm-tss, connector
	Passage        string  `json:"passage,omitempty"`
	Note           string  `json:"note,omitempty"`
}

const (
	notePassage   = "Passage narrower than the land data resolves; follow the canal or fairway and pilotage."
	noteConnector = "Port approach: connects the port to the network."
	noteApproach  = "Port approach: check chart and pilotage."
	noteUnchecked = "Land data does not cover this approach; check the chart."
)

// hazards checks every segment of path against land and depth and merges
// adjacent runs with the same severity, kind and passage. c supplies the depth
// limits (limitM 0 skips the depth check) and the requested points, around
// which findings become "approach"; it may be nil.
func (g *Graph) hazards(legs []Leg, path []Position, opts Options, c *pathCtx) []Hazard {
	out := []Hazard{}
	if g.land == nil && g.depth == nil {
		return out
	}
	if c == nil {
		c = &pathCtx{}
	}
	clr := opts.ClearanceNM
	if clr <= 0 {
		clr = DefaultClearanceNM
	}
	var tol float64
	if g.land != nil {
		tol = g.land.Tolerance
	}
	limit := clr*MetresPerNM + tol
	cc := *c
	cc.confined = g.confinedSamples(legs, path)
	inApproach := g.approachSegs(path, &cc, limit)

	k := 0 // index into path segments
	for _, l := range legs {
		for _, code := range l.segs {
			a, b := path[k], path[k+1]
			d := math.Inf(1)
			if g.land != nil {
				d = g.land.SegmentClearance(a, b, limit)
			}
			kind, pass := g.segKind(code), ""
			if code >= 0 {
				pass = g.passes[g.pass[code]]
			}
			var sev string
			clear := d - tol
			switch {
			case d <= tol:
				sev, clear = "aground", 0
			case code == segConnector && !g.inCorridor(a, b, limit):
				// Clear only of the land the dataset holds.
				sev, clear = "unchecked", 0
			case g.tooShallow(a, b, code, pass, &cc):
				sev = "shallow"
			case d < limit:
				sev = "close"
			default:
				k++
				continue
			}
			if sev != "unchecked" && inApproach[k] {
				sev = "approach"
			}
			if n := len(out); n > 0 {
				if h := &out[n-1]; h.To == k && h.Severity == sev && h.Kind == kind && h.Passage == pass {
					h.To, h.End = k+1, b
					h.MinClearanceNM = math.Min(h.MinClearanceNM, clear)
					k++
					continue
				}
			}
			out = append(out, Hazard{From: k, To: k + 1, Start: a, End: b, Severity: sev, MinClearanceNM: clear, Kind: kind, Passage: pass})
			k++
		}
	}

	for i := range out {
		h := &out[i]
		h.MinClearanceNM = math.Round(h.MinClearanceNM/MetresPerNM*10) / 10
		switch {
		case h.Severity == "unchecked":
			h.Note = noteUnchecked
		case h.Passage != "":
			h.Note = notePassage
		case h.Severity == "approach":
			h.Note = noteApproach
		case h.Kind == "connector":
			h.Note = noteConnector
		}
	}
	return out
}

// confinedSamples samples the confined passages the path uses, for nearConfined.
func (g *Graph) confinedSamples(legs []Leg, path []Position) []Position {
	var out []Position
	k := 0
	for _, l := range legs {
		for _, code := range l.segs {
			if code >= 0 && PassageLimits[g.passes[g.pass[code]]].Confined {
				out = append(out, rhumbSamples(path[k], path[k+1], tautConfinedNM/2)...)
			}
			k++
		}
	}
	return out
}

// depthLimit is the depth the vessel needs along a-b: the confined limit in or
// near a confined passage, the open-water limit elsewhere.
func (c *pathCtx) depthLimit(a, b Position) float64 {
	if c.nearConfined(a, b) {
		return c.limitConfinedM
	}
	return c.limitM
}

// tooShallow reports whether the stored depth along a-b is below the vessel's
// limit. A passage with a fixed draught limit is governed by that table, not
// by the grid, as in the routing.
func (g *Graph) tooShallow(a, b Position, code int32, pass string, c *pathCtx) bool {
	if g.depth == nil || c.limitM <= 0 {
		return false
	}
	if code >= 0 && PassageLimits[pass].MaxDraughtM > 0 {
		return false
	}
	lim := c.depthLimit(a, b)
	d, known := g.segmentDepth(a, b, g.depthRun(code), lim)
	return known && d < lim && g.approachOf(a, b) == ""
}

// approachSegs marks the path segments inside a port approach: from each
// requested point along the path (both ways) until the first vertex that
// passes the full point check, capped at endpointExemptNM of track.
func (g *Graph) approachSegs(path []Position, c *pathCtx, limit float64) []bool {
	in := make([]bool, max(len(path)-1, 0))
	if len(path) < 2 || len(c.ends) == 0 {
		return in
	}
	ok := func(p Position) bool {
		if g.land != nil && g.land.PointDistance(p, limit) < limit {
			return false
		}
		if g.depth != nil && c.limitM > 0 {
			lim := c.limitM
			if c.nearConfined(p, p) {
				lim = c.limitConfinedM
			}
			// The raw reading, not segmentDepth: a land reading or a shallow
			// cell near land is not safe water, so the approach goes on.
			if d, known := g.depth.At(p); known && d < lim {
				return false
			}
		}
		return true
	}
	capM := endpointExemptNM * MetresPerNM
	walk := func(from, step int) {
		cum := 0.0
		for i := from; i+step >= 0 && i+step < len(path); i += step {
			if ok(path[i]) {
				return
			}
			cum += Distance(path[i], path[i+step])
			if cum > capM {
				return
			}
			in[min(i, i+step)] = true
		}
	}
	for n, e := range c.ends {
		idx := 0
		switch {
		case n == len(c.ends)-1:
			idx = len(path) - 1
		case n > 0:
			best := math.Inf(1)
			for i, p := range path {
				if d := Distance(p, e); d < best {
					best, idx = d, i
				}
			}
		}
		walk(idx, 1)
		walk(idx, -1)
	}
	return in
}

// segKind names where a path segment came from. Unlike segFlag it ignores TSS
// coverage, so one canal or coast run stays one hazard.
func (g *Graph) segKind(code int32) string {
	switch {
	case code == segConnector:
		return "connector"
	case code >= 0 && g.flags[code]&(edgeLane|edgeJoin) != 0:
		return "osm-tss"
	}
	return "marnet"
}
