package route

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

const (
	landMagic         = "SRLAND1\n"
	landMaxRings      = 10_000_000
	landMaxVertices   = 50_000_000
	landMaxHeaderLen  = 1 << 16
	landCellsPerDeg   = 4 // 0.25 degree grid
	landMicro         = 1e6
	landSampleStepDeg = 0.1
)

// Land is a coastline edge set with a 0.25 degree grid index, used to measure
// how close a straight (rhumb) leg passes to land.
//
// The OSM "split" land polygons contain artificial tile-split edges that lie
// inside land. They do no harm: distance is the minimum over all edges, and a
// leg between two sea points that enters land must cross a real coastline
// edge, so "returns 0 when they intersect" is sufficient.
//
// Gaps are computed in Mercator and converted to metres as R * gap * cos(lat)
// with lat at the mid-height of the two closest points. The error is second
// order in the gap (about 1 m per degree of gap at mid latitudes, far below
// Tolerance for the 2 NM clearances used). Simplification can move the coast by
// up to Tolerance metres; callers add it to their clearance requirement.
type Land struct {
	Tolerance float64 // Douglas-Peucker tolerance of the dataset in metres
	Version   string  // "<date>/<tolerance_m>m"
	// CorridorNM is the radius around MARNET vertices that the dataset covers
	// (header corridor_nm). Land farther from every vertex may be missing. 0
	// means full coverage.
	CorridorNM float64

	// Vertices in 1e-6 degrees. Each ring is stored closed (first vertex
	// repeated at the end); edge i is vertex i to vertex i+1.
	vx, vy []int32
	my     []float64 // Mercator y of each vertex, so edge queries skip the logarithm
	cells  map[int64]cellRange
	refs   []uint32 // edge (start vertex) indices grouped by cell
}

type cellRange struct{ off, n uint32 }

func cellKey(cx, cy int32) int64 { return int64(cx)<<32 | int64(uint32(cy)) }

type landBuilder struct {
	vx, vy []int32
	edges  []uint32
}

func (b *landBuilder) addRing(xs, ys []int32) {
	if len(xs) < 2 {
		return
	}
	base := uint32(len(b.vx))
	b.vx = append(b.vx, xs...)
	b.vy = append(b.vy, ys...)
	n := len(xs)
	if xs[0] != xs[n-1] || ys[0] != ys[n-1] {
		b.vx = append(b.vx, xs[0])
		b.vy = append(b.vy, ys[0])
		n++
	}
	for i := 0; i < n-1; i++ {
		b.edges = append(b.edges, base+uint32(i))
	}
}

func (b *landBuilder) build(tol float64, version string) *Land {
	l := &Land{Tolerance: tol, Version: version, vx: b.vx, vy: b.vy, cells: make(map[int64]cellRange)}
	cellSpan := func(e uint32) (x0, y0, x1, y1 int32) {
		ax, ay, bx, by := l.vx[e], l.vy[e], l.vx[e+1], l.vy[e+1]
		f := func(v int32) int32 { return int32(math.Floor(float64(v) * landCellsPerDeg / landMicro)) }
		x0, x1 = f(min(ax, bx)), f(max(ax, bx))
		y0, y1 = f(min(ay, by)), f(max(ay, by))
		return
	}
	for _, e := range b.edges {
		x0, y0, x1, y1 := cellSpan(e)
		for x := x0; x <= x1; x++ {
			for y := y0; y <= y1; y++ {
				c := l.cells[cellKey(x, y)]
				c.n++
				l.cells[cellKey(x, y)] = c
			}
		}
	}
	var total uint32
	for k, c := range l.cells {
		c.off = total
		total += c.n
		c.n = 0
		l.cells[k] = c
	}
	l.refs = make([]uint32, total)
	for _, e := range b.edges {
		x0, y0, x1, y1 := cellSpan(e)
		for x := x0; x <= x1; x++ {
			for y := y0; y <= y1; y++ {
				k := cellKey(x, y)
				c := l.cells[k]
				l.refs[c.off+c.n] = e
				c.n++
				l.cells[k] = c
			}
		}
	}
	l.my = make([]float64, len(l.vy))
	for i, y := range l.vy {
		l.my[i] = Mercator(Position{0, float64(y) / landMicro})[1]
	}
	b.edges = nil
	return l
}

func toMicro(v float64) int32 { return int32(math.Round(v * landMicro)) }

// NewLand builds a Land from rings of (lon, lat) positions; for synthetic tests.
func NewLand(rings [][]Position, toleranceM float64, version string) *Land {
	var b landBuilder
	for _, r := range rings {
		xs, ys := make([]int32, len(r)), make([]int32, len(r))
		for i, p := range r {
			xs[i], ys[i] = toMicro(p[0]), toMicro(p[1])
		}
		b.addRing(xs, ys)
	}
	return b.build(toleranceM, version)
}

