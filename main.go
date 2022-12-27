package main

import (
	"encoding/json"
	geo "github.com/kellydunn/golang-geo"
	gdj "github.com/pitchinnate/golangGeojsonDijkstra"
	"io"
	"log"
	"os"
)

// Sample data for test Shanghai-New-York
var originCoords = gdj.Position{121.8, 31.2}
var destinationCoords = gdj.Position{-74.1, 40.7}

func main() {

	f, err := os.OpenFile("runtime.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()

	wrt := io.MultiWriter(os.Stdout, f)
	log.SetOutput(wrt)

	var fc gdj.FeatureCollection

	// Read the contents of the GeoJSON file into a byte slice
	data, err := os.ReadFile("marnet_densified_v2.geojson")
	if err != nil {
		log.Fatal(err)
	}

	//Unmarshall feature collection from geojson
	err = json.Unmarshal(data, &fc)
	if err != nil {
		return
	}

	//// Print the number of features in the collection
	//log.Printf("Number of features: %d", len(fc.Features))

	// Calculate the shortest path between two points
	path, distance, err := fc.FindPath(originCoords, destinationCoords, 0.00001)

	distanceInKm := distance / 1000

	// Get the first and last coordinates of the path
	lastWp := path[0]
	firstWp := path[len(path)-1]

	log.Println("First waypoint: ", firstWp)
	log.Println("Last waypoint: ", lastWp)

	// Calculate the gc distance between the origin coordinates and the first waypoint
	distToFirstWp := CalcDistance(originCoords, firstWp)
	// Calculate gc distance between the destination coordinates and the last waypoint
	distFromLastWp := CalcDistance(destinationCoords, lastWp)
	// Total distance of the path
	totalDistance := distToFirstWp + distanceInKm + distFromLastWp

	// Print the path and distance
	log.Printf("Waypoints: %v", path)
	log.Printf("Origin to First Waypoint: %f Km", distToFirstWp)
	log.Printf("Waypoint Distance: %f Km", distanceInKm)
	log.Printf("Last Waypoint to Destination: %f Km", distFromLastWp)
	log.Printf("Total Distance: %f Km", totalDistance)

}

// CalcDistance calculates the distance between two points in meters
func CalcDistance(p1, p2 gdj.Position) float64 {
	wp1 := geo.NewPoint(p1[1], p1[0])
	wp2 := geo.NewPoint(p2[1], p2[0])

	return wp1.GreatCircleDistance(wp2)
}
