package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/captv89/seaRoute/internal/corridor"
)

// writeShp writes a minimal polygon (type 5) shapefile with one record per
// polygon; each polygon is a list of rings.
func writeShp(t *testing.T, path string, polys [][][][2]float64) {
	t.Helper()
	var body bytes.Buffer
	for i, rings := range polys {
		var rec bytes.Buffer
		np := 0
		for _, r := range rings {
			np += len(r)
		}
		binary.Write(&rec, binary.LittleEndian, int32(5))
		binary.Write(&rec, binary.LittleEndian, [4]float64{0, 0, 0, 0})
		binary.Write(&rec, binary.LittleEndian, int32(len(rings)))
		binary.Write(&rec, binary.LittleEndian, int32(np))
		off := int32(0)
		for _, r := range rings {
			binary.Write(&rec, binary.LittleEndian, off)
			off += int32(len(r))
		}
		for _, r := range rings {
			for _, p := range r {
				binary.Write(&rec, binary.LittleEndian, p)
			}
		}
		binary.Write(&body, binary.BigEndian, int32(i+1))
		binary.Write(&body, binary.BigEndian, int32(rec.Len()/2))
		body.Write(rec.Bytes())
	}
	hdr := make([]byte, 100)
	binary.BigEndian.PutUint32(hdr[0:], 9994)
	binary.BigEndian.PutUint32(hdr[24:], uint32((100+body.Len())/2))
	binary.LittleEndian.PutUint32(hdr[28:], 1000)
	binary.LittleEndian.PutUint32(hdr[32:], 5)
	if err := os.WriteFile(path, append(hdr, body.Bytes()...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadShpPolygons(t *testing.T) {
	a := [][2]float64{{0, 0}, {0, 1}, {1, 1}, {1, 0}, {0, 0}}
	b := [][2]float64{{10, 10}, {10, 12.5}, {12.5, 12.5}, {10, 10}}
	c := [][2]float64{{20, -5}, {21, -5}, {21, -4}, {20, -5}}
	p := filepath.Join(t.TempDir(), "land.shp")
	writeShp(t, p, [][][][2]float64{{a, b}, {c}})
	var got [][][2]float64
	if err := readShp(p, func(r [][2]float64) { got = append(got, r) }); err != nil {
		t.Fatal(err)
	}
	want := [][][2]float64{a, b, c}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestReadShpRejectsOtherTypes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.shp")
	hdr := make([]byte, 100)
	binary.BigEndian.PutUint32(hdr[0:], 9994)
	binary.LittleEndian.PutUint32(hdr[32:], 3)
	os.WriteFile(p, hdr, 0o644)
	if err := readShp(p, func([][2]float64) {}); err == nil {
		t.Fatal("expected error for polyline shapefile")
	}
}

func distM(a, b [2]float64, cosLat float64) float64 {
	dx := (a[0] - b[0]) * cosLat * math.Pi / 180 * 6371008.8
	dy := (a[1] - b[1]) * math.Pi / 180 * 6371008.8
	return math.Hypot(dx, dy)
}

func segDistM(p, a, b [2]float64, cosLat float64) float64 {
	px, py := p[0]*cosLat, p[1]
	ax, ay := a[0]*cosLat, a[1]
	bx, by := b[0]*cosLat, b[1]
	dx, dy := bx-ax, by-ay
	t := 0.0
	if l := dx*dx + dy*dy; l > 0 {
		t = math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/l))
	}
	q := [2]float64{(ax + t*dx) / cosLat, ay + t*dy}
	return distM(p, q, cosLat)
}

func TestSimplifyKeepsWithinTolerance(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	ring := make([][2]float64, 1000)
	for i := range ring {
		ring[i] = [2]float64{10 + float64(i)*0.001, 50 + 0.02*math.Sin(float64(i)/40) + (rng.Float64()-0.5)*0.0008}
	}
	const tol = 100.0
	out := simplify(ring, tol)
	if len(out) >= len(ring) || len(out) < 2 {
		t.Fatalf("simplify kept %d of %d", len(out), len(ring))
	}
	if out[0] != ring[0] || out[len(out)-1] != ring[len(ring)-1] {
		t.Fatal("endpoints not kept")
	}
	cosLat := math.Cos(50.01 * math.Pi / 180)
	// every input vertex must lie within tol (small slack for the local projection) of the kept polyline
	for _, p := range ring {
		best := math.Inf(1)
		for i := 0; i+1 < len(out); i++ {
			best = math.Min(best, segDistM(p, out[i], out[i+1], cosLat))
		}
		if best > tol*1.02 {
			t.Fatalf("vertex %v is %.1f m from simplified line (tol %.0f)", p, best, tol)
		}
	}
}

// TestSimplifyLatitudeAware uses a ring spanning 25N to 55N with wide
// east-west wiggle; each vertex is measured against the kept polyline in a
// local projection at that vertex's own latitude.
func TestSimplifyLatitudeAware(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	var ring [][2]float64
	for i := 0; i <= 3000; i++ {
		lat := 25 + 30*float64(i)/3000
		ring = append(ring, [2]float64{-100 + 3*math.Sin(float64(i)/25) + (rng.Float64()-0.5)*0.01, lat})
	}
	const tol = 50.0
	out := simplify(ring, tol)
	if len(out) >= len(ring) {
		t.Fatal("nothing removed")
	}
	for _, p := range ring {
		cosLat := math.Cos(p[1] * math.Pi / 180)
		best := math.Inf(1)
		for i := 0; i+1 < len(out); i++ {
			best = math.Min(best, segDistM(p, out[i], out[i+1], cosLat))
		}
		if best > tol {
			t.Fatalf("vertex %v is %.2f m from simplified line (tol %.0f)", p, best, tol)
		}
	}
}

func writeNetwork(t *testing.T, coords [][2]float64) string {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"type": "FeatureCollection", "features": []any{
		map[string]any{"type": "Feature", "properties": map[string]any{}, "geometry": map[string]any{"type": "LineString", "coordinates": coords}},
	}})
	p := filepath.Join(t.TempDir(), "net.geojson")
	os.WriteFile(p, b, 0o644)
	return p
}