// LoadLand reads a dataset written by cmd/land-import (see its package comment
// for the format). The file is treated as untrusted: counts are bounded and
// truncation is an error.
func LoadLand(path string) (*Land, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("land %s: %w", path, err)
	}
	defer zr.Close()
	l, err := readLand(bufio.NewReaderSize(zr, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("land %s: %w", path, err)
	}
	return l, nil
}

func readLand(br *bufio.Reader) (*Land, error) {
	eof := func(err error) error {
		if err == io.EOF {
			return io.ErrUnexpectedEOF
		}
		return err
	}
	magic := make([]byte, len(landMagic))
	if _, err := io.ReadFull(br, magic); err != nil {
		return nil, eof(err)
	}
	if string(magic) != landMagic {
		return nil, errors.New("bad magic")
	}
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > landMaxHeaderLen {
			return nil, errors.New("header too long")
		}
		if err == nil {
			break
		}
		if err != bufio.ErrBufferFull {
			return nil, eof(err)
		}
	}
	var hdr struct {
		Date       string  `json:"date"`
		ToleranceM float64 `json:"tolerance_m"`
		CorridorNM float64 `json:"corridor_nm"`
		Rings      int     `json:"rings"`
	}
	if err := json.Unmarshal(line, &hdr); err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	if hdr.Rings < 0 || hdr.Rings > landMaxRings {
		return nil, fmt.Errorf("ring count %d out of range", hdr.Rings)
	}
	if !(hdr.ToleranceM >= 0) || math.IsInf(hdr.ToleranceM, 0) {
		return nil, errors.New("bad tolerance")
	}
	if !(hdr.CorridorNM >= 0) || math.IsInf(hdr.CorridorNM, 0) {
		return nil, errors.New("bad corridor")
	}
	var b landBuilder
	var xs, ys []int32
	for r := 0; r < hdr.Rings; r++ {
		n, err := binary.ReadUvarint(br)
		if err != nil {
			return nil, eof(err)
		}
		if n > uint64(landMaxVertices-len(b.vx)) {
			return nil, fmt.Errorf("ring %d: vertex count %d exceeds limit", r, n)
		}
		xs, ys = xs[:0], ys[:0]
		var px, py int64
		for i := uint64(0); i < n; i++ {
			dx, err := binary.ReadVarint(br)
			if err != nil {
				return nil, eof(err)
			}
			dy, err := binary.ReadVarint(br)
			if err != nil {
				return nil, eof(err)
			}
			px += dx
			py += dy
			if px < -180*landMicro || px > 180*landMicro || py < -90*landMicro || py > 90*landMicro {
				return nil, fmt.Errorf("ring %d: coordinate out of range", r)
			}
			xs = append(xs, int32(px))
			ys = append(ys, int32(py))
		}
		b.addRing(xs, ys)
	}
	l := b.build(hdr.ToleranceM, fmt.Sprintf("%s/%gm", hdr.Date, hdr.ToleranceM))
	l.CorridorNM = hdr.CorridorNM
	return l, nil
}

// PointDistance is the distance in metres from p to the nearest land edge,
// capped at limitM.
func (l *Land) PointDistance(p Position, limitM float64) float64 {
	return l.SegmentClearance(p, p, limitM)
}

// SegmentClearance is the smallest distance in metres from the rhumb leg a-b
// to any land edge, capped at limitM. It is 0 when the leg touches land. The
// leg is straight in Mercator coordinates at any length; a leg crossing the
// antimeridian is checked as two pieces.
func (l *Land) SegmentClearance(a, b Position, limitM float64) float64 {
	best := limitM
	for _, pc := range splitAntimeridian(a, b) {
		if best = l.pieceClearance(pc[0], pc[1], best); best == 0 {
			return 0
		}
	}
	return best
}

