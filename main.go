package main

import (
	"encoding/json"
	geo "github.com/kellydunn/golang-geo"
	gdj "github.com/pitchinnate/golangGeojsonDijkstra"
	"io"
	"log"
	"os"
)

func main() {

	// Setting the logger
	f, err := os.OpenFile("runtime.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}

	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			log.Fatal(err)
		}
	}(f)

	wrt := io.MultiWriter(os.Stdout, f)
	log.SetOutput(wrt)

	// TODO: Initiate a web router and start the server

	// Sample data for test Shanghai-New-York
	var originCoords = gdj.Position{72.9301, 19.0519}
	var destinationCoords = gdj.Position{-9.0905, 38.7062}
	var routeName = "Mumbai-Lisbon"

	// Calculate the passage info
	calculatePassageInfo(originCoords, destinationCoords, routeName)

}

// CalculatePassageInfo calculates the ocean waypoints and distance between two coordinates and generates a GeoJSON output
func calculatePassageInfo(originCoords, destinationCoords gdj.Position, routeName string) {
	// FC variable is the GeoJSON FeatureCollection
	var fc gdj.FeatureCollection
	var newFc gdj.FeatureCollection
	var data []byte
	var splitAvailable bool
	// Check if splitCoords.geojson exists
	if _, err := os.Stat("splitCoords.geojson"); os.IsNotExist(err) {
		// If not, read the original file
		log.Println("splitCoords.geojson does not exist. Reading original file.")
		data, err = os.ReadFile("marnet_densified_v2.geojson")
		if err != nil {
			log.Fatal(err)
		}
	} else {
		// If it does, read the splitCoords.geojson file
		splitAvailable = true
		log.Println("splitCoords.geojson exists. Reading splitCoords.geojson file.")
		data, err = os.ReadFile("splitCoords.geojson")
		if err != nil {
			log.Fatal(err)
		}
	}

	//Unmarshall feature collection from geojson
	err := json.Unmarshal(data, &fc)
	if err != nil {
		return
	}

	//log.Println("Split file exists: ", splitAvailable)
	// Do not split if splitCoords.geojson exists
	if !splitAvailable {
		newFc = splitter(fc)
	} else {
		newFc = fc
	}
	//// Print the number of features in the collection
	//log.Printf("Number of features: %d", len(fc.Features))

	// Calculate the shortest path between two points
	path, distance, err := newFc.FindPath(originCoords, destinationCoords, 0.00001)

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

	//	Generate output geojson
	generateOutput(path, originCoords, destinationCoords, totalDistance, distToFirstWp, distFromLastWp, distanceInKm, routeName)
}

// CalcDistance calculates the distance between two points in meters
func CalcDistance(p1, p2 gdj.Position) float64 {
	wp1 := geo.NewPoint(p1[1], p1[0])
	wp2 := geo.NewPoint(p2[1], p2[0])

	return wp1.GreatCircleDistance(wp2)
}
