package main

import (
	"encoding/json"
	"testing"
)

func way(id int64, typ string, extra map[string]string, pts ...[2]float64) element {
	tags := map[string]string{"seamark:type": typ}
	for k, v := range extra {
		tags[k] = v
	}
	e := element{Type: "way", ID: id, Tags: tags}
	for _, p := range pts {
		e.Geometry = append(e.Geometry, point{Lon: p[0], Lat: p[1]})
	}
	return e
}

// ~1 NM of latitude is 1/60 degree.
const nm = 1.0 / 60

func TestGroupSchemes(t *testing.T) {
	lane1 := way(1, "separation_lane", map[string]string{"name": "Alpha"}, [2]float64{0, 0}, [2]float64{0.1, 0})
	lane2 := way(2, "separation_lane", nil, [2]float64{0, nm}, [2]float64{0.1, nm})
	zone := way(3, "separation_zone", nil,
		[2]float64{0, 2}, [2]float64{0.1, 2}, [2]float64{0.1, 2.05}, [2]float64{0, 2})

	a := convert(overpass{Elements: []element{lane1, lane2, zone}, Base: "t"})
	b := convert(overpass{Elements: []element{zone, lane2, lane1}, Base: "t"})
	if len(a.fc.Features) != 3 {
		t.Fatalf("features = %d, want 3", len(a.fc.Features))
	}
	ids := map[string]bool{}
	for _, f := range a.fc.Features {
		ids[f.Properties.Scheme] = true
	}
	if len(ids) != 2 {
		t.Fatalf("schemes = %d, want 2", len(ids))
	}
	if a.fc.Features[0].Properties.Scheme != a.fc.Features[1].Properties.Scheme {
		t.Error("the two lanes should share a scheme")
	}
	ja, _ := json.Marshal(a.fc)
	jb, _ := json.Marshal(b.fc)
	if string(ja) != string(jb) {
		t.Error("output depends on input order")
	}
	if a.fc.Features[0].Properties.Name != "Alpha" {
		t.Errorf("name = %q, want Alpha", a.fc.Features[0].Properties.Name)
	}
}

func TestClosedZoneBecomesPolygon(t *testing.T) {
	zone := way(5, "separation_zone", nil, [2]float64{0, 0}, [2]float64{1, 0}, [2]float64{1, 1}, [2]float64{0, 0})
	line := way(6, "separation_line", nil, [2]float64{5, 5}, [2]float64{6, 6}, [2]float64{5, 6}, [2]float64{5, 5})
	lane := way(7, "separation_lane", nil, [2]float64{9, 9}, [2]float64{9.1, 9})
	res := convert(overpass{Elements: []element{zone, line, lane}})
	got := map[int64]string{}
	for _, f := range res.fc.Features {
		got[f.Properties.OSMID] = f.Geometry.Type
	}
	if got[5] != "Polygon" || got[6] != "LineString" || got[7] != "LineString" {
		t.Errorf("geometry types = %v", got)
	}
}

func TestUnknownTypeSkipped(t *testing.T) {
	res := convert(overpass{Elements: []element{
		way(1, "separation_bogus", nil, [2]float64{0, 0}, [2]float64{1, 0}),
		way(2, "separation_line", nil, [2]float64{0, 0}, [2]float64{1, 0}),
	}})
	if len(res.fc.Features) != 1 || res.fc.Features[0].Properties.OSMID != 2 {
		t.Errorf("features = %+v", res.fc.Features)
	}
	if res.skippedType != 1 {
		t.Errorf("skippedType = %d", res.skippedType)
	}
}

func TestWaterwaySkipped(t *testing.T) {
	res := convert(overpass{Elements: []element{
		way(1, "separation_lane", map[string]string{"waterway": "canal"}, [2]float64{4.8, 52.3}, [2]float64{4.9, 52.4}),
		way(2, "separation_lane", nil, [2]float64{0, 0}, [2]float64{1, 0}),
	}})
	if len(res.fc.Features) != 1 || res.fc.Features[0].Properties.OSMID != 2 {
		t.Errorf("features = %+v", res.fc.Features)
	}
	if res.skippedWaterway != 1 {
		t.Errorf("skippedWaterway = %d", res.skippedWaterway)
	}
}
