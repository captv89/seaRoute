package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/captv89/seaRoute/route"
)

func TestReadEsriASCII(t *testing.T) {
	src := `ncols 4
nrows 3
xllcorner 10
yllcorner 20
cellsize 0.5
NODATA_value -9999
-12.7 3 -300 -9999
0 -1 -254.9 -255
5 -0.5 -9999 -9999
`
	var got [][]uint8
	var nod [][]bool
	h, err := readEsriASCII(strings.NewReader(src), func(_ esriHeader, row int, vals []uint8, nodata []bool) error {
		if row != len(got) {
			t.Fatalf("row %d out of order", row)
		}
		got = append(got, append([]uint8(nil), vals...))
		nod = append(nod, append([]bool(nil), nodata...))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.Ncols != 4 || h.Nrows != 3 || h.XLL != 10 || h.YLL != 20 || h.Cell != 0.5 {
		t.Errorf("header %+v", h)
	}
	want := [][]uint8{{12, 0, 255, 0}, {0, 1, 254, 255}, {0, 0, 0, 0}}
	wantNod := [][]bool{{false, false, false, true}, {false, false, false, false}, {false, false, true, true}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("values %v, want %v", got, want)
	}
	if fmt.Sprint(nod) != fmt.Sprint(wantNod) {
		t.Errorf("nodata %v, want %v", nod, wantNod)
	}
}

func TestReadEsriASCIIBadRow(t *testing.T) {
	_, err := readEsriASCII(strings.NewReader("ncols 3\nnrows 1\nxllcorner 0\nyllcorner 0\ncellsize 1\n1 2\n"), func(esriHeader, int, []uint8, []bool) error { return nil })
	if err == nil {
		t.Error("short row should fail")
	}
}

// fixture: 2 tiles wide (cx 0 and 1), 1 tile high (cy 0); cellsize 1/240.
func fixture(valueAt func(col, rowFromSouth int) string) string {
	var b strings.Builder
	b.WriteString("ncols 240\nnrows 120\nxllcorner 0\nyllcorner 0\ncellsize 0.00416666666666667\nNODATA_value -9999\n")
	for r := 0; r < 120; r++ {
		for c := 0; c < 240; c++ {
			if c > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(valueAt(c, 119-r))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func TestTilerKeepsWantedTiles(t *testing.T) {
	src := fixture(func(c, j int) string {
		switch {
		case c == 125 && j == 30: // tile 1, cell (5, 30)
			return "-7.9"
		case c >= 120:
			return "-1000"
		default:
			return "-1000"
		}
	})
	tl := newTiler(map[[2]int32]bool{{1, 0}: true})
	if _, err := readEsriASCII(strings.NewReader(src), tl.row); err != nil {
		t.Fatal(err)
	}
	tiles := tl.finish()
	if len(tiles) != 1 || tiles[0].CX != 1 || tiles[0].CY != 0 || tiles[0].Deep {
		t.Fatalf("tiles %+v", tiles)
	}
	if g := tiles[0].M[30*120+5]; g != 7 {
		t.Errorf("shoal cell = %d, want 7", g)
	}
	if g := tiles[0].M[0]; g != 255 {
		t.Errorf("deep cell = %d, want 255", g)
	}
}

func TestTilerDeepAndNoData(t *testing.T) {
	src := fixture(func(c, j int) string {
		if c < 120 {
			return "-9999"
		}
		return "-2000"
	})
	tl := newTiler(map[[2]int32]bool{{0, 0}: true, {1, 0}: true})
	if _, err := readEsriASCII(strings.NewReader(src), tl.row); err != nil {
		t.Fatal(err)
	}
	tiles := tl.finish()
	if len(tiles) != 1 || tiles[0].CX != 1 || !tiles[0].Deep || tiles[0].M != nil {
		t.Fatalf("want only the deep tile 1, got %+v", tiles)
	}
	var _ route.DepthTile = tiles[0]
}

func TestTilerMask(t *testing.T) {
	// Tile 0 is NODATA-free but holds a 7 m cell; tile 1 is all deep; tiles
	// elsewhere were never covered by the input.
	src := fixture(func(c, j int) string {
		if c == 5 && j == 30 {
			return "-7.9"
		}
		return "-2000"
	})
	tl := newTiler(map[[2]int32]bool{})
	if _, err := readEsriASCII(strings.NewReader(src), tl.row); err != nil {
		t.Fatal(err)
	}
	m := tl.mask()
	at := func(cx, cy int) uint8 { return m[(cy+180)*720+cx+360] }
	if at(0, 0) != 7 || at(1, 0) != 255 || at(5, 5) != 0 {
		t.Errorf("mask = %d, %d, %d; want 7, 255, 0", at(0, 0), at(1, 0), at(5, 5))
	}
}
