// Command tss-import turns OpenStreetMap traffic-separation-scheme ways into
// the dataset used by route.LoadTSS.
//
// Source: Overpass API, query
//
//	[out:json][timeout:300];way["seamark:type"~"^separation_(lane|zone|line|boundary|roundabout|crossing)$"];out tags geom;
//
// Licence: © OpenStreetMap contributors, ODbL.
//
// Ways are grouped into schemes (union-find over features whose bounding boxes
// lie within 3 NM). The output is a gzip GeoJSON FeatureCollection with the
// top-level member "osm_base" and, per feature, the properties type, osm_id,
// scheme ("tss-" + 8 hex of sha256 of the sorted way IDs) and name.
//
// Ways tagged "waterway" are skipped: inland canals are sometimes mis-tagged
// with seamark:type=separation_lane.
//
// Usage: go run ./cmd/tss-import -in overpass.json -out dataset/tss_osm.geojson.gz
package main

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

const groupGapNM = 3.0

type point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type element struct {
	Type     string            `json:"type"`
	ID       int64             `json:"id"`
	Nodes    []int64           `json:"nodes"`
	Geometry []point           `json:"geometry"`
	Tags     map[string]string `json:"tags"`
}

type overpass struct {
	Osm3s struct {
		Base string `json:"timestamp_osm_base"`
	} `json:"osm3s"`
	Base     string    `json:"-"` // used by tests instead of Osm3s
	Elements []element `json:"elements"`
}

type properties struct {
	Type   string `json:"type"`
	OSMID  int64  `json:"osm_id"`
	Scheme string `json:"scheme"`
	Name   string `json:"name"`
}

type geometry struct {
	Type        string `json:"type"`
	Coordinates any    `json:"coordinates"`
}

type feature struct {
	Type       string     `json:"type"`
	Properties properties `json:"properties"`
	Geometry   geometry   `json:"geometry"`
}

type collection struct {
	Type     string    `json:"type"`
	OSMBase  string    `json:"osm_base"`
	Features []feature `json:"features"`
}

type result struct {
	fc              collection
	skippedWaterway int
	skippedType     int
	skippedShort    int
	schemes         int
	byType          map[string]int
}

var knownTypes = map[string]bool{
	"lane": true, "zone": true, "line": true,
	"boundary": true, "roundabout": true, "crossing": true,
}

type item struct {
	id         int64
	typ, name  string
	coords     [][2]float64
	closed     bool
	minX, minY float64
	maxX, maxY float64
	scheme     string
}

func isClosed(e element) bool {
	if n := len(e.Nodes); n >= 2 && n == len(e.Geometry) {
		return e.Nodes[0] == e.Nodes[n-1]
	}
	g := e.Geometry
	return len(g) >= 2 && g[0] == g[len(g)-1]
}

func convert(op overpass) result {
	res := result{byType: map[string]int{}}
	base := op.Osm3s.Base
	if base == "" {
		base = op.Base
	}
	var items []*item
	for _, e := range op.Elements {
		if e.Type != "way" {
			continue
		}
		if _, ok := e.Tags["waterway"]; ok {
			res.skippedWaterway++
			continue
		}
		typ, ok := strings.CutPrefix(e.Tags["seamark:type"], "separation_")
		if !ok || !knownTypes[typ] {
			res.skippedType++
			continue
		}
		if len(e.Geometry) < 2 {
			res.skippedShort++
			continue
		}
		it := &item{id: e.ID, typ: typ, closed: isClosed(e)}
		it.name = firstNonEmpty(e.Tags["seamark:name"], e.Tags["name"])
		it.minX, it.minY, it.maxX, it.maxY = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, p := range e.Geometry {
			it.coords = append(it.coords, [2]float64{round6(p.Lon), round6(p.Lat)})
			it.minX, it.maxX = math.Min(it.minX, p.Lon), math.Max(it.maxX, p.Lon)
			it.minY, it.maxY = math.Min(it.minY, p.Lat), math.Max(it.maxY, p.Lat)
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })
	groupSchemes(items)

	// Scheme name: first name found in way-ID order, else the scheme ID.
	names := map[string]string{}
	for _, it := range items {
		if names[it.scheme] == "" && it.name != "" {
			names[it.scheme] = it.name
		}
	}
	seen := map[string]bool{}
	res.fc = collection{Type: "FeatureCollection", OSMBase: base, Features: []feature{}}
	for _, it := range items {
		seen[it.scheme] = true
		name := names[it.scheme]
		if name == "" {
			name = it.scheme
		}
		g := geometry{Type: "LineString", Coordinates: it.coords}
		poly := it.closed && len(it.coords) >= 4 &&
			(it.typ == "zone" || it.typ == "roundabout" || it.typ == "lane")
		if poly {
			g = geometry{Type: "Polygon", Coordinates: [][][2]float64{it.coords}}
		}
		res.fc.Features = append(res.fc.Features, feature{
			Type:       "Feature",
			Properties: properties{Type: it.typ, OSMID: it.id, Scheme: it.scheme, Name: name},
			Geometry:   g,
		})
		res.byType[it.typ]++
	}
	res.schemes = len(seen)
	return res
}

