package mcpserver

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/captv89/seaRoute/route"
)

var ports = map[string]route.Position{
	"Rotterdam": {4.4424468168432654, 51.904383352111097},
	"Gdynia":    {18.546982540197764, 54.538336522499797},
}

const depthFile = "../dataset/depth_gebco.bin.gz"

var (
	fullOnce  sync.Once
	fullGraph *route.Graph
	fullErr   error
)

// fullSession serves a graph with land and TSS, so hazards can occur.
func fullSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	fullOnce.Do(func() {
		d := route.Datasets{
			Network: "../dataset/marnet_plus_5km.geojson",
			Land:    "../dataset/land_osm.bin.gz",
			TSS:     "../dataset/tss_osm.geojson.gz",
		}
		if _, err := os.Stat(depthFile); err == nil {
			d.Depth = depthFile
		}
		fullGraph, fullErr = route.LoadAll(d)
	})
	if fullErr != nil {
		t.Fatal(fullErr)
	}
	return sessionOn(t, fullGraph)
}

func session(t *testing.T) *mcp.ClientSession {
	t.Helper()
	g, err := route.Load("../dataset/marnet_plus_5km.geojson")
	if err != nil {
		t.Fatal(err)
	}
	return sessionOn(t, g)
}

func sessionOn(t *testing.T, g *route.Graph) *mcp.ClientSession {
	t.Helper()
	srv := New(g, func(name string) (route.Position, bool) { p, ok := ports[name]; return p, ok })
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if out != nil && !res.IsError {
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatal(err)
		}
	}
	return res
}

func TestDistanceToGoPath(t *testing.T) {
	cs := session(t)
	var plain, withPath distanceOut
	args := map[string]any{"from": map[string]any{"port": "Rotterdam"}, "to": map[string]any{"port": "Gdynia"}}
	call(t, cs, "distance_to_go", args, &plain)
	args["include_path"] = true
	call(t, cs, "distance_to_go", args, &withPath)
	if len(plain.Path) != 0 {
		t.Errorf("path returned without include_path: %d points", len(plain.Path))
	}
	if len(withPath.Path) < 2 || withPath.DistanceNM != plain.DistanceNM {
		t.Fatalf("include_path: %d points, %.1f nm; want a path and %.1f nm", len(withPath.Path), withPath.DistanceNM, plain.DistanceNM)
	}
	first, last := withPath.Path[0], withPath.Path[len(withPath.Path)-1]
	if first != ports["Rotterdam"] || last != ports["Gdynia"] {
		t.Errorf("path runs %v to %v; want the two ports", first, last)
	}
}

func TestDistanceToGoAvoid(t *testing.T) {
	cs := session(t)
	var via, round distanceOut
	from := map[string]any{"lat": 51.9, "lon": 4.44}
	to := map[string]any{"lat": 1.26, "lon": 103.8}
	call(t, cs, "distance_to_go", map[string]any{"from": from, "to": to}, &via)
	call(t, cs, "distance_to_go", map[string]any{"from": from, "to": to, "avoid": []string{"suez"}}, &round)
	t.Logf("default %.1f nm %v; avoiding suez %.1f nm %v", via.DistanceNM, via.Passages, round.DistanceNM, round.Passages)
	if !slices.Contains(via.Passages, "suez") || slices.Contains(round.Passages, "suez") || round.DistanceNM <= via.DistanceNM {
		t.Errorf("avoid suez not applied: %+v vs %+v", via, round)
	}
}

func TestRouteLegs(t *testing.T) {
	var out routeOut
	call(t, session(t), "route", map[string]any{"points": []map[string]any{{"port": "Rotterdam"}, {"lat": 57.8, "lon": 10.8}, {"port": "Gdynia"}}, "avoid": []string{"oresund"}}, &out)
	if len(out.Legs) != 2 || out.Path != nil {
		t.Errorf("got %d legs, path %v; want 2 legs, no path", len(out.Legs), out.Path != nil)
	}
}

func TestErrorsAreToolErrors(t *testing.T) {
	cs := session(t)
	for _, args := range []map[string]any{
		{"from": map[string]any{"port": "Atlantis"}, "to": map[string]any{"port": "Gdynia"}},
		{"from": map[string]any{"port": "Rotterdam"}, "to": map[string]any{"port": "Gdynia"}, "avoid": []string{"kiell"}},
		{"from": map[string]any{"lat": 95.0, "lon": 0.0}, "to": map[string]any{"port": "Gdynia"}},
	} {
		res := call(t, cs, "distance_to_go", args, nil)
		if !res.IsError {
			t.Errorf("args %v: want tool error", args)
		}
	}
}

func TestListPassages(t *testing.T) {
	var out passagesOut
	call(t, session(t), "list_passages", map[string]any{}, &out)
	if !slices.Contains(out.Passages, "oresund") || !strings.Contains(strings.Join(out.ClosedByDefault, ","), "northeast") {
		t.Errorf("unexpected passages: %+v", out)
	}
}

var rotSin = map[string]any{"from": map[string]any{"port": "Rotterdam"}, "to": map[string]any{"lat": 1.26, "lon": 103.8}}

