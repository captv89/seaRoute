package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	geo "github.com/kellydunn/golang-geo"
	gdj "github.com/pitchinnate/golangGeojsonDijkstra"
)

// App holds the application state
type App struct {
	PortData   []Port
	RouteGraph gdj.FeatureCollection
}

// WaypointRequest represents a validated waypoint calculation request
type WaypointRequest struct {
	FromPort string `json:"fromPort" binding:"required"`
	ToPort   string `json:"toPort" binding:"required"`
}

// Rate limiting state
var (
	rateLimitMu sync.Mutex
	visitors    = make(map[string]*rateLimitEntry)
)

type rateLimitEntry struct {
	count   int
	resetAt time.Time
}

func main() {
	// Create a temp folder with secure permissions
	if _, err := os.Stat("temp"); os.IsNotExist(err) {
		if err := os.MkdirAll("temp", 0700); err != nil {
			log.Fatalf("Failed to create temp directory: %v", err)
		}
	}

	// Setting the logger
	f, err := os.OpenFile("temp/runtime.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		log.Fatalf("Error opening log file: %v", err)
	}
	defer func(f *os.File) {
		if err := f.Close(); err != nil {
			log.Printf("Error closing log file: %v", err)
		}
	}(f)

	wrt := io.MultiWriter(os.Stdout, f)
	log.SetOutput(wrt)

	// Initialize app state
	app := &App{}

	// Load the port data
	ports, err := LoadPorts()
	if err != nil {
		log.Fatalf("Error loading port data: %v", err)
	}
	app.PortData = ports
	log.Printf("Loaded %d ports", len(app.PortData))

	// Pre-load and cache the route graph at startup
	routeGraph, err := loadRouteGraph()
	if err != nil {
		log.Fatalf("Error loading route graph: %v", err)
	}
	app.RouteGraph = routeGraph
	log.Printf("Route graph loaded with %d features", len(app.RouteGraph.Features))

	// Set the router as the default one provided by Gin
	router := gin.Default()

	if err := router.SetTrustedProxies(nil); err != nil {
		log.Fatalf("Error setting trusted proxies: %v", err)
	}

	// Add security middleware
	router.Use(securityHeadersMiddleware())
	router.Use(rateLimitMiddleware())

	// Serve html template files
	router.LoadHTMLGlob("web/templates/**/*.gohtml")
	// Load the static files
	router.Static("/static", "./web/static")

	// Template variables helper
	templateVars := func() gin.H {
		return gin.H{
			"owmApiKey":    os.Getenv("OWM_API_KEY"),
			"gaTrackingID": os.Getenv("GA_TRACKING_ID"),
		}
	}

	// Handle the index route
	router.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "home.gohtml", templateVars())
	})
	router.HEAD("/", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Handle the about page
	router.GET("/about", func(c *gin.Context) {
		c.HTML(http.StatusOK, "about.gohtml", templateVars())
	})

	// Handle the search for ports
	router.GET("/ports", func(c *gin.Context) {
		searchQuery := c.Query("search")
		// Input length validation
		if len(searchQuery) > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "search query too long"})
			return
		}
		filteredPorts := app.filterPorts(searchQuery)
		c.JSON(http.StatusOK, filteredPorts)
	})

	// Handle request to calculate the passage
	router.POST("/waypoints", func(c *gin.Context) {
		var req WaypointRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: fromPort and toPort are required"})
			return
		}

		// Input length validation
		if len(req.FromPort) > 200 || len(req.ToPort) > 200 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "port name too long"})
			return
		}

		// Validate that ports exist
		originLong, originLat, err := app.getPortCoordinates(req.FromPort)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("origin port not found: %s", req.FromPort)})
			return
		}

		destLong, destLat, err := app.getPortCoordinates(req.ToPort)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("destination port not found: %s", req.ToPort)})
			return
		}

		originCoords := gdj.Position{originLong, originLat}
		destCoords := gdj.Position{destLong, destLat}
		routeName := fmt.Sprintf("%s -> %s", req.FromPort, req.ToPort)

		// Calculate the passage using the cached route graph
		data, err := calculatePassageInfo(originCoords, destCoords, routeName, app.RouteGraph)
		if err != nil {
			log.Printf("Error calculating passage for %s: %v", routeName, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to calculate route"})
			return
		}

		c.JSON(http.StatusOK, data)
	})

	// Start server with graceful shutdown
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		if os.Getenv("APP_ENV") == "prod" {
			log.Printf("Starting server in production environment on port %s", port)
			if err := srv.ListenAndServeTLS(os.Getenv("CERT_PATH"), os.Getenv("KEY_PATH")); err != nil && err != http.ErrServerClosed {
				log.Fatalf("TLS server error: %v", err)
			}
		} else {
			log.Printf("Starting server in development environment on port %s", port)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Server error: %v", err)
			}
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("Server exited gracefully")
}