// groupSchemes assigns a scheme ID to every item: union-find over items whose
// bounding boxes are within groupGapNM of each other.
func groupSchemes(items []*item) {
	parent := make([]int, len(items))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	// Sweep by minY so only nearby latitudes are compared.
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return items[order[a]].minY < items[order[b]].minY })
	gapDeg := groupGapNM / 60
	for ai, a := range order {
		A := items[a]
		for _, b := range order[ai+1:] {
			B := items[b]
			if B.minY-A.maxY > gapDeg {
				break
			}
			if bboxGapNM(A, B) <= groupGapNM {
				parent[find(a)] = find(b)
			}
		}
	}
	groups := map[int][]int64{}
	for i, it := range items {
		r := find(i)
		groups[r] = append(groups[r], it.id)
	}
	ids := map[int]string{}
	for r, g := range groups {
		sort.Slice(g, func(i, j int) bool { return g[i] < g[j] })
		parts := make([]string, len(g))
		for i, id := range g {
			parts[i] = strconv.FormatInt(id, 10)
		}
		sum := sha256.Sum256([]byte(strings.Join(parts, ",")))
		ids[r] = "tss-" + hex.EncodeToString(sum[:])[:8]
	}
	for i, it := range items {
		it.scheme = ids[find(i)]
	}
}

// bboxGapNM is the distance between two bounding boxes in NM (0 if they
// overlap), using the longitude scale at the boxes' mean latitude and taking
// the short way round the antimeridian.
func bboxGapNM(a, b *item) float64 {
	gy := math.Max(0, math.Max(a.minY-b.maxY, b.minY-a.maxY))
	gx := math.Max(0, math.Max(a.minX-b.maxX, b.minX-a.maxX))
	span := math.Max(a.maxX, b.maxX) - math.Min(a.minX, b.minX)
	if wrapped := 360 - span; wrapped >= 0 && wrapped < gx {
		gx = wrapped
	}
	lat := (math.Max(a.maxY, b.maxY) + math.Min(a.minY, b.minY)) / 2
	return math.Hypot(gy*60, gx*60*math.Cos(lat*math.Pi/180))
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

func main() {
	in := flag.String("in", "", "Overpass JSON (out tags geom)")
	out := flag.String("out", "", "output .geojson.gz")
	flag.Parse()
	if *in == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	raw, err := os.ReadFile(*in)
	if err != nil {
		log.Fatal(err)
	}
	var op overpass
	if err := json.Unmarshal(raw, &op); err != nil {
		log.Fatal(err)
	}
	if op.Osm3s.Base == "" {
		log.Fatal("input has no osm3s.timestamp_osm_base")
	}
	res := convert(op)
	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	zw, _ := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err := json.NewEncoder(zw).Encode(res.fc); err != nil {
		log.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	st, _ := os.Stat(*out)
	fmt.Printf("osm_base %s: %d elements, %d features in %d schemes, by type %v; skipped: waterway %d, unknown type %d, too short %d; %d bytes\n",
		res.fc.OSMBase, len(op.Elements), len(res.fc.Features), res.schemes, res.byType,
		res.skippedWaterway, res.skippedType, res.skippedShort, st.Size())
}
