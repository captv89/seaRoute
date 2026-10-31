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
