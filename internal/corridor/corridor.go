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

// StepNM is the largest gap between the points Densify returns. A point of a
// segment is within StepNM/2 of one of them.
const StepNM = 5.0

// Densify returns points along the segment (lon0,lat0)-(lon1,lat1), linear in
// longitude and latitude, at most StepNM apart (measured conservatively), both
// ends included. A segment whose longitudes differ by more than 180 degrees
// crosses the antimeridian and is walked the short way; longitudes are then
// returned unwrapped (the caller wraps them).
func Densify(lon0, lat0, lon1, lat1 float64) [][2]float64 {
	dl := lon1 - lon0
	if dl > 180 {
		dl -= 360
	} else if dl < -180 {
		dl += 360
	}
	dy := (lat1 - lat0) * NMPerDeg
	dx := dl * NMPerDeg // longitude degrees are never longer than latitude degrees
	n := max(1, int(math.Ceil(math.Hypot(dx, dy)/StepNM)))
	out := make([][2]float64, 0, n+1)
	for k := 0; k <= n; k++ {
		t := float64(k) / float64(n)
		out = append(out, [2]float64{lon0 + t*dl, lat0 + t*(lat1-lat0)})
	}
	return out
}

// wrapCX wraps a cell column index so longitude wraps at 180 degrees.
func wrapCX(cx int32, cellDeg float64) int32 {
	n := int32(360 / cellDeg)
	cx = (cx + n/2) % n
	if cx < 0 {
		cx += n
	}
	return cx - n/2
}

// Cells marks cellDeg-degree cells within radiusNM of any point of the
// network's LineString / MultiLineString features. Each line is densified to
// points at most StepNM apart before marking, so the long open-water edges of
// the network are covered along their whole length, not only near their ends.
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
	line := func(c [][]float64) {
		for i, p := range c {
			if len(p) < 2 {
				continue
			}
			if i > 0 && len(c[i-1]) >= 2 {
				for _, q := range Densify(c[i-1][0], c[i-1][1], p[0], p[1]) {
					mark(q[0], q[1])
				}
				continue
			}
			mark(p[0], p[1])
		}
	}
	for _, ft := range fc.Features {
		switch ft.Geometry.Type {
		case "LineString":
			var c [][]float64
			if json.Unmarshal(ft.Geometry.Coordinates, &c) != nil {
				continue
			}
			line(c)
		case "MultiLineString":
			var c [][][]float64
			if json.Unmarshal(ft.Geometry.Coordinates, &c) != nil {
				continue
			}
			for _, l := range c {
				line(l)
			}
		}
	}
	if len(cells) == 0 {
		return nil, fmt.Errorf("network %s has no line vertices", networkPath)
	}
	return cells, nil
}
