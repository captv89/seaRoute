package route

import (
	"slices"
	"testing"
)

func TestPassageLimits(t *testing.T) {
	g, err := Load("../dataset/marnet_plus_5km.geojson")
	if err != nil {
		t.Fatal(err)
	}
	passages := g.Passages()
	for tag, lim := range PassageLimits {
		if !slices.Contains(passages, tag) {
			t.Errorf("%s is not a passage tag in the network", tag)
		}
		if lim.Source == "" {
			t.Errorf("%s has no Source", tag)
		}
	}
	want := map[string]PassageLimit{
		"suez":    {MaxDraughtM: 20.1, Confined: true},
		"panama":  {MaxDraughtM: 15.24, Confined: true},
		"kiel":    {MaxDraughtM: 9.5, Confined: true},
		"corinth": {Confined: true},
		"malacca": {MaxDraughtM: 20.5, Confined: true},
		"dover":   {MaxDraughtM: 0, Confined: true},
	}
	for tag, w := range want {
		got, ok := PassageLimits[tag]
		if !ok {
			t.Errorf("%s missing from PassageLimits", tag)
			continue
		}
		if tag != "corinth" && got.MaxDraughtM != w.MaxDraughtM {
			t.Errorf("%s: MaxDraughtM %v, want %v", tag, got.MaxDraughtM, w.MaxDraughtM)
		}
		if got.Confined != w.Confined {
			t.Errorf("%s: Confined %v, want %v", tag, got.Confined, w.Confined)
		}
	}
	if PassageLimits["corinth"].MaxDraughtM <= 0 {
		t.Errorf("corinth: MaxDraughtM must be set from the operator's figure")
	}
}
