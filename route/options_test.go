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

func TestCheckVesselLimits(t *testing.T) {
	for _, c := range []struct {
		name string
		v    Vessel
		want string
	}{
		{"min draught", Vessel{1, 14, "bulk"}, ""},
		{"max draught", Vessel{25, 14, "bulk"}, ""},
		{"draught below", Vessel{0.9, 14, "bulk"}, "draught_m must be between 1 and 25"},
		{"draught above", Vessel{25.1, 14, "bulk"}, "draught_m must be between 1 and 25"},
		{"min speed", Vessel{10, 0.9, "bulk"}, "speed_kn must be between 1 and 30"},
		{"max speed", Vessel{10, 30, "bulk"}, ""},
		{"speed above", Vessel{10, 30.1, "bulk"}, "speed_kn must be between 1 and 30"},
		{"bad type", Vessel{10, 14, "ferry"}, "vessel_type must be one of tanker, bulk, general, container"},
	} {
		err := CheckVessel(c.v)
		if c.want == "" && err != nil || c.want != "" && (err == nil || err.Error() != c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
	}
}

func TestNewVessel(t *testing.T) {
	v, err := NewVessel(nil, nil, "")
	if err != nil || v != (Vessel{DraughtM: 10, SpeedKn: 14, Type: Bulk}) {
		t.Errorf("defaults = %+v, %v", v, err)
	}
	zero, eight := 0.0, 8.0
	if _, err := NewVessel(&zero, nil, ""); err == nil {
		t.Error("explicit 0 draught: want error")
	}
	if _, err := NewVessel(nil, &zero, ""); err == nil {
		t.Error("explicit 0 speed: want error")
	}
	v, err = NewVessel(&eight, nil, "tanker")
	if err != nil || v.DraughtM != 8 || v.Type != Tanker {
		t.Errorf("explicit = %+v, %v", v, err)
	}
	if _, err := NewVessel(nil, nil, "ferry"); err == nil {
		t.Error("unknown type: want error")
	}
}
