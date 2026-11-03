// Command land-import builds the coastline dataset used for land-clearance
// checks from the OSM land polygons (split, EPSG:4326).
//
// Source: https://osmdata.openstreetmap.de/data/land-polygons.html
// Licence: © OpenStreetMap contributors, ODbL.
//
// Only rings within a corridor of the sea-route network are kept, simplified
// with Douglas-Peucker in metres, and written as a gzip file:
//
//	"SRLAND1\n"
//	{"source":...,"date":...,"tolerance_m":...,"corridor_nm":...,"rings":N}\n
//	per ring: uvarint vertex count, then zigzag-varint (dlon, dlat) in 1e-6 degrees,
//	the first vertex being a delta from (0,0).
//
// Usage: go run ./cmd/land-import -shp land_polygons.shp -network dataset/marnet_plus_5km.geojson -tolerance 200 -out dataset/land_osm.bin.gz
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"flag"
	"io"
	"log"
	"math"
	"os"

	"github.com/captv89/seaRoute/internal/corridor"
)

const (
	earthR   = 6371008.8
	cellSize = 0.5 // degrees
	source   = "osm-land-polygons-split-4326"
)

type header struct {
	Source     string  `json:"source"`
	Date       string  `json:"date"`
	ToleranceM float64 `json:"tolerance_m"`
	CorridorNM float64 `json:"corridor_nm"`
	Rings      int     `json:"rings"`
}

func main() {
	shp := flag.String("shp", "", "land_polygons.shp (polygon shapefile, EPSG:4326)")
	network := flag.String("network", "", "network GeoJSON (MARNET) defining the corridor")
	tol := flag.Float64("tolerance", 200, "Douglas-Peucker tolerance in metres")
	corridorNM := flag.Float64("corridor", 30, "corridor radius around network vertices in NM")
	date := flag.String("date", "", "source data date YYYY-MM-DD for the header")
	out := flag.String("out", "", "output .bin.gz")
	flag.Parse()
	if *shp == "" || *network == "" || *out == "" || *date == "" {
		flag.Usage()
		os.Exit(2)
	}
	cells, err := corridor.Cells(*network, *corridorNM, cellSize)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("corridor cells: %d", len(cells))

	var rings [][][2]float64
	var read, inVerts, outVerts int
	err = readShp(*shp, func(r [][2]float64) {
		read++
		inVerts += len(r)
		if !touchesCorridor(r, cells) {
			return
		}
		s := simplify(r, *tol)
		if len(s) < 4 {
			return
		}
		outVerts += len(s)
		rings = append(rings, s)
	})
	if err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	hdr := header{Source: source, Date: *date, ToleranceM: *tol, CorridorNM: *corridorNM, Rings: len(rings)}
	if err := encode(f, hdr, rings); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	st, _ := os.Stat(*out)
	log.Printf("rings read %d (%d vertices); kept %d rings, %d vertices; %s = %d bytes (%.1f MB)",
		read, inVerts, len(rings), outVerts, *out, st.Size(), float64(st.Size())/1e6)
}

func wrapCX(cx int32) int32 {
	const n = int32(360 / cellSize)
	cx = (cx + n/2) % n
	if cx < 0 {
		cx += n
	}
	return cx - n/2
}

// touchesCorridor reports whether the ring's bounding box overlaps a marked cell.
func touchesCorridor(ring [][2]float64, cells map[[2]int32]bool) bool {
	if len(ring) == 0 {
		return false
	}
	minX, maxX := ring[0][0], ring[0][0]
	minY, maxY := ring[0][1], ring[0][1]
	for _, p := range ring {
		minX, maxX = math.Min(minX, p[0]), math.Max(maxX, p[0])
		minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
	}
	x0, x1 := int32(math.Floor(minX/cellSize)), int32(math.Floor(maxX/cellSize))
	y0, y1 := int32(math.Floor(minY/cellSize)), int32(math.Floor(maxY/cellSize))
	if int64(x1-x0+1)*int64(y1-y0+1) > int64(len(cells)) {
		for k := range cells {
			if k[0] >= x0 && k[0] <= x1 && k[1] >= y0 && k[1] <= y1 {
				return true
			}
		}
		return false
	}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if cells[[2]int32{wrapCX(x), y}] {
				return true
			}
		}
	}
	return false
}

// simplify runs Douglas-Peucker with tolerance tolM metres. Distances are
// measured per span in a local equirectangular projection whose longitude
// scale is cos(latitude closest to the equator within the span). That cosine
// is at least the true one at every vertex of the span, so east-west distances
// are never underestimated: every removed vertex lies within tolM of the kept
// polyline (conservative, may keep slightly more vertices at high latitude).
// The first and last vertices are always kept.
func simplify(ring [][2]float64, tolM float64) [][2]float64 {
	n := len(ring)
	if n < 3 || tolM <= 0 {
		return ring
	}
	k := earthR * math.Pi / 180
	keep := make([]bool, n)
	keep[0], keep[n-1] = true, true
	type span struct{ a, b int }
	stack := []span{{0, n - 1}}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if s.b-s.a < 2 {
			continue
		}
		lo, hi := ring[s.a][1], ring[s.a][1]
		for i := s.a + 1; i <= s.b; i++ {
			lo, hi = math.Min(lo, ring[i][1]), math.Max(hi, ring[i][1])
		}
		minAbs := math.Min(math.Abs(lo), math.Abs(hi))
		if lo <= 0 && hi >= 0 {
			minAbs = 0
		}
		c := math.Cos(minAbs*math.Pi/180) * k
		ax, ay := ring[s.a][0], ring[s.a][1]
		dx, dy := (ring[s.b][0]-ax)*c, (ring[s.b][1]-ay)*k
		l2 := dx*dx + dy*dy
		far, farD := -1, tolM
		for i := s.a + 1; i < s.b; i++ {
			px, py := (ring[i][0]-ax)*c, (ring[i][1]-ay)*k
			var d float64
			if l2 == 0 {
				d = math.Hypot(px, py)
			} else {
				t := math.Max(0, math.Min(1, (px*dx+py*dy)/l2))
				d = math.Hypot(px-t*dx, py-t*dy)
			}
			if d > farD {
				far, farD = i, d
			}
		}
		if far >= 0 {
			keep[far] = true
			stack = append(stack, span{s.a, far}, span{far, s.b})
		}
	}
	out := make([][2]float64, 0, n/4)
	for i, p := range ring {
		if keep[i] {
			out = append(out, p)
		}
	}
	return out
}

// encode writes the gzip land file described in the package comment.
func encode(w io.Writer, hdr header, rings [][][2]float64) error {
	hdr.Rings = len(rings)
	zw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(zw)
	bw.WriteString("SRLAND1\n")
	hj, err := json.Marshal(hdr)
	if err != nil {
		return err
	}
	bw.Write(hj)
	bw.WriteByte('\n')
	var buf [binary.MaxVarintLen64]byte
	for _, r := range rings {
		bw.Write(buf[:binary.PutUvarint(buf[:], uint64(len(r)))])
		var px, py int64
		for _, p := range r {
			x, y := int64(math.Round(p[0]*1e6)), int64(math.Round(p[1]*1e6))
			bw.Write(buf[:binary.PutVarint(buf[:], x-px)])
			bw.Write(buf[:binary.PutVarint(buf[:], y-py)])
			px, py = x, y
		}
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return zw.Close()
}