// loadRouteGraph loads and caches the GeoJSON route graph at startup
func loadRouteGraph() (gdj.FeatureCollection, error) {
	var fc gdj.FeatureCollection
	var data []byte
	var err error

	// Check if splitCoords.geojson exists
	if _, statErr := os.Stat("dataset/splitCoords.geojson"); os.IsNotExist(statErr) {
		log.Println("splitCoords.geojson does not exist. Reading original file.")
		data, err = os.ReadFile("dataset/marnet_densified_v2.geojson")
		if err != nil {
			return fc, fmt.Errorf("failed to read route data: %w", err)
		}

		if err := json.Unmarshal(data, &fc); err != nil {
			return fc, fmt.Errorf("failed to parse route data: %w", err)
		}

		// Split the coordinates for better pathfinding
		fc = splitter(fc)
	} else {
		log.Println("splitCoords.geojson exists. Reading splitCoords.geojson file.")
		data, err = os.ReadFile("dataset/splitCoords.geojson")
		if err != nil {
			return fc, fmt.Errorf("failed to read split route data: %w", err)
		}

		if err := json.Unmarshal(data, &fc); err != nil {
			return fc, fmt.Errorf("failed to parse split route data: %w", err)
		}
	}

	return fc, nil
}

// calculatePassageInfo calculates the ocean waypoints and distance between two coordinates
func calculatePassageInfo(originCoords, destinationCoords gdj.Position, routeName string, fc gdj.FeatureCollection) (Output, error) {
	path, distance, err := fc.FindPath(originCoords, destinationCoords, 0.00001)
	if err != nil {
		return Output{}, fmt.Errorf("pathfinding failed: %w", err)
	}

	if len(path) == 0 {
		return Output{}, fmt.Errorf("no path found between the given coordinates")
	}

	distanceInKm := distance / 1000

	// Get the first and last coordinates of the path
	lastWp := path[0]
	firstWp := path[len(path)-1]

	// Calculate the gc distance between the origin coordinates and the first waypoint
	distToFirstWp := CalcDistance(originCoords, firstWp)
	// Calculate gc distance between the destination coordinates and the last waypoint
	distFromLastWp := CalcDistance(destinationCoords, lastWp)
	// Total distance of the path
	totalDistance := distToFirstWp + distanceInKm + distFromLastWp

	log.Printf("Route: %s | Total: %.2f Km (Origin->WP: %.2f, WP: %.2f, WP->Dest: %.2f)",
		routeName, totalDistance, distToFirstWp, distanceInKm, distFromLastWp)

	// Generate output geojson
	output := generateOutput(path, originCoords, destinationCoords, totalDistance, distToFirstWp, distFromLastWp, distanceInKm, routeName)

	return output, nil
}

// CalcDistance calculates the great circle distance between two points in kilometers
func CalcDistance(p1, p2 gdj.Position) float64 {
	wp1 := geo.NewPoint(p1[1], p1[0])
	wp2 := geo.NewPoint(p2[1], p2[0])

	return wp1.GreatCircleDistance(wp2)
}

// filterPorts filters the ports from the []Port struct
func (app *App) filterPorts(query string) []Port {
	filteredPorts := make([]Port, 0)
	for _, port := range app.PortData {
		if strings.Contains(strings.ToLower(port.Port), strings.ToLower(query)) || strings.Contains(strings.ToLower(port.Country), strings.ToLower(query)) {
			filteredPorts = append(filteredPorts, port)
		}
	}
	return filteredPorts
}

// getPortCoordinates returns the coordinates of a port or an error if not found
func (app *App) getPortCoordinates(portName string) (float64, float64, error) {
	for _, port := range app.PortData {
		if port.Port == portName {
			return port.Longitude, port.Latitude, nil
		}
	}
	return 0, 0, fmt.Errorf("port not found: %s", portName)
}

// securityHeadersMiddleware adds security headers to all responses
func securityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		c.Next()
	}
}

// rateLimitMiddleware implements a simple per-IP rate limiter (60 requests/minute)
func rateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		rateLimitMu.Lock()

		now := time.Now()
		entry, exists := visitors[ip]
		if !exists || now.After(entry.resetAt) {
			visitors[ip] = &rateLimitEntry{count: 1, resetAt: now.Add(time.Minute)}
			rateLimitMu.Unlock()
			c.Next()
			return
		}

		entry.count++
		if entry.count > 60 {
			rateLimitMu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded, try again later"})
			return
		}

		rateLimitMu.Unlock()
		c.Next()
	}
}
