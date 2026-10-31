// Package route finds sea routes over the Eurostat MARNET network.
//
// The graph is built once with Load and is safe for concurrent use.
// Positions are [longitude, latitude] in decimal degrees, as in GeoJSON.
package route

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"time"
)

// ErrUnknownPassage is returned when Avoid or Allow names a passage the network lacks.
var ErrUnknownPassage = errors.New("unknown passage")

// MetresPerNM is the international nautical mile.
const MetresPerNM = 1852.0

// earthRadiusM is the IUGG mean Earth radius.
const earthRadiusM = 6371008.8

// Position is [longitude, latitude] in decimal degrees.
type Position [2]float64

// Algorithm selects the shortest-path search.
type Algorithm int

const (
	// AStar searches towards the target using landmark and great-circle
	// lower bounds. Same result as Dijkstra, far fewer nodes visited.
	AStar Algorithm = iota
	// Dijkstra searches outwards evenly. Kept as a reference for tests.
	Dijkstra
)

// Options adjust a route request.
type Options struct {
	// Avoid lists passage tags the route must not use, e.g. "kiel", "suez".
	// See Graph.Passages for the tags in the loaded network.
	Avoid []string
	// Allow opens passages that are closed by default (ClosedByDefault).
	Allow     []string
	Algorithm Algorithm
	// NoTSS routes on the plain MARNET network: lane, join and wrong-way
	// edges are skipped and TSS blocks are ignored. Internal; not exposed in
	// the UI or APIs.
	NoTSS bool
	// ClearanceNM is the land clearance hazards are measured against; 0 means 2.
	ClearanceNM float64
	// Vessel is the ship the route is planned for; zero fields mean the defaults.
	Vessel Vessel
}

// ClosedByDefault are passages a route avoids unless Options.Allow opens
// them: the Arctic routes, which need ice class or a summer window.
var ClosedByDefault = []string{"northwest", "northeast"}

// Area tags network segments inside a box with a passage name.
type Area struct {
	Pass                           string
	MinLon, MinLat, MaxLon, MaxLat float64
}

// ExtraPassages tags passages that MARNET leaves untagged. A segment that
// touches the box gets the tag, unless it already has one.
var ExtraPassages = []Area{
	// The Sound (Øresund), Helsingør to Drogden. Drogden channel limits draught.
	{Pass: "oresund", MinLon: 12.45, MinLat: 55.45, MaxLon: 12.95, MaxLat: 56.08},
	// Strait of Belle Isle, between Newfoundland and Labrador. Ice-bound in winter.
	{Pass: "belle_isle", MinLon: -57.3, MinLat: 51.0, MaxLon: -55.6, MaxLat: 51.9},
}

// Leg is the route between two consecutive requested points.
type Leg struct {
	From Position `json:"from"`
	To   Position `json:"to"`
	// Path runs From -> To and includes both ends.
	Path []Position `json:"path"`
	// DistanceM is the full leg: From -> network, along the network, network -> To.
	DistanceM float64 `json:"distance_m"`
	// NetworkM is the part along the network. The rest is straight
	// connectors at each end, which are not checked against land.
	NetworkM float64 `json:"network_m"`
	// Passages are the tagged canals and straits the leg uses, in order.
	Passages []string `json:"passages"`

	// lifted is the set of schemes whose TSS rules were lifted on this leg
	// (indexed by scheme, nil when none); liftOrder lists them in the order
	// they were lifted.
	lifted    []bool
	liftOrder []int32
	// nodes and edges are the network search result behind the path.
	nodes, edges []int32
	// segs describes each path segment: the network edge index it follows, or
	// segConnector (see provenance.go).
	segs []int32
}

// Warning tells the caller that a route may break a rule.
type Warning struct {
	Flag    string `json:"flag"`
	Scheme  string `json:"scheme"`
	Name    string `json:"name"`
	Message string `json:"message"`
}

