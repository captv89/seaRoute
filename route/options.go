package route

import (
	"errors"
	"fmt"
)

// Limits on the kink-removal options, shared by the REST and MCP front ends.
const (
	DefaultClearanceNM = 2.0
	MinClearanceNM     = 0.5
	MaxClearanceNM     = 10.0
	MaxAcceptIDs       = 200
	MaxAcceptIDLen     = 32
)

// CheckKinkOptions validates a clearance (in nautical miles, already
// defaulted) and the accept IDs against the limits above.
func CheckKinkOptions(clearanceNM float64, accept []string) error {
	if clearanceNM < MinClearanceNM || clearanceNM > MaxClearanceNM {
		return errors.New("clearance_nm must be between 0.5 and 10")
	}
	if len(accept) > MaxAcceptIDs {
		return fmt.Errorf("too many accept IDs (max %d)", MaxAcceptIDs)
	}
	for _, id := range accept {
		if len(id) > MaxAcceptIDLen {
			return fmt.Errorf("accept ID too long (max %d characters)", MaxAcceptIDLen)
		}
	}
	return nil
}
