// Package route finds sea routes over the Eurostat MARNET network.
//
// The graph is built once with Load and is safe for concurrent use.
// Positions are [longitude, latitude] in decimal degrees, as in GeoJSON.
package route

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
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
}

// Route is a sequence of legs through the requested points.
type Route struct {
	Legs      []Leg      `json:"legs"`
	Path      []Position `json:"path"`
	DistanceM float64    `json:"distance_m"`
	Passages  []string   `json:"passages"`
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
}

// Passages returns the passage tags present in the network, sorted.
func (g *Graph) Passages() []string {
	out := slices.Clone(g.passes[1:])
	sort.Strings(out)
	return out
}

// Nodes returns the number of network nodes.
func (g *Graph) Nodes() int { return len(g.nodes) }

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

// Load reads a GeoJSON network of LineStrings. A "pass" property tags
// every segment of the line with that passage.
func Load(path string) (*Graph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read network: %w", err)
	}
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
	return New(segs), nil
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
func New(segs []Segment) *Graph {
	g := &Graph{passes: []string{""}}
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

	type edge struct {
		from, to int32
		w        float64
		pass     uint8
	}
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
		edges = append(edges, edge{a, b, w, pid}, edge{b, a, w, pid})
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
	fill := slices.Clone(g.first[:n])
	for _, e := range edges {
		i := fill[e.from]
		fill[e.from]++
		g.to[i], g.weight[i], g.pass[i] = e.to, e.w, e.pass
	}
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

// markLargestComponent marks the nodes of the largest connected component.
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

	snapped := make([]int32, len(points))
	for i, p := range points {
		v, ok := g.nearest(p, blocked)
		if !ok {
			return Route{}, errors.New("network has no usable nodes")
		}
		snapped[i] = v
	}

	var r Route
	for i := 0; i+1 < len(points); i++ {
		from, to := points[i], points[i+1]
		if i > 0 {
			from = g.nodes[snapped[i]]
		}
		if i+2 < len(points) {
			to = g.nodes[snapped[i+1]]
		}
		leg, err := g.leg(from, to, snapped[i], snapped[i+1], blocked, opts.Algorithm)
		if err != nil {
			return Route{}, fmt.Errorf("leg %d: %w", i+1, err)
		}
		r.Legs = append(r.Legs, leg)
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

// nearest returns the closest usable node that has at least one open edge.
func (g *Graph) nearest(p Position, blocked []bool) (int32, bool) {
	// Chord length on the unit sphere rises with great-circle distance,
	// so the nearest by chord is the nearest by distance.
	q := toUnit(p)
	best, bestC := int32(-1), math.Inf(1)
	for v := range g.nodes {
		if !g.usable[v] || !g.hasOpenEdge(int32(v), blocked) {
			continue
		}
		if c := q.chord2(g.unit[v]); c < bestC {
			best, bestC = int32(v), c
		}
	}
	return best, best >= 0
}

func (g *Graph) hasOpenEdge(v int32, blocked []bool) bool {
	for e := g.first[v]; e < g.first[v+1]; e++ {
		if !blocked[g.pass[e]] {
			return true
		}
	}
	return false
}

func (g *Graph) leg(from, to Position, s, t int32, blocked []bool, alg Algorithm) (Leg, error) {
	nodes, edges, networkM, ok := g.search(s, t, blocked, alg)
	if !ok {
		return Leg{}, errors.New("no path on the network with these passages avoided")
	}
	leg := Leg{From: from, To: to, NetworkM: networkM, Passages: []string{}}
	leg.Path = []Position{from}
	for _, v := range nodes {
		if p := g.nodes[v]; p != leg.Path[len(leg.Path)-1] {
			leg.Path = append(leg.Path, p)
		}
	}
	if to != leg.Path[len(leg.Path)-1] {
		leg.Path = append(leg.Path, to)
	}
	for _, e := range edges {
		if name := g.passes[g.pass[e]]; name != "" && (len(leg.Passages) == 0 || leg.Passages[len(leg.Passages)-1] != name) {
			leg.Passages = append(leg.Passages, name)
		}
	}
	leg.DistanceM = Distance(from, g.nodes[s]) + networkM + Distance(g.nodes[t], to)
	return leg, nil
}

// Distance is the great-circle (haversine) distance in metres.
func Distance(a, b Position) float64 {
	const rad = math.Pi / 180
	lat1, lat2 := a[1]*rad, b[1]*rad
	dLat := lat2 - lat1
	dLon := (b[0] - a[0]) * rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(h)))
}
