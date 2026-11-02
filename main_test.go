package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/captv89/seaRoute/route"
	"github.com/captv89/seaRoute/weather"
	"github.com/gin-gonic/gin"
)

var (
	testGraphOnce sync.Once
	testGraphVal  *route.Graph
	testGraphErr  error
)

// testGraph loads the full datasets (network, land, TSS, depth when built) once per test run.
func testGraph(t *testing.T) *route.Graph {
	t.Helper()
	testGraphOnce.Do(func() {
		d := route.Datasets{
			Network: "dataset/marnet_plus_5km.geojson",
			Land:    "dataset/land_osm.bin.gz",
			TSS:     "dataset/tss_osm.geojson.gz",
		}
		if _, err := os.Stat("dataset/depth_gebco.bin.gz"); err == nil {
			d.Depth = "dataset/depth_gebco.bin.gz"
		}
		testGraphVal, testGraphErr = route.LoadAll(d)
	})
	if testGraphErr != nil {
		t.Fatalf("failed to load route graph: %v", testGraphErr)
	}
	return testGraphVal
}

func setupTestRouter(t *testing.T) *gin.Engine {
	gin.SetMode(gin.TestMode)
	app := &App{}
	ports, err := LoadPorts()
	if err != nil {
		t.Fatalf("failed to load ports: %v", err)
	}
	app.PortData = ports
	app.RouteGraph = testGraph(t)

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
	// A port request names the first and last waypoints after the ports.
	if props.Waypoints[0].Name != "Rotterdam" {
		t.Errorf("first waypoint name should be Rotterdam, got %q", props.Waypoints[0].Name)
	}
	lastWP := props.Waypoints[len(props.Waypoints)-1]
	if lastWP.Name != "Singapore" {
		t.Errorf("last waypoint name should be Singapore, got %q", lastWP.Name)
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
		Edges           int      `json:"edges"`
		PortsCount      int      `json:"ports_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pData); err != nil {
		t.Fatalf("failed to decode passages json: %v", err)
	}
	if len(pData.Passages) == 0 {
		t.Error("expected non-empty passages list")
	}
	if pData.Edges <= 0 || pData.Edges == 118400 {
		t.Errorf("edges should be computed from the graph, got %d", pData.Edges)
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

	// /network is gzip-compressed for clients that accept it
	reqGz := httptest.NewRequest(http.MethodGet, "/network", nil)
	reqGz.Header.Set("Accept-Encoding", "gzip, deflate, br")
	wGz := httptest.NewRecorder()
	r.ServeHTTP(wGz, reqGz)
	if wGz.Code != http.StatusOK || wGz.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("GET /network gzip: status %d, encoding %q", wGz.Code, wGz.Header().Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(wGz.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	if !bytes.Equal(plain, wNet.Body.Bytes()) {
		t.Error("gzip /network body differs from uncompressed body")
	}
	if wGz.Body.Len() >= wNet.Body.Len()/2 {
		t.Errorf("gzip body %d bytes, uncompressed %d: expected strong compression", wGz.Body.Len(), wNet.Body.Len())
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
	want := map[string]string{
		"DOTENV_PLAIN":     "abc123",
		"DOTENV_COMMENT":   "abc123",
		"DOTENV_HASH":      "ab#c123",
		"DOTENV_DQUOTE":    "a b #c",
		"DOTENV_SQUOTE":    "xyz",
		"DOTENV_QUOTE_CMT": "q#1",
	}
	for k := range want {
		t.Setenv(k, "")
	}
	t.Setenv("DOTENV_PRESET", "kept")

	path := t.TempDir() + "/.env"
	content := `# comment line
DOTENV_PLAIN=abc123
DOTENV_COMMENT=abc123 # trailing comment
DOTENV_HASH=ab#c123
DOTENV_DQUOTE="a b #c"
DOTENV_SQUOTE='xyz'
DOTENV_QUOTE_CMT="q#1" # comment
DOTENV_PRESET=overwritten
not a pair
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	loadDotEnv(path)

	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if got := os.Getenv("DOTENV_PRESET"); got != "kept" {
		t.Errorf("existing env var overwritten: got %q", got)
	}
}

func TestTrustedProxies(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	if got := trustedProxies(); len(got) != len(defaultTrustedProxies) {
		t.Errorf("default: got %v", got)
	}
	t.Setenv("TRUSTED_PROXIES", "none")
	if got := trustedProxies(); got != nil {
		t.Errorf("none: got %v, want nil", got)
	}
	t.Setenv("TRUSTED_PROXIES", " 10.1.0.0/16 , 127.0.0.1 ")
	got := trustedProxies()
	if len(got) != 2 || got[0] != "10.1.0.0/16" || got[1] != "127.0.0.1" {
		t.Errorf("custom: got %v", got)
	}
}

func TestClientIPBehindProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := r.SetTrustedProxies(defaultTrustedProxies); err != nil {
		t.Fatal(err)
	}
	r.GET("/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })

	cases := []struct{ remote, xff, want string }{
		{"172.18.0.1:5000", "203.0.113.7", "203.0.113.7"},    // via proxy on the Docker bridge
		{"198.51.100.2:5000", "203.0.113.7", "198.51.100.2"}, // direct client cannot spoof
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/ip", nil)
		req.RemoteAddr = tc.remote
		req.Header.Set("X-Forwarded-For", tc.xff)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if got := w.Body.String(); got != tc.want {
			t.Errorf("remote %s: ClientIP = %s, want %s", tc.remote, got, tc.want)
		}
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
	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"frame-ancestors 'none'", "object-src 'none'", "'strict-dynamic'", "script-src 'nonce-"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q: %s", want, csp)
		}
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

func TestRateLimitScopedToAPI(t *testing.T) {
	r := setupTestRouter(t)
	testIP := "198.51.100.77:4000"

	// Map layers are not rate limited
	for i := range 70 {
		req := httptest.NewRequest(http.MethodGet, "/passages", nil)
		req.RemoteAddr = testIP
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("/passages request %d: got %d", i+1, w.Code)
		}
	}

	// The port search API is
	var last int
	for range 61 {
		req := httptest.NewRequest(http.MethodGet, "/ports?search=rot", nil)
		req.RemoteAddr = testIP
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		last = w.Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("61st /ports request: got %d, want 429", last)
	}
}

func TestCSPNonceInPages(t *testing.T) {
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

	r := gin.New()
	r.Use(securityHeadersMiddleware())
	r.LoadHTMLGlob("web/templates/**/*.gohtml")
	setupRoutes(r, app)

	nonceRe := regexp.MustCompile(`script-src 'nonce-([A-Za-z0-9_-]+)'`)
	seen := map[string]bool{}
	for _, path := range []string{"/", "/", "/about"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, w.Code)
		}
		m := nonceRe.FindStringSubmatch(w.Header().Get("Content-Security-Policy"))
		if m == nil {
			t.Fatalf("GET %s: no nonce in CSP", path)
		}
		nonce := m[1]
		if seen[nonce] {
			t.Errorf("nonce %s reused across requests", nonce)
		}
		seen[nonce] = true

		body := w.Body.String()
		scripts := strings.Count(body, "<script")
		tagged := strings.Count(body, `<script nonce="`+nonce+`"`)
		if scripts == 0 || scripts != tagged {
			t.Errorf("GET %s: %d of %d <script> tags carry the response nonce", path, tagged, scripts)
		}
	}
}

