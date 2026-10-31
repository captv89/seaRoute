package route

import (
	"container/heap"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"runtime"
	"sort"
	"sync"
	"time"
)

// Edge flag bits (Graph.flags).
const (
	// edgeLane is a TSS lane segment, one-way in the lane's direction.
	edgeLane uint8 = 1 << iota
	// edgeJoin links the network to a lane start, a lane end to the network, or
	// a lane end to another lane's start. Also set on the two-way pieces of a
	// MARNET edge split at an access node (see addAccess), so NoTSS skips them.
	edgeJoin
	// edgeWrongWay is the reverse twin of a lane or join edge. Never usable; it
	// exists so twin is total and the relaxed (landmark) graph is symmetric.
	edgeWrongWay
	// edgeTSSBlocked is a MARNET edge that crosses a separation zone or line
	// (both directions) or runs against a lane (that direction only). Usable
	// only with NoTSS or when its scheme is lifted.
	edgeTSSBlocked
)

const (
	tssJoinNM    = 5.0  // join lane ends to MARNET nodes within this distance
	tssJoinMaxNM = 30.0 // radius for the extra access-point joins (see joinTargets)
	tssChainNM   = 10.0 // chain a lane end to other lane starts within this distance
	tssJoinCount = 3    // at most this many joins per lane end

	tssAccessStepNM = 0.5 // step when searching a MARNET edge for a join point
	tssAccessSteps  = 20  // steps each way from the edge's closest point
)

// tssStats counts what applyTSS built, for logs and tests. Edge counts are
// directed edges, except marnet, which counts segments (edge pairs).
type tssStats struct {
	marnet       int // MARNET segments
	lane         int // forward lane edges
	join         int // forward join edges to MARNET nodes within tssJoinNM
	joinFar      int // forward access-point join edges longer than tssJoinNM
	chain        int // forward lane-end -> lane-start join edges
	access       int // access nodes splitting a MARNET edge for a join
	blockedCross int // directed MARNET edges blocked for crossing a zone or line
	blockedLane  int // directed MARNET edges blocked only for running against a lane
	blockedOwn   int // forward lane or join edges blocked (lane data crossing a zone or line)
	tssBuild     time.Duration
}

// schemeReasons lists the TSS rules the rhumb leg a->b breaks in scheme s, as
// Scheme.segmentReasons does. It mirrors the violations oracle exactly
// (antimeridian split, scheme longitude normalisation, bbox prefilter), so the
// graph blocks what the oracle would flag.
func schemeReasons(s *Scheme, a, b Position) (cross, againstLane bool) {
	for _, piece := range splitAntimeridian(a, b) {
		pa, pb := s.norm(piece[0]), s.norm(piece[1])
		if !s.box.overlaps(segBox(pa, pb)) {
			continue
		}
		for _, r := range s.segmentReasons(Mercator(pa), Mercator(pb)) {
			if r == "against lane" {
				againstLane = true
			} else {
				cross = true
			}
		}
	}
	return cross, againstLane
}

// breaksRules reports whether the rhumb leg a-b crosses a separation zone or
// line, or runs against a lane, in any scheme: the rules MARNET edges obey.
func (t *TSS) breaksRules(a, b Position) bool {
	for j := range t.Schemes {
		if c, l := schemeReasons(&t.Schemes[j], a, b); c || l {
			return true
		}
	}
	return false
}

