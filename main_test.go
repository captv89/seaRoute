package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/captv89/seaRoute/route"
	"github.com/gin-gonic/gin"
)

func setupTestRouter(t *testing.T) *gin.Engine {
	gin.SetMode(gin.TestMode)
	app := &App{}
	ports, err := LoadPorts()
	if err != nil {
		t.Fatalf("failed to load ports: %v", err)
	}
	app.PortData = ports
	rg, err := route.Load("dataset/marnet_plus_5km.geojson")
	if err != nil {
		t.Fatalf("failed to load route graph: %v", err)
	}
	app.RouteGraph = rg

	router := gin.New()
	setupRoutes(router, app)
	return router
}

func TestWaypointRequest_LegacyAndCoordinates(t *testing.T) {
	r := setupTestRouter(t)

	cases := []struct {
		name       string
		payload    map[string]any
		wantStatus int
	}{
		{
			name: "legacy port names",
			payload: map[string]any{
				"fromPort": "Rotterdam",
				"toPort":   "Singapore",
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "coordinate points",
			payload: map[string]any{
				"from": map[string]any{"lat": 51.95, "lon": 4.05},
				"to":   map[string]any{"lat": 1.26, "lon": 103.82},
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "mixed point types",
			payload: map[string]any{
				"from": map[string]any{"port": "Rotterdam"},
				"to":   map[string]any{"lat": 1.26, "lon": 103.82},
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "astar algorithm",
			payload: map[string]any{
				"fromPort":  "Rotterdam",
				"toPort":    "Singapore",
				"algorithm": "astar",
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "invalid latitude out of bounds",
			payload: map[string]any{
				"from": map[string]any{"lat": 95.0, "lon": 4.05},
				"to":   map[string]any{"port": "Singapore"},
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := json.Marshal(tc.payload)
			req := httptest.NewRequest(http.MethodPost, "/waypoints", bytes.NewReader(b))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("got status %d, want %d. Body: %s", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

func TestWaypointRequest_EnrichedOutput(t *testing.T) {
	r := setupTestRouter(t)
	payload := map[string]any{
		"fromPort": "Rotterdam",
		"toPort":   "Singapore",
	}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/waypoints", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}

	var res struct {
		Type     string `json:"type"`
		Features []struct {
			Properties struct {
				TotalDist   float64 `json:"total_dist"`
				TotalDistNM float64 `json:"total_dist_nm"`
				TotalDistMI float64 `json:"total_dist_mi"`
				NodeCount   int     `json:"node_count"`
				Waypoints   []struct {
					Index         int            `json:"index"`
					Name          string         `json:"name"`
					Coordinates   route.Position `json:"coordinates"`
					LegDistanceNM float64        `json:"leg_distance_nm"`
					CumDistanceNM float64        `json:"cum_distance_nm"`
					Passage       string         `json:"passage,omitempty"`
				} `json:"waypoints"`
			} `json:"properties"`
		} `json:"features"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(res.Features) == 0 {
		t.Fatal("expected at least one feature")
	}
	props := res.Features[0].Properties
	if props.TotalDistNM <= 0 || props.TotalDistMI <= 0 {
		t.Errorf("expected positive TotalDistNM and TotalDistMI, got NM=%.2f MI=%.2f", props.TotalDistNM, props.TotalDistMI)
	}
	if len(props.Waypoints) < 2 {
		t.Fatalf("expected at least 2 waypoints in manifest, got %d", len(props.Waypoints))
	}
	if props.Waypoints[0].CumDistanceNM != 0 {
		t.Errorf("first waypoint cumulative distance should be 0, got %.2f", props.Waypoints[0].CumDistanceNM)
	}
	if props.NodeCount != len(props.Waypoints) {
		t.Errorf("node count %d does not match waypoints length %d", props.NodeCount, len(props.Waypoints))
	}
	if props.Waypoints[0].Name != "Origin" {
		t.Errorf("first waypoint name should be Origin, got %q", props.Waypoints[0].Name)
	}
	lastWP := props.Waypoints[len(props.Waypoints)-1]
	if lastWP.Name != "Destination" {
		t.Errorf("last waypoint name should be Destination, got %q", lastWP.Name)
	}
	if lastWP.CumDistanceNM <= 0 {
		t.Errorf("last waypoint cumulative distance should be positive, got %.2f", lastWP.CumDistanceNM)
	}
}

func TestPassagesAndNetworkEndpoints(t *testing.T) {
	r := setupTestRouter(t)

	// Test /passages
	req := httptest.NewRequest(http.MethodGet, "/passages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /passages failed: %d", w.Code)
	}

	var pData struct {
		Passages        []string `json:"passages"`
		ClosedByDefault []string `json:"closed_by_default"`
		Engine          string   `json:"engine"`
		Nodes           int      `json:"nodes"`
		PortsCount      int      `json:"ports_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pData); err != nil {
		t.Fatalf("failed to decode passages json: %v", err)
	}
	if len(pData.Passages) == 0 {
		t.Error("expected non-empty passages list")
	}
	if pData.Nodes <= 0 || pData.PortsCount <= 0 {
		t.Errorf("invalid nodes (%d) or portsCount (%d)", pData.Nodes, pData.PortsCount)
	}

	// Test /network
	reqNet := httptest.NewRequest(http.MethodGet, "/network", nil)
	wNet := httptest.NewRecorder()
	r.ServeHTTP(wNet, reqNet)
	if wNet.Code != http.StatusOK {
		t.Fatalf("GET /network failed: %d", wNet.Code)
	}
	if cc := wNet.Header().Get("Cache-Control"); cc == "" {
		t.Error("expected Cache-Control header on /network")
	}

	// Test /emissions
	reqEmissions := httptest.NewRequest(http.MethodGet, "/emissions", nil)
	wEmissions := httptest.NewRecorder()
	r.ServeHTTP(wEmissions, reqEmissions)
	if wEmissions.Code != http.StatusOK {
		t.Fatalf("GET /emissions failed: %d", wEmissions.Code)
	}
	if cc := wEmissions.Header().Get("Cache-Control"); cc == "" {
		t.Error("expected Cache-Control header on /emissions")
	}
}

func TestLoadDotEnv(t *testing.T) {
	loadDotEnv(".env")
	// If .env contains CARTO_API_KEY, verify it was loaded
	cartoKey := os.Getenv("CARTO_API_KEY")
	if cartoKey == "" {
		t.Log("No CARTO_API_KEY in environment or .env")
	} else if strings.Contains(cartoKey, "#") {
		t.Errorf("CARTO_API_KEY should not contain inline comments: %q", cartoKey)
	}
}

func TestLayersEndpoint(t *testing.T) {
	app := &App{}
	r := gin.Default()
	setupRoutes(r, app)

	layers := []string{
		"baseline",
		"eez",
		"piracy",
		"seaports",
		"eca-sox",
		"eca-nox",
		"boundaries",
		"fao",
	}

	for _, layer := range layers {
		// With gzip
		req := httptest.NewRequest(http.MethodGet, "/layers/"+layer, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /layers/%s with gzip failed: code %d", layer, w.Code)
		}
		if cc := w.Header().Get("Cache-Control"); cc == "" {
			t.Errorf("expected Cache-Control on /layers/%s", layer)
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("expected Content-Type application/json on /layers/%s, got %s", layer, ct)
		}

		// Without gzip (decompression fallback when uncompressed file absent)
		reqNoGz := httptest.NewRequest(http.MethodGet, "/layers/"+layer, nil)
		wNoGz := httptest.NewRecorder()
		r.ServeHTTP(wNoGz, reqNoGz)

		if wNoGz.Code != http.StatusOK {
			t.Errorf("GET /layers/%s without gzip failed: code %d", layer, wNoGz.Code)
		}
	}

	// Test invalid layer returns 404
	reqInv := httptest.NewRequest(http.MethodGet, "/layers/unknown-layer", nil)
	wInv := httptest.NewRecorder()
	r.ServeHTTP(wInv, reqInv)
	if wInv.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown layer, got %d", wInv.Code)
	}
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(securityHeadersMiddleware())
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("expected nosniff, got %s", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("expected DENY, got %s", got)
	}
	if got := w.Header().Get("X-XSS-Protection"); got != "1; mode=block" {
		t.Errorf("expected 1; mode=block, got %s", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("expected strict-origin-when-cross-origin, got %s", got)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(rateLimitMiddleware())
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	testIP := "198.51.100.99:12345"

	// 60 requests should pass
	for i := 0; i < 60; i++ {
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.RemoteAddr = testIP
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d expected 200, got %d", i+1, w.Code)
		}
	}

	// 61st request should be rate-limited (429)
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.RemoteAddr = testIP
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d", w.Code)
	}
}





