package corridor

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func writeNet(t *testing.T, lines ...[][2]float64) string {
	t.Helper()
	var feats []any
	for _, c := range lines {
		feats = append(feats, map[string]any{"type": "Feature", "properties": map[string]any{},
			"geometry": map[string]any{"type": "LineString", "coordinates": c}})
	}
	b, _ := json.Marshal(map[string]any{"type": "FeatureCollection", "features": feats})
	p := filepath.Join(t.TempDir(), "net.geojson")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCellsCoverNetworkVertex(t *testing.T) {
	// Two segments meeting at (10.25, 20.25), the middle of cell (20, 40).
	net := writeNet(t, [][2]float64{{10.25, 19}, {10.25, 20.25}, {11, 20.25}})
	cells, err := Cells(net, 30, 0.5) // 30 NM = 0.5 degrees of latitude
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]int32{{20, 40}, {19, 40}, {21, 40}, {20, 39}, {20, 41}} {
		if !cells[c] {
			t.Errorf("cell %v should be marked", c)
		}
	}
	if cells[[2]int32{30, 40}] {
		t.Error("far cell should not be marked")
	}
}

func TestCellsWrapAntimeridian(t *testing.T) {
	net := writeNet(t, [][2]float64{{179.9, 0}, {179.8, 0.1}})
	cells, err := Cells(net, 30, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if !cells[[2]int32{-360, 0}] || !cells[[2]int32{359, 0}] {
		t.Error("cells either side of the antimeridian should be marked")
	}
}

// TestCellsCoverLongEdge: the middle of a long edge is covered, not only the
// cells around its two vertices.
func TestCellsCoverLongEdge(t *testing.T) {
	net := writeNet(t, [][2]float64{{0.25, 0.25}, {10.25, 0.25}}) // 600 NM edge
	cells, err := Cells(net, 30, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if !cells[[2]int32{10, 0}] { // cell of (5.25, 0.25)
		t.Error("midpoint cell of a long edge should be marked")
	}
}

func TestDensifyAntimeridian(t *testing.T) {
	ps := Densify(179.9, 0, -179.9, 0)
	if last := ps[len(ps)-1]; len(ps) != 4 || math.Abs(last[0]-180.1) > 1e-9 {
		t.Errorf("short way across the antimeridian: got %v", ps)
	}
}
