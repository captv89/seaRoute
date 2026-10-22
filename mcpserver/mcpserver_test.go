package mcpserver

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/captv89/seaRoute/route"
)

var ports = map[string]route.Position{
	"Rotterdam": {4.4424468168432654, 51.904383352111097},
	"Gdynia":    {18.546982540197764, 54.538336522499797},
}

func session(t *testing.T) *mcp.ClientSession {
	t.Helper()
	g, err := route.Load("../dataset/marnet_plus_5km.geojson")
	if err != nil {
		t.Fatal(err)
	}
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
