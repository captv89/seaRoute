package route

import "errors"

// Limits on the clearance option, shared by the REST and MCP front ends.
const (
	DefaultClearanceNM = 2.0
	MinClearanceNM     = 0.5
	MaxClearanceNM     = 10.0
)

// CheckClearance validates a clearance in nautical miles, already defaulted.
func CheckClearance(clearanceNM float64) error {
	if clearanceNM < MinClearanceNM || clearanceNM > MaxClearanceNM {
		return errors.New("clearance_nm must be between 0.5 and 10")
	}
	return nil
}

// CheckVessel validates a vessel's draught, speed and type. Call it after
// WithDefaults.
func CheckVessel(v Vessel) error {
	if v.DraughtM < 1 || v.DraughtM > 25 {
		return errors.New("draught_m must be between 1 and 25")
	}
	if v.SpeedKn < 1 || v.SpeedKn > 30 {
		return errors.New("speed_kn must be between 1 and 30")
	}
	if _, ok := BlockCoefficient(v.Type); !ok {
		return errors.New("vessel_type must be one of tanker, bulk, general, container")
	}
	return nil
}

// NewVessel builds a vessel from optional request fields and validates it. A
// nil pointer or empty type means the default; an explicit value, including 0,
// is kept as given.
func NewVessel(draughtM, speedKn *float64, vesselType string) (Vessel, error) {
	v := Vessel{DraughtM: DefaultDraughtM, SpeedKn: DefaultSpeedKn, Type: DefaultVesselType}
	if draughtM != nil {
		v.DraughtM = *draughtM
	}
	if speedKn != nil {
		v.SpeedKn = *speedKn
	}
	if vesselType != "" {
		v.Type = VesselType(vesselType)
	}
	return v, CheckVessel(v)
}
