// Package weather reads forecast fields (wind, waves, currents) from GRIB2
// files such as ECMWF open data. Decoding is pure Go: CCSDS packing (GRIB2
// template 5.42, used by ECMWF) is handled by a port of libaec, so the build
// needs no C toolchain.
package weather

import (
	"errors"
	"fmt"
	"math"
	"time"

	grib "github.com/pspoerri/go-tiled-eccodes"
)

// Grid is a regular latitude/longitude grid in natural order: row 0 is the
// northernmost row, column 0 the westernmost. The value at index row*NX+col
// sits at (Lat0 + row*DLat, Lon0 + col*DLon). DLat is negative, DLon positive.
type Grid struct {
	NX, NY     int
	Lat0, Lon0 float64 // north-west grid point, degrees
	DLat, DLon float64
}

// Field is one decoded GRIB2 message: one parameter at one forecast time.
type Field struct {
	Grid
	Ref    time.Time // model run (analysis) time, UTC
	Valid  time.Time // time the values apply to, UTC
	Values []float32 // natural order; NaN where the field has no value (land for sea fields)
}

// DecodeField decodes a single GRIB2 message. Only regular lat/lon grids are
// accepted; the data is treated as untrusted.
func DecodeField(msg []byte) (Field, error) {
	f, err := grib.FromBytes(msg)
	if err != nil {
		return Field{}, fmt.Errorf("grib: %w", err)
	}
	ms := f.Messages()
	if len(ms) != 1 {
		return Field{}, fmt.Errorf("grib: %d messages, want 1", len(ms))
	}
	m := ms[0]
	ll, ok := m.RegularLatLon()
	if !ok {
		return Field{}, errors.New("grib: not a regular lat/lon grid")
	}
	if ll.Nx <= 0 || ll.Ny <= 0 || ll.DLon <= 0 || ll.DLat >= 0 {
		return Field{}, fmt.Errorf("grib: bad grid %+v", ll)
	}
	h := m.Header()
	valid, ok := h.ValidTime()
	if !ok {
		return Field{}, errors.New("grib: no valid time")
	}
	vals, err := m.DecodeNaturalFloat32(nil)
	if err != nil {
		return Field{}, fmt.Errorf("grib: decode: %w", err)
	}
	if len(vals) != ll.Nx*ll.Ny {
		return Field{}, fmt.Errorf("grib: %d values for a %dx%d grid", len(vals), ll.Nx, ll.Ny)
	}
	return Field{
		Grid:   Grid{NX: ll.Nx, NY: ll.Ny, Lat0: ll.Lat0, Lon0: ll.Lon0, DLat: ll.DLat, DLon: ll.DLon},
		Ref:    h.ReferenceTime.UTC(),
		Valid:  valid.UTC(),
		Values: vals,
	}, nil
}

// At returns the value at the grid point nearest to (lat, lon), or NaN when
// the point lies outside the grid or the field has no value there.
func (f *Field) At(lat, lon float64) float32 {
	row := int(math.Round((lat - f.Lat0) / f.DLat))
	if row < 0 || row >= f.NY {
		return float32(math.NaN())
	}
	col := int(math.Round(math.Mod(math.Mod(lon-f.Lon0, 360)+360, 360) / f.DLon))
	if float64(f.NX)*f.DLon >= 359.999 { // global: wrap across the seam
		col %= f.NX
	}
	if col < 0 || col >= f.NX {
		return float32(math.NaN())
	}
	return f.Values[row*f.NX+col]
}
