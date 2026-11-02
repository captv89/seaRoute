package route

import (
	"container/heap"
	"maps"
	"math"
	"runtime"
	"slices"
	"sync"
)

const (
	tautStationNM  = 5.0 // spacing of the lattice stations along a run
	tautCorridorM  = 4 * MetresPerNM
	tautConfinedNM = 5.0 // a segment this close to a confined passage uses the confined depth limit
	tautMaxOffset  = 24.0
	tautSightNM    = 100.0 // longest line of sight Theta* tries; tighten joins the pieces
)

// tautOffsetsNM are the lateral lattice offsets, in the order they are tried.
// tautWideOffsetsNM is the fallback lattice when a station has no safe node
// within 16 NM: it keeps the inner offsets, so a gap a few miles off the
// coarse path (between an island and a reef) is still found, and reaches out
// to tautMaxOffset.
var (
	tautOffsetsNM     = []float64{0, 1, -1, 2, -2, 4, -4, 8, -8, 16, -16}
	tautWideOffsetsNM = []float64{0, 1, -1, 2, -2, 4, -4, 6, -6, 8, -8, 12, -12, 16, -16, 20, -20, tautMaxOffset, -tautMaxOffset}
)

// pathCtx is what the taut search needs to judge a leg.
type pathCtx struct {
	clearM         float64    // requested clearance from land
	limitM         float64    // open-water depth limit; 0 skips the depth check
	limitConfinedM float64    // depth limit near confined passages
	ends           []Position // requested points: approach exemption
	noTSS          bool       // skip the TSS rules
	strictDepth    bool       // judge depth cell by cell (pull), not by shallow runs
	lifted         []bool     // schemes whose rules the leg lifted (by scheme); nil = none

	// confined samples the confined passages the leg uses (set by tautLeg). A
	// segment whose midpoint is within tautConfinedNM of one uses limitConfinedM.
	confined []Position

	// joins maps each point where a taut run meets a pinned piece to its
	// clearance from land. The pinned side is as drawn, so a segment leaving
	// the point is held to the join's own clearance when that is below the
	// requested one, instead of making the whole run unroutable.
	joins map[Position]float64

	// band lets the depth grid vouch, cell by cell, for open ocean in a tile
	// that is not deep throughout (see covered). tautSplit sets it only to
	// rescue a coarse segment that would otherwise be kept as drawn over land.
	band bool
}

// deepTileM is the minimum depth of a 0.5 degree tile that makes it open
// ocean: no land and no shoal anywhere in the tile at 15 arc-seconds.
const deepTileM = 200.0

// covered reports whether the land data covers the leg a-b (every sample, as
// in inCorridor), or the samples it does not cover lie in fully deep tiles
// (open ocean: the depth grid vouches for it). The coarse network path is not
// trusted where the data is missing: some MARNET edges cross land.
func (g *Graph) covered(a, b Position, c *pathCtx) bool {
	r := g.land.CorridorNM*MetresPerNM - tautCorridorM
	for _, pc := range splitAntimeridian(a, b) {
		ps := rhumbSamples(pc[0], pc[1], corridorSampleNM)
		lo, hi := -1, -1 // first and last sample the land data does not cover
		for i, p := range ps {
			if r > 0 && g.nearNetwork(p, r) {
				continue
			}
			if lo < 0 {
				lo = i
			}
			hi = i
		}
		if lo < 0 {
			continue
		}
		// Open ocean is vouched for by the depth grid, and only exactly: a
		// corner of a shallow, land or unstored tile can lie between two
		// samples, so the whole stretch from the sample before the first
		// uncovered one to the sample after the last is walked tile by tile.
		// A tile holding an island or a seamount is not deep throughout; with
		// band set the grid vouches cell by cell for open-ocean depth over a
		// band as wide as the clearance, so a ship can round an ocean island
		// far from the network.
		if g.depth == nil {
			return false
		}
		sa, sb := ps[max(lo-1, 0)], ps[min(hi+1, len(ps)-1)]
		if !g.depth.DeepTiles(sa, sb, deepTileM) && !(c.band && g.depth.DeepBand(sa, sb, c.clearM+g.land.Tolerance, deepTileM)) {
			return false
		}
	}
	return true
}

// nearEnd reports whether both a and b lie within endpointExemptNM of one of
// the requested points (the port approach).
func (c *pathCtx) nearEnd(a, b Position) bool {
	for _, e := range c.ends {
		if Distance(a, e) <= endpointExemptNM*MetresPerNM && Distance(b, e) <= endpointExemptNM*MetresPerNM {
			return true
		}
	}
	return false
}

