package route

import (
	"math"
	"runtime"
	"testing"
	"time"
)

const degLatM = earthRadiusM * math.Pi / 180

func island() *Land {
	ring := []Position{{0, 50}, {1, 50}, {1, 51}, {0, 51}, {0, 50}}
	return NewLand([][]Position{ring}, 50, "test")
}

func TestSegmentClearanceCrossing(t *testing.T) {
	if d := island().SegmentClearance(Position{-1, 50.5}, Position{2, 50.5}, 10000); d != 0 {
		t.Fatalf("got %v want 0", d)
	}
}

func TestSegmentClearanceParallel(t *testing.T) {
	d := island().SegmentClearance(Position{0, 51.05}, Position{1, 51.05}, 100000)
	if want := 0.05 * degLatM; math.Abs(d-want) > 20 {
		t.Fatalf("got %v want %v", d, want)
	}
}

func TestSegmentClearanceCapped(t *testing.T) {
	if d := island().SegmentClearance(Position{10, 60}, Position{11, 60}, 1000); d != 1000 {
		t.Fatalf("got %v want 1000", d)
	}
}

func TestPointDistance(t *testing.T) {
	lon := 1 + 2.0/60/math.Cos(50.5*math.Pi/180)
	d := island().PointDistance(Position{lon, 50.5}, 100000)
	if math.Abs(d-1852*2) > 20 {
		t.Fatalf("got %v want 3704", d)
	}
}

func TestLongRhumbLegChecked(t *testing.T) {
	l := island()
	if d := l.SegmentClearance(Position{-10, 50.5}, Position{5, 50.5}, 1e6); d != 0 {
		t.Fatalf("crossing got %v", d)
	}
	d := l.SegmentClearance(Position{-10, 52}, Position{5, 52}, 2e5)
	if want := degLatM; math.Abs(d-want) > 200 {
		t.Fatalf("got %v want %v", d, want)
	}
}

func TestSplitAntimeridian(t *testing.T) {
	p := splitAntimeridian(Position{179, 0}, Position{-179, 1})
	if len(p) != 2 || p[0][0] != (Position{179, 0}) || p[0][1][0] != 180 || p[1][0][0] != -180 || p[1][1] != (Position{-179, 1}) {
		t.Fatalf("got %v", p)
	}
	if math.Abs(p[0][1][1]-p[1][0][1]) > 1e-12 || p[0][1][1] <= 0 || p[0][1][1] >= 1 {
		t.Fatalf("bad crossing lat %v", p)
	}
	if q := splitAntimeridian(Position{0, 0}, Position{10, 1}); len(q) != 1 {
		t.Fatalf("got %v", q)
	}
}

func TestLoadLandRejectsGarbage(t *testing.T) {
	if _, err := LoadLand("nonexistent.bin.gz"); err == nil {
		t.Fatal("want error")
	}
}

func TestLoadRealLand(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	start := time.Now()
	l, err := LoadLand("../dataset/land_osm.bin.gz")
	if err != nil {
		t.Fatal(err)
	}
	el := time.Since(start)
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	t.Logf("load %v heap %.1f MB version %s tolerance %v", el, float64(m.HeapAlloc)/1e6, l.Version, l.Tolerance)
	if el > 5*time.Second {
		t.Errorf("load too slow: %v", el)
	}
	if m.HeapAlloc > 300e6 {
		t.Errorf("heap too large: %d", m.HeapAlloc)
	}
	if d := l.PointDistance(Position{1.335, 51.115}, 100000); d >= 1852 {
		t.Errorf("Dover entrance %v m", d)
	}
	if d := l.PointDistance(Position{1.4, 51.0}, 100000); d <= 5*1852 {
		t.Errorf("mid-Channel (1.4,51.0; the brief point 1.6,50.9 is 3.1 km off Cap Gris-Nez) %v m", d)
	}
	runtime.KeepAlive(l)
}

func TestWithinMatchesPointDistance(t *testing.T) {
	l := island()
	for lon := -1.0; lon <= 2.0; lon += 0.07 {
		for lat := 49.0; lat <= 52.0; lat += 0.07 {
			p := Position{lon, lat}
			if got, want := l.Within(p, 20*1852), l.PointDistance(p, 20*1852) < 20*1852; got != want {
				t.Fatalf("%v: Within %v, PointDistance says %v", p, got, want)
			}
		}
	}
}
