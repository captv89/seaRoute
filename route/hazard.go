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
	// "close" (clear of land, nearer than the clearance) or "unchecked" (a
	// connector that leaves the area the land data covers, so its clearance
	// is unknown).
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
	noteUnchecked = "Land data does not cover this approach; check the chart."
)

// hazards checks every segment of path against land and merges adjacent runs
// with the same severity, kind and passage.
func (g *Graph) hazards(legs []Leg, path []Position, opts Options) []Hazard {
	out := []Hazard{}
	if g.land == nil {
		return out
	}
	c := opts.ClearanceNM
	if c <= 0 {
		c = DefaultClearanceNM
	}
	tol := g.land.Tolerance
	limit := c*MetresPerNM + tol

	k := 0 // index into path segments
	for _, l := range legs {
		for _, code := range l.segs {
			a, b := path[k], path[k+1]
			d := g.land.SegmentClearance(a, b, limit)
			var sev string
			clear := d - tol
			switch {
			case d <= tol:
				sev, clear = "aground", 0
			case code == segConnector && !g.inCorridor(a, b, limit):
				// Clear only of the land the dataset holds.
				sev, clear = "unchecked", 0
			case d < limit:
				sev = "close"
			default:
				k++
				continue
			}
			kind, pass := g.segKind(code), ""
			if code >= 0 {
				pass = g.passes[g.pass[code]]
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
		case h.Kind == "connector":
			h.Note = noteConnector
		}
	}
	return out
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
