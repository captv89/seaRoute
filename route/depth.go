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
	"slices"
)

const (
	depthTileDeg      = 0.5
	depthCellsPerTile = 120 // 15 arc-seconds
	depthCellDeg      = depthTileDeg / depthCellsPerTile
	depthSampleM      = 200.0
	depthMagic1       = "SRDEPTH1\n" // tiles only (no deep-ocean mask)
	depthMagic        = "SRDEPTH2\n" // tiles, then the deep-ocean mask
	depthMaskW        = 720          // mask columns: 360 degrees / 0.5
	depthMaskH        = 360
	depthMaxTiles     = 720 * 360 // every 0.5 degree tile on Earth
	depthMaxHeaderLen = 1 << 16
	depthSource       = "gebco2026"
)

// DepthTile is one 0.5 degree tile of the depth grid. Cell values: 0 = land or
// drying, 1-254 = depth in whole metres rounded down, 255 = 255 m or deeper.
// M holds depthCellsPerTile^2 cells row-major from the south-west corner
// (M[j*120+i] is column i from the west edge, row j from the south edge); it is
// nil when Deep, meaning every cell is 255.
type DepthTile struct {
	CX, CY int32 // floor(lon/0.5), floor(lat/0.5)
	Deep   bool
	M      []uint8
}

// Depth is a conservative water-depth grid over the tiles near the route
// network. Outside the stored tiles depth is unknown.
type Depth struct {
	Version string // "<date>/gebco2026"
	tiles   map[int64]*DepthTile
	mask    []uint8 // depthMaskW*depthMaskH minimum depth per 0.5 degree tile, row-major from (-180,-90); nil when absent
}

// NewDepth builds a Depth from tiles; for the loader and synthetic tests.
func NewDepth(tiles []DepthTile, version string) *Depth {
	d := &Depth{Version: version, tiles: make(map[int64]*DepthTile, len(tiles))}
	for i := range tiles {
		t := tiles[i]
		d.tiles[cellKey(t.CX, t.CY)] = &t
	}
	return d
}

// NewDepthMask is NewDepth plus the worldwide deep-ocean mask: one byte per
// 0.5 degree tile (720 columns from 180 W, 360 rows from 90 S), the minimum
// depth over the tile in the cell encoding. mask must have 720*360 bytes or be nil.
func NewDepthMask(tiles []DepthTile, mask []uint8, version string) *Depth {
	d := NewDepth(tiles, version)
	if len(mask) == depthMaskW*depthMaskH {
		d.mask = mask
	}
	return d
}

// TileMinDepth returns the minimum stored depth in metres over the 0.5 degree
// tile containing p (0 when the mask is absent, so callers treat unknown as
// shallow).
func (d *Depth) TileMinDepth(p Position) float64 {
	if d.mask == nil {
		return 0
	}
	cx := int(math.Floor(wrapLon(p[0])/depthTileDeg)) + depthMaskW/2
	cy := int(math.Floor(p[1]/depthTileDeg)) + depthMaskH/2
	if cx < 0 || cx >= depthMaskW || cy < 0 || cy >= depthMaskH {
		return 0
	}
	return float64(d.mask[cy*depthMaskW+cx])
}

// DeepTiles reports whether every 0.5 degree tile the rhumb leg a-b passes
// through has a minimum depth of at least minM. The tiles are found exactly: the
// leg is cut where it crosses a tile edge (a straight line in Mercator
// coordinates) and one point inside every piece is tested, so a tile whose
// corner the leg only clips is never missed. The leg is split at the
// antimeridian.
func (d *Depth) DeepTiles(a, b Position, minM float64) bool {
	for _, pc := range splitAntimeridian(a, b) {
		ma, mb := Mercator(pc[0]), Mercator(pc[1])
		ts := []float64{0, 1}
		cut := func(lo, hi float64, at func(k float64) float64, from, to float64) {
			if lo > hi {
				lo, hi = hi, lo
			}
			for k := math.Ceil(lo / depthTileDeg); k*depthTileDeg <= hi; k++ {
				if t := (at(k*depthTileDeg) - from) / (to - from); t > 0 && t < 1 {
					ts = append(ts, t)
				}
			}
		}
		if mb[0] != ma[0] {
			cut(pc[0][0], pc[1][0], func(deg float64) float64 { return deg * math.Pi / 180 }, ma[0], mb[0])
		}
		if mb[1] != ma[1] {
			cut(pc[0][1], pc[1][1], func(deg float64) float64 { return Mercator(Position{0, deg})[1] }, ma[1], mb[1])
		}
		slices.Sort(ts)
		for i := 0; i+1 < len(ts); i++ {
			t := (ts[i] + ts[i+1]) / 2
			p := Position{(ma[0] + t*(mb[0]-ma[0])) * 180 / math.Pi, invMercatorLat(ma[1] + t*(mb[1]-ma[1]))}
			if d.TileMinDepth(p) < minM {
				return false
			}
		}
	}
	return true
}

