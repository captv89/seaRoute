package mcpserver

import (
	"context"
	"encoding/json"
	"math"
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

var (
	fullOnce  sync.Once
	fullGraph *route.Graph
	fullErr   error
)

// fullSession serves a graph with land and TSS, so proposals can occur.
func fullSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	fullOnce.Do(func() {
		fullGraph, fullErr = route.LoadAll(route.Datasets{
			Network: "../dataset/marnet_plus_5km.geojson",
			Land:    "../dataset/land_osm.bin.gz",
			TSS:     "../dataset/tss_osm.geojson.gz",
		})
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
	call(t, cs, "distance_to_go", map[string]any{"from": map[string]any{"port": "Rotterdam"}, "to": map[string]any{"port": "Gdynia"}}, &via)
	call(t, cs, "distance_to_go", map[string]any{"from": map[string]any{"lat": 51.9, "lon": 4.44}, "to": map[string]any{"port": "Gdynia"}, "avoid": []string{"kiel"}}, &round)
	t.Logf("via Kiel %.1f nm %v; avoiding %.1f nm %v", via.DistanceNM, via.Passages, round.DistanceNM, round.Passages)
	if !slices.Contains(via.Passages, "kiel") || slices.Contains(round.Passages, "kiel") || round.DistanceNM <= via.DistanceNM {
		t.Errorf("avoid kiel not applied: %+v vs %+v", via, round)
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

func TestDistanceToGoProposalsNotApplied(t *testing.T) {
	cs := fullSession(t)
	var smooth, plain distanceOut
	call(t, cs, "distance_to_go", withArgs(nil), &smooth)
	call(t, cs, "distance_to_go", withArgs(map[string]any{"kinks": false}), &plain)
	if len(smooth.Proposals) == 0 {
		t.Fatal("no proposals for Rotterdam to Singapore")
	}
	if len(plain.Proposals) != 0 {
		t.Errorf("kinks=false returned %d proposals", len(plain.Proposals))
	}
	for _, p := range smooth.Proposals {
		if p.ID == "" || p.Reason == "" || len(p.Path) != 0 {
			t.Errorf("bad proposal without include_path: %+v", p)
		}
	}
	var again distanceOut
	call(t, cs, "distance_to_go", withArgs(map[string]any{"accept": []string{}}), &again)
	if again.DistanceNM != smooth.DistanceNM {
		t.Errorf("listing proposals changed distance: %.1f vs %.1f", again.DistanceNM, smooth.DistanceNM)
	}
}

func TestDistanceToGoAcceptApplies(t *testing.T) {
	cs := fullSession(t)
	var base, applied, ignored distanceOut
	call(t, cs, "distance_to_go", withArgs(nil), &base)
	if len(base.Proposals) == 0 {
		t.Fatal("no proposals")
	}
	p := base.Proposals[0]
	call(t, cs, "distance_to_go", withArgs(map[string]any{"accept": []string{p.ID}}), &applied)
	if drop := base.DistanceNM - applied.DistanceNM; math.Abs(drop-p.SavedNM) > 0.11 {
		t.Errorf("distance dropped %.2f nm; proposal saved_nm %.2f", drop, p.SavedNM)
	}
	if len(applied.IgnoredAccept) != 0 {
		t.Errorf("ignored_accept = %v; want empty", applied.IgnoredAccept)
	}
	call(t, cs, "distance_to_go", withArgs(map[string]any{"kinks": false, "accept": []string{p.ID}}), &ignored)
	if !slices.Equal(ignored.IgnoredAccept, []string{p.ID}) {
		t.Errorf("kinks=false ignored_accept = %v; want [%s]", ignored.IgnoredAccept, p.ID)
	}
}

func TestIncludePathAddsProposalPathAndStretches(t *testing.T) {
	cs := fullSession(t)
	var out routeOut
	call(t, cs, "route", map[string]any{"points": []map[string]any{{"port": "Rotterdam"}, {"lat": 1.26, "lon": 103.8}}, "include_path": true}, &out)
	if len(out.Proposals) == 0 || len(out.Proposals[0].Path) < 2 || len(out.Stretches) == 0 {
		t.Errorf("want proposal paths and stretches with include_path; got %d proposals, %d stretches", len(out.Proposals), len(out.Stretches))
	}
	var bare routeOut
	call(t, cs, "route", map[string]any{"points": []map[string]any{{"port": "Rotterdam"}, {"lat": 1.26, "lon": 103.8}}}, &bare)
	if len(bare.Stretches) != 0 || len(bare.Proposals) == 0 || len(bare.Proposals[0].Path) != 0 {
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
	long := strings.Repeat("x", 33)
	if res := call(t, cs, "route", map[string]any{"points": []map[string]any{{"port": "Rotterdam"}, {"port": "Gdynia"}}, "accept": []string{long}}, nil); !res.IsError {
		t.Error("overlong accept ID: want tool error")
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
