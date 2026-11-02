package weather

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var testGrid = Grid{NX: 4, NY: 3, Lat0: 10, Lon0: 20, DLat: -1, DLon: 1}

var testRef = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

// readAll returns every value of one field at one step, decoded.
func readAll(t *testing.T, r *Run, f FieldID, step int) []float32 {
	t.Helper()
	var out []float32
	row := make([]int16, r.Grid.NX)
	for y := 0; y < r.Grid.NY; y++ {
		if err := r.readRow(f, step, y, row); err != nil {
			t.Fatal(err)
		}
		for _, q := range row {
			out = append(out, r.decode(f, q))
		}
	}
	return out
}

func makeRun(t *testing.T, put func(w *RunWriter)) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run.srwx")
	w, err := CreateRun(path, testRef, testGrid, []int{0, 3})
	if err != nil {
		t.Fatal(err)
	}
	put(w)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunRoundTrip(t *testing.T) {
	vals := make([]float32, 12)
	for i := range vals {
		vals[i] = 1.234
	}
	vals[5] = float32(math.NaN())
	path := makeRun(t, func(w *RunWriter) {
		if err := w.Put(U10, 1, vals); err != nil {
			t.Fatal(err)
		}
	})
	r, err := OpenRun(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.ID != "2026110212" || !r.Ref.Equal(testRef) || r.Grid != testGrid || len(r.Steps) != 2 {
		t.Fatalf("header: %+v", r)
	}
	got := readAll(t, r, U10, 1)
	for i, v := range got {
		if i == 5 {
			if !math.IsNaN(float64(v)) {
				t.Errorf("idx 5 = %v, want NaN", v)
			}
			continue
		}
		if math.Abs(float64(v)-1.23) > 0.005 {
			t.Errorf("idx %d = %v, want 1.23", i, v)
		}
	}
	if !r.ValidTime(1).Equal(testRef.Add(3 * time.Hour)) {
		t.Errorf("ValidTime(1) = %v", r.ValidTime(1))
	}
	if !r.End().Equal(testRef.Add(3 * time.Hour)) {
		t.Errorf("End = %v", r.End())
	}
}

func TestRunUnwrittenIsNoValue(t *testing.T) {
	path := makeRun(t, func(w *RunWriter) {
		if err := w.Put(U10, 0, make([]float32, 12)); err != nil {
			t.Fatal(err)
		}
	})
	r, err := OpenRun(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for s := 0; s < 2; s++ {
		for i, v := range readAll(t, r, MWD, s) {
			if !math.IsNaN(float64(v)) {
				t.Fatalf("MWD step %d idx %d = %v, want NaN", s, i, v)
			}
		}
	}
}

func TestRunMSLOffset(t *testing.T) {
	vals := make([]float32, 12)
	for i := range vals {
		vals[i] = 101325
	}
	path := makeRun(t, func(w *RunWriter) {
		if err := w.Put(MSL, 0, vals); err != nil {
			t.Fatal(err)
		}
	})
	r, err := OpenRun(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if v := readAll(t, r, MSL, 0)[0]; math.Abs(float64(v)-101325) > 1 {
		t.Errorf("MSL = %v", v)
	}
}

func TestRunClamp(t *testing.T) {
	vals := make([]float32, 12)
	vals[0] = 1000
	path := makeRun(t, func(w *RunWriter) {
		if err := w.Put(U10, 0, vals); err != nil {
			t.Fatal(err)
		}
	})
	r, err := OpenRun(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if v := readAll(t, r, U10, 0)[0]; math.Abs(float64(v)-327.67) > 0.005 {
		t.Errorf("clamped = %v, want 327.67", v)
	}
}

func TestOpenRunRejectsTruncated(t *testing.T) {
	path := makeRun(t, func(w *RunWriter) {})
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, st.Size()-1); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRun(path); err == nil {
		t.Fatal("want error for truncated file")
	}
}

func TestOpenRunRejectsBadMagic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad")
	if err := os.WriteFile(path, []byte("NOPE!\n{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRun(path); err == nil {
		t.Fatal("want error for bad magic")
	}
}

func TestDefaultSteps(t *testing.T) {
	s := DefaultSteps()
	if len(s) != 85 || s[0] != 0 || s[48] != 144 || s[49] != 150 || s[84] != 360 {
		t.Fatalf("steps: len %d, %v", len(s), s)
	}
}

func TestRunID(t *testing.T) {
	if got := RunID(testRef); got != "2026110212" {
		t.Fatalf("RunID = %q", got)
	}
}