// applyTSS adds lane nodes and edges, join edges, and TSS blocks to the
// MARNET edge list (forward/reverse pairs; see edge). Rules:
//
//   - A MARNET edge that crosses a zone or line of a scheme is blocked in both
//     directions; one that runs against a lane is blocked in that direction.
//   - Each lane polyline gets its own nodes (one per distinct consecutive
//     vertex, never merged with MARNET nodes) and a one-way edge per segment.
//     The reverse twin is flagged edgeWrongWay.
//   - Each lane start gets join edges from, and each lane end join edges to,
//     the tssJoinCount nearest MARNET access points (see joinTargets). A join
//     is dropped when it touches land (if land is loaded) or breaks any TSS
//     rule (crosses a zone or line, or runs against a lane) in any scheme.
//   - Each lane end also chains to the nearest starts of the scheme's other
//     lanes within tssChainNM, under the same join rules.
//
// When several schemes block the same edge, scheme[] records the lowest
// scheme index (schemes are scanned in index order and the first wins).
// Lifting that scheme in Task 6 unblocks the edge even if another scheme also
// blocks it; overlapping schemes are rare and lifting is a last resort.
func (g *Graph) applyTSS(edges *[]edge) {
	t := g.tss
	es := *edges
	marnetPairs := len(es)

	for i := 0; i < marnetPairs; i += 2 {
		cross, lane := g.blockPair(es, i)
		g.stats.blockedCross += cross
		g.stats.blockedLane += lane
	}

	// Lane nodes and edges, per scheme.
	type laneEnds struct {
		k             int
		firsts, lasts []int32
	}
	var schemes []laneEnds
	for k := range t.Schemes {
		s := &t.Schemes[k]
		le := laneEnds{k: k}
		for _, lane := range s.Lanes {
			var ids []int32
			for j, p := range lane {
				if j > 0 && p == lane[j-1] {
					continue
				}
				ids = append(ids, int32(len(g.nodes)))
				g.nodes = append(g.nodes, p)
			}
			if len(ids) < 2 {
				g.nodes = g.nodes[:len(g.nodes)-len(ids)]
				continue
			}
			for j := 0; j+1 < len(ids); j++ {
				es = g.addOneWay(es, ids[j], ids[j+1], edgeLane, k)
				g.stats.lane++
			}
			le.firsts = append(le.firsts, ids[0])
			le.lasts = append(le.lasts, ids[len(ids)-1])
		}
		if len(le.firsts) > 0 {
			schemes = append(schemes, le)
		}
	}

	// Choose joins in parallel (read-only on the graph), then apply them in a
	// fixed order so the build is deterministic.
	type job struct {
		k      int
		v      int32
		toLane bool
		nodes  []int32
		pairs  []int
		picked []joinCand
	}
	var jobs []job
	for _, le := range schemes {
		nodes, pairs := g.joinCandidates(&t.Schemes[le.k], es[:marnetPairs])
		for _, f := range le.firsts {
			jobs = append(jobs, job{k: le.k, v: f, toLane: true, nodes: nodes, pairs: pairs})
		}
		for _, l := range le.lasts {
			jobs = append(jobs, job{k: le.k, v: l, nodes: nodes, pairs: pairs})
		}
	}
	marnet := es[:marnetPairs:marnetPairs]
	var wg sync.WaitGroup
	next := make(chan int)
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				j := &jobs[i]
				j.picked = g.joinTargets(marnet, j.v, j.nodes, j.pairs, j.toLane)
			}
		}()
	}
	for i := range jobs {
		next <- i
	}
	close(next)
	wg.Wait()
	for _, j := range jobs {
		for _, c := range j.picked {
			m := c.node
			if m < 0 {
				es, m = g.addAccess(es, c.pair, c.p, j.k)
			}
			if j.toLane {
				es = g.addOneWay(es, m, j.v, edgeJoin, j.k)
			} else {
				es = g.addOneWay(es, j.v, m, edgeJoin, j.k)
			}
			if c.d <= tssJoinNM*MetresPerNM {
				g.stats.join++
			} else {
				g.stats.joinFar++
			}
		}
	}

	// Chain each lane end to the starts of the scheme's other lanes: OSM often
	// splits one traffic lane into several ways with gaps between them.
	for _, le := range schemes {
		for i, l := range le.lasts {
			for _, f := range g.nearestOf(g.nodes[l], le.firsts, tssChainNM, i) {
				if g.joinOK(g.nodes[l], g.nodes[f]) {
					es = g.addOneWay(es, l, f, edgeJoin, le.k)
					g.stats.chain++
				}
			}
		}
	}
	*edges = es
}

