package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
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
