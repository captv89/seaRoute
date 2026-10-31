package route

import "testing"

func TestCheckClearance(t *testing.T) {
	for _, c := range []struct {
		name      string
		clearance float64
		want      string
	}{
		{"defaults", DefaultClearanceNM, ""},
		{"min", MinClearanceNM, ""},
		{"max", MaxClearanceNM, ""},
		{"too low", 0.4, "clearance_nm must be between 0.5 and 10"},
		{"too high", 10.1, "clearance_nm must be between 0.5 and 10"},
	} {
		err := CheckClearance(c.clearance)
		if c.want == "" && err != nil || c.want != "" && (err == nil || err.Error() != c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
	}
}