func TestCSPReportOnlyMode(t *testing.T) {
	t.Setenv("CSP_MODE", "report-only")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(securityHeadersMiddleware())
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if w.Header().Get("Content-Security-Policy") != "" {
		t.Error("enforcing CSP header set in report-only mode")
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy-Report-Only"), "'strict-dynamic'") {
		t.Error("missing Content-Security-Policy-Report-Only header")
	}
}

// postWaypoints sends a /waypoints request and returns the recorder.
func postWaypoints(t *testing.T, r *gin.Engine, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/waypoints", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestWaypointsDefaults(t *testing.T) {
	r := setupTestRouter(t)
	w := postWaypoints(t, r, map[string]any{"fromPort": "Rotterdam", "toPort": "Singapore"})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Features []struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"features"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	props := res.Features[0].Properties
	if string(props["clearance_nm"]) != "2" {
		t.Errorf("clearance_nm = %s, want 2", props["clearance_nm"])
	}
	for _, k := range []string{"stretches", "warnings", "hazards", "notices"} {
		if v := string(props[k]); !strings.HasPrefix(v, "[") {
			t.Errorf("%s = %s, want an array", k, v)
		}
	}
}

func TestWaypointsClearanceRange(t *testing.T) {
	r := setupTestRouter(t)
	for _, tc := range []struct {
		c    float64
		want int
	}{{0.4, 400}, {10.5, 400}, {0.5, 200}, {10, 200}} {
		w := postWaypoints(t, r, map[string]any{"fromPort": "Rotterdam", "toPort": "Hamburg", "clearance_nm": tc.c})
		if w.Code != tc.want {
			t.Errorf("clearance_nm %v: status %d, want %d", tc.c, w.Code, tc.want)
		}
	}
}