// wrapLon maps a longitude into [-180, 180).
func wrapLon(lon float64) float64 {
	if lon >= -180 && lon < 180 {
		return lon
	}
	lon = math.Mod(lon+180, 360)
	if lon < 0 {
		lon += 360
	}
	return lon - 180
}

// tileAt returns the tile holding p and p's cell index within it.
func (d *Depth) tileAt(p Position) (t *DepthTile, i, j int) {
	lon := wrapLon(p[0])
	cx, cy := int32(math.Floor(lon/depthTileDeg)), int32(math.Floor(p[1]/depthTileDeg))
	t = d.tiles[cellKey(cx, cy)]
	if t == nil {
		return nil, 0, 0
	}
	i = min(depthCellsPerTile-1, int((lon-float64(cx)*depthTileDeg)/depthCellDeg))
	j = min(depthCellsPerTile-1, int((p[1]-float64(cy)*depthTileDeg)/depthCellDeg))
	return t, max(i, 0), max(j, 0)
}

func (t *DepthTile) at(i, j int) float64 {
	if t.Deep {
		return 255
	}
	return float64(t.M[j*depthCellsPerTile+i])
}

// At returns the stored depth in metres at p (0 = land or drying, 255 = 255 m
// or deeper). known is false outside the stored tiles.
func (d *Depth) At(p Position) (m float64, known bool) {
	t, i, j := d.tileAt(p)
	if t == nil {
		return 0, false
	}
	return t.at(i, j), true
}

// SegmentMin is the minimum stored depth along the rhumb leg a-b, sampled every
// 200 m with both ends included. known is false if any sample lies outside the
// stored tiles; the minimum over the known samples is still returned (0 when
// none is known, the conservative value).
func (d *Depth) SegmentMin(a, b Position) (m float64, known bool) {
	m, known, any := 255.0, true, false
	var last *DepthTile // last tile seen when it was Deep: its samples cannot lower m
	for _, pc := range splitAntimeridian(a, b) {
		for _, p := range rhumbSamples(pc[0], pc[1], depthSampleM/MetresPerNM) {
			t, i, j := d.tileAt(p)
			if t == nil {
				known = false
				continue
			}
			any = true
			if t == last {
				continue
			}
			if t.Deep {
				last = t
				continue
			}
			last = nil
			m = min(m, t.at(i, j))
		}
	}
	if !any {
		return 0, false
	}
	return m, known
}

// WriteDepth writes tiles in the SRDEPTH2 format read by LoadDepth: gzip of
// the magic line, a JSON header line, per tile zigzag-varint CX and CY, a byte
// (1 = deep, 0 = cells) and, when not deep, 14400 cell bytes, then the
// deep-ocean mask (720*360 bytes, see NewDepthMask) when the header says
// "mask":true. mask may be nil to write none.
func WriteDepth(w io.Writer, tiles []DepthTile, mask []uint8, date string) error {
	if mask != nil && len(mask) != depthMaskW*depthMaskH {
		return fmt.Errorf("mask has %d bytes, want %d", len(mask), depthMaskW*depthMaskH)
	}
	zw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(zw)
	bw.WriteString(depthMagic)
	hj, err := json.Marshal(depthHeader{Source: depthSource, Date: date, TileDeg: depthTileDeg, Arcsec: 15, Tiles: len(tiles), Mask: mask != nil})
	if err != nil {
		return err
	}
	bw.Write(hj)
	bw.WriteByte('\n')
	var buf [binary.MaxVarintLen64]byte
	for _, t := range tiles {
		if !t.Deep && len(t.M) != depthCellsPerTile*depthCellsPerTile {
			return fmt.Errorf("tile (%d,%d): %d cells, want %d", t.CX, t.CY, len(t.M), depthCellsPerTile*depthCellsPerTile)
		}
		bw.Write(buf[:binary.PutVarint(buf[:], int64(t.CX))])
		bw.Write(buf[:binary.PutVarint(buf[:], int64(t.CY))])
		if t.Deep {
			bw.WriteByte(1)
			continue
		}
		bw.WriteByte(0)
		bw.Write(t.M)
	}
	bw.Write(mask)
	if err := bw.Flush(); err != nil {
		return err
	}
	return zw.Close()
}

