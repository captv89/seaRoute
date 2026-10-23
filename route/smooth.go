package route

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
)

// Kink removal works per leg on the network part of the path. Connector
// segments (requested point to network and back) and edges that follow a
// passage, a TSS lane or join, or cross the antimeridian are never shortcut.
//
// Pipeline per leg: auto shortcuts first (clear of land by the clearance, and
// either inside OSM TSS coverage or in open sea), then proposals on the result
// (shortcuts that fail one of those two), then the proposals whose ID the
// caller accepted are applied. Smoothing is not run again after an accept.

const (
	smoothMaxSpan      = 40  // longest shortcut, in path vertices
	smoothSampleNM     = 5.0 // spacing of the open-sea samples along a shortcut
	smoothMinSavedM    = 1.0 // a shortcut must be shorter by more than this
	proposalMinSavedNM = 0.1 // a proposal must save at least this
	defaultClearanceNM = 2.0 // Options.ClearanceNM when 0
)

// Shortcut classes returned by classify.
const (
	classNever = iota
	classAuto
	classProposal
)

// Leg.segs codes below zero; zero and above are network edge indices.
const (
	segConnector int32 = -1 - iota
	segSmoothed
	segAccepted
)

// Proposal is a shortcut the caller may accept by passing its ID in
// Options.Accept. Path is the replacement (its two end vertices). The ID does
// not depend on the clearance, only on the dataset version and the vertices
// it replaces.
type Proposal struct {
	ID             string     `json:"id"`
	Path           []Position `json:"path"`
	Reason         string     `json:"reason"` // "below clearance" or "no TSS data"
	MinClearanceNM float64    `json:"min_clearance_nm"`
	SavedNM        float64    `json:"saved_nm"`
}

// Stretch labels path[From..To] of Route.Path with where it came from.
type Stretch struct {
	From     int    `json:"from"`
	To       int    `json:"to"`
	Flag     string `json:"flag"`
	Severity string `json:"severity"`
	Scheme   string `json:"scheme,omitempty"`
}

// proposalID is "k-" plus 12 hex digits of sha256 over the dataset version and
// the replaced vertices.
func proposalID(version string, replaced []Position) string {
	h := sha256.New()
	fmt.Fprintf(h, "v1|%s|", version)
	for _, p := range replaced {
		fmt.Fprintf(h, "%.6f,%.6f;", p[0], p[1])
	}
	return "k-" + hex.EncodeToString(h.Sum(nil))[:12]
}

type smoother struct {
	g      *Graph
	clearM float64 // requested clearance C
	tol    float64 // land tolerance T
	limit  float64 // C + T
	accept map[string]bool
	used   map[string]bool
	cache  sync.Map // [2]Position -> float64; shared by the scan workers
}

func (g *Graph) newSmoother(opts Options) *smoother {
	c := opts.ClearanceNM
	if c <= 0 {
		c = defaultClearanceNM
	}
	s := &smoother{
		g: g, clearM: c * MetresPerNM, tol: g.land.Tolerance,
		accept: map[string]bool{}, used: map[string]bool{},
	}
	s.limit = s.clearM + s.tol
	for _, id := range opts.Accept {
		s.accept[id] = true
	}
	return s
}

// ignoredAccept lists the accepted IDs that matched nothing, in request order.
func ignoredAccept(accept []string, s *smoother) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, id := range accept {
		if !seen[id] && (s == nil || !s.used[id]) {
			out = append(out, id)
		}
		seen[id] = true
	}
	return out
}

// skipSeg reports whether a path segment may not be part of a shortcut.
func (g *Graph) skipSeg(code int32) bool {
	if code == segConnector {
		return true
	}
	if code < 0 {
		return false
	}
	if g.flags[code]&(edgeLane|edgeJoin) != 0 || g.pass[code] != 0 {
		return true
	}
	return math.Abs(g.nodes[g.to[code]][0]-g.nodes[g.to[g.twin[code]]][0]) > 180
}

func pathLength(p []Position) float64 {
	var d float64
	for i := 0; i+1 < len(p); i++ {
		d += Distance(p[i], p[i+1])
	}
	return d
}

// clearance is the land clearance of the leg a-b, capped at C + T. Cached: the
// auto and proposal passes test many of the same pairs.
func (s *smoother) clearance(a, b Position) float64 {
	k := [2]Position{a, b}
	if c, ok := s.cache.Load(k); ok {
		return c.(float64)
	}
	c := s.g.land.SegmentClearance(a, b, s.limit)
	s.cache.Store(k, c)
	return c
}

// classify decides what the shortcut a-b may do to the path it replaces
// (replaced runs a..b, edges are the segment codes between them). minClearM is
// the clearance beyond the land tolerance, capped at C.
func (s *smoother) classify(a, b Position, replaced []Position, edges []int32) (class int, minClearM float64) {
	g := s.g
	if slices.ContainsFunc(edges, g.skipSeg) {
		return classNever, 0
	}
	if math.Abs(b[0]-a[0]) > 180 {
		return classNever, 0
	}
	saved := pathLength(replaced) - Distance(a, b)
	if saved <= smoothMinSavedM {
		return classNever, 0
	}
	if g.tss != nil && g.tss.SegmentTouchesArea(a, b) {
		return classNever, 0
	}
	c := s.clearance(a, b)
	if c <= s.tol {
		return classNever, 0
	}
	if c >= s.limit && s.coveredOrOpen(a, b) {
		return classAuto, c - s.tol
	}
	if saved < proposalMinSavedNM*MetresPerNM {
		return classNever, 0
	}
	return classProposal, c - s.tol
}