func TestWaypointsTotalsMatchPath(t *testing.T) {
	r := setupTestRouter(t)
	w := postWaypoints(t, r, map[string]any{"fromPort": "Rotterdam", "toPort": "Singapore"})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var res struct {
		Features []struct {
			Properties struct {
				TotalDist float64 `json:"total_dist"`
				Waypoints []struct {
					LegDistanceM float64 `json:"leg_distance_m"`
					Flag         string  `json:"flag"`
				} `json:"waypoints"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	p := res.Features[0].Properties
	var sum float64
	for _, wp := range p.Waypoints {
		sum += wp.LegDistanceM
	}
	// total_dist is in km, leg distances in metres
	if d := sum - p.TotalDist*1000; d > 1 || d < -1 {
		t.Errorf("sum of legs %.1f m, total_dist %.1f m", sum, p.TotalDist*1000)
	}
	if p.Waypoints[0].Flag != "connector" {
		t.Errorf("origin flag = %q, want connector", p.Waypoints[0].Flag)
	}
	for i, wp := range p.Waypoints {
		if wp.Flag == "" {
			t.Errorf("waypoint %d has no flag", i)
		}
	}
}

func TestLayersTSS(t *testing.T) {
	r := gin.New()
	setupRoutes(r, &App{})
	req := httptest.NewRequest(http.MethodGet, "/layers/tss", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if ce := w.Header().Get("Content-Encoding"); ce != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip", ce)
	}
}

func TestWaypointsRejectsNoKinkFields(t *testing.T) {
	r := setupTestRouter(t)
	w := postWaypoints(t, r, map[string]any{"fromPort": "Rotterdam", "toPort": "Hamburg", "accept": []string{"k-1"}})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Features []struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"features"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	props := res.Features[0].Properties
	for _, k := range []string{"proposals", "ignored_accept", "kinks"} {
		if _, ok := props[k]; ok {
			t.Errorf("response still has %q", k)
		}
	}
}

func TestWaypointsVesselDefaults(t *testing.T) {
	r := setupTestRouter(t)
	w := postWaypoints(t, r, map[string]any{"fromPort": "Rotterdam", "toPort": "Hamburg"})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Features []struct {
			Properties struct {
				Depth   route.DepthLimit `json:"depth"`
				Notices []route.Notice   `json:"notices"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	p := res.Features[0].Properties
	if p.Depth.DraughtM != 10 || math.Abs(p.Depth.LimitM-13.568) > 1e-6 {
		t.Errorf("depth = %+v, want draught 10, limit 13.568", p.Depth)
	}
	// With the real GEBCO grid the Dutch coast may add a reroute notice; the
	// default vessel must still have a route deep enough (the Elbe fairway is
	// below the grid's resolution and must not close it).
	if p.Notices == nil {
		t.Error("notices is null, want an array")
	}
	for _, n := range p.Notices {
		if n.Kind == "draught-no-route" {
			t.Errorf("default vessel has no route deep enough: %+v", n)
		}
	}
}

func TestWaypointsVesselValidation(t *testing.T) {
	r := setupTestRouter(t)
	for _, tc := range []struct {
		field string
		val   any
		want  string
	}{
		{"draught_m", 30.0, "draught_m must be between 1 and 25"},
		{"draught_m", 0.0, "draught_m must be between 1 and 25"},
		{"speed_kn", 0.0, "speed_kn must be between 1 and 30"},
		{"vessel_type", "ferry", "vessel_type must be one of"},
	} {
		w := postWaypoints(t, r, map[string]any{"fromPort": "Rotterdam", "toPort": "Hamburg", tc.field: tc.val})
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), tc.want) {
			t.Errorf("%s=%v: status %d body %s, want 400 containing %q", tc.field, tc.val, w.Code, w.Body.String(), tc.want)
		}
	}
}

func TestResolvePointNames(t *testing.T) {
	app := &App{PortData: []Port{{Country: "Japan", Port: "Yokohama", Latitude: 35.4576, Longitude: 139.6345}}}
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		name  string
		p     WaypointPoint
		want  string
		named bool
	}{
		{"client label", WaypointPoint{Lat: f(1.26), Lon: f(103.82), Name: " Singapore "}, "Singapore", true},
		{"on a known port", WaypointPoint{Lat: f(35.46), Lon: f(139.63)}, "Yokohama", true},
		{"open sea", WaypointPoint{Lat: f(30), Lon: f(150)}, "30.00, 150.00", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, got, named, err := resolvePoint(&tc.p, "", app)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want || named != tc.named {
				t.Errorf("got %q named=%v, want %q named=%v", got, named, tc.want, tc.named)
			}
		})
	}
	long := WaypointPoint{Lat: f(0), Lon: f(0), Name: strings.Repeat("x", maxPointNameLen+1)}
	if _, _, _, err := resolvePoint(&long, "", app); err == nil {
		t.Error("over-long name accepted")
	}
}

// weatherRunTime is the reference time of the synthetic forecast run in tests.
var weatherRunTime = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