// blockPair applies the zone, line and against-lane rules to the two-way pair
// es[i] (a->b), es[i+1] (b->a), and returns how many directed edges it newly
// blocked for crossing and for running against a lane.
func (g *Graph) blockPair(es []edge, i int) (cross, lane int) {
	a, b := g.nodes[es[i].from], g.nodes[es[i].to]
	box := segBox(a, b)
	for k := range g.tss.Schemes {
		s := &g.tss.Schemes[k]
		// Cheap prefilter; schemeReasons repeats it per normalised piece.
		if !s.wrap && math.Abs(b[0]-a[0]) <= 180 && !s.box.overlaps(box) {
			continue
		}
		cf, lf := schemeReasons(s, a, b)
		cr, lr := schemeReasons(s, b, a)
		for d, against := range [2]bool{lf, lr} {
			if j := i + d; (cf || cr || against) && es[j].flags&edgeTSSBlocked == 0 {
				es[j].flags |= edgeTSSBlocked
				es[j].scheme = int32(k)
				if cf || cr {
					cross++
				} else {
					lane++
				}
			}
		}
	}
	return cross, lane
}

// joinTargets picks the nodes to join lane node v to (toLane: joins run
// node -> v, else v -> node), each passing joinOK:
//
//   - the tssJoinCount nearest MARNET nodes within tssJoinNM (the plan's rule),
//   - plus the tssJoinCount nearest access points within tssJoinMaxNM, where an
//     access point is a MARNET node or a point on a MARNET edge.
//
// The plan's rule alone leaves many lanes unreachable: MARNET is sparse in
// straits (Malacca lane ends lie 10+ NM from any node) and runs down the lanes
// themselves (Suez south: the only TSS-legal entry to the southbound lane is
// from the middle of a MARNET edge), so routes took detours of up to 2x or
// found no path. For each nearby edge the nearest passing point is searched
// in tssAccessStepNM steps around the closest point; a chosen edge point
// becomes an access node (see addAccess). It is read-only on g, so lane ends
// are processed in parallel.
func (g *Graph) joinTargets(marnet []edge, v int32, nodes []int32, pairs []int, toLane bool) []joinCand {
	pv := g.nodes[v]
	ok := func(p Position) bool {
		if toLane {
			return g.joinOK(p, pv)
		}
		return g.joinOK(pv, p)
	}
	// The brief's rule: the nearest passing MARNET nodes within tssJoinNM.
	var out []joinCand
	taken := map[int32]bool{}
	for _, m := range g.sortedWithin(pv, nodes, tssJoinNM) {
		if len(out) == tssJoinCount {
			break
		}
		if ok(g.nodes[m]) {
			out = append(out, joinCand{d: Distance(pv, g.nodes[m]), node: m, pair: -1})
			taken[m] = true
		}
	}
	// Plus the nearest passing access points (nodes or edge points) within
	// tssJoinMaxNM, best-first: nodes carry their exact distance; an edge
	// carries its closest-point distance (a lower bound) until expanded.
	var h candHeap
	for _, m := range g.sortedWithin(pv, nodes, tssJoinMaxNM) {
		if !taken[m] {
			h = append(h, joinCand{d: Distance(pv, g.nodes[m]), node: m, pair: -1})
		}
	}
	for _, i := range pairs {
		if d, inside := g.edgeBound(pv, marnet[i]); inside {
			h = append(h, joinCand{d: d, node: -1, pair: i})
		}
	}
	heap.Init(&h)
	for added := 0; h.Len() > 0 && added < tssJoinCount; {
		c := heap.Pop(&h).(joinCand)
		switch {
		case c.node < 0 && !c.expanded:
			if p, d, found := g.edgeAccess(pv, marnet[c.pair], ok); found {
				heap.Push(&h, joinCand{d: d, node: -1, pair: c.pair, p: p, expanded: true})
			}
			continue
		case c.node >= 0 && !ok(g.nodes[c.node]):
			continue
		}
		out = append(out, c)
		added++
	}
	return out
}

