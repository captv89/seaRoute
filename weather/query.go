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
	WindKn       *float64 `json:"wind_kn"`
	WindFromDeg  *float64 `json:"wind_from_deg"`
	GustKn       *float64 `json:"gust_kn"`
	PressureHPa  *float64 `json:"pressure_hpa"`
	CurrentKn    *float64 `json:"current_kn"`
	CurrentToDeg *float64 `json:"current_to_deg"`
	WaveM        *float64 `json:"wave_m"`
	WaveFromDeg  *float64 `json:"wave_from_deg"`
	WavePeriodS  *float64 `json:"wave_period_s"`
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
	cs := r.stencil(lat, lon)
	if len(cs) == 0 {
		return out
	}
	src := r.newRowCache().get
	for f := FieldID(0); f < NumFields; f++ {
		if f == MWD {
			sin0, cos0, ok0 := r.vectorCorners(src, cs, f, s0)
			sin1, cos1, ok1 := r.vectorCorners(src, cs, f, s1)
			if ok0 && ok1 {
				out[f] = direction(sin0+(sin1-sin0)*w, cos0+(cos1-cos0)*w)
			}
			continue
		}
		a, b := float64(r.scalarCorners(src, cs, f, s0)), float64(r.scalarCorners(src, cs, f, s1))
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

// rowSource returns one grid row (NX quantized values) of a field at a step.
// ok is false when the row cannot be read. The returned slice must not be
// modified or kept past the next call that may refill the source.
type rowSource func(f FieldID, stepIdx, row int) (row16 []int16, ok bool)

type rowKey struct {
	f         FieldID
	step, row int
}

// rowCache is a rowSource that reads each row from the run once. Create one
// per batch of nearby points (one point query, or one map tile) and use get
// as the source; rows are shared across fields, steps and points. It is not
// safe for concurrent use.
type rowCache struct {
	r    *Run
	rows map[rowKey][]int16 // nil value: the row could not be read
}

func (r *Run) newRowCache() *rowCache {
	return &rowCache{r: r, rows: make(map[rowKey][]int16)}
}

// get implements rowSource.
func (c *rowCache) get(f FieldID, stepIdx, row int) ([]int16, bool) {
	k := rowKey{f, stepIdx, row}
	buf, seen := c.rows[k]
	if !seen {
		buf = make([]int16, c.r.Grid.NX)
		if err := c.r.readRow(f, stepIdx, row, buf); err != nil {
			buf = nil
		}
		c.rows[k] = buf
	}
	return buf, buf != nil
}

// sample calls use with each valid value of field f at step stepIdx among the
// stencil corners cs, with its weight. Missing values are skipped, but if any
// needed row cannot be read nothing is used. It returns the total weight of
// the valid points (0 when none).
func (r *Run) sample(src rowSource, cs []corner, f FieldID, stepIdx int, use func(v, w float64)) float64 {
	rows := make([][]int16, len(cs))
	for i, c := range cs {
		row, ok := src(f, stepIdx, c.row)
		if !ok {
			return 0 // a read failure is not land: give no value at all
		}
		rows[i] = row
	}
	total := 0.0
	for i, c := range cs {
		v := r.decode(f, rows[i][c.col])
		if math.IsNaN(float64(v)) {
			continue
		}
		use(float64(v), c.w)
		total += c.w
	}
	return total
}

// scalarCorners is the weighted mean of a scalar field over the valid
// corners, or NaN when none is valid.
func (r *Run) scalarCorners(src rowSource, cs []corner, f FieldID, stepIdx int) float32 {
	sum := 0.0
	total := r.sample(src, cs, f, stepIdx, func(v, w float64) { sum += v * w })
	if total == 0 {
		return nan32()
	}
	return float32(sum / total)
}

// vectorCorners is the weighted mean of a direction field (degrees) over the
// valid corners, as the unit vector (sin, cos). ok is false when no corner is
// valid.
func (r *Run) vectorCorners(src rowSource, cs []corner, f FieldID, stepIdx int) (sin, cos float64, ok bool) {
	var s, c float64
	total := r.sample(src, cs, f, stepIdx, func(v, w float64) {
		rad := v * math.Pi / 180
		s += math.Sin(rad) * w
		c += math.Cos(rad) * w
	})
	if total == 0 {
		return 0, 0, false
	}
	return s / total, c / total, true
}

// direction turns a mean (sin, cos) vector into compass degrees in [0, 360),
// or NaN when the directions cancel out.
func direction(s, c float64) float32 {
	if math.Hypot(s, c) < 1e-9 {
		return nan32()
	}
	d := float32(compass(math.Atan2(s, c) * 180 / math.Pi))
	if d >= 360 {
		d = 0
	}
	return d
}

// spatial is the bilinear value of one field at one forecast step, reading
// rows through src (use a rowCache's get to share reads between points).
// Missing corners (land, off-grid rows) are dropped and the remaining weights
// renormalized; NaN when no corner has a value. There is no interpolation in
// time, so a caller such as the map tile renderer can use it for a single
// field and step. For MWD the direction is averaged as a vector and returned
// in degrees.
func (r *Run) spatial(src rowSource, f FieldID, stepIdx int, lat, lon float64) float32 {
	cs := r.stencil(lat, lon)
	if f == MWD {
		s, c, ok := r.vectorCorners(src, cs, f, stepIdx)
		if !ok {
			return nan32()
		}
		return direction(s, c)
	}
	return r.scalarCorners(src, cs, f, stepIdx)
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