func square(lon, lat, d float64) [][2]float64 {
	return [][2]float64{{lon, lat}, {lon, lat + d}, {lon + d, lat + d}, {lon + d, lat}, {lon, lat}}
}

func TestCorridorKeepsNearRings(t *testing.T) {
	net := writeNetwork(t, [][2]float64{{0, 0}, {0.1, 0.1}})
	cells, err := corridor.Cells(net, 30, cellSize)
	if err != nil {
		t.Fatal(err)
	}
	near := square(0.05, 10.0/60+0.1, 0.01) // ~10 NM north of the node
	far := square(0.05, 100.0/60+0.1, 0.01) // ~100 NM north
	if !touchesCorridor(near, cells) {
		t.Fatal("near ring should be kept")
	}
	if touchesCorridor(far, cells) {
		t.Fatal("far ring should be dropped")
	}
}

func TestCorridorWrapsAntimeridian(t *testing.T) {
	net := writeNetwork(t, [][2]float64{{179.99, 0}, {179.98, 0.1}})
	cells, _ := corridor.Cells(net, 30, cellSize)
	if !touchesCorridor(square(-179.9, 0, 0.01), cells) {
		t.Fatal("ring across the antimeridian should be kept")
	}
}

// decode is the Task 3 loader signature.
func decode(r io.Reader) (header, [][][2]float64, error) {
	var h header
	zr, err := gzip.NewReader(r)
	if err != nil {
		return h, nil, err
	}
	br := bufio.NewReader(zr)
	magic, err := br.ReadString('\n')
	if err != nil || magic != "SRLAND1\n" {
		return h, nil, errors.New("bad magic")
	}
	line, err := br.ReadBytes('\n')
	if err != nil {
		return h, nil, err
	}
	if err := json.Unmarshal(line, &h); err != nil {
		return h, nil, err
	}
	rings := make([][][2]float64, 0, h.Rings)
	for i := 0; i < h.Rings; i++ {
		n, err := binary.ReadUvarint(br)
		if err != nil {
			return h, nil, err
		}
		ring := make([][2]float64, n)
		var x, y int64
		for j := range ring {
			dx, err := binary.ReadVarint(br)
			if err != nil {
				return h, nil, err
			}
			dy, err := binary.ReadVarint(br)
			if err != nil {
				return h, nil, err
			}
			x += dx
			y += dy
			ring[j] = [2]float64{float64(x) / 1e6, float64(y) / 1e6}
		}
		rings = append(rings, ring)
	}
	return h, rings, nil
}

func TestEncodeRoundTrip(t *testing.T) {
	rings := [][][2]float64{
		{{-179.123456, -85.5}, {12.345678, 45.000001}, {179.999999, 0.000001}, {-179.123456, -85.5}},
		square(3.25, 51.5, 0.5),
	}
	var buf bytes.Buffer
	hdr := header{Source: "osm-land-polygons-split-4326", Date: "2026-10-23", ToleranceM: 200, CorridorNM: 30, Rings: len(rings)}
	if err := encode(&buf, hdr, rings); err != nil {
		t.Fatal(err)
	}
	h, got, err := decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if h != hdr {
		t.Fatalf("header %+v != %+v", h, hdr)
	}
	if len(got) != len(rings) {
		t.Fatalf("rings %d", len(got))
	}
	for i := range rings {
		if len(got[i]) != len(rings[i]) {
			t.Fatalf("ring %d len", i)
		}
		for j := range rings[i] {
			for k := 0; k < 2; k++ {
				if math.Abs(got[i][j][k]-rings[i][j][k]) > 1e-6 {
					t.Fatalf("ring %d vertex %d: %v vs %v", i, j, got[i][j], rings[i][j])
				}
			}
		}
	}
}
