package route

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const depthCell = depthTileDeg / depthCellsPerTile

func deepCells() []uint8 {
	m := make([]uint8, depthCellsPerTile*depthCellsPerTile)
	for i := range m {
		m[i] = 255
	}
	return m
}

// cellCentre returns the centre of cell (i, j) in tile (cx, cy).
func cellCentre(cx, cy int32, i, j int) Position {
	return Position{float64(cx)*depthTileDeg + (float64(i)+0.5)*depthCell, float64(cy)*depthTileDeg + (float64(j)+0.5)*depthCell}
}

func shoalTile() DepthTile {
	m := deepCells()
	m[20*depthCellsPerTile+10] = 7
	return DepthTile{CX: 0, CY: 0, M: m}
}

func TestDepthAt(t *testing.T) {
	d := NewDepth([]DepthTile{shoalTile(), {CX: 1, CY: 0, Deep: true}}, "x/gebco2026")
	if m, ok := d.At(cellCentre(0, 0, 10, 20)); !ok || m != 7 {
		t.Errorf("shoal cell: got %v, %v", m, ok)
	}
	if m, ok := d.At(cellCentre(0, 0, 11, 20)); !ok || m != 255 {
		t.Errorf("neighbour cell: got %v, %v", m, ok)
	}
	if m, ok := d.At(Position{0.75, 0.25}); !ok || m != 255 {
		t.Errorf("deep tile: got %v, %v", m, ok)
	}
	if _, ok := d.At(Position{5, 5}); ok {
		t.Error("outside coverage should be unknown")
	}
}

func TestSegmentMinCrossesShoal(t *testing.T) {
	d := NewDepth([]DepthTile{shoalTile()}, "x/gebco2026")
	a, b := cellCentre(0, 0, 2, 20), cellCentre(0, 0, 40, 20)
	if m, ok := d.SegmentMin(a, b); !ok || m != 7 {
		t.Errorf("crossing leg: got %v, %v", m, ok)
	}
	a, b = cellCentre(0, 0, 2, 22), cellCentre(0, 0, 40, 22)
	if m, ok := d.SegmentMin(a, b); !ok || m != 255 {
		t.Errorf("parallel leg: got %v, %v", m, ok)
	}
	if _, ok := d.SegmentMin(a, Position{3, 3}); ok {
		t.Error("leg leaving coverage should be unknown")
	}
}

func TestSegmentMinAntimeridian(t *testing.T) {
	d := NewDepth([]DepthTile{{CX: -360, CY: 0, Deep: true}, {CX: 359, CY: 0, Deep: true}}, "x/gebco2026")
	m, ok := d.SegmentMin(Position{179.9, 0.2}, Position{-179.9, 0.2})
	if !ok || m != 255 {
		t.Errorf("got %v, %v", m, ok)
	}
}

