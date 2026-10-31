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
	// Severity is "aground" (within the land data tolerance of land) or
	// "close" (clear of land, nearer than the clearance).
	Severity string `json:"severity"`
	// MinClearanceNM is the smallest clearance beyond the land tolerance,
	// rounded to 0.1; 0 when aground.
	MinClearanceNM float64 `json:"min_clearance_nm"`
	Kind           string  `json:"kind"` // provenance: marnet, osm-tss, connector, smoothed, user-accepted
	Passage        string  `json:"passage,omitempty"`
	Note           string  `json:"note,omitempty"`
	// Fixes lists the pending proposals that replace part of the run.
	Fixes []string `json:"fixes"`
}

const (
	notePassage   = "Passage narrower than the land data resolves; follow the canal or fairway and pilotage."
	noteConnector = "Port approach: connects the port to the network."
)

// hazards checks every segment of path against land and merges adjacent runs
// with the same severity, kind and passage. Proposals not in opts.Accept that
// overlap a run are listed as its fixes.
func (g *Graph) hazards(legs []Leg, path []Position, props []Proposal, opts Options) []Hazard {
	out := []Hazard{}
	if g.land == nil {
		return out
	}
	c := opts.ClearanceNM
	if c <= 0 {
		c = defaultClearanceNM
	}
	tol := g.land.Tolerance
	limit := c*MetresPerNM + tol
	accepted := map[string]bool{}
	for _, id := range opts.Accept {
		accepted[id] = true
	}

	k := 0 // index into path segments
	for _, l := range legs {
		for _, code := range l.segs {
			a, b := path[k], path[k+1]
			d := g.land.SegmentClearance(a, b, limit)
			if d >= limit {
				k++
				continue
			}
			sev, clear := "close", d-tol
			if d <= tol {
				sev, clear = "aground", 0
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
		case h.Passage != "":
			h.Note = notePassage
		case h.Kind == "connector":
			h.Note = noteConnector
		}
		h.Fixes = []string{}
		for _, p := range props {
			if !accepted[p.ID] && p.from < h.To && p.to > h.From {
				h.Fixes = append(h.Fixes, p.ID)
			}
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
	case code == segAccepted:
		return "user-accepted"
	case code == segSmoothed:
		return "smoothed"
	case g.flags[code]&(edgeLane|edgeJoin) != 0:
		return "osm-tss"
	}
	return "marnet"
}
