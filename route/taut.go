package route

import (
	"container/heap"
	"maps"
	"math"
	"runtime"
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
var tautOffsetsNM = [...]float64{0, 1, -1, 2, -2, 4, -4, 8, -8, 16, -16}

// pathCtx is what the taut search needs to judge a leg.
type pathCtx struct {
	clearM         float64    // requested clearance from land
	limitM         float64    // open-water depth limit; 0 skips the depth check
	limitConfinedM float64    // depth limit near confined passages
	ends           []Position // requested points: approach exemption
	noTSS          bool       // skip the TSS rules
	lifted         []bool     // schemes whose rules the leg lifted (by scheme); nil = none

	// confined samples the confined passages the leg uses (set by tautLeg). A
	// segment whose midpoint is within tautConfinedNM of one uses limitConfinedM.
	confined []Position

	// joins maps each point where a taut run meets a pinned piece to its
	// clearance from land. The pinned side is as drawn, so a segment leaving
	// the point is held to the join's own clearance when that is below the
	// requested one, instead of making the whole run unroutable.
	joins map[Position]float64

	// track indexes samples of the coarse leg (set by tautLeg). The land data
	// is dense within CorridorNM of MARNET vertices only, and MARNET has long
	// edges in open water, so a point that is out of reach of every vertex but
	// within trackNM of the coarse leg counts as covered: the coarse leg is
	// trusted as drawn, and the taut path may follow it there.
	track map[int64][]Position
}

const (
	trackNM      = 4.0  // reach of the coarse track
	trackStepNM  = 1.0  // spacing of the track samples
	trackCellDeg = 0.25 // cell size of the track index
)

func trackKey(p Position) int64 {
	return cellKey(int32(math.Floor(p[0]/trackCellDeg)), int32(math.Floor(p[1]/trackCellDeg)))
}

// newTrack indexes samples of the coarse path.
func newTrack(path []Position) map[int64][]Position {
	t := map[int64][]Position{}
	add := func(p Position) { t[trackKey(p)] = append(t[trackKey(p)], p) }
	for k := 0; k+1 < len(path); k++ {
		for _, pc := range splitAntimeridian(path[k], path[k+1]) {
			for _, p := range rhumbSamples(pc[0], pc[1], trackStepNM) {
				add(p)
			}
		}
	}
	return t
}

// onTrack reports whether p is within trackNM of a track sample. The distance
// is flat-earth, which is plenty at this range.
func (c *pathCtx) onTrack(p Position) bool {
	cx, cy := int32(math.Floor(p[0]/trackCellDeg)), int32(math.Floor(p[1]/trackCellDeg))
	const reach = trackNM / 60 // degrees of latitude
	cosP := math.Cos(p[1] * math.Pi / 180)
	for dx := int32(-1); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for _, q := range c.track[cellKey(cx+dx, cy+dy)] {
				dl := math.Abs(q[0] - p[0])
				if dl > 180 {
					dl = 360 - dl
				}
				if x, y := dl*cosP, q[1]-p[1]; x*x+y*y <= reach*reach {
					return true
				}
			}
		}
	}
	return false
}

// deepTileM is the minimum depth of a 0.5 degree tile that makes it open
// ocean: no land and no shoal anywhere in the tile at 15 arc-seconds.
const deepTileM = 200.0