// nearConfined reports whether the midpoint of a-b is near a confined passage.
func (c *pathCtx) nearConfined(a, b Position) bool {
	if len(c.confined) == 0 {
		return false
	}
	m := segMid(a, b)
	const degR = tautConfinedNM / 60 * 1.5 // degrees, with slack for the longitude scale
	for _, p := range c.confined {
		if math.Abs(p[1]-m[1]) > degR {
			continue
		}
		dl := math.Abs(p[0] - m[0])
		if dl > 180 {
			dl = 360 - dl
		}
		if dl*math.Cos(m[1]*math.Pi/180) > degR {
			continue
		}
		if Distance(m, p) <= tautConfinedNM*MetresPerNM {
			return true
		}
	}
	return false
}

// safeLeg reports whether the rhumb leg a-b keeps the requested clearance from
// land, has enough water for the vessel, obeys the TSS rules and stays inside
// the area the land data covers. Within endpointExemptNM of a requested point
// the clearance and depth checks relax to "does not touch land".
func (g *Graph) safeLeg(a, b Position, c *pathCtx) bool {
	relaxed := c.nearEnd(a, b)
	if g.land != nil {
		if !g.landClear(a, b, c, relaxed) {
			return false
		}
		if g.land.CorridorNM != 0 && !g.covered(a, b, c) {
			return false
		}
	}
	if g.tss != nil && !c.noTSS && g.breaksTSS(a, b, c.lifted) {
		return false
	}
	if g.depth != nil && !relaxed && c.limitM > 0 {
		lim := c.limitM
		if c.nearConfined(a, b) {
			lim = c.limitConfinedM
		}
		// A taut leg has no network edge, so no kind: it is free water and is
		// judged like MARNET (runMarnet). Lane and join edges kept as drawn are
		// not judged here; the hazards pass judges them with runTSS, which is
		// the laxer run, so a free leg is never judged less strictly than it
		// is reported.
		run := runMarnet
		if c.strictDepth {
			run = 1
		}
		if d, _ := g.segmentDepth(a, b, run, lim); d < lim && g.approachOf(a, b) == "" {
			return false
		}
	}
	return true
}

// joinReachNM is how far from a join the join's own clearance applies.
const joinReachNM = 2.0

// landClear checks the clearance of the leg a-b. Near a join (a run end whose
// clearance is below the requested one) only the first joinReachNM of the leg
// is held to the join's clearance, floored at "does not touch land"; the rest
// keeps the full clearance.
func (g *Graph) landClear(a, b Position, c *pathCtx, relaxed bool) bool {
	tol := g.land.Tolerance
	check := func(p, q Position, need float64, touchOnly bool) bool {
		if g.openWater(p, q, need) {
			return true
		}
		d := g.land.SegmentClearance(p, q, need)
		return d >= need && !(touchOnly && d <= tol)
	}
	if relaxed {
		return check(a, b, tol+1, true)
	}
	full := c.clearM + tol
	needOf := func(p Position) float64 {
		if jc, ok := c.joins[p]; ok && jc < full {
			return math.Max(jc*0.99, tol+1)
		}
		return full
	}
	needA, needB := needOf(a), needOf(b)
	if needA == full && needB == full {
		return check(a, b, full, false)
	}
	// Cut points as fractions of the leg: [0,tA] needA, [tA,tB] full, [tB,1] needB.
	l := Distance(a, b)
	tA, tB := 0.0, 1.0
	if needA < full {
		tA = math.Min(1, joinReachNM*MetresPerNM/l)
	}
	if needB < full {
		tB = math.Max(tA, 1-joinReachNM*MetresPerNM/l)
	}
	at := func(t float64) Position {
		switch t {
		case 0:
			return a
		case 1:
			return b
		}
		bl := b[0]
		if bl-a[0] > 180 {
			bl -= 360
		} else if a[0]-bl > 180 {
			bl += 360
		}
		ma, mb := Mercator(a), Mercator(Position{bl, b[1]})
		return Position{math.Remainder((ma[0]+t*(mb[0]-ma[0]))*180/math.Pi, 360), invMercatorLat(ma[1] + t*(mb[1]-ma[1]))}
	}
	pa, pb := at(tA), at(tB)
	return (tA == 0 || check(a, pa, needA, false)) &&
		(tB <= tA || check(pa, pb, full, false)) &&
		(tB == 1 || check(pb, b, needB, false))
}

