package route

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
)

const (
	// TSSAreaNM is the buffer around a scheme's features that makes its area:
	// inside it, shortcuts are forbidden and OSM TSS data is the authority.
	TSSAreaNM = 1.0
	// TSSCoverageNM is the wider buffer inside which the OSM TSS data is trusted
	// to be complete.
	TSSCoverageNM = 20.0

	tssCirclePoints = 16
	tssMaxFeatures  = 200_000
	tssMaxCoords    = 5_000_000
	tssMaxBytes     = 256 << 20
)

// Scheme is one traffic separation scheme (a group of OSM ways within 3 NM of
// each other).
//
// Area and Coverage are closed convex rings (counter-clockwise, first vertex
// repeated) with longitudes wrapped to [-180, 180]. A scheme whose features
// span the antimeridian is hulled internally with longitudes shifted into a
// continuous 0..360 range (negative longitudes get +360), so its exported
// rings jump across +-180; all queries on the TSS handle this.
type Scheme struct {
	ID, Name string
	Area     []Position
	Coverage []Position
	Zones    [][]Position // closed polygons that must not be crossed
	Lines    [][]Position // separation lines that must not be crossed
	Lanes    [][]Position // direction of the polyline is the direction of traffic

	wrap           bool // longitudes < 0 are shifted by +360 internally
	area, cover    []Position
	areaM          [][2]float64
	box, coverBox  bbox
	zonesM, linesM [][][2]float64
	lanesM         [][][2]float64
}

type bbox struct{ minX, minY, maxX, maxY float64 }

func (b bbox) has(p Position) bool {
	return p[0] >= b.minX && p[0] <= b.maxX && p[1] >= b.minY && p[1] <= b.maxY
}

func (b bbox) overlaps(o bbox) bool {
	return b.minX <= o.maxX && o.minX <= b.maxX && b.minY <= o.maxY && o.minY <= b.maxY
}

// TSS is the set of schemes in the dataset. Version is the OSM base timestamp
// the data was extracted at.
type TSS struct {
	Schemes []Scheme
	Version string
}

// TSSFeature is one imported OSM way. Type is the seamark type without the
// "separation_" prefix: lane, zone, line, boundary, roundabout or crossing.
// A closed ring repeats its first coordinate at the end.
type TSSFeature struct {
	Type, Scheme, Name string
	OSMID              int64
	Coords             []Position
}