// Route is a sequence of legs through the requested points.
type Route struct {
	Legs      []Leg      `json:"legs"`
	Path      []Position `json:"path"`
	DistanceM float64    `json:"distance_m"`
	Passages  []string   `json:"passages"`
	// Warnings has one entry per scheme whose rules were lifted on any leg.
	Warnings []Warning `json:"warnings"`
	// Stretches label runs of Path by provenance; From and To index Path.
	Stretches []Stretch `json:"stretches"`
	// Hazards are runs of Path that touch land or pass closer than the
	// clearance. Empty without land data.
	Hazards []Hazard `json:"hazards"`
	// Depth is the depth the vessel needs in open water; ConfinedDepthM the
	// same in confined passages.
	Depth          DepthLimit `json:"depth"`
	ConfinedDepthM float64    `json:"confined_depth_limit_m"`
	// Notices report where the draught changed the route.
	Notices []Notice `json:"notices"`
}

// DistanceNM returns the total distance in nautical miles.
func (r Route) DistanceNM() float64 { return r.DistanceM / MetresPerNM }

// Graph is the routing network in compressed adjacency form.
type Graph struct {
	nodes  []Position
	first  []int32   // edges of node v are first[v]..first[v+1]
	to     []int32   // edge target
	weight []float64 // edge length in metres
	pass   []uint8   // edge passage tag, index into passes; 0 = none
	passes []string  // passes[0] is ""
	usable []bool    // node is in the largest connected component
	unit   []vec3    // node position on the unit sphere, for trig-free distance bounds
	lm     landmarks // precomputed distances for the A* estimate

	// Nodes 0..marnetNodes-1 are MARNET nodes; the rest are TSS lane vertices
	// and access nodes (applyTSS). Only MARNET nodes are snapped to.
	marnetNodes int
	flags       []uint8 // per directed edge: edgeLane, edgeJoin, edgeWrongWay, edgeTSSBlocked
	scheme      []int32 // per directed edge: responsible scheme (index into tss.Schemes), -1 for none
	twin        []int32 // per directed edge: the reverse edge
	land        *Land
	corridor    map[int64][]int32 // MARNET vertices by 1-degree cell, for inCorridor
	tss         *TSS
	depth       *Depth
	minDepth    []float32 // per directed edge: shallowest depth along it, +Inf when unknown or exempt
	version     string
	stats       tssStats
}

// edge is a directed edge before the CSR build. New appends edges in
// forward/reverse pairs, so the twin of slice index i is i^1.
type edge struct {
	from, to int32
	w        float64
	pass     uint8
	flags    uint8
	scheme   int32
}

// Passages returns the passage tags present in the network, sorted.
func (g *Graph) Passages() []string {
	out := slices.Clone(g.passes[1:])
	sort.Strings(out)
	return out
}

// Nodes returns the number of network nodes.
func (g *Graph) Nodes() int { return len(g.nodes) }

// Edges returns the number of network segments. Each segment is stored
// once per direction, so this is half the directed edge count.
func (g *Graph) Edges() int { return len(g.to) / 2 }

type geojsonFC struct {
	Features []struct {
		Properties map[string]any `json:"properties"`
		Geometry   struct {
			Type        string     `json:"type"`
			Coordinates []Position `json:"coordinates"`
		} `json:"geometry"`
	} `json:"features"`
}

// Segment is one network edge with an optional passage tag.
type Segment struct {
	A, B Position
	Pass string
}

// Datasets names the files LoadAll reads. An empty Land or TSS path turns
// that feature off.
type Datasets struct {
	Network, Land, TSS, Depth string
}

// Load reads a GeoJSON network of LineStrings. A "pass" property tags
// every segment of the line with that passage. It equals
// LoadAll(Datasets{Network: path}).
func Load(path string) (*Graph, error) {
	return LoadAll(Datasets{Network: path})
}

