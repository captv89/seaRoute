package route

import (
	"strings"
	"testing"
)

func TestCheckKinkOptions(t *testing.T) {
	long := strings.Repeat("x", MaxAcceptIDLen+1)
	many := make([]string, MaxAcceptIDs+1)
	for _, c := range []struct {
		name      string
		clearance float64
		accept    []string
		want      string
	}{
		{"defaults", DefaultClearanceNM, nil, ""},
		{"min", MinClearanceNM, nil, ""},
		{"max", MaxClearanceNM, []string{strings.Repeat("x", MaxAcceptIDLen)}, ""},
		{"too low", 0.4, nil, "clearance_nm must be between 0.5 and 10"},
		{"too high", 10.1, nil, "clearance_nm must be between 0.5 and 10"},
		{"too many ids", 2, many, "too many accept IDs (max 200)"},
		{"id too long", 2, []string{"k-ok", long}, "accept ID too long (max 32 characters)"},
	} {
		err := CheckKinkOptions(c.clearance, c.accept)
		if c.want == "" && err != nil || c.want != "" && (err == nil || err.Error() != c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
	}
}