type depthHeader struct {
	Source  string  `json:"source"`
	Date    string  `json:"date"`
	TileDeg float64 `json:"tile_deg"`
	Arcsec  int     `json:"arcsec"`
	Tiles   int     `json:"tiles"`
	Mask    bool    `json:"mask,omitempty"`
}

// LoadDepth reads a dataset written by WriteDepth (cmd/depth-import). The file
// is treated as untrusted: counts and coordinates are bounded and truncation is
// an error.
func LoadDepth(path string) (*Depth, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("depth %s: %w", path, err)
	}
	defer zr.Close()
	d, err := readDepth(bufio.NewReaderSize(zr, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("depth %s: %w", path, err)
	}
	return d, nil
}

func readDepth(br *bufio.Reader) (*Depth, error) {
	eof := func(err error) error {
		if err == io.EOF {
			return io.ErrUnexpectedEOF
		}
		return err
	}
	magic := make([]byte, len(depthMagic))
	if _, err := io.ReadFull(br, magic); err != nil {
		return nil, eof(err)
	}
	if string(magic) != depthMagic && string(magic) != depthMagic1 {
		return nil, errors.New("bad magic")
	}
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > depthMaxHeaderLen {
			return nil, errors.New("header too long")
		}
		if err == nil {
			break
		}
		if err != bufio.ErrBufferFull {
			return nil, eof(err)
		}
	}
	var hdr depthHeader
	if err := json.Unmarshal(line, &hdr); err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	if hdr.TileDeg != depthTileDeg || hdr.Arcsec != 15 {
		return nil, fmt.Errorf("unsupported grid %g deg / %d arcsec", hdr.TileDeg, hdr.Arcsec)
	}
	if hdr.Tiles < 0 || hdr.Tiles > depthMaxTiles {
		return nil, fmt.Errorf("tile count %d out of range", hdr.Tiles)
	}
	tiles := make([]DepthTile, 0, hdr.Tiles)
	seen := make(map[int64]bool, hdr.Tiles)
	for n := 0; n < hdr.Tiles; n++ {
		cx, err := binary.ReadVarint(br)
		if err != nil {
			return nil, eof(err)
		}
		cy, err := binary.ReadVarint(br)
		if err != nil {
			return nil, eof(err)
		}
		if cx < -360 || cx > 359 || cy < -180 || cy > 179 {
			return nil, fmt.Errorf("tile %d: index (%d,%d) out of range", n, cx, cy)
		}
		k := cellKey(int32(cx), int32(cy))
		if seen[k] {
			return nil, fmt.Errorf("tile %d: duplicate (%d,%d)", n, cx, cy)
		}
		seen[k] = true
		flag, err := br.ReadByte()
		if err != nil {
			return nil, eof(err)
		}
		t := DepthTile{CX: int32(cx), CY: int32(cy)}
		switch flag {
		case 1:
			t.Deep = true
		case 0:
			t.M = make([]uint8, depthCellsPerTile*depthCellsPerTile)
			if _, err := io.ReadFull(br, t.M); err != nil {
				return nil, eof(err)
			}
		default:
			return nil, fmt.Errorf("tile %d: bad flag %d", n, flag)
		}
		tiles = append(tiles, t)
	}
	var mask []uint8
	if hdr.Mask {
		mask = make([]uint8, depthMaskW*depthMaskH)
		if _, err := io.ReadFull(br, mask); err != nil {
			return nil, eof(err)
		}
	}
	return NewDepthMask(tiles, mask, fmt.Sprintf("%s/%s", hdr.Date, hdr.Source)), nil
}
