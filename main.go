package main

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	geo "github.com/kellydunn/golang-geo"
	gdj "github.com/pitchinnate/golangGeojsonDijkstra"
	"io"
	"log"
	"os"
	"strconv"
)

func main() {

	// Setting the logger
	f, err := os.OpenFile("temp/runtime.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
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

	// Set the router as the default one provided by Gin
	router := gin.Default()

	err = router.SetTrustedProxies(nil)
	if err != nil {
		log.Fatal(err)
	}

	// Serve html template files
	router.LoadHTMLGlob("web/templates/**/*.gohtml")
	// Load the static files
	router.Static("/static", "./web/static")

	// Setup route group for the API
	// Handle the index route
	router.GET("/", func(c *gin.Context) {
		c.HTML(200, "home.gohtml", gin.H{})
	})

	// Handle the about page
	router.GET("/about", func(c *gin.Context) {
		c.HTML(200, "about.gohtml", gin.H{})
	})

	// Handle request to calculate the passage
	router.POST("/waypoints", func(c *gin.Context) {

		var form map[string]string
		if err := c.Bind(&form); err != nil {
			// Handle error
			log.Println(err)
		}

		log.Println(form)

		originLongi := form["originLongitude"]
		originLati := form["originLatitude"]
		destinationLongi := form["destinationLongitude"]
		destinationLati := form["destinationLatitude"]

		// Convert all the waypoints to float64
		originLong, _ := strconv.ParseFloat(originLongi, 64)
		originLat, _ := strconv.ParseFloat(originLati, 64)
		destinationLong, _ := strconv.ParseFloat(destinationLongi, 64)
		destinationLat, _ := strconv.ParseFloat(destinationLati, 64)

		// Print the coordinates received from form
		log.Printf("Origin: %f, %f", originLong, originLat)
		log.Printf("Destination: %f, %f", destinationLong, destinationLat)

		// Get the origin coordinates
		originCoords := gdj.Position{originLong, originLat}
		// Get the destination coordinates
		destinationCoords := gdj.Position{destinationLong, destinationLat}
		// Set route name
		routeName := "my-route"

		// Call the function to calculate the passage
		data := calculatePassageInfo(originCoords, destinationCoords, routeName)

		// Send the data to the client
		c.JSON(200, data)

	})

	//Start and run the server if production environment
	if os.Getenv("APP_ENV") == "prod" {
		log.Println("Starting server in production environment")
		err = router.RunTLS(":443", os.Getenv("CERT_PATH"), os.Getenv("KEY_PATH"))
	} else {
		log.Println("Starting server in development environment")
		err = router.Run(":8080")
		if err != nil {
			log.Fatal(err)
		}
	}

	// Sample data for test Shanghai-New-York
	//var originCoords = gdj.Position{72.9301, 19.0519}
	//var destinationCoords = gdj.Position{-9.0905, 38.7062}
	//var routeName = "Mumbai-Lisbon"

	// Calculate the passage info
	//calculatePassageInfo(originCoords, destinationCoords, routeName)

}

// CalculatePassageInfo calculates the ocean waypoints and distance between two coordinates and generates a GeoJSON output
func calculatePassageInfo(originCoords, destinationCoords gdj.Position, routeName string) Output {
	// FC variable is the GeoJSON FeatureCollection
	var fc gdj.FeatureCollection
	var newFc gdj.FeatureCollection
	var data []byte
	var splitAvailable bool
	// Check if splitCoords.geojson exists
	if _, err := os.Stat("dataset/splitCoords.geojson"); os.IsNotExist(err) {
		// If not, read the original file
		log.Println("splitCoords.geojson does not exist. Reading original file.")
		data, err = os.ReadFile("dataset/marnet_densified_v2.geojson")
		if err != nil {
			log.Fatal(err)
		}
	} else {
		// If it does, read the splitCoords.geojson file
		splitAvailable = true
		log.Println("splitCoords.geojson exists. Reading splitCoords.geojson file.")
		data, err = os.ReadFile("dataset/splitCoords.geojson")
		if err != nil {
			log.Fatal(err)
		}
	}

	//Unmarshall feature collection from geojson
	err := json.Unmarshal(data, &fc)
	if err != nil {
		log.Fatal(err)
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
	output := generateOutput(path, originCoords, destinationCoords, totalDistance, distToFirstWp, distFromLastWp, distanceInKm, routeName)

	return output
}

// CalcDistance calculates the distance between two points in meters
func CalcDistance(p1, p2 gdj.Position) float64 {
	wp1 := geo.NewPoint(p1[1], p1[0])
	wp2 := geo.NewPoint(p2[1], p2[0])

	return wp1.GreatCircleDistance(wp2)
}
