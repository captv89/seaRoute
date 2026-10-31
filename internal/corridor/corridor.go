// Package corridor computes the grid cells around a sea-route network; the
// land and depth importers keep only data inside these cells.
package corridor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
)

const NMPerDeg = 60.0

// wrapCX wraps a cell column index so longitude wraps at 180 degrees.
func wrapCX(cx int32, cellDeg float64) int32 {
	n := int32(360 / cellDeg)
	cx = (cx + n/2) % n
	if cx < 0 {
		cx += n
	}
	return cx - n/2
}

// Cells marks cellDeg-degree cells within radiusNM of any vertex of the
// network's LineString / MultiLineString features.
func Cells(networkPath string, radiusNM, cellDeg float64) (map[[2]int32]bool, error) {
	f, err := os.Open(networkPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var fc struct {
		Features []struct {
			Geometry struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.NewDecoder(bufio.NewReaderSize(f, 1<<20)).Decode(&fc); err != nil {
		return nil, fmt.Errorf("network: %w", err)
	}
	cells := map[[2]int32]bool{}
	mark := func(lon, lat float64) {
		dLat := radiusNM / NMPerDeg
		cl := math.Cos(math.Min(math.Abs(lat)+dLat, 89) * math.Pi / 180)
		dLon := dLat / cl
		if dLon > 180 {
			dLon = 180
		}
		y0, y1 := int32(math.Floor((lat-dLat)/cellDeg)), int32(math.Floor((lat+dLat)/cellDeg))
		x0, x1 := int32(math.Floor((lon-dLon)/cellDeg)), int32(math.Floor((lon+dLon)/cellDeg))
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				cells[[2]int32{wrapCX(x, cellDeg), y}] = true
			}
		}
	}
	for _, ft := range fc.Features {
		switch ft.Geometry.Type {
		case "LineString":
			var c [][]float64
			if json.Unmarshal(ft.Geometry.Coordinates, &c) != nil {
				continue
			}
			for _, p := range c {
				if len(p) >= 2 {
					mark(p[0], p[1])
				}
			}
		case "MultiLineString":
			var c [][][]float64
			if json.Unmarshal(ft.Geometry.Coordinates, &c) != nil {
				continue
			}
			for _, l := range c {
				for _, p := range l {
					if len(p) >= 2 {
						mark(p[0], p[1])
					}
				}
			}
		}
	}
	if len(cells) == 0 {
		return nil, fmt.Errorf("network %s has no line vertices", networkPath)
	}
	return cells, nil
}
