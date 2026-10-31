package route

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
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
	if err := WriteDepth(&buf, tiles, "2026-01-01"); err != nil {
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
	if err := WriteDepth(&buf, []DepthTile{shoalTile()}, "2026-01-01"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "d.bin.gz")
	os.WriteFile(p, buf.Bytes()[:buf.Len()/2], 0o644)
	if _, err := LoadDepth(p); err == nil {
		t.Error("truncated file should fail")
	}
}