// NewTSS groups features by scheme ID and builds the scheme geometry. Feature
// roles: closed zone/roundabout ways are Zones; line ways are Lines; open lane
// ways are Lanes; everything else (boundary, crossing, closed lanes, open
// zones) only widens the scheme's area. Unknown types are ignored.
func NewTSS(features []TSSFeature, version string) *TSS {
	groups := map[string][]TSSFeature{}
	for _, f := range features {
		if len(f.Coords) >= 2 && knownTSSType(f.Type) {
			groups[f.Scheme] = append(groups[f.Scheme], f)
		}
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	t := &TSS{Version: version}
	for _, id := range ids {
		fs := groups[id]
		sort.SliceStable(fs, func(i, j int) bool { return fs[i].OSMID < fs[j].OSMID })
		t.Schemes = append(t.Schemes, buildScheme(id, fs))
	}
	return t
}

func knownTSSType(s string) bool {
	switch s {
	case "lane", "zone", "line", "boundary", "roundabout", "crossing":
		return true
	}
	return false
}

func buildScheme(id string, fs []TSSFeature) Scheme {
	s := Scheme{ID: id}
	minLon, maxLon := math.Inf(1), math.Inf(-1)
	for _, f := range fs {
		if s.Name == "" {
			s.Name = f.Name
		}
		for _, p := range f.Coords {
			minLon, maxLon = math.Min(minLon, p[0]), math.Max(maxLon, p[0])
		}
	}
	if s.Name == "" {
		s.Name = id
	}
	s.wrap = maxLon-minLon > 180

	var pts []Position
	for _, f := range fs {
		closed := len(f.Coords) >= 4 && f.Coords[0] == f.Coords[len(f.Coords)-1]
		switch {
		case (f.Type == "zone" || f.Type == "roundabout") && closed:
			s.Zones = append(s.Zones, f.Coords)
		case f.Type == "line":
			s.Lines = append(s.Lines, f.Coords)
		case f.Type == "lane" && !closed:
			s.Lanes = append(s.Lanes, f.Coords)
		}
		for _, p := range f.Coords {
			pts = append(pts, s.norm(p))
		}
	}
	s.area = bufferedHull(pts, TSSAreaNM)
	s.cover = bufferedHull(pts, TSSCoverageNM)
	s.Area = s.unwrapRing(s.area)
	s.Coverage = s.unwrapRing(s.cover)
	s.box = ringBox(s.area)
	s.coverBox = ringBox(s.cover)
	s.areaM = mercRing(s.area)
	for _, z := range s.Zones {
		s.zonesM = append(s.zonesM, s.merc(z))
	}
	for _, l := range s.Lines {
		s.linesM = append(s.linesM, s.merc(l))
	}
	for _, l := range s.Lanes {
		s.lanesM = append(s.lanesM, s.merc(l))
	}
	return s
}

// norm maps p into the scheme's continuous longitude range.
func (s *Scheme) norm(p Position) Position {
	if s.wrap && p[0] < 0 {
		p[0] += 360
	}
	return p
}

func (s *Scheme) unwrapRing(r []Position) []Position {
	out := make([]Position, len(r))
	for i, p := range r {
		if p[0] > 180 {
			p[0] -= 360
		}
		out[i] = p
	}
	return out
}

func (s *Scheme) merc(line []Position) [][2]float64 {
	out := make([][2]float64, len(line))
	for i, p := range line {
		out[i] = Mercator(s.norm(p))
	}
	return out
}

func mercRing(r []Position) [][2]float64 {
	out := make([][2]float64, len(r))
	for i, p := range r {
		out[i] = Mercator(p)
	}
	return out
}

func ringBox(r []Position) bbox {
	b := bbox{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range r {
		b.minX, b.maxX = math.Min(b.minX, p[0]), math.Max(b.maxX, p[0])
		b.minY, b.maxY = math.Min(b.minY, p[1]), math.Max(b.maxY, p[1])
	}
	return b
}

// bufferedHull puts a 16-point circle of radius rNM around every point
// (equirectangular, scaled by the cosine of the point's own latitude) and
// returns the counter-clockwise convex hull as a closed ring. The hull is taken
// in lon/lat, which is an affine image of the local metric plane, so convexity
// is the same in both.
func bufferedHull(pts []Position, rNM float64) []Position {
	rM := rNM * MetresPerNM
	dLat := rM / earthRadiusM * 180 / math.Pi
	circle := make([]Position, 0, len(pts)*tssCirclePoints)
	var sin, cos [tssCirclePoints]float64
	for k := range sin {
		a := 2 * math.Pi * float64(k) / tssCirclePoints
		sin[k], cos[k] = math.Sin(a), math.Cos(a)
	}
	for _, p := range pts {
		dLon := dLat / math.Max(math.Cos(p[1]*math.Pi/180), 0.01)
		for k := range sin {
			lat := math.Max(-90, math.Min(90, p[1]+dLat*sin[k]))
			circle = append(circle, Position{p[0] + dLon*cos[k], lat})
		}
	}
	h := convexHull(circle)
	return append(h, h[0])
}

// convexHull is Andrew's monotone chain; the result is counter-clockwise and
// not closed.
func convexHull(pts []Position) []Position {
	sort.Slice(pts, func(i, j int) bool {
		if pts[i][0] != pts[j][0] {
			return pts[i][0] < pts[j][0]
		}
		return pts[i][1] < pts[j][1]
	})
	cross := func(o, a, b Position) float64 {
		return (a[0]-o[0])*(b[1]-o[1]) - (a[1]-o[1])*(b[0]-o[0])
	}
	h := make([]Position, 0, 2*len(pts))
	for _, p := range pts {
		for len(h) >= 2 && cross(h[len(h)-2], h[len(h)-1], p) <= 0 {
			h = h[:len(h)-1]
		}
		h = append(h, p)
	}
	lower := len(h) + 1
	for i := len(pts) - 2; i >= 0; i-- {
		p := pts[i]
		for len(h) >= lower && cross(h[len(h)-2], h[len(h)-1], p) <= 0 {
			h = h[:len(h)-1]
		}
		h = append(h, p)
	}
	return h[:len(h)-1]
}

// inConvex reports whether p is inside (or on) the counter-clockwise closed
// convex ring.
func inConvex(ring []Position, p Position) bool {
	for i := 0; i+1 < len(ring); i++ {
		a, b := ring[i], ring[i+1]
		if (b[0]-a[0])*(p[1]-a[1])-(b[1]-a[1])*(p[0]-a[0]) < 0 {
			return false
		}
	}
	return true
}

// InArea reports whether p is within 1 NM of any scheme feature's hull, and
// which scheme (index into Schemes) it is in.
func (t *TSS) InArea(p Position) (schemeIdx int, ok bool) {
	for i := range t.Schemes {
		s := &t.Schemes[i]
		q := s.norm(p)
		if s.box.has(q) && inConvex(s.area, q) {
			return i, true
		}
	}
	return 0, false
}

// InCoverage reports whether p lies within the 20 NM coverage hull of any
// scheme, where OSM TSS data is trusted.
func (t *TSS) InCoverage(p Position) bool {
	for i := range t.Schemes {
		s := &t.Schemes[i]
		q := s.norm(p)
		if s.coverBox.has(q) && inConvex(s.cover, q) {
			return true
		}
	}
	return false
}

// SegmentTouchesArea reports whether the rhumb leg a-b touches any scheme area.
func (t *TSS) SegmentTouchesArea(a, b Position) bool {
	for _, piece := range splitAntimeridian(a, b) {
		for i := range t.Schemes {
			s := &t.Schemes[i]
			pa, pb := s.norm(piece[0]), s.norm(piece[1])
			if !s.box.overlaps(segBox(pa, pb)) {
				continue
			}
			if inConvex(s.area, pa) || inConvex(s.area, pb) {
				return true
			}
			ma, mb := Mercator(pa), Mercator(pb)
			for j := 0; j+1 < len(s.areaM); j++ {
				if _, hit := segIntersect(ma, mb, s.areaM[j], s.areaM[j+1]); hit {
					return true
				}
			}
		}
	}
	return false
}

func segBox(a, b Position) bbox {
	return bbox{math.Min(a[0], b[0]), math.Min(a[1], b[1]), math.Max(a[0], b[0]), math.Max(a[1], b[1])}
}

// violations is a test oracle: it lists each path segment that crosses a zone
// or line, or runs against a lane, as "<scheme>: seg <i> <reason>". Schemes in
// lifted are skipped. Against-lane rule: the segment's midpoint is within 1 NM
// of a lane polyline and its bearing differs from the nearest lane segment's
// by more than 90 degrees. Crossing rule: the segment intersects a zone
// polygon (or lies inside one), or crosses a separation line.
func (t *TSS) violations(path []Position, lifted map[string]bool) []string {
	var out []string
	for i := 0; i+1 < len(path); i++ {
		seen := map[string]bool{}
		for _, piece := range splitAntimeridian(path[i], path[i+1]) {
			for k := range t.Schemes {
				s := &t.Schemes[k]
				if lifted[s.ID] {
					continue
				}
				pa, pb := s.norm(piece[0]), s.norm(piece[1])
				if !s.box.overlaps(segBox(pa, pb)) {
					continue
				}
				ma, mb := Mercator(pa), Mercator(pb)
				for _, reason := range s.segmentReasons(ma, mb) {
					msg := fmt.Sprintf("%s: seg %d %s", s.ID, i, reason)
					if !seen[msg] {
						seen[msg] = true
						out = append(out, msg)
					}
				}
			}
		}
	}
	return out
}

func (s *Scheme) segmentReasons(ma, mb [2]float64) []string {
	var reasons []string
	for _, z := range s.zonesM {
		if segHitsRing(ma, mb, z) || pointInPolygon(ma, z) {
			reasons = append(reasons, "crosses zone")
			break
		}
	}
	for _, l := range s.linesM {
		hit := false
		for j := 0; j+1 < len(l) && !hit; j++ {
			_, hit = segIntersect(ma, mb, l[j], l[j+1])
		}
		if hit {
			reasons = append(reasons, "crosses line")
			break
		}
	}
	if len(s.lanesM) > 0 && (ma != mb) {
		mid := [2]float64{(ma[0] + mb[0]) / 2, (ma[1] + mb[1]) / 2}
		best, bearing := math.Inf(1), 0.0
		for _, l := range s.lanesM {
			for j := 0; j+1 < len(l); j++ {
				if l[j] == l[j+1] {
					continue
				}
				c := closestOnSeg(mid, l[j], l[j+1])
				if d := math.Hypot(mid[0]-c[0], mid[1]-c[1]); d < best {
					best = d
					bearing = math.Atan2(l[j+1][0]-l[j][0], l[j+1][1]-l[j][1])
				}
			}
		}
		metres := earthRadiusM * best * math.Cos(invMercatorLat(mid[1])*math.Pi/180)
		if metres <= TSSAreaNM*MetresPerNM {
			seg := math.Atan2(mb[0]-ma[0], mb[1]-ma[1])
			if math.Abs(math.Remainder(seg-bearing, 2*math.Pi)) > math.Pi/2 {
				reasons = append(reasons, "against lane")
			}
		}
	}
	return reasons
}

func segHitsRing(a, b [2]float64, ring [][2]float64) bool {
	for j := 0; j+1 < len(ring); j++ {
		if _, hit := segIntersect(a, b, ring[j], ring[j+1]); hit {
			return true
		}
	}
	return false
}

// pointInPolygon is an even-odd ray cast.
func pointInPolygon(p [2]float64, ring [][2]float64) bool {
	in := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		a, b := ring[i], ring[j]
		if (a[1] > p[1]) != (b[1] > p[1]) &&
			p[0] < (b[0]-a[0])*(p[1]-a[1])/(b[1]-a[1])+a[0] {
			in = !in
		}
	}
	return in
}

// LoadTSS reads the gzip GeoJSON written by cmd/tss-import. The file is treated
// as untrusted: feature and coordinate counts are bounded and NaN or
// out-of-range coordinates are rejected.
func LoadTSS(path string) (*TSS, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("tss %s: %w", path, err)
	}
	defer zr.Close()
	t, err := readTSS(io.LimitReader(zr, tssMaxBytes))
	if err != nil {
		return nil, fmt.Errorf("tss %s: %w", path, err)
	}
	return t, nil
}

