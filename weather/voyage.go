package weather

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/captv89/seaRoute/route"
)

// Limits on a voyage request.
const (
	MaxTrackPoints = 20000
	MaxVoyageHours = 2000
	MinSpeedKn     = 6
	MaxSpeedKn     = 30

	// maxAbsLon is how far an unwrapped client longitude may stray.
	maxAbsLon = 540
)

// ErrBadVoyage wraps every error PlanVoyage gives for an unusable request.
var ErrBadVoyage = errors.New("bad voyage request")

// VoyageRequest is a track to sail at a constant speed.
type VoyageRequest struct {
	Track     []route.Position `json:"track"` // [lon, lat] points; legs run between neighbours
	Departure time.Time        `json:"departure"`
	SpeedKn   float64          `json:"speed_kn"`
}

// Sample is the ship's place and the weather there at one moment.
type Sample struct {
	Time      time.Time   `json:"time"`
	Lon       float64     `json:"lon"`
	Lat       float64     `json:"lat"`
	Leg       int         `json:"leg"`
	DistNM    float64     `json:"dist_nm"`
	CourseDeg float64     `json:"course_deg"`
	Weather   *Conditions `json:"weather"` // nil outside the forecast
}

// LegSummary describes one leg of the track. The maxima are nil when no
// sample on the leg has that value.
type LegSummary struct {
	Leg          int       `json:"leg"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end"`
	MaxWindKn    *float64  `json:"max_wind_kn"`
	MaxGustKn    *float64  `json:"max_gust_kn"`
	MaxWaveM     *float64  `json:"max_wave_m"`
	MaxCurrentKn *float64  `json:"max_current_kn"`
	ForecastFrac float64   `json:"forecast_frac"` // share of the leg's time that has weather
}

// Voyage is a planned passage with the weather along it. Run, RunTime and
// ForecastEnd are nil when no forecast run was given.
type Voyage struct {
	Run         *string      `json:"run"`
	RunTime     *time.Time   `json:"run_time"`
	ForecastEnd *time.Time   `json:"forecast_end"`
	Departure   time.Time    `json:"departure"`
	ETA         time.Time    `json:"eta"`
	DistanceNM  float64      `json:"distance_nm"`
	Samples     []Sample     `json:"samples"`
	Legs        []LegSummary `json:"legs"`
}

func badVoyage(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBadVoyage, fmt.Sprintf(format, args...))
}

// wrapLon folds a longitude into [-180, 180).
func wrapLon(lon float64) float64 {
	l := math.Mod(lon+180, 360)
	if l < 0 {
		l += 360
	}
	return l - 180
}

