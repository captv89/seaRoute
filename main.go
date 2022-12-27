package main

import (
	"fmt"
	geo "github.com/kellydunn/golang-geo"
	"github.com/twpayne/go-geom/encoding/geojson"
	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/multi"
	"gonum.org/v1/gonum/graph/path"
	"hash/fnv"
	"log"
	"os"
	"strconv"
	"strings"
)

func main() {
	// Read the contents of the GeoJSON file into a byte slice
	data, err := os.ReadFile("marnet_densified_v2.geojson")
	if err != nil {
		log.Fatal(err)
	}

	//Unmarshall feature collection from geojson
	collection := geojson.FeatureCollection{}
	err = collection.UnmarshalJSON(data)
	if err != nil {
		log.Fatal(err)
	}

	// Print the number of features in the collection
	log.Printf("Number of features: %d", len(collection.Features))

	// Create the graph
	log.Println("Creating graph...")
	g := CreateGraph(collection)

	//	 Capture the start and end coordinates
	fromLat := "35.5074"
	fromLng := "0.1278"
	toLat := "-23.5074"
	toLng := "30.1278"

	// Convert the from and to coordinates to floats
	floatFromLat, err := strconv.ParseFloat(strings.TrimSpace(fromLat), 64)
	if err != nil {
		log.Fatal(err)
	}
	floatFromLng, err := strconv.ParseFloat(strings.TrimSpace(fromLng), 64)
	if err != nil {
		log.Fatal(err)
	}
	floatToLat, err := strconv.ParseFloat(strings.TrimSpace(toLat), 64)
	if err != nil {
		log.Fatal(err)
	}
	floatToLng, err := strconv.ParseFloat(strings.TrimSpace(toLng), 64)
	if err != nil {
		log.Fatal(err)
	}

	// Calculate the shortest distance between the start and end coordinates
	distance := shortestDistance(g, floatFromLat, floatFromLng, floatToLat, floatToLng)
	fmt.Println("Shortest distance:", distance)
}

// NewGeoNode creates a new node with the given ID, latitude, and longitude
func NewGeoNode(id int64, lat, lng float64) *GeoNode {
	return &GeoNode{
		ID:  id,
		Lat: lat,
		Lng: lng,
	}
}

func (n *GeoNode) GeoNode() {}

func CreateGraph(collection geojson.FeatureCollection) *multi.WeightedDirectedGraph {
	g := multi.NewWeightedDirectedGraph()

	// Add nodes to the graph
	for _, feature := range collection.Features {

		coords := feature.Geometry.FlatCoords()

		//fmt.Println(coords)
		//fmt.Println("\n Length of coords", len(coords))

		// For each coordinate, add a node to the graph
		for i := 0; i < len(coords); i += 2 {

			cnX := coords[i]
			cnY := coords[i+1]

			// Combine cnX and cnY into a single string and hash it to create a unique ID
			cnID := hash(strconv.Itoa(int(cnX)) + strconv.Itoa(int(cnY)))

			// Add the node to the graph
			node1 := &GeoNode{ID: cnID, Lat: cnX, Lng: cnY}
			g.AddNode(multi.Node(node1))

			//fmt.Printf("cnID: %d, cnX: %f, cnY: %f", cnID, cnX, cnY)

			// Check if node already exists in graph
			if g.Node(cnID) == nil {
				// Add node to graph
				g.AddNode(multi.Node(cnID))
			} else {
				//fmt.Println("Node already exists")
				g.Node(cnID)
			}

			nxCoords := coords[i+2:]
			//fmt.Println("\nLength of nxCoords: ", len(nxCoords))
			// Add edges to the graph edge adder
			for node := 2; node <= len(nxCoords); node += 2 {
				nxX := coords[i+node]
				nxY := coords[i+node+1]

				// Combine nxX and nxY into a single string and hash it to create a unique ID
				nxID := hash(strconv.Itoa(int(nxX)) + strconv.Itoa(int(nxY)))

				//fmt.Printf("\nnxID: %d, nxX: %f, nxY: %f", nxID, nxX, nxY)
				//Storing the node ID as a string
				g.From(cnID)
				g.To(nxID)

				// Calculate the distance between the two nodes
				point1 := geo.NewPoint(coords[i], coords[i+1])
				point2 := geo.NewPoint(coords[node], coords[node+1])
				dist := point1.GreatCircleDistance(point2)

				//fmt.Println("\tDistance:", dist)

				// Add the edge to the graph
				g.NewWeightedLine(multi.Node(cnID), multi.Node(nxID), dist)
				g.NewWeightedLine(multi.Node(nxID), multi.Node(cnID), dist)
			}
		}

	}

	return g
}

func hash(s string) int64 {
	h := fnv.New64a()
	_, err := h.Write([]byte(s))
	if err != nil {
		log.Fatal(err)
	}
	return int64(h.Sum64())
}

// Calculate the shortest distance between two coordinates
func shortestDistance(g graph.WeightedDirected, fromLat, fromLng, toLat, toLng float64) float64 {
	// Convert the from and to coordinates to points
	fromPoint := geo.NewPoint(fromLat, fromLng)
	toPoint := geo.NewPoint(toLat, toLng)

	// Find the nearest node to the from and to points
	fromNode := nearestNode(g, fromPoint)
	toNode := nearestNode(g, toPoint)

	// Convert toNode to int64
	toNodeInt64 := toNode.ID()

	// Use Dijkstra's algorithm to find the shortest path between the nodes
	p := path.DijkstraFrom(fromNode, g)

	// Return the distance of the shortest path
	return p.WeightTo(toNodeInt64)
}

// Find the nearest node to a point
func nearestNode(g graph.WeightedDirected, p *geo.Point) graph.Node {
	var nearestNode graph.Node
	minDistance := float64(1000000000)

	// Iterate over all nodes in the graph
	allNodes := g.Nodes()

	for allNodes.Next() {
		n := allNodes.Node()
		lat, lng, err := nodeCoords(n)
		if err != nil {
			log.Fatal(err)
		}

		point := geo.NewPoint(lat, lng)
		distance := point.GreatCircleDistance(p)

		if distance < minDistance {
			minDistance = distance
			nearestNode = n
		}
	}

	return nearestNode
}

// Get the latitude and longitude of a node
func nodeCoords(n graph.Node) (float64, float64, error) {
	lat, err := strconv.ParseFloat(strings.Split(fmt.Sprint(n), ",")[0], 64)
	if err != nil {
		return 0, 0, err
	}
	lng, err := strconv.ParseFloat(strings.Split(fmt.Sprint(n), ",")[1], 64)
	if err != nil {
		return 0, 0, err
	}
	return lat, lng, nil
}
