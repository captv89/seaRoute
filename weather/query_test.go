package weather

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

var testNaN = float32(math.NaN())

// fill gives the value of one field at (step, row, col).
type fill func(step, row, col int) float32

// newTestRun writes a run on grid g with steps {0, 3} h, one fill per field
// (fields without a fill stay NoValue), and opens it.
func newTestRun(t *testing.T, g Grid, fills map[FieldID]fill) *Run {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run.srwx")
	steps := []int{0, 3}
	w, err := CreateRun(path, testRef, g, steps)
	if err != nil {
		t.Fatal(err)
	}
	for f, fn := range fills {
		for s := range steps {
			vals := make([]float32, g.NX*g.NY)
			for y := 0; y < g.NY; y++ {
				for x := 0; x < g.NX; x++ {
					vals[y*g.NX+x] = fn(s, y, x)
				}
			}
			if err := w.Put(f, s, vals); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := OpenRun(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

// cornerFill gives the four cells at rows 0-1, cols 0-1 the values c[row][col]
// and every other cell zero.
func cornerFill(c [2][2]float32) fill {
	return func(_, row, col int) float32 {
		if row < 2 && col < 2 {
			return c[row][col]
		}
		return 0
	}
}

func near(t *testing.T, name string, got float32, want, tol float64) {
	t.Helper()
	if math.IsNaN(float64(got)) || math.Abs(float64(got)-want) > tol {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func isNaN(v float32) bool { return math.IsNaN(float64(v)) }

func TestAtBilinear(t *testing.T) {
	r := newTestRun(t, testGrid, map[FieldID]fill{U10: cornerFill([2][2]float32{{0, 1}, {2, 3}})})
	near(t, "U10", r.At(9.5, 20.5, testRef)[U10], 1.5, 0.01)
}

func TestAtTimeLinear(t *testing.T) {
	r := newTestRun(t, testGrid, map[FieldID]fill{U10: func(step, _, _ int) float32 { return float32(step) * 3 }})
	near(t, "U10", r.At(9.5, 20.5, testRef.Add(time.Hour))[U10], 1.0, 0.01)
}

func TestAtCoastUsesValidCorners(t *testing.T) {
	r := newTestRun(t, testGrid, map[FieldID]fill{U10: cornerFill([2][2]float32{{2, 2}, {2, testNaN}})})
	near(t, "U10", r.At(9.5, 20.5, testRef)[U10], 2.0, 0.01)
}

func TestAtAllLandIsNaN(t *testing.T) {
	r := newTestRun(t, testGrid, map[FieldID]fill{U10: func(_, _, _ int) float32 { return testNaN }})
	v := r.At(9.5, 20.5, testRef)
	for f, x := range v {
		if !isNaN(x) {
			t.Errorf("field %d = %v, want NaN", f, x)
		}
	}
}

func TestAtOutsideForecast(t *testing.T) {
	r := newTestRun(t, testGrid, map[FieldID]fill{U10: func(_, _, _ int) float32 { return 1 }})
	for _, tm := range []time.Time{testRef.Add(-time.Hour), r.End().Add(time.Hour)} {
		if v := r.At(9.5, 20.5, tm); !isNaN(v[U10]) {
			t.Errorf("At(%v) U10 = %v, want NaN", tm, v[U10])
		}
	}
	near(t, "at End", r.At(9.5, 20.5, r.End())[U10], 1, 0.01)
}

func TestAtWaveDirectionAcrossNorth(t *testing.T) {
	r := newTestRun(t, testGrid, map[FieldID]fill{MWD: cornerFill([2][2]float32{{350, 10}, {350, 10}})})
	got := float64(r.At(9.5, 20.5, testRef)[MWD])
	if d := math.Abs(math.Remainder(got, 360)); d > 0.5 {
		t.Errorf("MWD = %v, want about 0", got)
	}
}

func TestAtLonWrap(t *testing.T) {
	g := Grid{NX: 1440, NY: 2, Lat0: 10, Lon0: 180, DLat: -0.25, DLon: 0.25}
	r := newTestRun(t, g, map[FieldID]fill{U10: func(_, _, col int) float32 { return float32(col) / 10 }})
	a, b := r.At(10, 190, testRef)[U10], r.At(10, -170, testRef)[U10]
	near(t, "lon 190", a, 4.0, 0.01) // col 40
	near(t, "lon -170", b, 4.0, 0.01)
	// Across the seam between the last column and the first.
	near(t, "seam", r.At(10, 179.875, testRef)[U10], (143.9+0)/2, 0.05)
}

func TestConditionsUnits(t *testing.T) {
	var v Values
	for i := range v {
		v[i] = testNaN
	}
	v[U10], v[V10], v[Gust], v[MSL] = -10, 0, 12, 101325
	v[CurE], v[CurN] = 0, 1
	v[SWH], v[MWD], v[MWP] = 2.5, 270, 8
	c := v.Conditions()
	if c == nil {
		t.Fatal("nil conditions")
	}
	p := func(name string, got *float64, want float64) {
		t.Helper()
		if got == nil {
			t.Errorf("%s nil", name)
			return
		}
		near(t, name, float32(*got), want, 0.01)
	}
	p("wind_kn", c.WindKn, 19.44)
	p("wind_from_deg", c.WindFromDeg, 90)
	p("gust_kn", c.GustKn, 12*3600.0/1852)
	p("pressure_hpa", c.PressureHPa, 1013.25)
	p("current_kn", c.CurrentKn, 1.94)
	p("current_to_deg", c.CurrentToDeg, 0)
	p("wave_m", c.WaveM, 2.5)
	p("wave_from_deg", c.WaveFromDeg, 270)
	p("wave_period_s", c.WavePeriodS, 8)
}

func TestConditionsPartial(t *testing.T) {
	var v Values
	for i := range v {
		v[i] = testNaN
	}
	v[U10] = 3 // V10 missing: no wind
	v[MSL] = 100000
	c := v.Conditions()
	if c == nil || c.WindKn != nil || c.WindFromDeg != nil || c.PressureHPa == nil {
		t.Errorf("conditions = %+v", c)
	}
}

func TestConditionsNilWhenEmpty(t *testing.T) {
	var v Values
	for i := range v {
		v[i] = testNaN
	}
	if c := v.Conditions(); c != nil {
		t.Errorf("conditions = %+v, want nil", c)
	}
}
