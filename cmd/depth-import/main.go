// Command depth-import builds the depth dataset used for draught checks from
// the GEBCO grid in Esri ASCII tiles.
//
// Source: GEBCO Bathymetric Compilation Group (2026) GEBCO 2026 Grid
// (doi:10.5285/4f68d5c7-45eb-f999-e063-7086abc036fa). Public domain,
// attribution requested.
//
// Only 0.5 degree tiles within a corridor of the sea-route network are kept
// (the same coverage as the land data). Each ASCII tile is read line by line;
// only the wanted tiles of the current 120-row band are held in memory. The
// output format is documented on route.WriteDepth.
//
// Usage: go run ./cmd/depth-import -asc 'gebco/*.asc' -network dataset/marnet_plus_5km.geojson -corridor 30 -date 2026-01-01 -out dataset/depth_gebco.bin.gz
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/captv89/seaRoute/internal/corridor"
	"github.com/captv89/seaRoute/route"
)

const (
	tileDeg      = 0.5
	cellsPerTile = 120
	cellsPerDeg  = 240 // 15 arc-seconds
)

func main() {
	asc := flag.String("asc", "", "glob of GEBCO Esri ASCII tiles")
	network := flag.String("network", "", "network GeoJSON (MARNET) defining the corridor")
	corridorNM := flag.Float64("corridor", 30, "corridor radius around network vertices in NM")
	date := flag.String("date", "", "source data date YYYY-MM-DD for the header")
	out := flag.String("out", "", "output .bin.gz")
	flag.Parse()
	if *asc == "" || *network == "" || *out == "" || *date == "" {
		flag.Usage()
		os.Exit(2)
	}
	files, err := filepath.Glob(*asc)
	if err != nil || len(files) == 0 {
		log.Fatalf("no files match %q (%v)", *asc, err)
	}
	sort.Strings(files)
	want, err := corridor.Cells(*network, *corridorNM, tileDeg)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("corridor tiles: %d", len(want))

	tl := newTiler(want)
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("reading %s", name)
		_, err = readEsriASCII(f, tl.row)
		f.Close()
		if err != nil {
			log.Fatalf("%s: %v", name, err)
		}
	}
	tiles := tl.finish()
	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	if err := route.WriteDepth(f, tiles, *date); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	deep := 0
	for _, t := range tiles {
		if t.Deep {
			deep++
		}
	}
	st, _ := os.Stat(*out)
	log.Printf("kept %d of %d corridor tiles (%d deep); %s = %d bytes (%.1f MB)",
		len(tiles), len(want), deep, *out, st.Size(), float64(st.Size())/1e6)
}

// acc collects one tile's cells while its 120-row band streams past.
type acc struct {
	m     []uint8
	valid int // cells that were not NODATA
}

// tiler turns streamed raster rows (north to south) into DepthTiles.
//
// NODATA handling: a tile whose every cell is NODATA is skipped (unknown). In
// any other tile NODATA cells are stored as 0, the conservative value that
// reads as land and so never lets a route through unknown water.
type tiler struct {
	want  map[[2]int32]bool
	band  map[[2]int32]*acc
	done  []route.DepthTile
	seen  map[[2]int32]bool
	bandY int32 // cy of the band being collected
}

func newTiler(want map[[2]int32]bool) *tiler {
	return &tiler{want: want, band: map[[2]int32]*acc{}, seen: map[[2]int32]bool{}}
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func intCorner(v float64) (int64, error) {
	r := math.Round(v * cellsPerDeg)
	if math.Abs(v*cellsPerDeg-r) > 1e-3 {
		return 0, fmt.Errorf("corner %g is not on the 15 arc-second grid", v)
	}
	return int64(r), nil
}

// row is the readEsriASCII callback.
func (t *tiler) row(h esriHeader, row int, vals []uint8, nodata []bool) error {
	if math.Abs(h.Cell*cellsPerDeg-1) > 1e-6 {
		return fmt.Errorf("cellsize %g is not 15 arc-seconds", h.Cell)
	}
	x0, err := intCorner(h.XLL)
	if err != nil {
		return err
	}
	y0, err := intCorner(h.YLL)
	if err != nil {
		return err
	}
	yi := y0 + int64(h.Nrows-1-row) // cell row counted from lat 0, southwards negative
	cy, j := int32(floorDiv(yi, cellsPerTile)), int(yi-floorDiv(yi, cellsPerTile)*cellsPerTile)
	if len(t.band) > 0 && cy != t.bandY {
		t.flush()
	}
	t.bandY = cy
	for c := 0; c < h.Ncols; {
		xi := x0 + int64(c)
		cx := int32(floorDiv(xi, cellsPerTile))
		i := int(xi - int64(cx)*cellsPerTile)
		n := min(cellsPerTile-i, h.Ncols-c)
		k := [2]int32{cx, cy}
		if t.want[k] {
			a := t.band[k]
			if a == nil {
				if t.seen[k] {
					return fmt.Errorf("tile (%d,%d) appears in more than one input file", cx, cy)
				}
				a = &acc{m: make([]uint8, cellsPerTile*cellsPerTile)}
				t.band[k] = a
			}
			copy(a.m[j*cellsPerTile+i:], vals[c:c+n])
			for _, nd := range nodata[c : c+n] {
				if !nd {
					a.valid++
				}
			}
		}
		c += n
	}
	if j == 0 {
		t.flush()
	}
	return nil
}

// flush finishes the tiles of the current band.
func (t *tiler) flush() {
	for k, a := range t.band {
		t.seen[k] = true
		delete(t.band, k)
		if a.valid == 0 {
			continue
		}
		deep := true
		for _, v := range a.m {
			if v != 255 {
				deep = false
				break
			}
		}
		dt := route.DepthTile{CX: k[0], CY: k[1], Deep: deep}
		if !deep {
			dt.M = a.m
		}
		t.done = append(t.done, dt)
	}
}

// finish flushes the last band and returns the tiles in (CY, CX) order.
func (t *tiler) finish() []route.DepthTile {
	t.flush()
	sort.Slice(t.done, func(a, b int) bool {
		if t.done[a].CY != t.done[b].CY {
			return t.done[a].CY < t.done[b].CY
		}
		return t.done[a].CX < t.done[b].CX
	})
	return t.done
}