// breaksTSS is TSS.breaksRules without the schemes the leg lifted. A leg that
// stays on one side of the antimeridian skips the schemes whose box it misses
// before the exact test, which is most of them.
func (g *Graph) breaksTSS(a, b Position, lifted []bool) bool {
	plain := math.Abs(b[0]-a[0]) <= 180
	box := segBox(a, b)
	for j := range g.tss.Schemes {
		s := &g.tss.Schemes[j]
		if (lifted != nil && lifted[j]) || (plain && !s.wrap && !s.box.overlaps(box)) {
			continue
		}
		if c, l := schemeReasons(s, a, b); c || l {
			return true
		}
	}
	return false
}

// openWater is a cheap sufficient test that the rhumb leg a-b stays needM from
// land: no land edge within needM plus half the leg (plus 1 NM of slack) of its
// midpoint. Land.Within stops at the first edge it finds, so near a coast the
// answer is a quick false and the exact query decides.
func (g *Graph) openWater(a, b Position, needM float64) bool {
	half := Distance(a, b) / 2
	if half > tautSightNM*MetresPerNM/2 {
		return false
	}
	return !g.land.Within(segMid(a, b), needM+half+MetresPerNM)
}

// pinnedSeg reports whether segment k of a coarse leg is kept as drawn:
// connectors, TSS lanes and passage edges. A leg over the antimeridian is free:
// the lattice, links and every land, depth and coverage check unwrap or split
// at ±180, and pinning it forced the path through the network's vertices there. TSS
// joins are free, even inside a tagged passage: a join is only the network's
// way into a lane end, and a ship heads for the lane end directly. safeLeg
// enforces the TSS rules on the taut legs that replace them.
func (g *Graph) pinnedSeg(l *Leg, k int) bool {
	code := l.segs[k]
	switch {
	case code < 0, g.flags[code]&edgeLane != 0:
		return true
	case g.flags[code]&edgeJoin == 0 && g.pass[code] != 0:
		return true
	}
	return false
}

type tautResult struct {
	path []Position // run points after the first
	segs []int32
}

// tautLeg replaces every free run of the coarse leg with a taut safe-water
// chain (tautRun), keeping the pinned pieces. It rewrites l.Path and l.segs
// and calls l.measure. A run with no safe path stays as drawn.
func (g *Graph) tautLeg(l *Leg, c *pathCtx) {
	if g.land == nil {
		return // without land data a shortcut cannot be judged: keep the network path
	}
	type run struct{ lo, hi int } // segments lo..hi-1, path points lo..hi
	var runs []run
	cc := *c
	cc.lifted = l.lifted
	cc.confined, cc.joins = nil, map[Position]float64{}
	for k, code := range l.segs {
		if code >= 0 && PassageLimits[g.passes[g.pass[code]]].Confined {
			cc.confined = append(cc.confined, rhumbSamples(l.Path[k], l.Path[k+1], tautConfinedNM/2)...)
		}
	}
	for k := 0; k < len(l.segs); {
		if !g.pinnedSeg(l, k) {
			j := k
			for j < len(l.segs) && !g.pinnedSeg(l, j) {
				j++
			}
			runs = append(runs, run{k, j})
			k = j
			continue
		}
		k++
	}
	if len(runs) == 0 {
		return
	}
	out := make([]tautResult, len(runs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for i, r := range runs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out[i].path, out[i].segs = g.tautSplit(l, r.lo, r.hi, &cc)
		}()
	}
	wg.Wait()

	path := []Position{l.Path[0]}
	var segs []int32
	k, ri := 0, 0
	for k < len(l.segs) {
		if ri < len(runs) && runs[ri].lo == k {
			r := runs[ri]
			path = append(path, out[ri].path...)
			segs = append(segs, out[ri].segs...)
			k, ri = r.hi, ri+1
			continue
		}
		path = append(path, l.Path[k+1])
		segs = append(segs, l.segs[k])
		k++
	}
	l.Path, l.segs = path, segs
	l.measure()
}

