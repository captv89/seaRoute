// Package route finds sea routes over the MARNET network.
//
// The graph is built once with Load and is safe for concurrent use.
// Positions are [longitude, latitude], as in GeoJSON.
package route

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	gdj "github.com/pitchinnate/golangGeojsonDijkstra"
	"github.com/tomchavakis/turf-go/measurement"
)

// Precision used to round network coordinates into graph vertices.
const Precision = 0.00001

// MetresPerNM is the international nautical mile.
const MetresPerNM = 1852.0

// Position is [longitude, latitude] in decimal degrees.
type Position = gdj.Position

// Graph is the routing network, preprocessed for Dijkstra.
type Graph struct {
	g gdj.Graph
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
}

// Route is a sequence of legs through the requested points.
type Route struct {
	Legs      []Leg      `json:"legs"`
	Path      []Position `json:"path"`
	DistanceM float64    `json:"distance_m"`
}

// DistanceNM returns the total distance in nautical miles.
func (r Route) DistanceNM() float64 { return r.DistanceM / MetresPerNM }

// Load reads a GeoJSON LineString network and builds the graph.
// Multi-point lines are split into two-point segments in memory.
func Load(path string) (*Graph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read network: %w", err)
	}
	var fc gdj.FeatureCollection
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("parse network: %w", err)
	}
	return New(Split(fc)), nil
}

// New builds the graph from two-point LineString features.
func New(fc gdj.FeatureCollection) *Graph {
	topo := fc.ToTopology(Precision)
	return &Graph{g: topo.Preprocess()}
}

// Split turns multi-point LineStrings into two-point segments,
// which is the form the Dijkstra library expects.
func Split(fc gdj.FeatureCollection) gdj.FeatureCollection {
	out := gdj.FeatureCollection{Type: "FeatureCollection"}
	for _, f := range fc.Features {
		cs := f.Geometry.Coordinates
		for i := 0; i < len(cs)-1; i++ {
			out.Features = append(out.Features, gdj.Feature{
				Type:     "Feature",
				Geometry: gdj.Geometry{Type: "LineString", Coordinates: []gdj.Position{cs[i], cs[i+1]}},
			})
		}
	}
	return out
}

// Route finds the shortest route from the first point to the last,
// passing each point in between in order (via-points). A via-point is
// snapped to its nearest network vertex, so the route passes through
// the network there instead of detouring out to the point and back.
func (gr *Graph) Route(points ...Position) (Route, error) {
	if len(points) < 2 {
		return Route{}, errors.New("route needs at least two points")
	}
	points = append([]Position(nil), points...)
	for i := 1; i < len(points)-1; i++ {
		points[i], _ = gr.g.ClosestVertex(points[i])
	}
	var r Route
	for i := 0; i < len(points)-1; i++ {
		leg, err := gr.leg(points[i], points[i+1])
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
	}
	return r, nil
}

func (gr *Graph) leg(from, to Position) (Leg, error) {
	start, _ := gr.g.ClosestVertex(from)
	end, _ := gr.g.ClosestVertex(to)

	var network []Position
	var networkM float64
	if start.Key() == end.Key() {
		network = []Position{start}
	} else {
		p, d, err := gr.g.ShortestPath(start, end)
		if err != nil {
			return Leg{}, fmt.Errorf("no path on network: %w", err)
		}
		// The library returns the path end -> start.
		for i := len(p) - 1; i >= 0; i-- {
			network = append(network, p[i])
		}
		networkM = d
	}

	// Join the connectors, skipping repeats where an end is already on the network.
	path := []Position{from}
	for _, p := range append(network, to) {
		if p != path[len(path)-1] {
			path = append(path, p)
		}
	}
	return Leg{
		From:      from,
		To:        to,
		Path:      path,
		DistanceM: Distance(from, start) + networkM + Distance(end, to),
		NetworkM:  networkM,
	}, nil
}

// Distance is the great-circle distance in metres, using the same
// formula as the network edge weights.
func Distance(a, b Position) float64 {
	d, err := measurement.Distance(a[0], a[1], b[0], b[1], "meters")
	if err != nil {
		return 0
	}
	return d
}
