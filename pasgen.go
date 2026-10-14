package main

import (
	"github.com/captv89/seaRoute/route"
)

type Feature struct {
	Type     string `json:"type"`
	Geometry struct {
		Type        string           `json:"type"`
		Coordinates []route.Position `json:"coordinates"`
	} `json:"geometry"`
	Properties struct {
		OCoords   route.Position `json:"o_coords"`
		DCoords   route.Position `json:"d_coords"`
		OToWpDist float64        `json:"o_to_wp_dist"`
		WpToDDist float64        `json:"wp_to_d_dist"`
		WpDist    float64        `json:"wp_dist"`
		TotalDist float64        `json:"total_dist"`
		RouteName string         `json:"route_name"`
		Passages  []string       `json:"passages"`
	} `json:"properties"`
	ID string `json:"id"`
}

type Output struct {
	Type     string    `json:"type"`
	Name     string    `json:"name"`
	Features []Feature `json:"features"`
}

func generateOutput(path []route.Position, oCoords route.Position, dCoords route.Position, totalDistance float64, oToWpDist float64, wpToDDist float64, Wp2WpDist float64, routeName string, passages []string) Output {

	//	Add values to the output struct
	var output Output
	newFeature := Feature{
		Type: "Feature",
		Geometry: struct {
			Type        string           `json:"type"`
			Coordinates []route.Position `json:"coordinates"`
		}{
			Type:        "LineString",
			Coordinates: path,
		},
		Properties: struct {
			OCoords   route.Position `json:"o_coords"`
			DCoords   route.Position `json:"d_coords"`
			OToWpDist float64        `json:"o_to_wp_dist"`
			WpToDDist float64        `json:"wp_to_d_dist"`
			WpDist    float64        `json:"wp_dist"`
			TotalDist float64        `json:"total_dist"`
			RouteName string         `json:"route_name"`
			Passages  []string       `json:"passages"`
		}{
			OCoords:   oCoords,
			DCoords:   dCoords,
			OToWpDist: oToWpDist,
			WpToDDist: wpToDDist,
			WpDist:    Wp2WpDist,
			TotalDist: totalDistance,
			RouteName: routeName,
			Passages:  passages,
		},
		ID: "1",
	}
	output.Features = append(output.Features, newFeature)
	output.Type = "FeatureCollection"
	output.Name = "Short Sea Route"

	return output
}
