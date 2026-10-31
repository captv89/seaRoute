package route

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
)

// pub151Tolerance is the allowed relative difference to NGA Pub. 151.
const pub151Tolerance = 0.02

type pub151Port struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

// pub151Row is one reference distance from NGA Pub. 151 (2001, public domain).
type pub151Row struct {
	From pub151Port `json:"from"`
	To   pub151Port `json:"to"`
	NM   float64    `json:"nm"`
	Page int        `json:"page"`
	Note string     `json:"note"`
	Skip string     `json:"skip"`
}

var (
	fullGraphOnce sync.Once
	fullGraphG    *Graph
	fullGraphErr  error
)

// pub151Graph loads land, TSS and, when the depth dataset exists, depth.
func pub151Graph(t testing.TB) *Graph {
	t.Helper()
	fullGraphOnce.Do(func() {
		d := Datasets{Network: network, Land: landData, TSS: tssData}
		if _, err := os.Stat(depthData); err == nil {
			d.Depth = depthData
		}
		fullGraphG, fullGraphErr = LoadAll(d)
	})
	if fullGraphErr != nil {
		t.Fatal(fullGraphErr)
	}
	return fullGraphG
}

func TestPub151Distances(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: routes the Pub. 151 pairs on the full graph")
	}
	data, err := os.ReadFile("testdata/pub151.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []pub151Row
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	g := pub151Graph(t)
	var table []string
	for _, row := range rows {
		name := row.From.Name + " - " + row.To.Name
		t.Run(name, func(t *testing.T) {
			if row.Skip != "" {
				t.Skip(row.Skip)
			}
			r, err := g.Route([]Position{{row.From.Lon, row.From.Lat}, {row.To.Lon, row.To.Lat}}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			got := r.DistanceNM()
			dev := (got - row.NM) / row.NM
			table = append(table, fmt.Sprintf("%-28s pub151 %6.0f  ours %7.1f  %+6.2f%%  passages %v", name, row.NM, got, dev*100, r.Passages))
			if math.Abs(dev) > pub151Tolerance {
				t.Errorf("%s: got %.1f NM, want %.0f NM (%+.2f%%, tolerance %.0f%%); %s", name, got, row.NM, dev*100, pub151Tolerance*100, row.Note)
			}
		})
	}
	t.Logf("distances against NGA Pub. 151:\n%s", strings.Join(table, "\n"))
}
