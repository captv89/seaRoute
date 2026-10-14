package main

import (
	"context"
	"errors"
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

	"github.com/captv89/seaRoute/route"
	"github.com/gin-gonic/gin"
)

// App holds the application state
type App struct {
	PortData   []Port
	RouteGraph *route.Graph
}

// WaypointRequest represents a validated waypoint calculation request
type WaypointRequest struct {
	FromPort string `json:"fromPort" binding:"required"`
	ToPort   string `json:"toPort" binding:"required"`
	// Avoid lists passages to keep out of, e.g. ["kiel", "suez"]. Optional.
	Avoid []string `json:"avoid"`
	// Allow opens passages closed by default (the Arctic routes). Optional.
	Allow []string `json:"allow"`
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
	routeGraph, err := route.Load("dataset/marnet_plus_5km.geojson")
	if err != nil {
		log.Fatalf("Error loading route graph: %v", err)
	}
	app.RouteGraph = routeGraph
	log.Println("Route graph loaded")

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
			"cartoApiKey":  os.Getenv("CARTO_API_KEY"),
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

		originCoords := route.Position{originLong, originLat}
		destCoords := route.Position{destLong, destLat}
		routeName := fmt.Sprintf("%s -> %s", req.FromPort, req.ToPort)

		// Calculate the passage using the cached route graph
		data, err := calculatePassageInfo(originCoords, destCoords, routeName, app.RouteGraph, route.Options{Avoid: req.Avoid, Allow: req.Allow})
		if errors.Is(err, route.ErrUnknownPassage) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
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

// calculatePassageInfo calculates the ocean waypoints and distance between two coordinates
func calculatePassageInfo(originCoords, destinationCoords route.Position, routeName string, g *route.Graph, opts route.Options) (Output, error) {
	r, err := g.Route([]route.Position{originCoords, destinationCoords}, opts)
	if err != nil {
		return Output{}, fmt.Errorf("pathfinding failed: %w", err)
	}

	leg := r.Legs[0]
	totalDistance := leg.DistanceM / 1000
	networkDistance := leg.NetworkM / 1000
	// Connector lengths at each end, off the network
	distToFirstWp := route.Distance(originCoords, leg.Path[1]) / 1000
	distFromLastWp := route.Distance(leg.Path[len(leg.Path)-2], destinationCoords) / 1000

	log.Printf("Route: %s | Total: %.2f Km (Origin->WP: %.2f, WP: %.2f, WP->Dest: %.2f)",
		routeName, totalDistance, distToFirstWp, networkDistance, distFromLastWp)

	return generateOutput(r.Path, originCoords, destinationCoords, totalDistance, distToFirstWp, distFromLastWp, networkDistance, routeName, r.Passages), nil
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
