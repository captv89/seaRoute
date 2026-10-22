package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/captv89/seaRoute/mcpserver"
	"github.com/captv89/seaRoute/route"
	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// App holds the application state
type App struct {
	PortData   []Port
	RouteGraph *route.Graph
}

// WaypointPoint represents an origin or destination specified by name or coordinate
type WaypointPoint struct {
	Port string   `json:"port,omitempty"`
	Lat  *float64 `json:"lat,omitempty"`
	Lon  *float64 `json:"lon,omitempty"`
}

// WaypointRequest represents a validated waypoint calculation request
type WaypointRequest struct {
	FromPort  string         `json:"fromPort,omitempty"`
	ToPort    string         `json:"toPort,omitempty"`
	From      *WaypointPoint `json:"from,omitempty"`
	To        *WaypointPoint `json:"to,omitempty"`
	Algorithm string         `json:"algorithm,omitempty"`
	// Avoid lists passages to keep out of, e.g. ["kiel", "suez"]. Optional.
	Avoid []string `json:"avoid,omitempty"`
	// Allow opens passages closed by default (the Arctic routes). Optional.
	Allow []string `json:"allow,omitempty"`
}

// Rate limiting state
var (
	rateLimitMu      sync.Mutex
	visitors         = make(map[string]*rateLimitEntry)
	nextVisitorSweep time.Time
)

type rateLimitEntry struct {
	count   int
	resetAt time.Time
}