// tautSplit tautens the run of segments lo..hi-1 of l. When the whole run has
// no safe path it keeps the longest prefix that has one (binary search over
// the coarse vertices) and continues from its end, so one tight spot leaves
// only its own stretch as drawn. It returns the points after the first and the
// segment codes.
func (g *Graph) tautSplit(l *Leg, lo, hi int, c *pathCtx) ([]Position, []int32) {
	cc := *c
	// Joins entries for probe vertices that end up inside a chain are harmless:
	// they only matter for a segment that starts or ends exactly there.
	cc.joins = maps.Clone(c.joins)
	var outP []Position
	var outS []int32
	// try tautens the coarse vertices lo..m.
	try := func(lo, m int) ([]Position, bool) {
		if g.land != nil {
			need := c.clearM + g.land.Tolerance
			for _, p := range [2]Position{l.Path[lo], l.Path[m]} {
				if _, ok := cc.joins[p]; !ok {
					cc.joins[p] = g.land.PointDistance(p, need)
				}
			}
		}
		return g.tautRun(l.Path[lo:m+1], &cc)
	}
	first := l.Path[lo]
	split := false
	for lo < hi {
		if pts, ok := try(lo, hi); ok {
			outP = append(outP, pts[1:]...)
			outS = appendTaut(outS, len(pts)-1)
			break
		}
		split = true
		// Longest prefix lo..good that succeeds; bad is the shortest known failure.
		good, bad := lo, hi
		var goodPts []Position
		if pts, ok := try(lo, lo+1); ok {
			good, goodPts = lo+1, pts
			for bad-good > 1 {
				mid := (good + bad) / 2
				if pts, ok := try(lo, mid); ok {
					good, goodPts = mid, pts
				} else {
					bad = mid
				}
			}
		}
		if good == lo {
			// Not even one segment is safe. A network edge drawn over land
			// (an ocean island between two far MARNET vertices) is retried
			// with the depth grid vouching for the open water round it;
			// otherwise the segment is kept as drawn.
			if pts, ok := g.rescueAground(l.Path[lo], l.Path[lo+1], &cc); ok {
				outP = append(outP, pts[1:]...)
				outS = appendTaut(outS, len(pts)-1)
			} else {
				outP = append(outP, l.Path[lo+1])
				outS = append(outS, l.segs[lo])
			}
			lo++
			continue
		}
		outP = append(outP, goodPts[1:]...)
		outS = appendTaut(outS, len(goodPts)-1)
		lo = good
	}
	if split {
		outP, outS = g.tightenSplit(first, outP, outS, &cc)
	}
	return outP, outS
}

// rescueAground tautens the single coarse segment a-b with band set, when the
// segment as drawn touches land. Only then: elsewhere the band would let the
// path stray far from the network, where the land data may be missing.
func (g *Graph) rescueAground(a, b Position, c *pathCtx) ([]Position, bool) {
	if g.land == nil || g.depth == nil || g.land.SegmentClearance(a, b, 1) > 0 {
		return nil, false
	}
	cb := *c
	cb.band = true
	return g.tautRun([]Position{a, b}, &cb)
}

// tightenSplit drops every vertex of a split run (first, then pts with segment
// codes segs) whose neighbours see each other, across the seams between the
// pieces: each piece is tautened on its own, so a vertex where two pieces meet,
// or a segment kept as drawn between them, can be slack. A merged segment is
// taut. A vertex is dropped only when the direct rhumb leg is not longer than
// the two legs through it, so a great-circle chain is not folded back into one
// longer rhumb leg.
func (g *Graph) tightenSplit(first Position, pts []Position, segs []int32, c *pathCtx) ([]Position, []int32) {
	for changed := true; changed; {
		changed = false
		var outP []Position
		var outS []int32
		last, merged := first, false
		for i, p := range pts {
			if i+1 < len(pts) && Distance(last, pts[i+1]) <= Distance(last, p)+Distance(p, pts[i+1])+1 && g.safeLeg(last, pts[i+1], c) {
				merged, changed = true, true
				continue
			}
			code := segs[i]
			if merged {
				code = segTaut
			}
			outP, outS = append(outP, p), append(outS, code)
			last, merged = p, false
		}
		pts, segs = outP, outS
	}
	return pts, segs
}

func appendTaut(s []int32, n int) []int32 {
	for range n {
		s = append(s, segTaut)
	}
	return s
}

// tautRun returns a taut chain from pts[0] to pts[len-1] (both fixed) that
// stays near the run and passes safeLeg, or false and pts when no safe path
// exists. Theta* finds a path over a lattice along the run; its waypoints are
// then tightened and pulled to where the legs graze an obstacle, each leg
// sailed as a link (one rhumb leg, or a great-circle chain over open ocean).
func (g *Graph) tautRun(pts []Position, c *pathCtx) ([]Position, bool) {
	if len(pts) < 2 {
		return pts, true
	}
	s := newLinks(g, c)
	out, ok := g.thetaWidening(pts, c)
	if !ok {
		// No lattice path: the ends may still see each other over open ocean.
		if r := s.link(pts[0], pts[len(pts)-1]); r.ok {
			return append([]Position{pts[0]}, r.pts...), true
		}
		return pts, false
	}
	strict := *c
	strict.strictDepth = true
	way := s.tighten(newLinks(g, &strict).pull(s.tighten(out)))
	// A link's own points can be slack against the next link's: tighten the
	// joined path once more, never lengthening it.
	return g.tightenBy(s.expand(way), c, true), true
}

