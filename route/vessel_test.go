package route

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestSquat(t *testing.T) {
	for _, c := range []struct {
		name     string
		cb, v    float64
		confined bool
		want     float64
	}{
		{"open 0.80 at 14 kn", 0.80, 14, false, 1.568},
		{"confined 0.80 at 14 kn", 0.80, 14, true, 3.136},
		{"open 0.60 at 20 kn", 0.60, 20, false, 2.4},
	} {
		if got := Squat(c.cb, c.v, c.confined); !approx(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDataMargin(t *testing.T) {
	for draught, want := range map[float64]float64{10: 2.0, 25: 2.5, 15: 2.0} {
		if got := DataMargin(draught); !approx(got, want) {
			t.Errorf("draught %v: got %v, want %v", draught, got, want)
		}
	}
}

func TestDepthLimit(t *testing.T) {
	got := Vessel{10, 14, "tanker"}.DepthLimit(false)
	want := DepthLimit{DraughtM: 10, SquatM: 1.568, MarginM: 2, LimitM: 13.568}
	if !approx(got.DraughtM, want.DraughtM) || !approx(got.SquatM, want.SquatM) ||
		!approx(got.MarginM, want.MarginM) || !approx(got.LimitM, want.LimitM) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestWithDefaults(t *testing.T) {
	if got := (Vessel{}).WithDefaults(); got != (Vessel{10, 14, "bulk"}) {
		t.Errorf("got %+v, want bulk 10 m 14 kn", got)
	}
	// Non-zero fields are kept.
	if got := (Vessel{DraughtM: 12, Type: "tanker"}).WithDefaults(); got != (Vessel{12, 14, "tanker"}) {
		t.Errorf("kept fields changed: %+v", got)
	}
}