type joinCand struct {
	d        float64
	node     int32 // MARNET node, or -1 for a point on MARNET pair `pair`
	pair     int
	p        Position
	expanded bool // edge point found by edgeAccess (already passes the join rules)
}

type candHeap []joinCand

func (h candHeap) Len() int { return len(h) }
func (h candHeap) Less(i, j int) bool {
	if h[i].d != h[j].d {
		return h[i].d < h[j].d
	}
	return h[i].node > h[j].node || (h[i].node == h[j].node && h[i].pair < h[j].pair)
}
func (h candHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *candHeap) Push(x any)   { *h = append(*h, x.(joinCand)) }
func (h *candHeap) Pop() any {
	old := *h
	it := old[len(old)-1]
	*h = old[:len(old)-1]
	return it
}

// edgeBound returns the distance from p to the closest point of MARNET edge
// e, and whether that is within tssJoinMaxNM. It bounds every edgeAccess point.
func (g *Graph) edgeBound(p Position, e edge) (float64, bool) {
	a, b := g.nodes[e.from], g.nodes[e.to]
	if math.Abs(b[0]-a[0]) > 180 || math.Abs(p[0]-a[0]) > 180 || e.w == 0 {
		return 0, false
	}
	c := closestOnSeg(Mercator(p), Mercator(a), Mercator(b))
	d := Distance(p, Position{c[0] * 180 / math.Pi, invMercatorLat(c[1])})
	return d, d <= tssJoinMaxNM*MetresPerNM
}

// edgeAccess returns the point on MARNET edge e nearest to p (within
// tssJoinMaxNM, strictly inside the edge) whose join passes ok. Points are
// tried at the closest point and then outwards in tssAccessStepNM steps.
func (g *Graph) edgeAccess(p Position, e edge, ok func(Position) bool) (Position, float64, bool) {
	a, b := g.nodes[e.from], g.nodes[e.to]
	if math.Abs(b[0]-a[0]) > 180 || math.Abs(p[0]-a[0]) > 180 || e.w == 0 {
		return Position{}, 0, false
	}
	ma, mb, mp := Mercator(a), Mercator(b), Mercator(p)
	dx, dy := mb[0]-ma[0], mb[1]-ma[1]
	t0 := ((mp[0]-ma[0])*dx + (mp[1]-ma[1])*dy) / (dx*dx + dy*dy)
	step := tssAccessStepNM * MetresPerNM / e.w
	type sample struct {
		d float64
		q Position
	}
	var ss []sample
	for n := -tssAccessSteps; n <= tssAccessSteps; n++ {
		t := t0 + float64(n)*step
		if n == 0 {
			t = math.Max(0, math.Min(1, t0))
		}
		if t <= 0 || t >= 1 {
			continue // endpoints are node candidates
		}
		q := Position{(ma[0] + t*dx) * 180 / math.Pi, invMercatorLat(ma[1] + t*dy)}
		if d := Distance(p, q); d <= tssJoinMaxNM*MetresPerNM {
			ss = append(ss, sample{d, q})
		}
	}
	sort.SliceStable(ss, func(i, j int) bool { return ss[i].d < ss[j].d })
	for _, s := range ss {
		if ok(s.q) {
			return s.q, s.d, true
		}
	}
	return Position{}, 0, false
}

// addAccess splits MARNET pair i (a->b) at q with a new node P (index >=
// marnetNodes, so never snapped to) and two-way edges a<->P, P<->b, flagged
// edgeJoin so NoTSS skips them. They keep the edge's passage tag and get the
// same blocking rules as MARNET edges, applied to each piece. Both directions
// of a piece are usable (each is the other's twin).
func (g *Graph) addAccess(es []edge, i int, q Position, k int) ([]edge, int32) {
	a, b := es[i].from, es[i].to
	pid := int32(len(g.nodes))
	g.nodes = append(g.nodes, q)
	for _, piece := range [2][2]int32{{a, pid}, {pid, b}} {
		w := Distance(g.nodes[piece[0]], g.nodes[piece[1]])
		es = append(es,
			edge{piece[0], piece[1], w, es[i].pass, edgeJoin, int32(k)},
			edge{piece[1], piece[0], w, es[i].pass, edgeJoin, int32(k)})
		g.blockPair(es, len(es)-2)
	}
	g.stats.access++
	return es, pid
}

