package weather

import "time"

// FieldID identifies one stored forecast field.
type FieldID int

// The fields kept in a run file, in storage order.
const (
	U10  FieldID = iota // 10 m wind, eastward component, m/s
	V10                 // 10 m wind, northward component, m/s
	Gust                // 10 m wind gust, m/s
	MSL                 // mean sea level pressure, Pa
	CurE                // ocean current, eastward component, m/s
	CurN                // ocean current, northward component, m/s
	SWH                 // significant wave height, m
	MWD                 // mean wave direction, degrees
	MWP                 // mean wave period, s
	NumFields
)

// fieldSpec says where a field comes from in the ECMWF open data and how it
// is packed into int16: q = round((v-Offset)/Scale).
type fieldSpec struct {
	Param, Stream string
	Scale, Offset float64
}

var fieldSpecs = [NumFields]fieldSpec{
	U10:  {"10u", "oper", 0.01, 0},
	V10:  {"10v", "oper", 0.01, 0},
	Gust: {"10fg", "oper", 0.01, 0},
	MSL:  {"msl", "oper", 1, 97000},
	CurE: {"sve", "oper", 0.001, 0},
	CurN: {"svn", "oper", 0.001, 0},
	SWH:  {"swh", "wave", 0.01, 0},
	MWD:  {"mwd", "wave", 0.1, 0},
	MWP:  {"mwp", "wave", 0.01, 0},
}

// NoValue marks a missing sample (land, or a field that was never written).
const NoValue int16 = -32768

// Credit is the attribution shown wherever forecast data is displayed.
const Credit = "Forecast data: ECMWF IFS open data, CC BY 4.0"

// DefaultSteps returns the forecast hours stored per run: every 3 hours to
// hour 144, then every 6 hours to hour 360 (85 steps).
func DefaultSteps() []int {
	steps := make([]int, 0, 85)
	for h := 0; h <= 144; h += 3 {
		steps = append(steps, h)
	}
	for h := 150; h <= 360; h += 6 {
		steps = append(steps, h)
	}
	return steps
}

// RunID names a forecast run by its reference time, e.g. "2026110212".
func RunID(t time.Time) string { return t.UTC().Format("2006010215") }