// tautWidenStations is how many stations either side of where Theta* got
// stuck take the wide offsets on the next try; tautWidenTries bounds the tries.
const (
	tautWidenStations = 4
	tautWidenTries    = 4
)

// thetaWidening runs Theta* on the narrow lattice and, where it gets stuck
// (an island or reef across the coarse path that the narrow offsets cannot
// round), widens the lattice around that station and tries again. The last
// try is wide throughout. The tries share one line-of-sight cache, so a retry
// costs little more than the new nodes.
func (g *Graph) thetaWidening(pts []Position, c *pathCtx) ([]Position, bool) {
	cache := map[[2]Position]bool{}
	wide := map[int]bool{}
	for range tautWidenTries {
		out, ok, stuck := g.thetaRun(pts, c, func(i int) bool { return wide[i] }, cache)
		if ok {
			return out, true
		}
		grown := false
		for i := stuck - tautWidenStations; i <= stuck+tautWidenStations+1; i++ {
			if !wide[i] {
				wide[i], grown = true, true
			}
		}
		if !grown {
			break
		}
	}
	out, ok, _ := g.thetaRun(pts, c, func(int) bool { return true }, cache)
	return out, ok
}

// chainLength is the length of the rhumb chain p in metres.
func chainLength(p []Position) float64 {
	d := 0.0
	for i := 1; i < len(p); i++ {
		d += Distance(p[i-1], p[i])
	}
	return d
}

// gcLegNM is the longest rhumb leg of a great-circle chain.
const gcLegNM = 100.0

// greatCircleRun is a chain of rhumb legs of at most gcLegNM along the great
// circle from a to b, or false when the ends are within a single leg of each
// other or any leg fails safeLeg.
func (g *Graph) greatCircleRun(a, b Position, c *pathCtx) ([]Position, bool) {
	d := Distance(a, b)
	if d <= gcLegNM*MetresPerNM {
		return nil, false
	}
	n := int(math.Ceil(d / (gcLegNM * MetresPerNM)))
	rad := math.Pi / 180
	toVec := func(p Position) [3]float64 {
		la, lo := p[1]*rad, p[0]*rad
		return [3]float64{math.Cos(la) * math.Cos(lo), math.Cos(la) * math.Sin(lo), math.Sin(la)}
	}
	va, vb := toVec(a), toVec(b)
	dot := math.Max(-1, math.Min(1, va[0]*vb[0]+va[1]*vb[1]+va[2]*vb[2]))
	omega := math.Acos(dot)
	so := math.Sin(omega)
	if so < 1e-9 {
		return nil, false // coincident or antipodal ends
	}
	out := []Position{a}
	for i := 1; i < n; i++ {
		f := float64(i) / float64(n)
		wa, wb := math.Sin((1-f)*omega)/so, math.Sin(f*omega)/so
		x, y, z := wa*va[0]+wb*vb[0], wa*va[1]+wb*vb[1], wa*va[2]+wb*vb[2]
		p := Position{math.Atan2(y, x) / rad, math.Atan2(z, math.Hypot(x, y)) / rad}
		if !g.safeLeg(out[len(out)-1], p, c) {
			return nil, false
		}
		out = append(out, p)
	}
	if !g.safeLeg(out[len(out)-1], b, c) {
		return nil, false
	}
	return append(out, b), true
}

// tightenBy drops every inner vertex whose neighbours see each other, until
// nothing changes; with shorterOnly it drops a vertex only when the direct
// rhumb leg is not longer than the two legs through it. Over long
// distances a rhumb leg can be longer than a bent chain along the great
// circle, so the great-circle chain is tightened both ways and the shorter kept.
func (g *Graph) tightenBy(p []Position, c *pathCtx, shorterOnly bool) []Position {
	for changed := true; changed; {
		changed = false
		out := []Position{p[0]}
		for i := 1; i+1 < len(p); i++ {
			last := out[len(out)-1]
			if (!shorterOnly || Distance(last, p[i+1]) <= Distance(last, p[i])+Distance(p[i], p[i+1])+1) && g.safeLeg(last, p[i+1], c) {
				changed = true
				continue
			}
			out = append(out, p[i])
		}
		p = append(out, p[len(p)-1])
	}
	return p
}

// links judges the legs between waypoints of the taut path as they are
// sailed, remembering each answer: a link a-b is the rhumb leg when it is
// safe, or, beyond gcLegNM, the chain along the great circle when that is safe
// and shorter (an ocean crossing follows the great circle, not one long rhumb
// line).
type links struct {
	g    *Graph
	c    *pathCtx
	memo map[[2]Position]linkResult
}