type tssFile struct {
	OSMBase  string `json:"osm_base"`
	Features []struct {
		Properties struct {
			Type   string `json:"type"`
			OSMID  int64  `json:"osm_id"`
			Scheme string `json:"scheme"`
			Name   string `json:"name"`
		} `json:"properties"`
		Geometry struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
	} `json:"features"`
}

func readTSS(r io.Reader) (*TSS, error) {
	var file tssFile
	if err := json.NewDecoder(r).Decode(&file); err != nil {
		return nil, err
	}
	if len(file.Features) > tssMaxFeatures {
		return nil, fmt.Errorf("too many features: %d", len(file.Features))
	}
	if file.OSMBase == "" {
		return nil, errors.New("missing osm_base")
	}
	feats := make([]TSSFeature, 0, len(file.Features))
	total := 0
	for i, ft := range file.Features {
		pr := ft.Properties
		if pr.Scheme == "" || len(pr.Scheme) > 64 || len(pr.Name) > 1024 || len(pr.Type) > 32 {
			return nil, fmt.Errorf("feature %d: bad properties", i)
		}
		var raw [][]float64
		switch ft.Geometry.Type {
		case "LineString":
			if err := json.Unmarshal(ft.Geometry.Coordinates, &raw); err != nil {
				return nil, fmt.Errorf("feature %d: %w", i, err)
			}
		case "Polygon":
			var rings [][][]float64
			if err := json.Unmarshal(ft.Geometry.Coordinates, &rings); err != nil || len(rings) == 0 {
				return nil, fmt.Errorf("feature %d: bad polygon", i)
			}
			raw = rings[0]
		default:
			return nil, fmt.Errorf("feature %d: unsupported geometry %q", i, ft.Geometry.Type)
		}
		total += len(raw)
		if total > tssMaxCoords {
			return nil, errors.New("too many coordinates")
		}
		coords := make([]Position, len(raw))
		for j, c := range raw {
			if len(c) < 2 || math.IsNaN(c[0]) || math.IsNaN(c[1]) ||
				math.Abs(c[0]) > 180 || math.Abs(c[1]) > 90 {
				return nil, fmt.Errorf("feature %d: bad coordinate %d", i, j)
			}
			coords[j] = Position{c[0], c[1]}
		}
		feats = append(feats, TSSFeature{Type: pr.Type, Scheme: pr.Scheme, Name: pr.Name, OSMID: pr.OSMID, Coords: coords})
	}
	return NewTSS(feats, file.OSMBase), nil
}
