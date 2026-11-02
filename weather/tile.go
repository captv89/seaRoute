package weather

import (
	"bytes"
	"container/list"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
)

// ErrNotFound means a tile request names no tile: an unknown field, an hour
// that is not a forecast step, or a position outside the tile pyramid.
var ErrNotFound = errors.New("weather: tile not found")

// MaxTileZoom is the deepest zoom level tiles are rendered at. The forecast
// grid is 0.25 degrees, so deeper tiles would only show enlarged pixels.
const MaxTileZoom = 8

const (
	tileSize  = 256
	tileAlpha = 179 // 0.7 of 255
)

// ScaleStop is one colour of a legend: the colour at Value, in display units.
// Colours between stops are blended linearly.
type ScaleStop struct {
	Value float64 `json:"value"`
	Color string  `json:"color"` // "#rrggbb"
}

// Scales gives the colour scale of each tile field: "wind" in knots, "waves"
// in metres and "current" in knots. They are sequential, dark to light with a
// hue shift, so they stay readable with the common colour-vision deficiencies.
var Scales = map[string][]ScaleStop{
	"wind": {
		{0, "#440154"}, {10, "#414487"}, {20, "#2a788e"}, {30, "#22a884"},
		{40, "#7ad151"}, {50, "#fde725"}, {60, "#ffb000"},
	},
	"waves": {
		{0, "#440154"}, {2, "#414487"}, {4, "#2a788e"}, {6, "#22a884"},
		{8, "#7ad151"}, {10, "#fde725"}, {12, "#ffb000"},
	},
	"current": {
		{0, "#440154"}, {0.5, "#414487"}, {1, "#2a788e"}, {1.5, "#22a884"},
		{2, "#7ad151"}, {3, "#fde725"}, {4, "#ffb000"},
	},
}

const lutSize = 512

// tileField says how one tile field is computed from the run.
type tileField struct {
	a, b FieldID // components; b is -1 for a scalar field
	unit float64 // multiplier from the stored unit to the display unit
	lut  [lutSize][3]uint8
	max  float64
}

var tileFields = buildTileFields()

func buildTileFields() map[string]*tileField {
	defs := map[string]*tileField{
		"wind":    {a: U10, b: V10, unit: knotsPerMS},
		"current": {a: CurE, b: CurN, unit: knotsPerMS},
		"waves":   {a: SWH, b: -1, unit: 1},
	}
	for name, f := range defs {
		stops := Scales[name]
		f.max = stops[len(stops)-1].Value
		for i := range f.lut {
			f.lut[i] = blend(stops, f.max*float64(i)/(lutSize-1))
		}
	}
	return defs
}

// blend gives the colour of the scale at v, clamped to its ends.
func blend(stops []ScaleStop, v float64) [3]uint8 {
	if v <= stops[0].Value {
		return parseHex(stops[0].Color)
	}
	for i := 1; i < len(stops); i++ {
		if v <= stops[i].Value {
			lo, hi := stops[i-1], stops[i]
			t := (v - lo.Value) / (hi.Value - lo.Value)
			a, b := parseHex(lo.Color), parseHex(hi.Color)
			var c [3]uint8
			for k := range c {
				c[k] = uint8(math.Round(float64(a[k]) + (float64(b[k])-float64(a[k]))*t))
			}
			return c
		}
	}
	return parseHex(stops[len(stops)-1].Color)
}

func parseHex(s string) [3]uint8 {
	n, _ := strconv.ParseUint(s[1:], 16, 32)
	return [3]uint8{uint8(n >> 16), uint8(n >> 8), uint8(n)}
}

// Tiles renders and caches map tiles of forecast fields. It is safe for
// concurrent use.
type Tiles struct {
	sem      chan struct{} // one token per render in progress
	maxBytes int64
	renders  atomic.Int64 // tiles rendered so far

	mu    sync.Mutex
	bytes int64
	lru   *list.List // front is most recently used; values are *tileEntry
	byKey map[string]*list.Element
}

type tileEntry struct {
	key string
	png []byte
}