// covered reports whether the land data covers the leg a-b (every sample, as
// in inCorridor), or the leg stays on the coarse track where it does not, or
// the sample lies in a fully deep tile (open ocean: the depth grid vouches for
// it).
func (g *Graph) covered(a, b Position, c *pathCtx) bool {
	r := g.land.CorridorNM*MetresPerNM - tautCorridorM
	for _, pc := range splitAntimeridian(a, b) {
		for _, p := range rhumbSamples(pc[0], pc[1], corridorSampleNM) {
			if !c.onTrack(p) && !(r > 0 && g.nearVertex(p, r)) && !(g.depth != nil && g.depth.TileMinDepth(p) >= deepTileM) {
				return false
			}
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
		if d, known := g.segmentDepth(a, b, runMarnet, lim); known && d < lim && g.approachOf(a, b) == "" {
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
// connectors, TSS lanes, passage edges, legs over the antimeridian and TSS
// joins, except the joins marked in freeJoin (nil = none). A free join is
// judged by safeLeg, which enforces the TSS rules.
func (g *Graph) pinnedSeg(l *Leg, k int, freeJoin []bool) bool {
	code := l.segs[k]
	switch {
	case code < 0:
		return true
	case g.flags[code]&edgeLane != 0, g.pass[code] != 0, g.flags[code]&edgeJoin != 0 && (freeJoin == nil || !freeJoin[k]):
		return true
	}
	return math.Abs(l.Path[k][0]-l.Path[k+1][0]) > 180
}

// tautLeg replaces every free run of the coarse leg with a taut safe-water
// chain (tautRun), keeping the pinned pieces. It does this twice when the leg
// has TSS joins, with the joins pinned and with them free, and keeps the
// shorter result. It rewrites l.Path and l.segs and calls l.measure. A run
// with no safe path stays as drawn.
func (g *Graph) tautLeg(l *Leg, c *pathCtx) {
	if g.land == nil {
		return // without land data a shortcut cannot be judged: keep the network path
	}
	loose := g.looseJoins(l)
	if loose == nil {
		g.tautLegJoins(l, c, nil, nil)
		return
	}
	free := *l
	// A run the two passes share (no loose join in it) is tautened once.
	memo := map[[2]int]tautResult{}
	g.tautLegJoins(l, c, nil, memo)
	g.tautLegJoins(&free, c, loose, memo)
	if free.DistanceM < l.DistanceM {
		*l = free
	}
}

// A stretch of TSS joins is worth freeing when it is at least this much longer
// than the rhumb between its ends: on a short leg any join (the free pass is
// cheap), on a long one only a clear detour, because the free pass re-tautens
// every run it merges and would double the routing time.
const (
	looseJoinExcessNM     = 0.0
	looseJoinExcessLongNM = 5.0
	looseJoinShortLegNM   = 600.0
)

// looseJoins marks the segments of the joins of l that detour: each stretch of
// consecutive join segments that is long enough a detour (see looseJoinExcessNM). It returns nil when there is none.
func (g *Graph) looseJoins(l *Leg) []bool {
	var out []bool
	need := looseJoinExcessLongNM
	if l.DistanceM <= looseJoinShortLegNM*MetresPerNM {
		need = looseJoinExcessNM
	}
	for k := 0; k < len(l.segs); {
		if l.segs[k] < 0 || g.flags[l.segs[k]]&edgeJoin == 0 {
			k++
			continue
		}
		j, along := k, 0.0
		for j < len(l.segs) && l.segs[j] >= 0 && g.flags[l.segs[j]]&edgeJoin != 0 {
			along += Distance(l.Path[j], l.Path[j+1])
			j++
		}
		if along-Distance(l.Path[k], l.Path[j]) >= need*MetresPerNM {
			if out == nil {
				out = make([]bool, len(l.segs))
			}
			for i := k; i < j; i++ {
				out[i] = true
			}
		}
		k = j
	}
	return out
}

type tautResult struct {
	path []Position // run points after the first
	segs []int32
}

// tautLegJoins is one pass of tautLeg: the joins marked in freeJoin are free.
// memo, when not nil, holds the result of every run by its segment range.
func (g *Graph) tautLegJoins(l *Leg, c *pathCtx, freeJoin []bool, memo map[[2]int]tautResult) {
	type run struct{ lo, hi int } // segments lo..hi-1, path points lo..hi
	var runs []run
	cc := *c
	cc.lifted = l.lifted
	cc.confined, cc.joins, cc.track = nil, map[Position]float64{}, newTrack(l.Path)
	for k := 0; k < len(l.segs); {
		if !g.pinnedSeg(l, k, freeJoin) {
			j := k
			for j < len(l.segs) && !g.pinnedSeg(l, j, freeJoin) {
				j++
			}
			runs = append(runs, run{k, j})
			k = j
			continue
		}
		if code := l.segs[k]; code >= 0 && PassageLimits[g.passes[g.pass[code]]].Confined {
			cc.confined = append(cc.confined, rhumbSamples(l.Path[k], l.Path[k+1], tautConfinedNM/2)...)
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
		if res, ok := memo[[2]int{r.lo, r.hi}]; ok {
			out[i] = res
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out[i].path, out[i].segs = g.tautSplit(l, r.lo, r.hi, &cc)
		}()
	}
	wg.Wait()
	if memo != nil {
		for i, r := range runs {
			memo[[2]int{r.lo, r.hi}] = out[i]
		}
	}

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
	for lo < hi {
		if pts, ok := try(lo, hi); ok {
			outP = append(outP, pts[1:]...)
			outS = appendTaut(outS, len(pts)-1)
			break
		}
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
			// Not even one segment is safe: keep it as drawn.
			outP = append(outP, l.Path[lo+1])
			outS = append(outS, l.segs[lo])
			lo++
			continue
		}
		outP = append(outP, goodPts[1:]...)
		outS = appendTaut(outS, len(goodPts)-1)
		lo = good
	}
	return outP, outS
}

func appendTaut(s []int32, n int) []int32 {
	for range n {
		s = append(s, segTaut)
	}
	return s
}

// tautRun returns a taut chain from pts[0] to pts[len-1] (both fixed) that
// stays within about 16 NM of the run and passes safeLeg, or false and pts when
// no safe path exists. Where the ends are far apart it also tries a chain of
// rhumb legs along the great circle, which only passes where every sample is
// covered (fully deep tiles), and keeps the shorter.
func (g *Graph) tautRun(pts []Position, c *pathCtx) ([]Position, bool) {
	if len(pts) < 2 {
		return pts, true
	}
	var best []Position
	for _, scale := range [...]float64{1, 1.5} {
		if out, ok := g.thetaRun(pts, c, scale); ok {
			best = g.tighten(out, c)
			break
		}
	}
	if gc, ok := g.greatCircleRun(pts[0], pts[len(pts)-1], c); ok {
		for _, cand := range [][]Position{gc, g.tightenBy(gc, c, true), g.tightenBy(gc, c, false)} {
			if best == nil || chainLength(cand) < chainLength(best) {
				best = cand
			}
		}
	}
	if best == nil {
		return pts, false
	}
	return best, true
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

// tighten drops every inner vertex whose neighbours see each other, until
// nothing changes.
func (g *Graph) tighten(p []Position, c *pathCtx) []Position {
	return g.tightenBy(p, c, false)
}

// tightenBy is tighten; with shorterOnly it drops a vertex only when the
// direct rhumb leg is not longer than the two legs through it. Over long
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

type latticeNode struct {
	pos Position
	k   float64 // lateral offset in NM, for tie-breaking
}

// lattice places stations every tautStationNM of arc along pts, with lattice
// points at the lateral offsets (scaled) perpendicular to the local bearing.
// The ends are single nodes. Points failing the point check are dropped.
func (g *Graph) lattice(pts []Position, c *pathCtx, scale float64) [][]latticeNode {
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
		var st []latticeNode
		for _, off := range tautOffsetsNM {
			k := math.Max(-tautMaxOffset, math.Min(tautMaxOffset, off*scale))
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

// thetaRun runs Theta* over the lattice of one run.
func (g *Graph) thetaRun(pts []Position, c *pathCtx, scale float64) ([]Position, bool) {
	st := g.lattice(pts, c, scale)
	if st == nil {
		return nil, false
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

	cache := map[[2]int32]bool{}
	see := func(a, b int32) bool {
		key := [2]int32{a, b}
		ok, hit := cache[key]
		if !hit {
			ok = Distance(nodes[a].pos, nodes[b].pos) <= tautSightNM*MetresPerNM && g.safeLeg(nodes[a].pos, nodes[b].pos, c)
			cache[key] = ok
		}
		return ok
	}
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
		return nil, false
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
	return out, true
}
