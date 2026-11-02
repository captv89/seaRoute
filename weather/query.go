package weather

import (
	"math"
	"time"
)

// Values holds one value per field at a point and time. NaN means no value
// (land, outside the forecast, or a field missing from the run). Units are as
// stored: SI, pressure in Pa, and Values[MWD] in degrees.
type Values [NumFields]float32

// Conditions is Values converted for display: knots, hPa and compass degrees.
// A nil field has no value.
type Conditions struct {
	WindKn       *float64 `json:"wind_kn,omitempty"`
	WindFromDeg  *float64 `json:"wind_from_deg,omitempty"`
	GustKn       *float64 `json:"gust_kn,omitempty"`
	PressureHPa  *float64 `json:"pressure_hpa,omitempty"`
	CurrentKn    *float64 `json:"current_kn,omitempty"`
	CurrentToDeg *float64 `json:"current_to_deg,omitempty"`
	WaveM        *float64 `json:"wave_m,omitempty"`
	WaveFromDeg  *float64 `json:"wave_from_deg,omitempty"`
	WavePeriodS  *float64 `json:"wave_period_s,omitempty"`
}

const knotsPerMS = 3600.0 / 1852

func nan32() float32 { return float32(math.NaN()) }

// At returns the forecast at a position and time: bilinear in space, linear in
// time between the two surrounding steps. Times outside [Ref, End] give all
// NaN. A field that is missing on either side of the time step is NaN.
func (r *Run) At(lat, lon float64, t time.Time) Values {
	var out Values
	for i := range out {
		out[i] = nan32()
	}
	if t.Before(r.Ref) || t.After(r.End()) {
		return out
	}
	s1 := 0
	for s1 < len(r.Steps)-1 && r.ValidTime(s1).Before(t) {
		s1++
	}
	s0, w := s1, 0.0
	if r.ValidTime(s1).After(t) && s1 > 0 {
		s0 = s1 - 1
		w = float64(t.Sub(r.ValidTime(s0))) / float64(r.ValidTime(s1).Sub(r.ValidTime(s0)))
	}
	for f := FieldID(0); f < NumFields; f++ {
		if f == MWD {
			sin0, cos0, ok0 := r.spatialVector(f, s0, lat, lon)
			sin1, cos1, ok1 := r.spatialVector(f, s1, lat, lon)
			if ok0 && ok1 {
				s, c := sin0+(sin1-sin0)*w, cos0+(cos1-cos0)*w
				out[f] = float32(math.Mod(math.Atan2(s, c)*180/math.Pi+360, 360))
			}
			continue
		}
		a, b := float64(r.spatial(f, s0, lat, lon)), float64(r.spatial(f, s1, lat, lon))
		if !math.IsNaN(a) && !math.IsNaN(b) {
			out[f] = float32(a + (b-a)*w)
		}
	}
	return out
}

// corner is one grid point of a bilinear stencil.
type corner struct {
	row, col int
	w        float64
}

// stencil returns the up to four grid points around (lat, lon) with their
// bilinear weights. Points beyond the first or last row are left out, so
// callers renormalize over what remains. Longitude wraps when the grid spans
// the globe; otherwise points beyond its east edge are left out.
func (r *Run) stencil(lat, lon float64) []corner {
	g := r.Grid
	y := (lat - g.Lat0) / g.DLat
	lon = math.Mod(lon+180, 360)
	if lon < 0 {
		lon += 360
	}
	lon -= 180 // [-180, 180)
	d := math.Mod(lon-g.Lon0, 360)
	if d < 0 {
		d += 360
	}
	x := d / g.DLon
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(y, 0) {
		return nil
	}
	global := float64(g.NX)*g.DLon >= 360-g.DLon/2
	y0, x0 := math.Floor(y), math.Floor(x)
	fy, fx := y-y0, x-x0
	var cs []corner
	for dy := 0; dy < 2; dy++ {
		row := int(y0) + dy
		wy := fy
		if dy == 0 {
			wy = 1 - fy
		}
		if row < 0 || row >= g.NY || wy == 0 {
			continue
		}
		for dx := 0; dx < 2; dx++ {
			col := int(x0) + dx
			wx := fx
			if dx == 0 {
				wx = 1 - fx
			}
			if wx == 0 {
				continue
			}
			if global {
				col %= g.NX
			} else if col >= g.NX {
				continue
			}
			cs = append(cs, corner{row, col, wy * wx})
		}
	}
	return cs
}