// NewTiles returns a tile cache that keeps at most maxBytes of PNG data and
// renders at most workers tiles at a time.
func NewTiles(maxBytes int64, workers int) *Tiles {
	return &Tiles{
		sem:      make(chan struct{}, max(workers, 1)),
		maxBytes: maxBytes,
		lru:      list.New(),
		byKey:    make(map[string]*list.Element),
	}
}

// Get returns the PNG for tile (z, x, y) of field ("wind", "waves" or
// "current") at forecast hour stepHour of run r. Tiles are 256x256 and use the
// Web Mercator layout; x is taken modulo 2^z. The returned slice is shared and
// must not be modified.
func (t *Tiles) Get(r *Run, field string, stepHour, z, x, y int) ([]byte, error) {
	tf := tileFields[field]
	step := -1
	for i, h := range r.Steps {
		if h == stepHour {
			step = i
		}
	}
	if tf == nil || step < 0 || z < 0 || z > MaxTileZoom || y < 0 || y >= 1<<z {
		return nil, ErrNotFound
	}
	x &= 1<<z - 1
	key := fmt.Sprintf("%s/%s/%d/%d/%d/%d", r.ID, field, stepHour, z, x, y)

	t.mu.Lock()
	if e, ok := t.byKey[key]; ok {
		t.lru.MoveToFront(e)
		b := e.Value.(*tileEntry).png
		t.mu.Unlock()
		return b, nil
	}
	t.mu.Unlock()

	t.sem <- struct{}{}
	b, err := render(r, tf, step, z, x, y)
	<-t.sem
	if err != nil {
		return nil, err
	}
	t.renders.Add(1)
	t.put(key, b)
	return b, nil
}

// put stores a tile and evicts the least recently used ones until the cache
// fits. A tile larger than the whole cache is not kept.
func (t *Tiles) put(key string, b []byte) {
	if int64(len(b)) > t.maxBytes {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.byKey[key]; ok {
		return // a concurrent render got here first
	}
	t.byKey[key] = t.lru.PushFront(&tileEntry{key, b})
	t.bytes += int64(len(b))
	for t.bytes > t.maxBytes {
		last := t.lru.Back()
		e := t.lru.Remove(last).(*tileEntry)
		delete(t.byKey, e.key)
		t.bytes -= int64(len(e.png))
	}
}

// render draws one tile. A pixel is transparent where the field has no value.
func render(r *Run, tf *tileField, step, z, x, y int) ([]byte, error) {
	n := float64(uint(1)<<z) * tileSize
	lons := make([]float64, tileSize)
	lats := make([]float64, tileSize)
	for i := range lons {
		lons[i] = (float64(x*tileSize+i)+0.5)/n*360 - 180
		lats[i] = math.Atan(math.Sinh(math.Pi*(1-2*(float64(y*tileSize+i)+0.5)/n))) * 180 / math.Pi
	}
	cache := r.newRowCache()
	failed := false
	src := func(f FieldID, stepIdx, row int) ([]int16, bool) {
		rw, ok := cache.get(f, stepIdx, row)
		if !ok {
			failed = true
		}
		return rw, ok
	}
	img := image.NewNRGBA(image.Rect(0, 0, tileSize, tileSize))
	for py, lat := range lats {
		for px, lon := range lons {
			cs := r.stencil(lat, lon)
			if len(cs) == 0 {
				continue
			}
			v := float64(r.scalarCorners(src, cs, tf.a, step))
			if tf.b >= 0 {
				v = math.Hypot(v, float64(r.scalarCorners(src, cs, tf.b, step)))
			}
			if math.IsNaN(v) {
				continue
			}
			v = math.Max(0, math.Min(v*tf.unit, tf.max)) // corrupt data may be negative
			i := int(math.Round(v / tf.max * (lutSize - 1)))
			c := tf.lut[i]
			o := img.PixOffset(px, py)
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = c[0], c[1], c[2], tileAlpha
		}
	}
	if failed {
		return nil, errors.New("weather: tile: forecast rows could not be read")
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	return out, nil // exact size, so the cache byte cap holds
}
