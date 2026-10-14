// Package mcpserver exposes seaRoute routing as MCP tools:
// distance_to_go, route and list_passages.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/captv89/seaRoute/route"
)

// PortFinder returns a port's position by exact name.
type PortFinder func(name string) (route.Position, bool)

// Point is a position given either by port name or by latitude and longitude.
type Point struct {
	Port string   `json:"port,omitempty" jsonschema:"port name; use this or lat and lon"`
	Lat  *float64 `json:"lat,omitempty" jsonschema:"latitude in decimal degrees, north positive"`
	Lon  *float64 `json:"lon,omitempty" jsonschema:"longitude in decimal degrees, east positive"`
}

// Passages controls which canals and straits a route may use.
type Passages struct {
	Avoid []string `json:"avoid,omitempty" jsonschema:"passages the route must not use, e.g. kiel, suez, oresund; see list_passages"`
	Allow []string `json:"allow,omitempty" jsonschema:"passages closed by default to open, e.g. northeast"`
}

type distanceIn struct {
	From Point `json:"from" jsonschema:"ship position"`
	To   Point `json:"to" jsonschema:"destination"`
	Passages
}

type distanceOut struct {
	DistanceNM float64  `json:"distance_nm" jsonschema:"sea distance in nautical miles, rounded to 0.1"`
	Passages   []string `json:"passages" jsonschema:"tagged canals and straits on the route, in order"`
}

type routeIn struct {
	Points []Point `json:"points" jsonschema:"start, optional via-points, end"`
	Passages
	IncludePath bool `json:"include_path,omitempty" jsonschema:"return the route geometry as [lon, lat] pairs"`
}

type legOut struct {
	DistanceNM float64  `json:"distance_nm"`
	Passages   []string `json:"passages"`
}

type routeOut struct {
	DistanceNM float64          `json:"distance_nm"`
	Passages   []string         `json:"passages"`
	Legs       []legOut         `json:"legs"`
	Path       []route.Position `json:"path,omitempty"`
}

type passagesOut struct {
	Passages        []string `json:"passages"`
	ClosedByDefault []string `json:"closed_by_default"`
}

// New returns an MCP server with the seaRoute tools. ports may be nil,
// in which case points must be given as coordinates.
func New(g *route.Graph, ports PortFinder) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "searoute", Version: "2.0.0"}, nil)
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}
	resolve := func(p Point) (route.Position, error) { return resolvePoint(p, ports) }

	mcp.AddTool(s, &mcp.Tool{
		Name:        "distance_to_go",
		Description: "Sea distance in nautical miles from a position to a destination along the shipping network (Eurostat MARNET).",
		Annotations: readOnly,
	}, func(_ context.Context, _ *mcp.CallToolRequest, in distanceIn) (*mcp.CallToolResult, distanceOut, error) {
		from, err := resolve(in.From)
		if err != nil {
			return nil, distanceOut{}, fmt.Errorf("from: %w", err)
		}
		to, err := resolve(in.To)
		if err != nil {
			return nil, distanceOut{}, fmt.Errorf("to: %w", err)
		}
		r, err := g.Route([]route.Position{from, to}, route.Options{Avoid: in.Avoid, Allow: in.Allow})
		if err != nil {
			return nil, distanceOut{}, err
		}
		return nil, distanceOut{DistanceNM: round1(r.DistanceNM()), Passages: r.Passages}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "route",
		Description: "Shortest sea route through two or more points, in order, along the shipping network. Returns total and per-leg distance in nautical miles and the canals and straits used.",
		Annotations: readOnly,
	}, func(_ context.Context, _ *mcp.CallToolRequest, in routeIn) (*mcp.CallToolResult, routeOut, error) {
		if len(in.Points) < 2 {
			return nil, routeOut{}, errors.New("route needs at least two points")
		}
		pts := make([]route.Position, len(in.Points))
		for i, p := range in.Points {
			pos, err := resolve(p)
			if err != nil {
				return nil, routeOut{}, fmt.Errorf("point %d: %w", i+1, err)
			}
			pts[i] = pos
		}
		r, err := g.Route(pts, route.Options{Avoid: in.Avoid, Allow: in.Allow})
		if err != nil {
			return nil, routeOut{}, err
		}
		out := routeOut{DistanceNM: round1(r.DistanceNM()), Passages: r.Passages}
		for _, l := range r.Legs {
			out.Legs = append(out.Legs, legOut{DistanceNM: round1(l.DistanceM / route.MetresPerNM), Passages: l.Passages})
		}
		if in.IncludePath {
			out.Path = r.Path
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_passages",
		Description: "Canals and straits that routes can avoid by name, and those closed unless allowed.",
		Annotations: readOnly,
	}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, passagesOut, error) {
		return nil, passagesOut{Passages: g.Passages(), ClosedByDefault: route.ClosedByDefault}, nil
	})
	return s
}

func resolvePoint(p Point, ports PortFinder) (route.Position, error) {
	switch {
	case p.Lat != nil && p.Lon != nil:
		if math.Abs(*p.Lat) > 90 || math.Abs(*p.Lon) > 180 {
			return route.Position{}, fmt.Errorf("position %v, %v out of range", *p.Lat, *p.Lon)
		}
		return route.Position{*p.Lon, *p.Lat}, nil
	case p.Port != "":
		if ports == nil {
			return route.Position{}, errors.New("port names not supported here; give lat and lon")
		}
		pos, ok := ports(p.Port)
		if !ok {
			return route.Position{}, fmt.Errorf("unknown port %q", p.Port)
		}
		return pos, nil
	default:
		return route.Position{}, errors.New("give a port, or lat and lon")
	}
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