func main() {
	loadDotEnv(".env")

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

	// Trust X-Forwarded-For only from the reverse proxy, so ClientIP() is the
	// real visitor and not the proxy. Override with TRUSTED_PROXIES (comma-separated).
	if err := router.SetTrustedProxies(trustedProxies()); err != nil {
		log.Fatalf("Error setting trusted proxies: %v", err)
	}

	// Add security middleware
	router.Use(securityHeadersMiddleware())

	// Serve html template files
	router.LoadHTMLGlob("web/templates/**/*.gohtml")
	// Load the static files
	router.Static("/static", "./web/static")

	// Register routes
	setupRoutes(router, app)

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

// resolvePoint extracts a position and display name from a WaypointPoint or fallback port name
func resolvePoint(p *WaypointPoint, fallbackPort string, app *App) (route.Position, string, error) {
	if p != nil && p.Lat != nil && p.Lon != nil {
		if math.IsNaN(*p.Lat) || math.IsNaN(*p.Lon) || math.IsInf(*p.Lat, 0) || math.IsInf(*p.Lon, 0) || math.Abs(*p.Lat) > 90 || math.Abs(*p.Lon) > 180 {
			return route.Position{}, "", errors.New("latitude or longitude out of range")
		}
		return route.Position{*p.Lon, *p.Lat}, fmt.Sprintf("%.2f, %.2f", *p.Lat, *p.Lon), nil
	}
	if p != nil && p.Port != "" {
		if len(p.Port) > 200 {
			return route.Position{}, "", errors.New("port name too long")
		}
		lon, lat, err := app.getPortCoordinates(p.Port)
		if err != nil {
			return route.Position{}, "", err
		}
		return route.Position{lon, lat}, p.Port, nil
	}
	if fallbackPort != "" {
		if len(fallbackPort) > 200 {
			return route.Position{}, "", errors.New("port name too long")
		}
		lon, lat, err := app.getPortCoordinates(fallbackPort)
		if err != nil {
			return route.Position{}, "", err
		}
		return route.Position{lon, lat}, fallbackPort, nil
	}
	return route.Position{}, "", errors.New("point must specify a port or latitude and longitude")
}

// setupRoutes registers all application HTTP routes on the given gin engine
func setupRoutes(router *gin.Engine, app *App) {
	// Template variables helper
	templateVars := func(c *gin.Context) gin.H {
		return gin.H{
			"cspNonce":     c.GetString(cspNonceKey),
			"owmApiKey":    os.Getenv("OWM_API_KEY"),
			"cartoApiKey":  os.Getenv("CARTO_API_KEY"),
			"gaTrackingID": os.Getenv("GA_TRACKING_ID"),
		}
	}

	// Handle the index route
	router.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "home.gohtml", templateVars(c))
	})
	router.HEAD("/", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Handle the about page
	router.GET("/about", func(c *gin.Context) {
		c.HTML(http.StatusOK, "about.gohtml", templateVars(c))
	})

	// API endpoints are rate limited per client IP; pages, static files and
	// cacheable map layers are not, so loading the map does not use up the budget.
	limit := rateLimitMiddleware()

	// Handle the search for ports
	router.GET("/ports", limit, func(c *gin.Context) {
		searchQuery := c.Query("search")
		// Input length validation
		if len(searchQuery) > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "search query too long"})
			return
		}
		filteredPorts := app.filterPorts(searchQuery)
		c.JSON(http.StatusOK, filteredPorts)
	})

	// Handle passages metadata
	router.GET("/passages", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"passages":          app.RouteGraph.Passages(),
			"closed_by_default": route.ClosedByDefault,
			"engine":            "seaRoute v2.0",
			"mesh":              "Marnet Mesh 2024.3",
			"nodes":             app.RouteGraph.Nodes(),
			"edges":             app.RouteGraph.Edges(),
			"ports_count":       len(app.PortData),
		})
	})

	// Serve the routing mesh GeoJSON
	router.GET("/network", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=86400")
		c.Header("Content-Type", "application/json")
		c.Header("Vary", "Accept-Encoding")
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
			c.File("dataset/marnet_plus_5km.geojson")
			return
		}
		data, err := networkGzip()
		if err != nil {
			c.File("dataset/marnet_plus_5km.geojson")
			return
		}
		c.Header("Content-Encoding", "gzip")
		c.Data(http.StatusOK, "application/json", data)
	})

	// Serve the official IMO ECA SOx / PM Emission Control Areas GeoJSON
	router.GET("/emissions", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=86400")
		c.Header("Content-Type", "application/json")
		c.Header("Vary", "Accept-Encoding")

		filePath := "dataset/eca_reg14_sox_pm.geojson"
		gzPath := filePath + ".gz"

		if strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
			if _, err := os.Stat(gzPath); err == nil {
				c.Header("Content-Encoding", "gzip")
				c.File(gzPath)
				return
			}
		}

		if _, err := os.Stat(filePath); err != nil {
			if _, gzErr := os.Stat(gzPath); gzErr == nil {
				gzFile, openErr := os.Open(gzPath)
				if openErr == nil {
					defer func() { _ = gzFile.Close() }()
					zr, zrErr := gzip.NewReader(gzFile)
					if zrErr == nil {
						defer func() { _ = zr.Close() }()
						_, _ = io.Copy(c.Writer, zr)
						return
					}
				}
			}
			c.JSON(http.StatusNotFound, gin.H{"error": "emissions dataset file unavailable"})
			return
		}

		c.File(filePath)
	})

	// Map of layer names to dataset files
	layerFiles := map[string]string{
		"baseline":   "dataset/eez_12nm_v3.opt.geojson",
		"eez":        "dataset/eez_v11.opt.geojson",
		"piracy":     "dataset/ASAM_events.geojson",
		"seaports":   "dataset/WPI.geojson",
		"eca-sox":    "dataset/eca_reg14_sox_pm.geojson",
		"eca-nox":    "dataset/eca_reg13_nox.geojson",
		"boundaries": "dataset/eez_boundaries_v11.geojson",
		"fao":        "dataset/FAO_AREAS_CWP.geojson",
	}
	layerFallbacks := map[string]string{
		"baseline": "dataset/eez_12nm_v3.geojson",
		"eez":      "dataset/eez_v11.geojson",
	}

	// Serve compressed or uncompressed GeoJSON layers
	router.GET("/layers/:name", func(c *gin.Context) {
		name := c.Param("name")
		filePath, ok := layerFiles[name]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "layer not found"})
			return
		}

		// Fallback if optimized file or its .gz doesn't exist yet
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			if _, gzErr := os.Stat(filePath + ".gz"); os.IsNotExist(gzErr) {
				if fb, exists := layerFallbacks[name]; exists {
					filePath = fb
				}
			}
		}

		c.Header("Cache-Control", "public, max-age=86400")
		c.Header("Content-Type", "application/json")
		c.Header("Vary", "Accept-Encoding")

		acceptsGzip := strings.Contains(c.GetHeader("Accept-Encoding"), "gzip")
		gzPath := filePath + ".gz"

		if acceptsGzip {
			if _, err := os.Stat(gzPath); err == nil {
				c.Header("Content-Encoding", "gzip")
				c.File(gzPath)
				return
			}
		}

		if _, err := os.Stat(filePath); err != nil {
			if _, gzErr := os.Stat(gzPath); gzErr == nil {
				gzFile, openErr := os.Open(gzPath)
				if openErr == nil {
					defer func() { _ = gzFile.Close() }()
					zr, zrErr := gzip.NewReader(gzFile)
					if zrErr == nil {
						defer func() { _ = zr.Close() }()
						_, _ = io.Copy(c.Writer, zr)
						return
					}
				}
			}
			c.JSON(http.StatusNotFound, gin.H{"error": "layer dataset file unavailable"})
			return
		}

		c.File(filePath)
	})

	// Handle request to calculate the passage
	router.POST("/waypoints", limit, func(c *gin.Context) {
		var req WaypointRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request format"})
			return
		}

		originCoords, fromName, err := resolvePoint(req.From, req.FromPort, app)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		destCoords, toName, err := resolvePoint(req.To, req.ToPort, app)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		routeName := fmt.Sprintf("%s -> %s", fromName, toName)

		alg := route.Dijkstra
		if strings.EqualFold(req.Algorithm, "astar") {
			alg = route.AStar
		}

		opts := route.Options{
			Avoid:     req.Avoid,
			Allow:     req.Allow,
			Algorithm: alg,
		}

		// Calculate the passage using the cached route graph
		data, err := calculatePassageInfo(originCoords, destCoords, routeName, app.RouteGraph, opts)
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

	// MCP endpoint: distance_to_go, route and list_passages for AI agents
	mcpServer := mcpserver.New(app.RouteGraph, func(name string) (route.Position, bool) {
		lon, lat, err := app.getPortCoordinates(name)
		return route.Position{lon, lat}, err == nil
	})
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, nil)
	router.Any("/mcp", limit, gin.WrapH(mcpHandler))
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

	return generateOutput(r, originCoords, destinationCoords, totalDistance, distToFirstWp, distFromLastWp, networkDistance, routeName, r.Passages), nil
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