type linkResult struct {
	pts []Position // points after a, ending at b
	m   float64    // length in metres
	ok  bool
}

func newLinks(g *Graph, c *pathCtx) *links {
	return &links{g: g, c: c, memo: map[[2]Position]linkResult{}}
}

func (s *links) link(a, b Position) linkResult {
	key := [2]Position{a, b}
	if r, hit := s.memo[key]; hit {
		return r
	}
	r := linkResult{m: math.Inf(1)}
	if s.g.safeLeg(a, b, s.c) {
		r = linkResult{[]Position{b}, Distance(a, b), true}
	}
	if gc, ok := s.g.greatCircleRun(a, b, s.c); ok {
		gc = s.g.tightenBy(gc, s.c, true)
		if m := chainLength(gc); m < r.m {
			r = linkResult{gc[1:], m, true}
		}
	}
	s.memo[key] = r
	return r
}

// tighten drops every inner waypoint of p whose neighbours are linked by a
// safe link no longer than the two through it, until nothing changes.
func (s *links) tighten(p []Position) []Position {
	for changed := true; changed; {
		changed = false
		out := []Position{p[0]}
		for i := 1; i+1 < len(p); i++ {
			last := out[len(out)-1]
			if r := s.link(last, p[i+1]); r.ok && r.m <= s.link(last, p[i]).m+s.link(p[i], p[i+1]).m+1 {
				changed = true
				continue
			}
			out = append(out, p[i])
		}
		p = append(out, p[len(p)-1])
	}
	return p
}

// expand returns the points of the path through the waypoints p.
func (s *links) expand(p []Position) []Position {
	out := []Position{p[0]}
	for i := 1; i < len(p); i++ {
		out = append(out, s.link(p[i-1], p[i]).pts...)
	}
	return out
}

// pullTolNM is the precision pull places a waypoint to; pullSweeps bounds
// its passes.
const (
	pullTolNM  = 0.1
	pullSweeps = 8
)

// pull slides each inner waypoint of p towards the chord of its neighbours
// (the rhumb line, or the great circle when they are more than gcLegNM apart)
// as far as both links stay safe and the path gets shorter. Theta*
// leaves turns on lattice nodes, up to tautStationNM from where they belong;
// pull moves each to where a leg grazes an obstacle (the clearance round a
// headland, a shoal or a TSS boundary), the turn a navigator would draw. A
// waypoint that reaches the chord is left for tighten to drop. The ends stay
// fixed.
//
// Its links should judge depth cell by cell (strictDepth): safeLeg forgives a
// short shallow run as raster noise, and pull, which seeks the edge of what
// passes, would otherwise clip a shoal by just that much.
func (s *links) pull(p []Position) []Position {
	if len(p) < 3 {
		return p
	}
	p = slices.Clone(p)
	dirty := make([]bool, len(p))
	for i := range dirty {
		dirty[i] = true
	}
	tol := pullTolNM * MetresPerNM
	for range pullSweeps {
		moved := false
		for i := 1; i+1 < len(p); i++ {
			if !dirty[i] {
				continue
			}
			dirty[i] = false
			u, v, w := p[i-1], p[i], p[i+1]
			foot, ok := chordFoot(u, v, w)
			if Distance(u, w) > gcLegNM*MetresPerNM {
				foot, ok = arcFoot(u, v, w) // the link is sailed on the great circle
			}
			if !ok {
				continue
			}
			d := Distance(v, foot)
			if d < tol {
				continue
			}
			base := s.link(u, v).m + s.link(v, w).m
			try := func(f float64) bool {
				x := slide(v, foot, f)
				r1 := s.link(u, x)
				if !r1.ok {
					return false
				}
				r2 := s.link(x, w)
				return r2.ok && r1.m+r2.m < base-1
			}
			lo, hi := 0.0, 1.0
			if try(1) {
				lo = 1
			} else {
				for (hi-lo)*d > tol {
					if mid := (lo + hi) / 2; try(mid) {
						lo = mid
					} else {
						hi = mid
					}
				}
			}
			if lo > 0 {
				p[i] = slide(v, foot, lo)
				dirty[i-1], dirty[i], dirty[i+1] = true, true, true
				moved = true
			}
		}
		if !moved {
			break
		}
	}
	return p
}

type latticeNode struct {
	pos Position
	k   float64 // lateral offset in NM, for tie-breaking
}

