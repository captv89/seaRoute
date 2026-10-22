package main

import (
	"fmt"

	"github.com/captv89/seaRoute/route"
)

type WaypointItem struct {
	Index         int            `json:"index"`
	Name          string         `json:"name"`
	Coordinates   route.Position `json:"coordinates"`
	LegDistanceM  float64        `json:"leg_distance_m"`
	LegDistanceNM float64        `json:"leg_distance_nm"`
	CumDistanceM  float64        `json:"cum_distance_m"`
	CumDistanceNM float64        `json:"cum_distance_nm"`
	Passage       string         `json:"passage,omitempty"`
}

type FeatureProperties struct {
	OCoords     route.Position `json:"o_coords"`
	DCoords     route.Position `json:"d_coords"`
	OToWpDist   float64        `json:"o_to_wp_dist"`
	WpToDDist   float64        `json:"wp_to_d_dist"`
	WpDist      float64        `json:"wp_dist"`
	TotalDist   float64        `json:"total_dist"`
	TotalDistNM float64        `json:"total_dist_nm"`
	TotalDistMI float64        `json:"total_dist_mi"`
	NodeCount   int            `json:"node_count"`
	RouteName   string         `json:"route_name"`
	Passages    []string       `json:"passages"`
	Waypoints   []WaypointItem `json:"waypoints"`
}

type Feature struct {
	Type     string `json:"type"`
	Geometry struct {
		Type        string           `json:"type"`
		Coordinates []route.Position `json:"coordinates"`
	} `json:"geometry"`
	Properties FeatureProperties `json:"properties"`
	ID         string            `json:"id"`
}

type Output struct {
	Type     string    `json:"type"`
	Name     string    `json:"name"`
	Features []Feature `json:"features"`
}

func generateOutput(r route.Route, oCoords route.Position, dCoords route.Position, totalDistance float64, oToWpDist float64, wpToDDist float64, Wp2WpDist float64, routeName string, passages []string) Output {
	totalDistNM := totalDistance / 1.852
	totalDistMI := totalDistance * 0.621371

	waypoints := make([]WaypointItem, len(r.Path))
	var cumDistM float64

	for i, p := range r.Path {
		var legDistM float64
		if i > 0 {
			legDistM = route.Distance(r.Path[i-1], p)
			cumDistM += legDistM
		}
		legDistNM := legDistM / route.MetresPerNM
		cumDistNM := cumDistM / route.MetresPerNM

		name := fmt.Sprintf("Waypoint %d", i+1)
		if i == 0 {
			name = "Origin"
		} else if i == len(r.Path)-1 {
			name = "Destination"
		}

		var passage string
		for _, ep := range route.ExtraPassages {
			if p[0] >= ep.MinLon && p[0] <= ep.MaxLon && p[1] >= ep.MinLat && p[1] <= ep.MaxLat {
				passage = ep.Pass
				break
			}
		}

		waypoints[i] = WaypointItem{
			Index:         i + 1,
			Name:          name,
			Coordinates:   p,
			LegDistanceM:  legDistM,
			LegDistanceNM: legDistNM,
			CumDistanceM:  cumDistM,
			CumDistanceNM: cumDistNM,
			Passage:       passage,
		}
	}

	if passages == nil {
		passages = []string{}
	}

	var output Output
	newFeature := Feature{
		Type: "Feature",
		Geometry: struct {
			Type        string           `json:"type"`
			Coordinates []route.Position `json:"coordinates"`
		}{
			Type:        "LineString",
			Coordinates: r.Path,
		},
		Properties: FeatureProperties{
			OCoords:     oCoords,
			DCoords:     dCoords,
			OToWpDist:   oToWpDist,
			WpToDDist:   wpToDDist,
			WpDist:      Wp2WpDist,
			TotalDist:   totalDistance,
			TotalDistNM: totalDistNM,
			TotalDistMI: totalDistMI,
			NodeCount:   len(r.Path),
			RouteName:   routeName,
			Passages:    passages,
			Waypoints:   waypoints,
		},
		ID: "1",
	}
	output.Features = append(output.Features, newFeature)
	output.Type = "FeatureCollection"
	output.Name = "Short Sea Route"

	return output
}
