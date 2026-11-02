package weather

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/captv89/seaRoute/route"
)

var voyageDep = time.Date(2026, 11, 2, 10, 30, 0, 0, time.UTC)

func TestPlanETA(t *testing.T) {
	v, err := PlanVoyage(nil, VoyageRequest{
		Track:     []route.Position{{0, 0}, {0, 1}, {0, 2}},
		Departure: voyageDep, SpeedKn: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(v.DistanceNM-120) > 0.1 {
		t.Errorf("DistanceNM = %v, want 120", v.DistanceNM)
	}
	if d := v.ETA.Sub(voyageDep.Add(12 * time.Hour)); d < -time.Minute || d > time.Minute {
		t.Errorf("ETA = %v, want departure + 12h", v.ETA)
	}
}

func TestPlanSamplesHourlyAndAtVertices(t *testing.T) {
	// 60 NM legs at 10 kn: vertices at 10:30, 16:30, 22:30.
	v, err := PlanVoyage(nil, VoyageRequest{
		Track:     []route.Position{{0, 0}, {0, 1}, {0, 2}},
		Departure: voyageDep, SpeedKn: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Samples[0].Time; !got.Equal(voyageDep) {
		t.Errorf("first sample at %v, want departure", got)
	}
	if got := v.Samples[1].Time; !got.Equal(time.Date(2026, 11, 2, 11, 0, 0, 0, time.UTC)) {
		t.Errorf("second sample at %v, want 11:00", got)
	}
	var vertex *Sample
	for i := range v.Samples {
		s := &v.Samples[i]
		if i > 0 && !s.Time.After(v.Samples[i-1].Time) {
			t.Errorf("sample %d not after the previous one", i)
		}
		if s.Leg == 1 && vertex == nil {
			vertex = s
		}
	}
	if vertex == nil || math.Abs(vertex.DistNM-60) > 0.1 || vertex.Time.Minute() != 30 || vertex.Time.Hour() != 16 {
		t.Fatalf("first leg-1 sample is not the vertex at 16:30 and 60 NM: %+v", vertex)
	}
	for _, s := range v.Samples {
		wantLeg := 0
		if !s.Time.Before(vertex.Time) {
			wantLeg = 1
		}
		if s.Leg != wantLeg {
			t.Errorf("sample at %v: leg %d, want %d", s.Time, s.Leg, wantLeg)
		}
	}
	last := v.Samples[len(v.Samples)-1]
	if last.Leg != 1 || !last.Time.Equal(v.ETA) {
		t.Errorf("last sample = leg %d at %v, want leg 1 at ETA", last.Leg, last.Time)
	}
	// 10:30 + 12 h = 22:30: 12 hourly samples 11:00..22:00 minus none at the vertex, plus 3 vertices.
	if len(v.Samples) != 12+3 {
		t.Errorf("%d samples, want 15", len(v.Samples))
	}
}

func TestPlanNoRunHasNullWeather(t *testing.T) {
	v, err := PlanVoyage(nil, VoyageRequest{Track: []route.Position{{0, 0}, {0, 1}}, Departure: voyageDep, SpeedKn: 10})
	if err != nil {
		t.Fatal(err)
	}
	if v.Run != nil || v.RunTime != nil || v.ForecastEnd != nil {
		t.Errorf("run fields set without a run: %+v", v)
	}
	for _, s := range v.Samples {
		if s.Weather != nil {
			t.Errorf("weather at %v with no run", s.Time)
		}
	}
	if v.Legs[0].ForecastFrac != 0 {
		t.Errorf("ForecastFrac = %v, want 0", v.Legs[0].ForecastFrac)
	}
}

func TestPlanPastForecastIsNull(t *testing.T) {
	run := newTestRun(t, testGrid, map[FieldID]fill{
		U10: func(_, _, _ int) float32 { return 5 },
		V10: func(_, _, _ int) float32 { return 0 },
	})
	// 60 NM at 10 kn from the run time: 6 h, the run covers the first 3.
	v, err := PlanVoyage(run, VoyageRequest{Track: []route.Position{{20.5, 10}, {20.5, 9}}, Departure: testRef, SpeedKn: 10})
	if err != nil {
		t.Fatal(err)
	}
	if v.Run == nil || *v.Run != run.ID {
		t.Errorf("Run = %v, want %s", v.Run, run.ID)
	}
	for _, s := range v.Samples {
		inside := !s.Time.After(testRef.Add(3 * time.Hour))
		if inside != (s.Weather != nil) {
			t.Errorf("sample at +%v: weather present = %v, want %v", s.Time.Sub(testRef), s.Weather != nil, inside)
		}
	}
	if f := v.Legs[0].ForecastFrac; math.Abs(f-0.5) > 0.001 {
		t.Errorf("ForecastFrac = %v, want 0.5", f)
	}
	if w := v.Legs[0].MaxWindKn; w == nil || math.Abs(*w-5*3600.0/1852) > 0.01 {
		t.Errorf("MaxWindKn = %v", w)
	}
	if v.Legs[0].MaxCurrentKn != nil {
		t.Errorf("MaxCurrentKn = %v, want nil", *v.Legs[0].MaxCurrentKn)
	}
}

func TestPlanAntimeridian(t *testing.T) {
	v, err := PlanVoyage(nil, VoyageRequest{Track: []route.Position{{179.5, 0}, {180.5, 0}}, Departure: voyageDep, SpeedKn: 10})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(v.DistanceNM-60) > 0.1 {
		t.Errorf("DistanceNM = %v, want ~60", v.DistanceNM)
	}
	for _, s := range v.Samples {
		if s.Lon < -180 || s.Lon >= 180 {
			t.Errorf("lon %v outside [-180, 180)", s.Lon)
		}
		if math.Abs(s.CourseDeg-90) > 0.001 {
			t.Errorf("course %v, want 90", s.CourseDeg)
		}
	}
}

func TestPlanZeroLengthLeg(t *testing.T) {
	v, err := PlanVoyage(nil, VoyageRequest{Track: []route.Position{{0, 0}, {0, 0}, {0, 1}}, Departure: voyageDep, SpeedKn: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Legs) != 2 || v.Legs[0].ForecastFrac != 0 {
		t.Errorf("legs = %+v", v.Legs)
	}
	for _, s := range v.Samples {
		if math.IsNaN(s.Lat) || math.IsNaN(s.Lon) || math.IsNaN(s.DistNM) {
			t.Errorf("NaN in %+v", s)
		}
	}
}

func TestPlanRejects(t *testing.T) {
	pts := func(n int) []route.Position { return make([]route.Position, n) }
	cases := map[string]VoyageRequest{
		"one point": {Track: pts(1), Departure: voyageDep, SpeedKn: 10},
		"too many":  {Track: pts(MaxTrackPoints + 1), Departure: voyageDep, SpeedKn: 10},
		"too slow":  {Track: pts(2), Departure: voyageDep, SpeedKn: 5},
		"too fast":  {Track: pts(2), Departure: voyageDep, SpeedKn: 31},
		"bad lat":   {Track: []route.Position{{0, 0}, {0, 91}}, Departure: voyageDep, SpeedKn: 10},
		"NaN lat":   {Track: []route.Position{{0, 0}, {0, math.NaN()}}, Departure: voyageDep, SpeedKn: 10},
		"bad lon":   {Track: []route.Position{{0, 0}, {541, 0}}, Departure: voyageDep, SpeedKn: 10},
		"too long":  {Track: []route.Position{{0, 0}, {0, 80}, {0, -80}, {0, 80}}, Departure: voyageDep, SpeedKn: 6},
	}
	for name, req := range cases {
		if _, err := PlanVoyage(nil, req); !errors.Is(err, ErrBadVoyage) {
			t.Errorf("%s: err = %v, want ErrBadVoyage", name, err)
		}
	}
	for _, kn := range []float64{6, 30} {
		if _, err := PlanVoyage(nil, VoyageRequest{Track: []route.Position{{0, 0}, {0, 1}}, Departure: voyageDep, SpeedKn: kn}); err != nil {
			t.Errorf("speed %v: %v", kn, err)
		}
	}
}