// LoadAll reads the network and, when named, the land and TSS datasets, and
// builds the TSS-aware graph.
func LoadAll(d Datasets) (*Graph, error) {
	data, err := os.ReadFile(d.Network)
	if err != nil {
		return nil, fmt.Errorf("read network: %w", err)
	}
	segs, err := parseNetwork(data)
	if err != nil {
		return nil, err
	}
	var land *Land
	if d.Land != "" {
		if land, err = LoadLand(d.Land); err != nil {
			return nil, err
		}
	}
	var tss *TSS
	if d.TSS != "" {
		if tss, err = LoadTSS(d.TSS); err != nil {
			return nil, err
		}
	}
	var depth *Depth
	if d.Depth != "" {
		if depth, err = LoadDepth(d.Depth); err != nil {
			return nil, err
		}
	}
	sum := sha256.Sum256(data)
	return newGraph(segs, land, tss, depth, hex.EncodeToString(sum[:])[:8]), nil
}

func parseNetwork(data []byte) ([]Segment, error) {
	var fc geojsonFC
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("parse network: %w", err)
	}
	var segs []Segment
	for i, f := range fc.Features {
		if f.Geometry.Type != "LineString" {
			return nil, fmt.Errorf("feature %d: geometry %q, want LineString", i, f.Geometry.Type)
		}
		pass, _ := f.Properties["pass"].(string)
		cs := f.Geometry.Coordinates
		for j := 0; j+1 < len(cs); j++ {
			segs = append(segs, Segment{A: cs[j], B: cs[j+1], Pass: pass})
		}
	}
	return segs, nil
}

// nodeKey rounds to about 0.1 m and joins the antimeridian (-180 = 180),
// so lines that meet there connect.
func nodeKey(p Position) [2]int64 {
	lon := p[0]
	if lon == -180 {
		lon = 180
	}
	return [2]int64{int64(math.Round(lon * 1e6)), int64(math.Round(p[1] * 1e6))}
}

// New builds the graph from segments. Zero-length segments are dropped.
// It equals NewWith(segs, nil, nil).
func New(segs []Segment) *Graph { return NewWith(segs, nil, nil) }

// NewWith builds the graph from segments with optional land and TSS data.
// With TSS, lanes become one-way edges joined to the network, and MARNET
// edges that cross a separation zone or line, or run against a lane, are
// blocked (see applyTSS). Land is used only to drop joins that touch land.
func NewWith(segs []Segment, land *Land, tss *TSS) *Graph {
	return newGraph(segs, land, tss, nil, segmentsHash(segs))
}

func newGraph(segs []Segment, land *Land, tss *TSS, depth *Depth, netHash string) *Graph {
	g := &Graph{passes: []string{""}, land: land, tss: tss, depth: depth}
	ids := map[[2]int64]int32{}
	node := func(p Position) int32 {
		k := nodeKey(p)
		if id, ok := ids[k]; ok {
			return id
		}
		id := int32(len(g.nodes))
		ids[k] = id
		g.nodes = append(g.nodes, p)
		return id
	}
	passID := map[string]uint8{"": 0}

	var edges []edge
	for _, s := range segs {
		a, b := node(s.A), node(s.B)
		if a == b {
			continue
		}
		if s.Pass == "" {
			s.Pass = extraPass(s.A, s.B)
		}
		pid, ok := passID[s.Pass]
		if !ok {
			pid = uint8(len(g.passes))
			passID[s.Pass] = pid
			g.passes = append(g.passes, s.Pass)
		}
		w := Distance(g.nodes[a], g.nodes[b])
		edges = append(edges, edge{a, b, w, pid, 0, -1}, edge{b, a, w, pid, 0, -1})
	}
	g.marnetNodes = len(g.nodes)
	if land != nil {
		g.buildCorridor()
	}
	g.stats.marnet = len(edges) / 2
	if tss != nil {
		start := time.Now()
		g.applyTSS(&edges)
		g.stats.tssBuild = time.Since(start)
	}

	n := len(g.nodes)
	g.first = make([]int32, n+1)
	for _, e := range edges {
		g.first[e.from+1]++
	}
	for i := 1; i <= n; i++ {
		g.first[i] += g.first[i-1]
	}
	g.to = make([]int32, len(edges))
	g.weight = make([]float64, len(edges))
	g.pass = make([]uint8, len(edges))
	g.flags = make([]uint8, len(edges))
	g.scheme = make([]int32, len(edges))
	g.twin = make([]int32, len(edges))
	fill := slices.Clone(g.first[:n])
	pos := make([]int32, len(edges)) // slice index -> CSR index
	for k, e := range edges {
		i := fill[e.from]
		fill[e.from]++
		pos[k] = i
		g.to[i], g.weight[i], g.pass[i], g.flags[i], g.scheme[i] = e.to, e.w, e.pass, e.flags, e.scheme
	}
	for k := range edges {
		g.twin[pos[k]] = pos[k^1]
	}
	g.version = netHash + "/" + landVersion(land) + "/" + tssVersion(tss) + "/" + depthVersion(depth)
	g.buildMinDepth()
	g.markLargestComponent()
	g.unit = make([]vec3, len(g.nodes))
	for i, p := range g.nodes {
		g.unit[i] = toUnit(p)
	}
	g.lm = g.buildLandmarks(numLandmarks)
	return g
}