// lattice places stations every tautStationNM of arc along pts, with lattice
// points at the lateral offsets perpendicular to the local bearing: the wide
// offsets at station i when wide(i), or when no narrow offset is safe there.
// The ends are single nodes. Points failing the point check are dropped.
func (g *Graph) lattice(pts []Position, c *pathCtx, wide func(i int) bool) [][]latticeNode {
	// Unwrapped Mercator coordinates keep a run near the antimeridian continuous.
	m := make([][2]float64, len(pts))
	cum := make([]float64, len(pts))
	for i, p := range pts {
		m[i] = Mercator(p)
		if i > 0 {
			for m[i][0]-m[i-1][0] > math.Pi {
				m[i][0] -= 2 * math.Pi
			}
			for m[i][0]-m[i-1][0] < -math.Pi {
				m[i][0] += 2 * math.Pi
			}
			cum[i] = cum[i-1] + Distance(pts[i-1], p)
		}
	}
	total := cum[len(cum)-1]
	step := tautStationNM * MetresPerNM
	stations := [][]latticeNode{{{pos: pts[0]}}}
	seg := 0
	for s := step; s < total-step/2; s += step {
		for seg+2 < len(pts) && cum[seg+1] < s {
			seg++
		}
		l := cum[seg+1] - cum[seg]
		if l <= 0 {
			continue
		}
		t := (s - cum[seg]) / l
		x := m[seg][0] + t*(m[seg+1][0]-m[seg][0])
		y := m[seg][1] + t*(m[seg+1][1]-m[seg][1])
		dx, dy := m[seg+1][0]-m[seg][0], m[seg+1][1]-m[seg][1]
		n := math.Hypot(dx, dy)
		if n == 0 {
			continue
		}
		px, py := -dy/n, dx/n
		unit := MetresPerNM / earthRadiusM / math.Cos(invMercatorLat(y)*math.Pi/180)
		place := func(offs []float64) []latticeNode {
			var st []latticeNode
			for _, k := range offs {
				lat := invMercatorLat(y + k*unit*py)
				if math.Abs(lat) > 85 {
					continue
				}
				lon := math.Remainder((x+k*unit*px)*180/math.Pi, 360)
				p := Position{lon, lat}
				if g.safeLeg(p, p, c) {
					st = append(st, latticeNode{p, k})
				}
			}
			return st
		}
		var st []latticeNode
		if !wide(len(stations)) {
			st = place(tautOffsetsNM)
		}
		if len(st) == 0 {
			st = place(tautWideOffsetsNM)
		}
		if len(st) == 0 {
			return nil
		}
		stations = append(stations, st)
	}
	return append(stations, []latticeNode{{pos: pts[len(pts)-1]}})
}

type thetaItem struct {
	f, k float64
	id   int32
}

type thetaQueue []thetaItem

func (q thetaQueue) Len() int      { return len(q) }
func (q thetaQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q thetaQueue) Less(i, j int) bool {
	a, b := q[i], q[j]
	if a.f != b.f {
		return a.f < b.f
	}
	if aa, ab := math.Abs(a.k), math.Abs(b.k); aa != ab {
		return aa < ab
	}
	if a.k != b.k {
		return a.k < b.k
	}
	return a.id < b.id
}
func (q *thetaQueue) Push(x any) { *q = append(*q, x.(thetaItem)) }
func (q *thetaQueue) Pop() any {
	old := *q
	it := old[len(old)-1]
	*q = old[:len(old)-1]
	return it
}

