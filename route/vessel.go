package route

import "math"

// VesselType is the hull class, which sets the block coefficient.
type VesselType string

const (
	Tanker    VesselType = "tanker"
	Bulk      VesselType = "bulk"
	General   VesselType = "general"
	Container VesselType = "container"
)

// Defaults for a vessel that does not name its draught, speed or type.
const (
	DefaultDraughtM              = 10.0
	DefaultSpeedKn               = 14.0
	DefaultVesselType VesselType = "bulk"
)

// Vessel is the ship a route is planned for.
type Vessel struct {
	DraughtM float64
	SpeedKn  float64
	Type     VesselType
}

// WithDefaults fills zero fields with the defaults.
func (v Vessel) WithDefaults() Vessel {
	if v.DraughtM == 0 {
		v.DraughtM = DefaultDraughtM
	}
	if v.SpeedKn == 0 {
		v.SpeedKn = DefaultSpeedKn
	}
	if v.Type == "" {
		v.Type = DefaultVesselType
	}
	return v
}

var blockCoefficients = map[VesselType]float64{
	Tanker:    0.80,
	Bulk:      0.80,
	General:   0.70,
	Container: 0.60,
}

// BlockCoefficient returns the block coefficient of a vessel type, and false
// for an unknown type.
func BlockCoefficient(t VesselType) (float64, bool) {
	cb, ok := blockCoefficients[t]
	return cb, ok
}

// Squat is the Barrass squat in metres at speedKn knots: cb × V² / 100 in
// open water, cb × V² / 50 when confined.
func Squat(cb, speedKn float64, confined bool) float64 {
	divisor := 100.0
	if confined {
		divisor = 50.0
	}
	return cb * speedKn * speedKn / divisor
}

// DataMargin is the under-keel allowance for depth data error: 2 m, or 10%
// of the draught when that is larger.
func DataMargin(draughtM float64) float64 {
	return math.Max(2, 0.10*draughtM)
}

// DepthLimit is the water depth a vessel needs, with its parts. LimitM is
// DraughtM + SquatM + MarginM.
type DepthLimit struct {
	DraughtM float64 `json:"draught_m"`
	SquatM   float64 `json:"squat_m"`
	MarginM  float64 `json:"margin_m"`
	LimitM   float64 `json:"depth_limit_m"`
}

// DepthLimit applies WithDefaults, then works out the depth the vessel needs
// in open water or, when confined, in a canal or strait. An unknown type uses
// the largest block coefficient (0.80), which is the conservative choice.
func (v Vessel) DepthLimit(confined bool) DepthLimit {
	v = v.WithDefaults()
	cb, ok := BlockCoefficient(v.Type)
	if !ok {
		cb = 0.80
	}
	squat := Squat(cb, v.SpeedKn, confined)
	margin := DataMargin(v.DraughtM)
	return DepthLimit{
		DraughtM: v.DraughtM,
		SquatM:   squat,
		MarginM:  margin,
		LimitM:   v.DraughtM + squat + margin,
	}
}
