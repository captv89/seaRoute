package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// readShp streams a polygon (type 5) ESRI shapefile and calls fn once per
// ring (each part of each record). Ring slices are not reused.
func readShp(path string, fn func(ring [][2]float64)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 1<<20)

	hdr := make([]byte, 100)
	if _, err := io.ReadFull(br, hdr); err != nil {
		return fmt.Errorf("shp header: %w", err)
	}
	if binary.BigEndian.Uint32(hdr[0:]) != 9994 {
		return errors.New("not a shapefile (bad file code)")
	}
	if t := binary.LittleEndian.Uint32(hdr[32:]); t != 5 {
		return fmt.Errorf("unsupported shape type %d (want polygon, 5)", t)
	}

	rh := make([]byte, 8)
	for {
		if _, err := io.ReadFull(br, rh); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("record header: %w", err)
		}
		n := int(binary.BigEndian.Uint32(rh[4:])) * 2
		if n < 4 || n > 1<<30 {
			return fmt.Errorf("bad record length %d", n)
		}
		rec := make([]byte, n)
		if _, err := io.ReadFull(br, rec); err != nil {
			return fmt.Errorf("record body: %w", err)
		}
		switch binary.LittleEndian.Uint32(rec) {
		case 0: // null shape
			continue
		case 5:
		default:
			return fmt.Errorf("record shape type %d, want 5", binary.LittleEndian.Uint32(rec))
		}
		if n < 44 {
			return errors.New("short polygon record")
		}
		nParts := int(binary.LittleEndian.Uint32(rec[36:]))
		nPts := int(binary.LittleEndian.Uint32(rec[40:]))
		if nParts < 0 || nPts < 0 || 44+4*nParts+16*nPts > n {
			return errors.New("polygon record counts exceed its length")
		}
		parts := make([]int, nParts+1)
		for i := 0; i < nParts; i++ {
			parts[i] = int(binary.LittleEndian.Uint32(rec[44+4*i:]))
		}
		parts[nParts] = nPts
		base := 44 + 4*nParts
		for i := 0; i < nParts; i++ {
			a, b := parts[i], parts[i+1]
			if a < 0 || b < a || b > nPts {
				return errors.New("bad part index")
			}
			ring := make([][2]float64, b-a)
			for j := range ring {
				o := base + 16*(a+j)
				ring[j] = [2]float64{
					math.Float64frombits(binary.LittleEndian.Uint64(rec[o:])),
					math.Float64frombits(binary.LittleEndian.Uint64(rec[o+8:])),
				}
			}
			fn(ring)
		}
	}
}