// setupWeatherRouter builds a router whose App has a weather store over a
// temp dir holding one synthetic run with a uniform 5 m/s westerly wind.
func setupWeatherRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	id := weather.RunID(weatherRunTime)
	grid := weather.Grid{NX: 4, NY: 3, Lat0: 10, Lon0: 20, DLat: -1, DLon: 1}
	steps := []int{0, 3}
	w, err := weather.CreateRun(filepath.Join(dir, "ecmwf-"+id+".bin"), weatherRunTime, grid, steps)
	if err != nil {
		t.Fatal(err)
	}
	for s := range steps {
		for _, f := range []weather.FieldID{weather.U10, weather.V10} {
			vals := make([]float32, grid.NX*grid.NY)
			if f == weather.U10 {
				for i := range vals {
					vals[i] = 5
				}
			}
			if err := w.Put(f, s, vals); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := weather.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{Weather: store, Tiles: weather.NewTiles(1<<20, 2), RouteGraph: testGraph(t)}
	router := gin.New()
	setupRoutes(router, app)
	return router, id
}

func postVoyage(r *gin.Engine, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if s, ok := body.(string); ok {
		buf.WriteString(s)
	} else {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(http.MethodPost, "/voyage/weather", &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func voyageBody(speed float64) map[string]any {
	return map[string]any{
		"track":     [][2]float64{{20.5, 10}, {20.5, 9}},
		"departure": weatherRunTime.Format(time.RFC3339),
		"speed_kn":  speed,
	}
}

func TestVoyageWeatherNoRun(t *testing.T) {
	r := setupTestRouter(t)
	w := postVoyage(r, voyageBody(12))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var v struct {
		Run     *string          `json:"run"`
		ETA     string           `json:"eta"`
		Samples []map[string]any `json:"samples"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Run != nil || v.ETA == "" || len(v.Samples) == 0 {
		t.Errorf("run %v eta %q samples %d", v.Run, v.ETA, len(v.Samples))
	}
}

func TestVoyageWeatherWithRun(t *testing.T) {
	r, id := setupWeatherRouter(t)
	w := postVoyage(r, voyageBody(30))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var v struct {
		Run     *string          `json:"run"`
		Samples []map[string]any `json:"samples"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Run == nil || *v.Run != id {
		t.Fatalf("run %v, want %s", v.Run, id)
	}
	withWeather := 0
	for _, s := range v.Samples {
		if s["weather"] != nil {
			withWeather++
		}
	}
	if withWeather == 0 {
		t.Error("no sample has weather")
	}
}

func TestVoyageWeatherBadInput(t *testing.T) {
	r := setupTestRouter(t)
	if w := postVoyage(r, voyageBody(50)); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"error"`) {
		t.Errorf("speed 50: status %d body %s", w.Code, w.Body)
	}
	if w := postVoyage(r, "{not json"); w.Code != http.StatusBadRequest {
		t.Errorf("bad JSON: status %d", w.Code)
	}
	big := `{"track":[],"pad":"` + strings.Repeat("a", 2<<20) + `"}`
	if w := postVoyage(r, big); w.Code != http.StatusRequestEntityTooLarge && w.Code != http.StatusBadRequest {
		t.Errorf("big body: status %d", w.Code)
	}
}

func TestTileOldRun404(t *testing.T) {
	r, id := setupWeatherRouter(t)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	if w := get("/weather/tile/1999010100/wind/0/0/0/0.png"); w.Code != http.StatusNotFound {
		t.Errorf("old run: status %d", w.Code)
	}
	for _, bad := range []string{"/wind/0/0/0/0", "/wind/x/0/0/0.png", "/nope/0/0/0/0.png", "/wind/1/0/0/0.png"} {
		if w := get("/weather/tile/" + id + bad); w.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", bad, w.Code)
		}
	}
	w := get("/weather/tile/" + id + "/wind/0/0/0/0.png")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("current run: status %d type %q", w.Code, w.Header().Get("Content-Type"))
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=86400, immutable" {
		t.Errorf("Cache-Control %q", cc)
	}
	// Weather off: tiles are 404.
	off := setupTestRouter(t)
	w2 := httptest.NewRecorder()
	off.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/weather/tile/"+id+"/wind/0/0/0/0.png", nil))
	if w2.Code != http.StatusNotFound {
		t.Errorf("weather off: status %d", w2.Code)
	}
}

func TestPassagesWeatherStatus(t *testing.T) {
	r := setupTestRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/passages", nil))
	var body struct {
		Weather map[string]any `json:"weather"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Weather) != 1 || body.Weather["enabled"] != false {
		t.Errorf("weather = %v, want {enabled:false}", body.Weather)
	}
	r2, id := setupWeatherRouter(t)
	w = httptest.NewRecorder()
	r2.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/passages", nil))
	body.Weather = nil
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Weather["enabled"] != true || body.Weather["run"] != id {
		t.Errorf("weather = %v", body.Weather)
	}
}