// thetaRun runs Theta* over the lattice of one run (see lattice for wide).
// cache holds line-of-sight results by end points and may be shared between
// runs over the same pts and c. On failure it returns the furthest station
// reached.
func (g *Graph) thetaRun(pts []Position, c *pathCtx, wide func(i int) bool, cache map[[2]Position]bool) ([]Position, bool, int) {
	st := g.lattice(pts, c, wide)
	if st == nil {
		return nil, false, 0
	}
	var nodes []latticeNode
	var stationOf []int32
	first := make([]int32, len(st)+1)
	for i, s := range st {
		first[i] = int32(len(nodes))
		for range s {
			stationOf = append(stationOf, int32(i))
		}
		nodes = append(nodes, s...)
	}
	first[len(st)] = int32(len(nodes))
	end := int32(len(nodes) - 1)

	cost := make([]float64, len(nodes))
	parent := make([]int32, len(nodes))
	closed := make([]bool, len(nodes))
	for i := range cost {
		cost[i], parent[i] = math.Inf(1), -1
	}
	cost[0], parent[0] = 0, 0

	see := func(a, b int32) bool {
		key := [2]Position{nodes[a].pos, nodes[b].pos}
		ok, hit := cache[key]
		if !hit {
			ok = Distance(nodes[a].pos, nodes[b].pos) <= tautSightNM*MetresPerNM && g.safeLeg(nodes[a].pos, nodes[b].pos, c)
			cache[key] = ok
		}
		return ok
	}
	reached := int32(0)
	h := func(i int32) float64 { return Distance(nodes[i].pos, nodes[end].pos) }

	q := &thetaQueue{{f: h(0), id: 0}}
	for q.Len() > 0 {
		n := heap.Pop(q).(thetaItem).id
		if closed[n] {
			continue
		}
		closed[n] = true
		if n == end {
			break
		}
		si := stationOf[n]
		reached = max(reached, si)
		for m := first[si+1]; m < first[si+2]; m++ {
			var par int32
			var via float64
			switch p := parent[n]; {
			case p != n && see(p, m):
				par, via = p, cost[p]+Distance(nodes[p].pos, nodes[m].pos)
			case see(n, m):
				par, via = n, cost[n]+Distance(nodes[n].pos, nodes[m].pos)
			default:
				continue
			}
			if via < cost[m] {
				cost[m], parent[m] = via, par
				heap.Push(q, thetaItem{f: via + h(m), k: nodes[m].k, id: m})
			}
		}
	}
	if !closed[end] {
		return nil, false, int(reached)
	}
	var rev []Position
	for v := end; ; v = parent[v] {
		rev = append(rev, nodes[v].pos)
		if v == 0 {
			break
		}
	}
	out := make([]Position, len(rev))
	for i, p := range rev {
		out[len(rev)-1-i] = p
	}
	return out, true, 0
}

// mercUnwrap is the Mercator position of p with its longitude moved by whole
// turns to lie within half a turn of ref (radians), so a chain near the
// antimeridian stays continuous.
func mercUnwrap(p Position, ref float64) [2]float64 {
	m := Mercator(p)
	m[0] = ref + math.Remainder(m[0]-ref, 2*math.Pi)
	return m
}

func fromMerc(m [2]float64) Position {
	return Position{math.Remainder(m[0]*180/math.Pi, 360), invMercatorLat(m[1])}
}

// chordFoot is the point of the rhumb chord p-q nearest to v, in the Mercator
// plane, or false when the chord is degenerate.
func chordFoot(p, v, q Position) (Position, bool) {
	mv := Mercator(v)
	mp, mq := mercUnwrap(p, mv[0]), mercUnwrap(q, mv[0])
	dx, dy := mq[0]-mp[0], mq[1]-mp[1]
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return Position{}, false
	}
	t := math.Max(0, math.Min(1, ((mv[0]-mp[0])*dx+(mv[1]-mp[1])*dy)/l2))
	return fromMerc([2]float64{mp[0] + t*dx, mp[1] + t*dy}), true
}

// arcFoot is the point of the great-circle arc p-q nearest to v, or false when
// the arc is degenerate.
func arcFoot(p, v, q Position) (Position, bool) {
	rad := math.Pi / 180
	vec := func(x Position) [3]float64 {
		la, lo := x[1]*rad, x[0]*rad
		return [3]float64{math.Cos(la) * math.Cos(lo), math.Cos(la) * math.Sin(lo), math.Sin(la)}
	}
	cross := func(a, b [3]float64) [3]float64 {
		return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
	}
	dot := func(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
	a, b, x := vec(p), vec(q), vec(v)
	n := cross(a, b)
	nn := dot(n, n)
	if nn < 1e-18 {
		return Position{}, false
	}
	// Project x onto the plane of the great circle, then normalise.
	k := dot(x, n) / nn
	f := [3]float64{x[0] - k*n[0], x[1] - k*n[1], x[2] - k*n[2]}
	fl := math.Sqrt(dot(f, f))
	if fl < 1e-12 {
		return Position{}, false
	}
	f = [3]float64{f[0] / fl, f[1] / fl, f[2] / fl}
	// Outside the arc the nearest point is the nearer end.
	if dot(cross(a, f), n) < 0 {
		return p, true
	}
	if dot(cross(f, b), n) < 0 {
		return q, true
	}
	return Position{math.Atan2(f[1], f[0]) / rad, math.Atan2(f[2], math.Hypot(f[0], f[1])) / rad}, true
}

// slide is the point a fraction f of the way from v to target, in the
// Mercator plane.
func slide(v, target Position, f float64) Position {
	mv := Mercator(v)
	mt := mercUnwrap(target, mv[0])
	return fromMerc([2]float64{mv[0] + f*(mt[0]-mv[0]), mv[1] + f*(mt[1]-mv[1])})
}