func withArgs(extra map[string]any) map[string]any {
	a := map[string]any{}
	for k, v := range rotSin {
		a[k] = v
	}
	for k, v := range extra {
		a[k] = v
	}
	return a
}

func TestIncludePathAddsPathAndStretches(t *testing.T) {
	cs := fullSession(t)
	pts := []map[string]any{{"port": "Rotterdam"}, {"lat": 1.26, "lon": 103.8}}
	var out routeOut
	call(t, cs, "route", map[string]any{"points": pts, "include_path": true}, &out)
	if len(out.Path) < 2 || len(out.Stretches) == 0 {
		t.Errorf("want path and stretches with include_path; got %d vertices, %d stretches", len(out.Path), len(out.Stretches))
	}
	var bare routeOut
	call(t, cs, "route", map[string]any{"points": pts}, &bare)
	if len(bare.Path) != 0 || len(bare.Stretches) != 0 {
		t.Errorf("path data returned without include_path")
	}
}

func TestClearanceOutOfRangeToolError(t *testing.T) {
	cs := session(t)
	for _, c := range []float64{0.4, 10.5} {
		if res := call(t, cs, "distance_to_go", withArgs(map[string]any{"clearance_nm": c}), nil); !res.IsError {
			t.Errorf("clearance_nm %v: want tool error", c)
		}
	}
}

func TestRouteReturnsHazards(t *testing.T) {
	cs := fullSession(t)
	var out routeOut
	call(t, cs, "route", map[string]any{"points": []map[string]any{{"port": "Rotterdam"}, {"lat": 1.26, "lon": 103.8}}}, &out)
	suez := false
	for _, h := range out.Hazards {
		suez = suez || h.Passage == "suez"
	}
	if len(out.Hazards) == 0 || !suez {
		t.Errorf("want hazards including suez; got %+v", out.Hazards)
	}
	var d distanceOut
	call(t, cs, "distance_to_go", withArgs(nil), &d)
	if d.Hazards == nil || len(d.Hazards) == 0 {
		t.Errorf("distance_to_go hazards = %#v", d.Hazards)
	}
}

func TestDistanceToGoKielDraught(t *testing.T) {
	cs := session(t)
	args := map[string]any{"from": map[string]any{"port": "Rotterdam"}, "to": map[string]any{"port": "Gdynia"}}
	var deep, shoal distanceOut
	call(t, cs, "distance_to_go", args, &deep)
	args["draught_m"] = 8
	call(t, cs, "distance_to_go", args, &shoal)
	if slices.Contains(deep.Passages, "kiel") || !slices.Contains(shoal.Passages, "kiel") {
		t.Errorf("default draught %v, 8 m draught %v; want Kiel only with 8 m", deep.Passages, shoal.Passages)
	}
}

func TestVesselValidationToolError(t *testing.T) {
	cs := session(t)
	for _, extra := range []map[string]any{
		{"draught_m": 0}, {"draught_m": 26}, {"speed_kn": 0}, {"speed_kn": 31}, {"vessel_type": "ferry"},
	} {
		if res := call(t, cs, "distance_to_go", withArgs(extra), nil); !res.IsError {
			t.Errorf("%v: want tool error", extra)
		}
	}
}

func TestRouteDepthDefaults(t *testing.T) {
	var out routeOut
	call(t, session(t), "route", map[string]any{"points": []map[string]any{{"port": "Rotterdam"}, {"lat": 1.26, "lon": 103.8}}}, &out)
	if out.Depth.DraughtM != 10 || math.Abs(out.Depth.LimitM-13.568) > 1e-6 || out.Notices == nil || len(out.Notices) != 0 {
		t.Errorf("depth %+v notices %#v; want draught 10 and empty notices", out.Depth, out.Notices)
	}
}

// Jeddah to Piraeus at 21 m cannot use Suez (20.1 m limit); needs no depth data.
func TestRouteSuezDraughtNotice(t *testing.T) {
	var out routeOut
	pts := []map[string]any{{"lat": 21.48, "lon": 39.17}, {"lat": 37.94, "lon": 23.64}}
	call(t, session(t), "route", map[string]any{"points": pts, "draught_m": 21}, &out)
	found := false
	for _, n := range out.Notices {
		found = found || strings.Contains(n.Text, "suez limit of 20.1 m")
	}
	if !found {
		t.Errorf("no suez limit notice: %+v", out.Notices)
	}
}

func TestMCPRouteDraughtNotice(t *testing.T) {
	if _, err := os.Stat(depthFile); err != nil {
		t.Skip("depth dataset not built")
	}
	var out routeOut
	pts := []map[string]any{{"lat": 26.65, "lon": 50.17}, {"lat": 36.05, "lon": 120.3}}
	call(t, fullSession(t), "route", map[string]any{"points": pts, "draught_m": 22}, &out)
	for _, n := range out.Notices {
		if n.Kind == "draught-reroute" {
			return
		}
	}
	t.Errorf("no draught-reroute notice: %+v", out.Notices)
}
