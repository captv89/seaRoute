package route

import (
	"strings"
	"testing"
)

const nmDeg = 1.0 / 60 // one NM of latitude in degrees

func singleLane() *TSS {
	return NewTSS([]TSSFeature{{
		Type: "lane", Scheme: "tss-test", OSMID: 1,
		Coords: []Position{{0, 0}, {0.1, 0}},
	}}, "v")
}

func TestSchemeAreaBuffer(t *testing.T) {
	tss := singleLane()
	if len(tss.Schemes) != 1 {
		t.Fatalf("schemes = %d", len(tss.Schemes))
	}
	if _, ok := tss.InArea(Position{0.05, 0.9 * nmDeg}); !ok {
		t.Error("0.9 NM north should be in area")
	}
	if _, ok := tss.InArea(Position{0.05, 1.2 * nmDeg}); ok {
		t.Error("1.2 NM north should be outside area")
	}
	if !tss.InCoverage(Position{0.05, 19 * nmDeg}) {
		t.Error("19 NM north should be in coverage")
	}
	if tss.InCoverage(Position{0.05, 21 * nmDeg}) {
		t.Error("21 NM north should be outside coverage")
	}
	if !tss.SegmentTouchesArea(Position{0.05, -3 * nmDeg}, Position{0.05, 3 * nmDeg}) {
		t.Error("segment through the lane should touch the area")
	}
	if tss.SegmentTouchesArea(Position{0.05, 2 * nmDeg}, Position{0.05, 5 * nmDeg}) {
		t.Error("segment 2+ NM away should not touch the area")
	}
}

func TestViolationsOracle(t *testing.T) {
	tss := NewTSS([]TSSFeature{
		{Type: "zone", Scheme: "tss-z", OSMID: 1,
			Coords: []Position{{0, 0}, {0.1, 0}, {0.1, 0.02}, {0, 0.02}, {0, 0}}},
		{Type: "lane", Scheme: "tss-l", OSMID: 2,
			Coords: []Position{{1, 0}, {1.1, 0}}},
		{Type: "line", Scheme: "tss-n", OSMID: 3,
			Coords: []Position{{2, 0}, {2.1, 0}}},
	}, "v")

	across := []Position{{0.05, -0.05}, {0.05, 0.07}}
	if v := tss.violations(across, nil); len(v) != 1 || !strings.HasPrefix(v[0], "tss-z: seg 0") {
		t.Errorf("zone crossing: %v", v)
	}
	if v := tss.violations(across, map[string]bool{"tss-z": true}); len(v) != 0 {
		t.Errorf("lifted zone: %v", v)
	}
	inside := []Position{{0.02, 0.005}, {0.04, 0.005}}
	if v := tss.violations(inside, nil); len(v) != 1 {
		t.Errorf("segment inside zone: %v", v)
	}
	along := []Position{{1.08, 0.005}, {1.02, 0.005}} // lane runs east, this runs west
	if v := tss.violations(along, nil); len(v) != 1 || !strings.Contains(v[0], "against lane") {
		t.Errorf("against lane: %v", v)
	}
	with := []Position{{1.02, 0.005}, {1.08, 0.005}}
	if v := tss.violations(with, nil); len(v) != 0 {
		t.Errorf("with lane: %v", v)
	}
	farWest := []Position{{1.08, 0.5}, {1.02, 0.5}}
	if v := tss.violations(farWest, nil); len(v) != 0 {
		t.Errorf("far from lane: %v", v)
	}
	line := []Position{{2.05, -0.02}, {2.05, 0.02}}
	if v := tss.violations(line, nil); len(v) != 1 || !strings.Contains(v[0], "line") {
		t.Errorf("line crossing: %v", v)
	}
}

func TestSchemeAntimeridian(t *testing.T) {
	tss := NewTSS([]TSSFeature{{
		Type: "lane", Scheme: "tss-am", OSMID: 1,
		Coords: []Position{{179.95, 0}, {-179.95, 0}},
	}}, "v")
	if _, ok := tss.InArea(Position{180, 0.5 * nmDeg}); !ok {
		t.Error("point on the antimeridian should be in area")
	}
	if _, ok := tss.InArea(Position{-179.97, 0.5 * nmDeg}); !ok {
		t.Error("point just east of the antimeridian should be in area")
	}
	if _, ok := tss.InArea(Position{0, 0}); ok {
		t.Error("far point should not be in area")
	}
}

func TestLoadRealTSS(t *testing.T) {
	tss, err := LoadTSS("../dataset/tss_osm.geojson.gz")
	if err != nil {
		t.Fatal(err)
	}
	if len(tss.Schemes) < 100 {
		t.Errorf("schemes = %d, want >= 100", len(tss.Schemes))
	}
	if tss.Version == "" {
		t.Error("empty version")
	}
	if _, ok := tss.InArea(Position{1.45, 51.0}); !ok {
		t.Error("Dover mid-strait should be in a scheme area")
	}
	if tss.InCoverage(Position{-30, 40}) {
		t.Error("mid-Atlantic should not be in coverage")
	}
}
