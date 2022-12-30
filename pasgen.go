package main

import (
	"encoding/json"
	gdj "github.com/pitchinnate/golangGeojsonDijkstra"
	"os"
)

type Feature struct {
	Type     string `json:"type"`
	Geometry struct {
		Type        string   `json:"type"`
		Coordinates gdj.Path `json:"coordinates"`
	} `json:"geometry"`
	Properties struct {
		OCoords   gdj.Position `json:"o_coords"`
		DCoords   gdj.Position `json:"d_coords"`
		OToWpDist float64      `json:"o_to_wp_dist"`
		WpToDDist float64      `json:"wp_to_d_dist"`
		WpDist    float64      `json:"wp_dist"`
		TotalDist float64      `json:"total_dist"`
		RouteName string       `json:"route name"`
	} `json:"properties"`
	ID string `json:"id"`
}

type Output struct {
	Type     string    `json:"type"`
	Features []Feature `json:"features"`
}

func generateOutput(path gdj.Path, oCoords gdj.Position, dCoords gdj.Position, totalDistance float64, oToWpDist float64, wpToDDist float64, Wp2WpDist float64, routeName string) {

	//	Add values to the output struct
	var output Output
	output.Type = "FeatureCollection"
	newFeature := Feature{
		Type: "Feature",
		Geometry: struct {
			Type        string   `json:"type"`
			Coordinates gdj.Path `json:"coordinates"`
		}{
			Type:        "LineString",
			Coordinates: path,
		},
		Properties: struct {
			OCoords   gdj.Position `json:"o_coords"`
			DCoords   gdj.Position `json:"d_coords"`
			OToWpDist float64      `json:"o_to_wp_dist"`
			WpToDDist float64      `json:"wp_to_d_dist"`
			WpDist    float64      `json:"wp_dist"`
			TotalDist float64      `json:"total_dist"`
			RouteName string       `json:"route name"`
		}{
			OCoords:   oCoords,
			DCoords:   dCoords,
			OToWpDist: oToWpDist,
			WpToDDist: wpToDDist,
			WpDist:    Wp2WpDist,
			TotalDist: totalDistance,
			RouteName: routeName,
		},
		ID: "1",
	}
	output.Features = append(output.Features, newFeature)

	//	Convert the output struct to json
	jsonOutput, err := json.Marshal(output)
	if err != nil {
		panic(err)
	}

	//	Create a file and write the json to it
	file, err := os.Create("output.geojson")
	if err != nil {
		panic(err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			panic(err)
		}
	}(file)

	_, err = file.Write(jsonOutput)
	if err != nil {
		panic(err)
	}

}
