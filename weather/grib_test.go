package weather

import (
	"math"
	"os"
	"testing"
	"time"
)

// The fixture is one real message: ECMWF IFS open data, wave stream, swh
// (significant wave height, m), run 2026-11-01 00 UTC, step 24 h. The expected
// values below were read from the same file with ECMWF ecCodes 2.50.0.
const swhFixture = "testdata/swh-20261101T00-024h.grib2"

func TestDecodeFieldMatchesEcCodes(t *testing.T) {
	msg, err := os.ReadFile(swhFixture)
	if err != nil {
		t.Fatal(err)
	}
	f, err := DecodeField(msg)
	if err != nil {
		t.Fatal(err)
	}

	want := Grid{NX: 1440, NY: 721, Lat0: 90, Lon0: 180, DLat: -0.25, DLon: 0.25}
	if f.Grid != want {
		t.Fatalf("grid %+v, want %+v", f.Grid, want)
	}
	if !f.Ref.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("ref %v", f.Ref)
	}
	if !f.Valid.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("valid %v", f.Valid)
	}
	if len(f.Values) != 1440*721 {
		t.Fatalf("%d values", len(f.Values))
	}

	nan, sum := 0, 0.0
	for _, v := range f.Values {
		if math.IsNaN(float64(v)) {
			nan++
			continue
		}
		sum += float64(v)
	}
	if nan != 372612 {
		t.Errorf("%d missing (land) points, want 372612", nan)
	}
	// ecCodes sums float64 values; Values are float32, so allow the rounding of
	// ~665k values (about 1e-7 relative each), not a decoding difference.
	if math.Abs(sum-1447880.608867) > 1e-6*1447880.608867 {
		t.Errorf("sum %.6f, want 1447880.608867", sum)
	}

	for _, c := range []struct {
		lat, lon float64
		want     float64 // NaN = land
	}{
		{50, -20, 2.285643007606268},
		{0, -140, 1.551268007606268},
		{-40, 20, 2.379393007606268},
		{-60, -60, 2.879393007606268},
		{35, 140, math.NaN()},
		{48, 10, math.NaN()},
	} {
		got := float64(f.At(c.lat, c.lon))
		if math.IsNaN(c.want) {
			if !math.IsNaN(got) {
				t.Errorf("At(%g,%g) = %g, want NaN (land)", c.lat, c.lon, got)
			}
			continue
		}
		if math.Abs(got-c.want) > 1e-6 {
			t.Errorf("At(%g,%g) = %.9f, want %.9f", c.lat, c.lon, got, c.want)
		}
	}
}

func TestDecodeFieldRejectsGarbage(t *testing.T) {
	if _, err := DecodeField([]byte("not a grib message")); err == nil {
		t.Fatal("want error")
	}
}
