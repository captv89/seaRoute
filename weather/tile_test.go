package weather

import (
	"bytes"
	"errors"
	"image/png"
	"testing"
)

var tileGrid = Grid{NX: 36, NY: 19, Lat0: 90, Lon0: -180, DLat: -10, DLon: 10}

// tileRun has 10 m/s (about 19.4 kn) wind everywhere except the northern
// hemisphere west of the prime meridian, which has no value.
func tileRun(t *testing.T) *Run {
	t.Helper()
	wind := func(_, row, col int) float32 {
		if row < 9 && col < 18 {
			return testNaN
		}
		return 10
	}
	return newTestRun(t, tileGrid, map[FieldID]fill{U10: wind, V10: func(_, row, col int) float32 {
		if row < 9 && col < 18 {
			return testNaN
		}
		return 0
	}})
}

func TestTileRenders(t *testing.T) {
	r := tileRun(t)
	b, err := NewTiles(1<<20, 2).Get(r, "wind", 3, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if bd := img.Bounds(); bd.Dx() != 256 || bd.Dy() != 256 {
		t.Fatalf("size = %v, want 256x256", bd)
	}
	// Southeast quadrant has wind; northwest does not.
	_, _, _, a := img.At(192, 192).RGBA()
	if a>>8 < 170 || a>>8 > 190 {
		t.Errorf("sea alpha = %d, want about 179", a>>8)
	}
	if _, _, _, a := img.At(64, 64).RGBA(); a != 0 {
		t.Errorf("NaN pixel alpha = %d, want 0", a)
	}
}

func TestTileWrapsX(t *testing.T) {
	r := tileRun(t)
	ts := NewTiles(1<<22, 2)
	a, err := ts.Get(r, "wind", 3, 2, 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ts.Get(r, "wind", 3, 2, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("x=5 and x=1 differ at z=2")
	}
}

func TestTileRejects(t *testing.T) {
	r := tileRun(t)
	ts := NewTiles(1<<20, 1)
	for name, c := range map[string]struct {
		field      string
		hour, z, y int
	}{
		"field": {"rain", 3, 0, 0},
		"hour":  {"wind", 7, 0, 0},
		"zoom":  {"wind", 3, 9, 0},
		"negz":  {"wind", 3, -1, 0},
		"y":     {"wind", 3, 2, 4},
		"negy":  {"wind", 3, 2, -1},
	} {
		if _, err := ts.Get(r, c.field, c.hour, c.z, 0, c.y); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", name, err)
		}
	}
}

func TestTileCacheHit(t *testing.T) {
	r := tileRun(t)
	ts := NewTiles(1<<20, 1)
	a, _ := ts.Get(r, "wind", 3, 1, 0, 0)
	b, _ := ts.Get(r, "wind", 3, 1, 0, 0)
	if n := ts.renders.Load(); n != 1 {
		t.Errorf("renders = %d, want 1", n)
	}
	if &a[0] != &b[0] {
		t.Error("second Get did not return the cached slice")
	}
}

func TestTileCacheEvicts(t *testing.T) {
	r := tileRun(t)
	probe, _ := NewTiles(1<<20, 1).Get(r, "wind", 3, 1, 0, 0)
	ts := NewTiles(int64(len(probe))*3/2, 1) // room for one tile, not two
	ts.Get(r, "wind", 3, 1, 0, 0)
	ts.Get(r, "wind", 3, 1, 1, 0)
	ts.Get(r, "wind", 3, 1, 0, 0)
	if n := ts.renders.Load(); n != 3 {
		t.Errorf("renders = %d, want 3 (first tile evicted)", n)
	}
}

func TestScalesShape(t *testing.T) {
	for _, f := range []string{"wind", "waves", "current"} {
		if len(Scales[f]) < 6 {
			t.Errorf("%s has %d stops, want at least 6", f, len(Scales[f]))
		}
	}
}

func BenchmarkTileRender(b *testing.B) {
	g := Grid{NX: 1440, NY: 721, Lat0: 90, Lon0: -180, DLat: -0.25, DLon: 0.25}
	r := benchRun(b, g)
	ts := NewTiles(1, 1) // too small to keep anything: every Get renders
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ts.Get(r, "wind", 3, 3, 4, 3); err != nil {
			b.Fatal(err)
		}
	}
}

func benchRun(b *testing.B, g Grid) *Run {
	path := b.TempDir() + "/run.srwx"
	w, err := CreateRun(path, testRef, g, []int{0, 3})
	if err != nil {
		b.Fatal(err)
	}
	for _, f := range []FieldID{U10, V10} {
		for s := 0; s < 2; s++ {
			vals := make([]float32, g.NX*g.NY)
			for i := range vals {
				vals[i] = float32(i%37) / 2
			}
			if err := w.Put(f, s, vals); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		b.Fatal(err)
	}
	r, err := OpenRun(path)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { r.Close() })
	return r
}

func TestTileReadFailureNotCached(t *testing.T) {
	r := tileRun(t)
	r.Close()
	ts := NewTiles(1<<20, 1)
	if _, err := ts.Get(r, "wind", 3, 0, 0, 0); err == nil {
		t.Fatal("Get on a closed run succeeded")
	}
	if len(ts.byKey) != 0 || ts.renders.Load() != 0 {
		t.Errorf("failed render was cached: %d entries, %d renders", len(ts.byKey), ts.renders.Load())
	}
}

func TestTileNegativeValue(t *testing.T) {
	r := newTestRun(t, tileGrid, map[FieldID]fill{SWH: func(_, _, _ int) float32 { return -5 }})
	if _, err := NewTiles(1<<20, 1).Get(r, "waves", 3, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
}

func TestTileCachedExactSize(t *testing.T) {
	b, err := NewTiles(1<<20, 1).Get(tileRun(t), "wind", 3, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if cap(b) != len(b) {
		t.Errorf("cap %d != len %d", cap(b), len(b))
	}
}