// sample reads the stencil values of one field at one step, calling use with
// each valid value and its weight. Missing values are skipped. It returns the
// total weight of the valid points (0 when none, or on a read error).
func (r *Run) sample(f FieldID, stepIdx int, lat, lon float64, use func(v, w float64)) float64 {
	cs := r.stencil(lat, lon)
	if len(cs) == 0 {
		return 0
	}
	rows := make(map[int][]int16, 2)
	for _, c := range cs {
		if _, ok := rows[c.row]; ok {
			continue
		}
		buf := make([]int16, r.Grid.NX)
		if err := r.readRow(f, stepIdx, c.row, buf); err != nil {
			return 0
		}
		rows[c.row] = buf
	}
	total := 0.0
	for _, c := range cs {
		v := r.decode(f, rows[c.row][c.col])
		if math.IsNaN(float64(v)) {
			continue
		}
		use(float64(v), c.w)
		total += c.w
	}
	return total
}

// spatial is the bilinear value of one scalar field at one forecast step.
// Missing corners (land, off-grid rows) are dropped and the remaining weights
// renormalized; NaN when no corner has a value. It does not interpolate in
// time, so other callers (map tiles) can use it for a single step. For MWD it
// averages the direction as a vector and returns degrees.
func (r *Run) spatial(f FieldID, stepIdx int, lat, lon float64) float32 {
	if f == MWD {
		s, c, ok := r.spatialVector(f, stepIdx, lat, lon)
		if !ok {
			return nan32()
		}
		return float32(math.Mod(math.Atan2(s, c)*180/math.Pi+360, 360))
	}
	sum := 0.0
	total := r.sample(f, stepIdx, lat, lon, func(v, w float64) { sum += v * w })
	if total == 0 {
		return nan32()
	}
	return float32(sum / total)
}

// spatialVector is the bilinear mean of a direction field (degrees) at one
// step, as the unit vector (sin, cos). ok is false when no corner has a value.
func (r *Run) spatialVector(f FieldID, stepIdx int, lat, lon float64) (sin, cos float64, ok bool) {
	var s, c float64
	total := r.sample(f, stepIdx, lat, lon, func(v, w float64) {
		rad := v * math.Pi / 180
		s += math.Sin(rad) * w
		c += math.Cos(rad) * w
	})
	if total == 0 {
		return 0, 0, false
	}
	return s / total, c / total, true
}

func valid(v float32) bool { return !math.IsNaN(float64(v)) }

func ptr(v float64) *float64 { return &v }

func compass(deg float64) float64 { return math.Mod(deg+360, 360) }

// Conditions converts v for display. It is nil when every value is missing;
// a field is nil when any of its inputs is missing.
func (v Values) Conditions() *Conditions {
	var c Conditions
	hasValue := false
	for _, x := range v {
		if valid(x) {
			hasValue = true
		}
	}
	if !hasValue {
		return nil
	}
	if valid(v[U10]) && valid(v[V10]) {
		u, w := float64(v[U10]), float64(v[V10])
		c.WindKn = ptr(math.Hypot(u, w) * knotsPerMS)
		c.WindFromDeg = ptr(compass(math.Atan2(-u, -w) * 180 / math.Pi))
	}
	if valid(v[Gust]) {
		c.GustKn = ptr(float64(v[Gust]) * knotsPerMS)
	}
	if valid(v[MSL]) {
		c.PressureHPa = ptr(float64(v[MSL]) / 100)
	}
	if valid(v[CurE]) && valid(v[CurN]) {
		e, n := float64(v[CurE]), float64(v[CurN])
		c.CurrentKn = ptr(math.Hypot(e, n) * knotsPerMS)
		c.CurrentToDeg = ptr(compass(math.Atan2(e, n) * 180 / math.Pi))
	}
	if valid(v[SWH]) {
		c.WaveM = ptr(float64(v[SWH]))
	}
	if valid(v[MWD]) {
		c.WaveFromDeg = ptr(float64(v[MWD]))
	}
	if valid(v[MWP]) {
		c.WavePeriodS = ptr(float64(v[MWP]))
	}
	return &c
}