// coveredOrOpen reports whether the rhumb leg a-b stays inside OSM TSS coverage
// or farther than 20 NM from land, sampled every smoothSampleNM.
func (s *smoother) coveredOrOpen(a, b Position) bool {
	ma, mb := Mercator(a), Mercator(b)
	n := max(1, int(math.Ceil(Distance(a, b)/(smoothSampleNM*MetresPerNM))))
	for k := 0; k <= n; k++ {
		p := a
		switch k {
		case n:
			p = b
		case 0:
		default:
			t := float64(k) / float64(n)
			p = Position{
				(ma[0] + t*(mb[0]-ma[0])) * 180 / math.Pi,
				invMercatorLat(ma[1] + t*(mb[1]-ma[1])),
			}
		}
		if s.g.unknownAt(p) {
			return false
		}
	}
	return true
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

type span struct {
	i, j int     // the shortcut replaces vertices i..j
	c    float64 // classify's minClearM
}

// farthest returns the largest j (within smoothMaxSpan) for which the
// shortcut i-j classifies as want, or 0.
func (s *smoother) farthest(pts []Position, segs []int32, i, want int) span {
	if s.g.skipSeg(segs[i]) {
		return span{}
	}
	for j := min(i+smoothMaxSpan, len(pts)-1); j >= i+2; j-- {
		if class, c := s.classify(pts[i], pts[j], pts[i:j+1], segs[i:j]); class == want {
			return span{i, j, c}
		}
	}
	return span{}
}

// scan is the greedy pass: at each vertex i it takes the farthest shortcut
// classified as want, then continues from its end. The land checks dominate, so
// farthest is evaluated for all vertices in parallel first; the result does not
// depend on scheduling.
func (s *smoother) scan(pts []Position, segs []int32, want int) []span {
	n := len(pts)
	if n < 3 {
		return nil
	}
	best := make([]span, n)
	var next atomic.Int64
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), n) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i+2 >= n {
					return
				}
				best[i] = s.farthest(pts, segs, i, want)
			}
		}()
	}
	wg.Wait()
	var out []span
	for i := 0; i+2 < n; {
		if sp := best[i]; sp.j > 0 {
			out = append(out, sp)
			i = sp.j
		} else {
			i++
		}
	}
	return out
}

// apply replaces each span (ascending, non-overlapping) by one segment with the given code.
func apply(pts []Position, segs []int32, spans []span, code int32) ([]Position, []int32) {
	if len(spans) == 0 {
		return pts, segs
	}
	op, os := []Position{pts[0]}, make([]int32, 0, len(segs))
	k := 0
	for _, sp := range spans {
		for ; k < sp.i; k++ {
			op, os = append(op, pts[k+1]), append(os, segs[k])
		}
		op, os = append(op, pts[sp.j]), append(os, code)
		k = sp.j
	}
	for ; k+1 < len(pts); k++ {
		op, os = append(op, pts[k+1]), append(os, segs[k])
	}
	return op, os
}

// smoothLeg runs the pipeline on one leg and returns its proposals.
func (s *smoother) smoothLeg(l *Leg) []Proposal {
	ver := s.g.Version()
	pts, segs := l.Path, l.segs

	autos := s.scan(pts, segs, classAuto)
	for _, sp := range autos {
		if id := proposalID(ver, pts[sp.i:sp.j+1]); s.accept[id] {
			s.used[id] = true // already smoothed: accepting it is a no-op
		}
	}
	pts, segs = apply(pts, segs, autos, segSmoothed)

	var props []Proposal
	var taken []span
	for _, sp := range s.scan(pts, segs, classProposal) {
		a, b := pts[sp.i], pts[sp.j]
		id := proposalID(ver, pts[sp.i:sp.j+1])
		reason := "no TSS data"
		if s.clearance(a, b) < s.limit {
			reason = "below clearance"
		}
		props = append(props, Proposal{
			ID:             id,
			Path:           []Position{a, b},
			Reason:         reason,
			MinClearanceNM: math.Round(sp.c/MetresPerNM*10) / 10,
			SavedNM:        math.Round((pathLength(pts[sp.i:sp.j+1])-Distance(a, b))/MetresPerNM*100) / 100,
		})
		if s.accept[id] {
			s.used[id] = true
			taken = append(taken, sp)
		}
	}
	l.Path, l.segs = apply(pts, segs, taken, segAccepted)
	l.measure()
	return props
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

// stretches labels the segments of the concatenated route path. Flag
// precedence per segment: connector, user-accepted, smoothed, tss-unresolved,
// osm-tss, tss-unknown, marnet. Adjacent segments with the same flag and
// scheme merge.
func (g *Graph) stretches(legs []Leg) []Stretch {
	out := []Stretch{}
	unknown := map[Position]bool{}
	base := 0
	for _, l := range legs {
		for k, code := range l.segs {
			flag, scheme := g.segFlag(code, l.Path[k], l.Path[k+1], l.lifted, unknown)
			from := base + k
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
		base += len(l.Path) - 1
	}
	return out
}

func (g *Graph) segFlag(code int32, a, b Position, lifted []bool, unknown map[Position]bool) (flag, scheme string) {
	switch code {
	case segConnector:
		return "connector", ""
	case segAccepted:
		return "user-accepted", ""
	case segSmoothed:
		return "smoothed", ""
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
	f := g.flags[code]
	k := int(g.scheme[code])
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