// addOneWay appends a lane or join edge u->v and its wrong-way twin. A lane
// segment that itself breaks a TSS rule (bad OSM data) is blocked like a
// MARNET edge, so it stays liftable.
func (g *Graph) addOneWay(es []edge, u, v int32, kind uint8, k int) []edge {
	a, b := g.nodes[u], g.nodes[v]
	w := Distance(a, b)
	fwd := edge{u, v, w, 0, kind, int32(k)}
	if kind == edgeLane {
		for j := range g.tss.Schemes {
			if c, l := schemeReasons(&g.tss.Schemes[j], a, b); c || l {
				fwd.flags |= edgeTSSBlocked
				fwd.scheme = int32(j)
				g.stats.blockedOwn++
				break
			}
		}
	}
	return append(es, fwd, edge{v, u, w, 0, kind | edgeWrongWay, int32(k)})
}

// joinOK reports whether a join leg a->b keeps off land and breaks no TSS rule.
// The TSS rules are checked first: they are much cheaper than the land test.
func (g *Graph) joinOK(a, b Position) bool {
	for j := range g.tss.Schemes {
		if c, l := schemeReasons(&g.tss.Schemes[j], a, b); c || l {
			return false
		}
	}
	return g.land == nil || g.land.SegmentClearance(a, b, 1) > 0
}

// joinCandidates returns the MARNET nodes and MARNET edge pairs (index of the
// forward edge in marnet) within the scheme's area box grown by tssJoinMaxNM
// (with margin), in the scheme's normalised longitudes.
func (g *Graph) joinCandidates(s *Scheme, marnet []edge) ([]int32, []int) {
	dLat := tssJoinMaxNM / 60 * 1.1
	maxLat := math.Min(89, math.Max(math.Abs(s.box.minY), math.Abs(s.box.maxY))+dLat)
	dLon := dLat / math.Cos(maxLat*math.Pi/180)
	box := bbox{s.box.minX - dLon, s.box.minY - dLat, s.box.maxX + dLon, s.box.maxY + dLat}
	var nodes []int32
	for v := range g.marnetNodes {
		if box.has(s.norm(g.nodes[v])) {
			nodes = append(nodes, int32(v))
		}
	}
	var pairs []int
	for i := 0; i < len(marnet); i += 2 {
		a, b := s.norm(g.nodes[marnet[i].from]), s.norm(g.nodes[marnet[i].to])
		if math.Abs(b[0]-a[0]) <= 180 && box.overlaps(segBox(a, b)) {
			pairs = append(pairs, i)
		}
	}
	return nodes, pairs
}

// nearestOf returns up to tssJoinCount of nodes within rNM of p, nearest
// first, skipping index skip.
func (g *Graph) nearestOf(p Position, nodes []int32, rNM float64, skip int) []int32 {
	var keep []int32
	for i, v := range nodes {
		if i != skip {
			keep = append(keep, v)
		}
	}
	out := g.sortedWithin(p, keep, rNM)
	return out[:min(len(out), tssJoinCount)]
}

// sortedWithin returns the nodes within rNM of p, nearest first (ties by node
// index, so the build is deterministic).
func (g *Graph) sortedWithin(p Position, nodes []int32, rNM float64) []int32 {
	type near struct {
		v int32
		d float64
	}
	var ns []near
	for _, v := range nodes {
		if d := Distance(p, g.nodes[v]); d <= rNM*MetresPerNM {
			ns = append(ns, near{v, d})
		}
	}
	sort.Slice(ns, func(i, j int) bool {
		if ns[i].d != ns[j].d {
			return ns[i].d < ns[j].d
		}
		return ns[i].v < ns[j].v
	})
	out := make([]int32, len(ns))
	for i, n := range ns {
		out[i] = n.v
	}
	return out
}