// PlanVoyage sails req.Track at a constant speed and samples the ship every
// whole hour, at each track point and at departure and arrival. Each sample
// carries the forecast from run, or nil weather when run is nil or the time is
// outside it.
func PlanVoyage(run *Run, req VoyageRequest) (Voyage, error) {
	n := len(req.Track)
	switch {
	case n < 2:
		return Voyage{}, badVoyage("track needs at least 2 points, got %d", n)
	case n > MaxTrackPoints:
		return Voyage{}, badVoyage("track has %d points, the limit is %d", n, MaxTrackPoints)
	case !(req.SpeedKn >= MinSpeedKn && req.SpeedKn <= MaxSpeedKn):
		return Voyage{}, badVoyage("speed %v kn is outside %d to %d", req.SpeedKn, MinSpeedKn, MaxSpeedKn)
	}
	track := make([]route.Position, n)
	for i, p := range req.Track {
		lon, lat := p[0], p[1]
		if math.IsNaN(lon) || math.IsInf(lon, 0) || math.Abs(lon) > maxAbsLon {
			return Voyage{}, badVoyage("point %d: longitude %v out of range", i, lon)
		}
		if math.IsNaN(lat) || math.IsInf(lat, 0) || math.Abs(lat) > 90 {
			return Voyage{}, badVoyage("point %d: latitude %v out of range", i, lat)
		}
		track[i] = route.Position{wrapLon(lon), lat}
	}

	dep := req.Departure.UTC()
	legs := n - 1
	cumNM := make([]float64, n) // distance at each track point
	vt := make([]time.Time, n)  // time at each track point
	vt[0] = dep
	for i := 0; i < legs; i++ {
		cumNM[i+1] = cumNM[i] + route.Distance(track[i], track[i+1])/route.MetresPerNM
		hours := cumNM[i+1] / req.SpeedKn
		if hours > MaxVoyageHours {
			return Voyage{}, badVoyage("voyage takes more than %d hours", MaxVoyageHours)
		}
		vt[i+1] = dep.Add(time.Duration(hours * float64(time.Hour)))
	}
	eta := vt[n-1]

	// at builds the sample on leg i at time t.
	at := func(i int, t time.Time) Sample {
		a, b := track[i], track[i+1]
		f := 0.0
		if d := vt[i+1].Sub(vt[i]); d > 0 {
			f = float64(t.Sub(vt[i])) / float64(d)
		}
		p := route.RhumbPoint(a, b, f)
		s := Sample{
			Time:      t,
			Lon:       wrapLon(p[0]),
			Lat:       p[1],
			Leg:       i,
			DistNM:    cumNM[i] + f*(cumNM[i+1]-cumNM[i]),
			CourseDeg: route.RhumbCourse(a, b),
		}
		if run != nil {
			s.Weather = run.At(s.Lat, s.Lon, t).Conditions()
		}
		return s
	}

	samples := make([]Sample, 0, n+int(eta.Sub(dep)/time.Hour)+2)
	leg, hour := 0, dep.Truncate(time.Hour).Add(time.Hour)
	for v := 0; v < n; v++ {
		// Whole hours strictly before this vertex's time.
		for ; hour.Before(vt[v]); hour = hour.Add(time.Hour) {
			for leg < legs-1 && !hour.Before(vt[leg+1]) {
				leg++
			}
			samples = append(samples, at(leg, hour))
		}
		// A whole hour equal to the vertex time is covered by the vertex.
		if hour.Equal(vt[v]) {
			hour = hour.Add(time.Hour)
		}
		samples = append(samples, at(min(v, legs-1), vt[v]))
	}

	summaries := make([]LegSummary, legs)
	hasWeather := make([]bool, legs)
	fracTime := make([]time.Duration, legs)
	for i := range summaries {
		summaries[i] = LegSummary{Leg: i, Start: vt[i], End: vt[i+1]}
	}
	for k, s := range samples {
		ls := &summaries[s.Leg]
		if s.Weather == nil {
			continue
		}
		hasWeather[s.Leg] = true
		// A span counts when the weather is known at both of its ends.
		if k+1 < len(samples) && samples[k+1].Weather != nil {
			fracTime[s.Leg] += samples[k+1].Time.Sub(s.Time)
		}
		raiseTo(&ls.MaxWindKn, s.Weather.WindKn)
		raiseTo(&ls.MaxGustKn, s.Weather.GustKn)
		raiseTo(&ls.MaxWaveM, s.Weather.WaveM)
		raiseTo(&ls.MaxCurrentKn, s.Weather.CurrentKn)
	}
	for i := range summaries {
		switch d := vt[i+1].Sub(vt[i]); {
		case d > 0:
			summaries[i].ForecastFrac = math.Min(1, float64(fracTime[i])/float64(d))
		case hasWeather[i]:
			summaries[i].ForecastFrac = 1
		}
	}

	v := Voyage{
		Departure:  dep,
		ETA:        eta,
		DistanceNM: cumNM[n-1],
		Samples:    samples,
		Legs:       summaries,
	}
	if run != nil {
		id, ref, end := run.ID, run.Ref, run.End()
		v.Run, v.RunTime, v.ForecastEnd = &id, &ref, &end
	}
	return v, nil
}

// raiseTo makes *max the larger of itself and v; a nil v changes nothing.
func raiseTo(max **float64, v *float64) {
	if v != nil && (*max == nil || *v > **max) {
		*max = ptr(*v)
	}
}