// Within reports whether any land edge lies within rM metres of p. It looks at
// the cells around p nearest first and stops at the first edge in range, so it
// is cheap near coasts, where PointDistance would measure every edge nearby.
func (l *Land) Within(p Position, rM float64) bool {
	const rad = math.Pi / 180
	m := Mercator(p)
	ey := rM/(earthRadiusM*rad) + landSampleStepDeg*1.5
	ex := rM/(earthRadiusM*rad*math.Max(0.01, math.Cos((math.Min(89, math.Abs(p[1]))+ey)*rad))) + landSampleStepDeg*1.5
	ex = math.Min(ex, 360)
	cx0, cx1 := int32(math.Floor((p[0]-ex)*landCellsPerDeg)), int32(math.Floor((p[0]+ex)*landCellsPerDeg))
	cy0, cy1 := int32(math.Floor((p[1]-ey)*landCellsPerDeg)), int32(math.Floor((p[1]+ey)*landCellsPerDeg))
	px, py := int32(math.Floor(p[0]*landCellsPerDeg)), int32(math.Floor(p[1]*landCellsPerDeg))
	// Cheap equirectangular pre-test in degrees (scaled by cos lat at p, with
	// slack for the scale change across the cells) so most edges skip the
	// Mercator maths.
	cosP := math.Cos(p[1] * rad)
	reach := 1.25 * rM / (earthRadiusM * rad)
	for r := int32(0); r <= max(px-cx0, cx1-px, py-cy0, cy1-py); r++ {
		for cx := max(px-r, cx0); cx <= min(px+r, cx1); cx++ {
			for cy := max(py-r, cy0); cy <= min(py+r, cy1); cy++ {
				if max(abs32(cx-px), abs32(cy-py)) != r {
					continue
				}
				c, ok := l.cells[cellKey(cx, cy)]
				if !ok {
					continue
				}
				for _, e := range l.refs[c.off : c.off+c.n] {
					ax, ay := (float64(l.vx[e])/landMicro-p[0])*cosP, float64(l.vy[e])/landMicro-p[1]
					bx, by := (float64(l.vx[e+1])/landMicro-p[0])*cosP, float64(l.vy[e+1])/landMicro-p[1]
					if q := closestOnSeg([2]float64{}, [2]float64{ax, ay}, [2]float64{bx, by}); q[0]*q[0]+q[1]*q[1] > reach*reach {
						continue
					}
					if l.edgeClearance(m, m, e) < rM {
						return true
					}
				}
			}
		}
	}
	return false
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func (l *Land) pieceClearance(a, b Position, limit float64) float64 {
	ma, mb := Mercator(a), Mercator(b)
	const rad = math.Pi / 180
	// Cell lookup only: walk the leg in sub-cell steps and visit the cells
	// within limit of each sample (plus slack for the gap between samples).
	n := int(math.Ceil(math.Max(math.Abs(b[0]-a[0]), math.Abs(b[1]-a[1]))/landSampleStepDeg)) + 1
	maxLat := math.Min(89, math.Max(math.Abs(a[1]), math.Abs(b[1])))
	// A Mercator-straight leg can bulge in latitude between samples only
	// monotonically, so the end latitudes bound it.
	ey := limit/(earthRadiusM*rad) + landSampleStepDeg*1.5
	ex := limit/(earthRadiusM*rad*math.Max(0.01, math.Cos((maxLat+ey)*rad))) + landSampleStepDeg*1.5
	ex = math.Min(ex, 360)
	seen := make(map[int64]struct{})
	best := limit
	// An edge whose Mercator bounding box is farther from the leg's than best
	// cannot be nearer: metres = R * gap * cos(lat) with lat inside the band
	// the visited cells span, so cosMin bounds the conversion from below.
	cosMin := math.Max(math.Cos(math.Min(89.9, maxLat+ey)*rad), 0)
	bx0, bx1 := math.Min(ma[0], mb[0]), math.Max(ma[0], mb[0])
	by0, by1 := math.Min(ma[1], mb[1]), math.Max(ma[1], mb[1])
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		lon := (ma[0] + t*(mb[0]-ma[0])) / rad
		lat := invMercatorLat(ma[1] + t*(mb[1]-ma[1]))
		cx0 := int32(math.Floor((lon - ex) * landCellsPerDeg))
		cx1 := int32(math.Floor((lon + ex) * landCellsPerDeg))
		cy0 := int32(math.Floor((lat - ey) * landCellsPerDeg))
		cy1 := int32(math.Floor((lat + ey) * landCellsPerDeg))
		for cx := cx0; cx <= cx1; cx++ {
			for cy := cy0; cy <= cy1; cy++ {
				k := cellKey(cx, cy)
				c, ok := l.cells[k]
				if !ok {
					continue
				}
				if _, dup := seen[k]; dup {
					continue
				}
				seen[k] = struct{}{}
				for _, e := range l.refs[c.off : c.off+c.n] {
					ex0, ex1 := float64(l.vx[e])/landMicro*rad, float64(l.vx[e+1])/landMicro*rad
					if ex0 > ex1 {
						ex0, ex1 = ex1, ex0
					}
					ey0, ey1 := l.my[e], l.my[e+1]
					if ey0 > ey1 {
						ey0, ey1 = ey1, ey0
					}
					gx, gy := math.Max(0, math.Max(bx0-ex1, ex0-bx1)), math.Max(0, math.Max(by0-ey1, ey0-by1))
					if earthRadiusM*cosMin*math.Hypot(gx, gy) >= best {
						continue
					}
					if d := l.edgeClearance(ma, mb, e); d < best {
						if best = d; best == 0 {
							return 0
						}
					}
				}
			}
		}
	}
	return best
}

// edgeClearance is the metre distance from the Mercator leg ma-mb to edge e.
func (l *Land) edgeClearance(ma, mb [2]float64, e uint32) float64 {
	const rad = math.Pi / 180
	c := [2]float64{float64(l.vx[e]) / landMicro * rad, l.my[e]}
	d := [2]float64{float64(l.vx[e+1]) / landMicro * rad, l.my[e+1]}
	gap, onLeg := segSegDist(ma, mb, c, d)
	if gap == 0 {
		return 0
	}
	onEdge := closestOnSeg(onLeg, c, d)
	cosLat := math.Cos(invMercatorLat((onLeg[1]+onEdge[1])/2) * math.Pi / 180)
	return earthRadiusM * gap * math.Max(cosLat, 0)
}