// Version identifies the datasets the graph was built from:
// "<network sha256[:8]>/<land version>/<tss version>", with "-" for a dataset
// that is not loaded. Land version is "<date>/<tolerance>m".
func (g *Graph) Version() string { return g.version }

func landVersion(l *Land) string {
	if l == nil {
		return "-"
	}
	return l.Version
}

func tssVersion(t *TSS) string {
	if t == nil {
		return "-"
	}
	return t.Version
}

// segmentsHash is the network hash for graphs built from segments rather than
// a file: sha256 over the coordinates and passage tags.
func segmentsHash(segs []Segment) string {
	h := sha256.New()
	var buf [8]byte
	for _, s := range segs {
		for _, v := range [4]float64{s.A[0], s.A[1], s.B[0], s.B[1]} {
			binary.LittleEndian.PutUint64(buf[:], math.Float64bits(v))
			h.Write(buf[:])
		}
		h.Write([]byte(s.Pass))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

// reach runs Dijkstra from src over edges that open allows and returns the
// distance to every node (+Inf when unreached). With rev set it runs backward:
// from u it visits each v that has an open edge v -> u.
func (g *Graph) reach(src int32, rev bool, blocked, lifted []bool) []float64 {
	dist := make([]float64, len(g.nodes))
	for i := range dist {
		dist[i] = math.Inf(1)
	}
	dist[src] = 0
	q := &queue{{node: src}}
	for q.Len() > 0 {
		it := heap.Pop(q).(queueItem)
		v := it.node
		if it.g > dist[v] {
			continue
		}
		for e := g.first[v]; e < g.first[v+1]; e++ {
			use := e
			if rev {
				use = g.twin[e]
			}
			if !g.open(use, blocked, lifted, false) {
				continue
			}
			u := g.to[e]
			if d := dist[v] + g.weight[use]; d < dist[u] {
				dist[u] = d
				heap.Push(q, queueItem{node: u, g: d, f: d})
			}
		}
	}
	return dist
}

// liftFor picks the scheme to lift when s cannot reach t. The candidates are
// edges that are closed only by an unlifted scheme's TSS rules. It prefers the
// cheapest candidate that links what s reaches to what reaches t; if none
// does, the one that enters the set reaching t closest to t. Ties go to the
// lower scheme ID. ok is false when no candidate exists.
func (g *Graph) liftFor(s, t int32, blocked, lifted []bool) (int32, bool) {
	ds := g.reach(s, false, blocked, lifted)
	dt := g.reach(t, true, blocked, lifted)
	best, bestCost, linked := int32(-1), math.Inf(1), false
	better := func(k int32, cost float64) bool {
		if cost != bestCost {
			return cost < bestCost
		}
		return best < 0 || g.tss.Schemes[k].ID < g.tss.Schemes[best].ID
	}
	for v := range g.nodes {
		for e := g.first[v]; e < g.first[v+1]; e++ {
			k := g.scheme[e]
			if g.flags[e]&(edgeWrongWay|edgeTSSBlocked) != edgeTSSBlocked || blocked[g.pass[e]] || k < 0 || (lifted != nil && lifted[k]) {
				continue
			}
			u := g.to[e]
			if math.IsInf(dt[u], 1) {
				continue
			}
			if !math.IsInf(ds[v], 1) {
				cost := ds[v] + g.weight[e] + dt[u]
				if !linked || better(k, cost) {
					best, bestCost, linked = k, cost, true
				}
			} else if !linked && math.IsInf(dt[v], 1) && better(k, dt[u]) {
				best, bestCost = k, dt[u]
			}
		}
	}
	return best, best >= 0
}