func extraPass(a, b Position) string {
	for _, x := range ExtraPassages {
		if x.crosses(a, b) {
			return x.Pass
		}
	}
	return ""
}

// crosses reports whether segment a-b touches the box (Liang-Barsky clip).
// Network edges can be long, so testing only end or mid points misses some.
func (x Area) crosses(a, b Position) bool {
	t0, t1 := 0.0, 1.0
	dx, dy := b[0]-a[0], b[1]-a[1]
	for _, c := range [4][2]float64{
		{-dx, a[0] - x.MinLon},
		{dx, x.MaxLon - a[0]},
		{-dy, a[1] - x.MinLat},
		{dy, x.MaxLat - a[1]},
	} {
		p, q := c[0], c[1]
		if p == 0 {
			if q < 0 {
				return false
			}
			continue
		}
		r := q / p
		if p < 0 {
			t0 = max(t0, r)
		} else {
			t1 = min(t1, r)
		}
		if t0 > t1 {
			return false
		}
	}
	return true
}

// markLargestComponent marks the nodes of the largest connected component of
// the relaxed graph (all edges, flags ignored).
// Points snap only to these, so a route never starts on a stray island.
func (g *Graph) markLargestComponent() {
	n := len(g.nodes)
	comp := make([]int32, n)
	for i := range comp {
		comp[i] = -1
	}
	var best, bestSize int32 = -1, 0
	stack := []int32{}
	for s := range n {
		if comp[s] >= 0 {
			continue
		}
		id, size := int32(s), int32(0)
		comp[s] = id
		stack = append(stack[:0], int32(s))
		for len(stack) > 0 {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			size++
			for e := g.first[v]; e < g.first[v+1]; e++ {
				if u := g.to[e]; comp[u] < 0 {
					comp[u] = id
					stack = append(stack, u)
				}
			}
		}
		if size > bestSize {
			best, bestSize = id, size
		}
	}
	g.usable = make([]bool, n)
	for v := range n {
		g.usable[v] = comp[v] == best
	}
}

