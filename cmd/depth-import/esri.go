package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
)

// esriHeader is the header of an Esri ASCII raster. XLL and YLL are the
// lower-left corner of the grid (not of the first cell centre).
type esriHeader struct {
	Ncols, Nrows int
	XLL, YLL     float64
	Cell         float64
	NoData       float64
	HasNoData    bool
}

// elevToDepth converts a GEBCO elevation in metres to a depth cell: 0 for land
// or drying (>= 0), whole metres rounded down (conservative) for water, and
// 255 for 255 m or deeper.
func elevToDepth(v float64) uint8 {
	if v >= 0 {
		return 0
	}
	d := math.Floor(-v)
	if d >= 255 {
		return 255
	}
	return uint8(d)
}

// readEsriASCII streams an Esri ASCII raster one row at a time, north to south,
// so that a 21600x21600 tile never sits in memory. fn gets the row index (0 =
// northmost), the converted depth cells and which cells were NODATA (stored as
// 0, the conservative value). vals and nodata are reused between calls.
func readEsriASCII(r io.Reader, fn func(h esriHeader, row int, vals []uint8, nodata []bool) error) (esriHeader, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	var h esriHeader
	var have [5]bool
	readLine := func() ([]byte, error) {
		line, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			return nil, errors.New("line too long")
		}
		if err == io.EOF && len(line) > 0 {
			err = nil
		}
		return bytes.TrimSpace(line), err
	}
	var pending []byte // first data line, read while looking for the end of the header
	for {
		line, err := readLine()
		if err != nil {
			return h, fmt.Errorf("esri header: %w", err)
		}
		f := bytes.Fields(line)
		if len(f) == 0 {
			continue
		}
		key := string(bytes.ToLower(f[0]))
		if len(f) != 2 || !(key[0] >= 'a' && key[0] <= 'z') {
			pending = append([]byte(nil), line...)
			break
		}
		v, err := strconv.ParseFloat(string(f[1]), 64)
		if err != nil {
			return h, fmt.Errorf("esri header %s: %w", key, err)
		}
		switch key {
		case "ncols":
			h.Ncols, have[0] = int(v), true
		case "nrows":
			h.Nrows, have[1] = int(v), true
		case "xllcorner":
			h.XLL, have[2] = v, true
		case "yllcorner":
			h.YLL, have[3] = v, true
		case "cellsize":
			h.Cell, have[4] = v, true
		case "nodata_value":
			h.NoData, h.HasNoData = v, true
		}
	}
	for _, ok := range have {
		if !ok {
			return h, errors.New("esri header: missing ncols/nrows/xllcorner/yllcorner/cellsize")
		}
	}
	if h.Ncols <= 0 || h.Nrows <= 0 || !(h.Cell > 0) {
		return h, fmt.Errorf("esri header: bad dimensions %dx%d cellsize %g", h.Ncols, h.Nrows, h.Cell)
	}
	vals := make([]uint8, h.Ncols)
	nodata := make([]bool, h.Ncols)
	for row := 0; row < h.Nrows; row++ {
		line := pending
		pending = nil
		if line == nil {
			var err error
			if line, err = readLine(); err != nil {
				return h, fmt.Errorf("esri row %d: %w", row, unexpected(err))
			}
		}
		n := 0
		for len(line) > 0 {
			for len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
				line = line[1:]
			}
			if len(line) == 0 {
				break
			}
			end := bytes.IndexAny(line, " \t")
			if end < 0 {
				end = len(line)
			}
			if n >= h.Ncols {
				return h, fmt.Errorf("esri row %d: more than %d values", row, h.Ncols)
			}
			v, err := parseNumber(line[:end])
			if err != nil {
				return h, fmt.Errorf("esri row %d col %d: %w", row, n, err)
			}
			if h.HasNoData && v == h.NoData {
				vals[n], nodata[n] = 0, true
			} else {
				vals[n], nodata[n] = elevToDepth(v), false
			}
			n++
			line = line[end:]
		}
		if n != h.Ncols {
			return h, fmt.Errorf("esri row %d: %d values, want %d", row, n, h.Ncols)
		}
		if err := fn(h, row, vals, nodata); err != nil {
			return h, err
		}
	}
	return h, nil
}

func unexpected(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

// parseNumber parses a decimal with an integer fast path (GEBCO is integers).
func parseNumber(b []byte) (float64, error) {
	neg, i, n := false, 0, int64(0)
	if len(b) > 0 && (b[0] == '-' || b[0] == '+') {
		neg, i = b[0] == '-', 1
	}
	if i < len(b) && len(b)-i < 18 {
		j := i
		for ; j < len(b) && b[j] >= '0' && b[j] <= '9'; j++ {
			n = n*10 + int64(b[j]-'0')
		}
		if j == len(b) {
			if neg {
				n = -n
			}
			return float64(n), nil
		}
	}
	return strconv.ParseFloat(string(b), 64)
}