func TestLoadDepthRoundTrip(t *testing.T) {
	tiles := []DepthTile{shoalTile(), {CX: -360, CY: -3, Deep: true}}
	var buf bytes.Buffer
	if err := WriteDepth(&buf, tiles, nil, "2026-01-01"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "d.bin.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := LoadDepth(p)
	if err != nil {
		t.Fatal(err)
	}
	if d.Version != "2026-01-01/gebco2026" {
		t.Errorf("version %q", d.Version)
	}
	if m, ok := d.At(cellCentre(0, 0, 10, 20)); !ok || m != 7 {
		t.Errorf("shoal: %v %v", m, ok)
	}
	if m, ok := d.At(Position{-179.9, -1.2}); !ok || m != 255 {
		t.Errorf("deep tile: %v %v", m, ok)
	}
	if _, ok := d.At(Position{9, 9}); ok {
		t.Error("outside coverage should be unknown")
	}
}

func TestLoadDepthRejectsTruncated(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteDepth(&buf, []DepthTile{shoalTile()}, nil, "2026-01-01"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "d.bin.gz")
	if err := os.WriteFile(p, buf.Bytes()[:buf.Len()/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDepth(p); err == nil {
		t.Error("truncated file should fail")
	}
}

// maskWith returns a mask that is 255 everywhere except the given tiles.
func maskWith(shallow map[[2]int]uint8) []uint8 {
	m := make([]uint8, depthMaskW*depthMaskH)
	for i := range m {
		m[i] = 255
	}
	for k, v := range shallow {
		m[(k[1]+180)*depthMaskW+k[0]+360] = v
	}
	return m
}

func TestDeepTilesMask(t *testing.T) {
	// Shallow tile (cx=4, cy=0) covers lon 2..2.5, lat 0..0.5.
	d := NewDepthMask(nil, maskWith(map[[2]int]uint8{{4, 0}: 30}), "x/gebco2026")
	if got := d.TileMinDepth(Position{2.2, 0.2}); got != 30 {
		t.Errorf("TileMinDepth = %v, want 30", got)
	}
	if !d.DeepTiles(Position{0.1, 0.2}, Position{1.9, 0.2}, 60) {
		t.Error("leg clear of the shoal tile should be deep")
	}
	if d.DeepTiles(Position{0.1, 0.2}, Position{4, 0.2}, 60) {
		t.Error("leg through the shoal tile should not be deep")
	}
	if !d.DeepTiles(Position{0.1, 0.2}, Position{4, 0.2}, 20) {
		t.Error("30 m tile passes a 20 m limit")
	}
	// Round trip through the file.
	var buf bytes.Buffer
	if err := WriteDepth(&buf, nil, maskWith(map[[2]int]uint8{{4, 0}: 30}), "2026-01-01"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "d.bin.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := LoadDepth(p)
	if err != nil {
		t.Fatal(err)
	}
	if l.DeepTiles(Position{0.1, 0.2}, Position{4, 0.2}, 60) || !l.DeepTiles(Position{0.1, 0.2}, Position{1.9, 0.2}, 60) {
		t.Error("loaded mask differs")
	}
	if NewDepth(nil, "x").DeepTiles(Position{0, 0}, Position{1, 0}, 60) {
		t.Error("absent mask must read as shallow")
	}
}

func TestDeepTilesAntimeridian(t *testing.T) {
	// Shallow tile cx=-360 spans lon -180..-179.5, just east of the antimeridian.
	d := NewDepthMask(nil, maskWith(map[[2]int]uint8{{-360, 0}: 10}), "x/gebco2026")
	if d.DeepTiles(Position{179.9, 0.2}, Position{-179.9, 0.2}, 60) {
		t.Error("leg crossing the shallow tile west of 180 should not be deep")
	}
	if !d.DeepTiles(Position{179.9, 0.2}, Position{-179.9, 1.2}, 5) {
		t.Error("10 m tile passes a 5 m limit across the antimeridian")
	}
	d = NewDepthMask(nil, maskWith(nil), "x/gebco2026")
	if !d.DeepTiles(Position{179.9, 0.2}, Position{-179.9, 0.2}, 60) {
		t.Error("all-deep mask should be deep across the antimeridian")
	}
}

// TestDeepTilesRealData checks the worldwide mask of the built GEBCO dataset.
func TestDeepTilesRealData(t *testing.T) {
	if _, err := os.Stat(depthData); err != nil {
		t.Skip("depth dataset not built yet")
	}
	start := time.Now()
	d, err := LoadDepth(depthData)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("LoadDepth %v, %d tiles, version %s", time.Since(start), len(d.tiles), d.Version)
	if !d.DeepTiles(Position{-150, 30}, Position{-140, 30}, 255) {
		t.Error("mid-Pacific leg 30N 150W-140W should be deep")
	}
	if d.DeepTiles(Position{-157, 20}, Position{-154, 22}, 60) {
		t.Error("leg across Hawaii should not be deep")
	}
}

func TestLoadAllWithDepth(t *testing.T) {
	g := testDepthGraph(t)
	if g.depth == nil || !strings.Contains(g.Version(), "gebco2026") {
		t.Errorf("depth %v, version %q; want depth loaded and gebco2026 in version", g.depth != nil, g.Version())
	}
}