// cspNonceKey is the gin context key holding the per-request CSP script nonce
const cspNonceKey = "cspNonce"

// cspPolicy builds the Content-Security-Policy for one response. Scripts run
// only with the nonce; 'strict-dynamic' lets those scripts load others (the
// cookie-consent tool injects Google Analytics). 'unsafe-inline' and https:
// are ignored by browsers that support nonces and only apply to old ones.
func cspPolicy(nonce string) string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'nonce-" + nonce + "' 'strict-dynamic' 'unsafe-inline' https:",
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://unpkg.com https://cdnjs.cloudflare.com https://www.privacypolicies.com",
		"font-src 'self' https://fonts.gstatic.com",
		"img-src 'self' data: blob: https:",
		"connect-src 'self' https://searoute.goatcounter.com https://*.google-analytics.com https://*.analytics.google.com https://www.googletagmanager.com https://www.privacypolicies.com",
		"object-src 'none'",
		"base-uri 'self'",
		"frame-ancestors 'none'",
		"form-action 'self'",
	}, "; ")
}

// newCSPNonce returns a random 128-bit nonce, URL-safe base64 so html/template
// writes it into the nonce attribute unchanged.
func newCSPNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// securityHeadersMiddleware adds security headers to all responses.
// CSP_MODE=report-only sends the CSP as Content-Security-Policy-Report-Only,
// a fallback if the enforced policy breaks something in production.
func securityHeadersMiddleware() gin.HandlerFunc {
	cspHeader := "Content-Security-Policy"
	if strings.EqualFold(os.Getenv("CSP_MODE"), "report-only") {
		cspHeader = "Content-Security-Policy-Report-Only"
	}
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		nonce, err := newCSPNonce()
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Set(cspNonceKey, nonce)
		c.Header(cspHeader, cspPolicy(nonce))
		c.Next()
	}
}

// rateLimitMiddleware implements a simple per-IP rate limiter (60 requests/minute)
func rateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		rateLimitMu.Lock()

		now := time.Now()
		// Drop expired entries now and then so the map does not grow without bound
		if now.After(nextVisitorSweep) {
			for k, e := range visitors {
				if now.After(e.resetAt) {
					delete(visitors, k)
				}
			}
			nextVisitorSweep = now.Add(time.Minute)
		}
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

// defaultTrustedProxies covers loopback and private networks, where a reverse
// proxy such as Caddy or a Docker bridge gateway connects from.
var defaultTrustedProxies = []string{"127.0.0.1/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"}

// trustedProxies returns the proxy CIDRs from TRUSTED_PROXIES, or the defaults.
// Set TRUSTED_PROXIES=none to ignore X-Forwarded-For entirely.
func trustedProxies() []string {
	v := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	if v == "" {
		return defaultTrustedProxies
	}
	if strings.EqualFold(v, "none") {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var (
	networkGzOnce sync.Once
	networkGzData []byte
	networkGzErr  error
)

// networkGzip returns the routing mesh GeoJSON gzip-compressed, built once and kept in memory.
func networkGzip() ([]byte, error) {
	networkGzOnce.Do(func() {
		raw, err := os.ReadFile("dataset/marnet_plus_5km.geojson")
		if err != nil {
			networkGzErr = err
			return
		}
		var buf bytes.Buffer
		zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if err != nil {
			networkGzErr = err
			return
		}
		if _, err := zw.Write(raw); err != nil {
			networkGzErr = err
			return
		}
		if err := zw.Close(); err != nil {
			networkGzErr = err
			return
		}
		networkGzData = buf.Bytes()
	})
	return networkGzData, networkGzErr
}

// loadDotEnv reads environment variables from a .env file if present
func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') {
			// Quoted value: keep everything inside the quotes, including '#'
			if end := strings.IndexByte(val[1:], val[0]); end != -1 {
				val = val[1 : end+1]
			}
		} else if idx := strings.Index(val, " #"); idx != -1 {
			// Strip comments following whitespace, e.g. "val #comment"; a bare
			// '#' inside a value such as an API key is kept
			val = strings.TrimSpace(val[:idx])
		}
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}
}