// Route finds the shortest route from the first point to the last,
// passing each point in between in order (via-points). A via-point is
// snapped to its nearest network node, so the route passes through the
// network there instead of detouring out to the point and back.
func (g *Graph) Route(points []Position, opts Options) (Route, error) {
	if len(points) < 2 {
		return Route{}, errors.New("route needs at least two points")
	}
	blocked, err := g.blockedPasses(opts.Avoid, opts.Allow)
	if err != nil {
		return Route{}, err
	}
	vessel := opts.Vessel.WithDefaults()
	rr := reroute{vessel: vessel, lim: vessel.DepthLimit(false), limConfine: vessel.DepthLimit(true).LimitM}
	// Constrained requests also close passages the draught exceeds and edges
	// too shallow; the unconstrained ones keep every other option.
	plain := searchCtx{blocked: blocked, noTSS: opts.NoTSS} // snapping never lifts schemes; each leg lifts its own
	constrained := searchCtx{noTSS: opts.NoTSS}
	constrained.blocked, rr.closed = g.draughtBlocked(blocked, vessel.DraughtM)
	rr.shallow = g.shallowEdges(rr.lim.LimitM, rr.limConfine, points)
	constrained.shallow = rr.shallow
	limited := rr.shallow != nil || len(rr.closed) > 0

	snapped := make([]int32, len(points))
	for i, p := range points {
		v, ok := g.nearest(p, constrained)
		if !ok {
			v, ok = g.nearest(p, plain)
		}
		if !ok {
			return Route{}, errors.New("network has no usable nodes")
		}
		snapped[i] = v
	}

	r := Route{Warnings: []Warning{}, Stretches: []Stretch{}, Notices: []Notice{}, Depth: rr.lim, ConfinedDepthM: rr.limConfine}
	noRoute := false
	warned := map[int32]bool{}
	for i := 0; i+1 < len(points); i++ {
		from, to := points[i], points[i+1]
		if i > 0 {
			from = g.nodes[snapped[i]]
		}
		if i+2 < len(points) {
			to = g.nodes[snapped[i+1]]
		}
		leg, err := g.leg(from, to, snapped[i], snapped[i+1], constrained, opts.Algorithm)
		if err != nil && limited {
			// No path deep enough: show the shortest one, and say so once.
			if leg, err = g.leg(from, to, snapped[i], snapped[i+1], plain, opts.Algorithm); err == nil && !noRoute {
				noRoute = true
				r.Notices = append(r.Notices, noRouteNotice(vessel, rr.lim))
			}
		} else if err == nil && limited {
			if free, ferr := g.leg(from, to, snapped[i], snapped[i+1], plain, opts.Algorithm); ferr == nil {
				c, u := pathSeq{leg.nodes, leg.edges}, pathSeq{free.nodes, free.edges}
				r.Notices = append(r.Notices, g.notices(g.divergences(c, u), rr)...)
			}
		}
		if err != nil {
			return Route{}, fmt.Errorf("leg %d: %w", i+1, err)
		}
		r.Legs = append(r.Legs, leg)
		for _, k := range leg.liftOrder {
			if !warned[k] {
				warned[k] = true
				sc := &g.tss.Schemes[k]
				r.Warnings = append(r.Warnings, Warning{
					Flag:    "tss-unresolved",
					Scheme:  sc.ID,
					Name:    sc.Name,
					Message: fmt.Sprintf("OSM scheme data inconsistent near %s, TSS rules lifted here; check the chart", sc.Name),
				})
			}
		}
		r.DistanceM += leg.DistanceM
		if i == 0 {
			r.Path = append(r.Path, leg.Path...)
		} else {
			r.Path = append(r.Path, leg.Path[1:]...)
		}
		for _, p := range leg.Passages {
			if len(r.Passages) == 0 || r.Passages[len(r.Passages)-1] != p {
				r.Passages = append(r.Passages, p)
			}
		}
	}
	if r.Passages == nil {
		r.Passages = []string{}
	}
	flags, schemes := g.segFlags(r.Legs)
	r.Stretches = stretches(flags, schemes)
	r.Hazards = g.hazards(r.Legs, r.Path, opts)
	return r, nil
}

func (g *Graph) blockedPasses(avoid, allow []string) ([]bool, error) {
	index := func(name string) (int, error) {
		i := slices.Index(g.passes, name)
		if i <= 0 {
			return 0, fmt.Errorf("%w %q; known: %v", ErrUnknownPassage, name, g.Passages())
		}
		return i, nil
	}
	blocked := make([]bool, len(g.passes))
	for _, name := range ClosedByDefault {
		if i := slices.Index(g.passes, name); i > 0 {
			blocked[i] = true
		}
	}
	for _, name := range allow {
		i, err := index(name)
		if err != nil {
			return nil, err
		}
		blocked[i] = false
	}
	for _, name := range avoid {
		i, err := index(name)
		if err != nil {
			return nil, err
		}
		blocked[i] = true
	}
	return blocked, nil
}

