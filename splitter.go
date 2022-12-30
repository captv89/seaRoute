package main

import (
	gdj "github.com/pitchinnate/golangGeojsonDijkstra"
)

// Splitter is a function that fc with more than 2 pairs of coordinates into fc with 2 pairs of coordinates
func splitter(fc gdj.FeatureCollection) gdj.FeatureCollection {

	var newFc gdj.FeatureCollection

	//	Loop through all features in the feature collection
	for _, feature := range fc.Features {
		// Loop through all coordinates in the feature
		//log.Println("Feature: ", len(feature.Geometry.Coordinates))
		for i, coord1 := range feature.Geometry.Coordinates {
			for _, coord2 := range feature.Geometry.Coordinates[i+1:] {
				// Create a new feature with the first and next coordinate and append it to the new feature collection
				newFeature := gdj.Feature{
					Geometry: gdj.Geometry{
						Type:        "LineString",
						Coordinates: []gdj.Position{coord1, coord2},
					},
				}
				newFc.Features = append(newFc.Features, newFeature)
			}
		}
	}
	return newFc
}
