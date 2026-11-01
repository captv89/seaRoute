package route

import (
	"math"
	"math/rand/v2"
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
	// Heap growth caused by the land load itself: other tests may already hold graphs.
	var before, m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	l, err := LoadLand("../dataset/land_osm.bin.gz")
	if err != nil {
		t.Fatal(err)
	}
	el := time.Since(start)
	runtime.GC()
	runtime.ReadMemStats(&m)
	grown := int64(m.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("load %v heap growth %.1f MB version %s tolerance %v", el, float64(grown)/1e6, l.Version, l.Tolerance)
	if l.CorridorNM != 30 {
		t.Errorf("CorridorNM = %v, want 30", l.CorridorNM)
	}
	if el > 5*time.Second {
		t.Errorf("load too slow: %v", el)
	}
	if grown > 300e6 {
		t.Errorf("heap growth too large: %d", grown)
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

// bruteClearance is SegmentClearance without the cell walk and the bounding-box
// prune: the minimum over every edge, capped at limit.
func bruteClearance(l *Land, a, b Position, limit float64) float64 {
	seen := map[uint32]bool{}
	var edges []uint32
	for _, e := range l.refs {
		if !seen[e] {
			seen[e] = true
			edges = append(edges, e)
		}
	}
	best := limit
	for _, pc := range splitAntimeridian(a, b) {
		ma, mb := Mercator(pc[0]), Mercator(pc[1])
		for _, e := range edges {
			best = math.Min(best, l.edgeClearance(ma, mb, e))
		}
	}
	return best
}

func TestSegmentClearanceMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	for _, tc := range []struct {
		name     string
		lon0     float64
		lat0     float64
		spanLon  float64
		spanLat  float64
		crossing bool
	}{
		{"lat50", 0, 50, 4, 2, false},
		{"lat70", 20, 70, 6, 2, false},
		{"antimeridian", 178, 40, 4, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rings [][]Position
			for range 40 {
				cx := tc.lon0 + rng.Float64()*tc.spanLon
				cy := tc.lat0 + rng.Float64()*tc.spanLat
				r := 0.02 + rng.Float64()*0.1
				var ring []Position
				for k := range 6 {
					ang := 2 * math.Pi * float64(k) / 6
					lon := cx + r*(0.7+0.6*rng.Float64())*math.Cos(ang)
					if lon > 180 {
						lon -= 360
					}
					ring = append(ring, Position{lon, cy + r*(0.7+0.6*rng.Float64())*math.Sin(ang)})
				}
				rings = append(rings, append(ring, ring[0]))
			}
			l := NewLand(rings, 0, "test")
			pt := func() Position {
				lon := tc.lon0 + rng.Float64()*tc.spanLon
				if lon > 180 {
					lon -= 360
				}
				return Position{lon, tc.lat0 + rng.Float64()*tc.spanLat}
			}
			zero := 0
			for range 400 {
				a, b := pt(), pt()
				for _, limit := range []float64{2 * MetresPerNM, 20 * MetresPerNM} {
					got, want := l.SegmentClearance(a, b, limit), bruteClearance(l, a, b, limit)
					if math.Abs(got-want) > 1e-6*math.Max(1, want) {
						t.Fatalf("%v -> %v limit %.0f: got %.3f, brute force %.3f", a, b, limit, got, want)
					}
					if want == 0 {
						zero++
					}
				}
			}
			if zero == 0 {
				t.Error("no random leg touched land: the test does not exercise the prune")
			}
		})
	}
}