// nearest returns the closest usable MARNET node that has at least one open
// edge. Lane nodes are never snapped to.
func (g *Graph) nearest(p Position, sc searchCtx) (int32, bool) {
	// Chord length on the unit sphere rises with great-circle distance,
	// so the nearest by chord is the nearest by distance.
	q := toUnit(p)
	best, bestC := int32(-1), math.Inf(1)
	for v := range g.marnetNodes {
		if !g.usable[v] || !g.hasOpenEdge(int32(v), sc) {
			continue
		}
		if c := q.chord2(g.unit[v]); c < bestC {
			best, bestC = int32(v), c
		}
	}
	return best, best >= 0
}

func (g *Graph) hasOpenEdge(v int32, sc searchCtx) bool {
	for e := g.first[v]; e < g.first[v+1]; e++ {
		if g.open(e, sc) {
			return true
		}
	}
	return false
}

// leg routes s -> t. When TSS leaves no path it lifts one scheme at a time
// (see liftFor) and searches again, until a path exists or nothing is left to lift.
func (g *Graph) leg(from, to Position, s, t int32, sc searchCtx, alg Algorithm) (Leg, error) {
	var lifted []bool
	var order []int32
	nodes, edges, networkM, ok := g.search(s, t, sc, alg)
	for !ok && !sc.noTSS && g.tss != nil {
		k, found := g.liftFor(s, t, sc)
		if !found {
			break
		}
		if lifted == nil {
			lifted = make([]bool, len(g.tss.Schemes))
		}
		lifted[k] = true
		sc.lifted = lifted
		order = append(order, k)
		nodes, edges, networkM, ok = g.search(s, t, sc, alg)
	}
	if !ok {
		return Leg{}, errors.New("no path on the network with these passages avoided")
	}
	leg := Leg{From: from, To: to, NetworkM: networkM, Passages: []string{}, lifted: lifted, liftOrder: order, nodes: nodes, edges: edges}
	leg.Path = []Position{from}
	for k, v := range nodes {
		if p := g.nodes[v]; p != leg.Path[len(leg.Path)-1] {
			leg.Path = append(leg.Path, p)
			if k == 0 {
				leg.segs = append(leg.segs, segConnector)
			} else {
				leg.segs = append(leg.segs, edges[k-1])
			}
		}
	}
	if to != leg.Path[len(leg.Path)-1] {
		leg.Path = append(leg.Path, to)
		leg.segs = append(leg.segs, segConnector)
	}
	for _, e := range edges {
		if name := g.passes[g.pass[e]]; name != "" && (len(leg.Passages) == 0 || leg.Passages[len(leg.Passages)-1] != name) {
			leg.Passages = append(leg.Passages, name)
		}
	}
	leg.measure()
	return leg, nil
}

// Distance is the rhumb-line (loxodrome) distance in metres, the length of a
// constant-bearing leg between a and b.
func Distance(a, b Position) float64 {
	const rad = math.Pi / 180
	lat1, lat2 := a[1]*rad, b[1]*rad
	dLat := lat2 - lat1
	dLon := math.Remainder((b[0]-a[0])*rad, 2*math.Pi)
	dPsi := math.Log(math.Tan(math.Pi/4+lat2/2) / math.Tan(math.Pi/4+lat1/2))
	q := math.Cos(lat1)
	if math.Abs(dPsi) >= 1e-12 {
		q = dLat / dPsi
	}
	return earthRadiusM * math.Hypot(dLat, q*dLon)
}

// Mercator projects p to Mercator (x, y) in radians. Straight lines in this
// plane are rhumb lines.
func Mercator(p Position) [2]float64 {
	const rad = math.Pi / 180
	return [2]float64{p[0] * rad, math.Log(math.Tan(math.Pi/4 + p[1]*rad/2))}
}
