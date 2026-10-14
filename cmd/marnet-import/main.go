// Command marnet-import converts a Eurostat MARNET GeoPackage into the
// GeoJSON network that package route loads, keeping the passage tags.
//
// Source: https://github.com/eurostat/searoute, modules/core/src/main/resources/marnet/
// (marnet_plus_<res>km.gpkg). Licence EUPL-1.2.
//
// Usage: go run ./cmd/marnet-import -in marnet_plus_5km.gpkg -out dataset/marnet_plus_5km.geojson
package main

import (
	"bufio"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	in := flag.String("in", "", "MARNET GeoPackage (.gpkg)")
	out := flag.String("out", "", "output GeoJSON")
	flag.Parse()
	if *in == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*in, *out); err != nil {
		log.Fatal(err)
	}
}

func run(in, out string) error {
	db, err := sql.Open("sqlite3", "file:"+in+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()

	var table string
	if err := db.QueryRow(`SELECT table_name FROM gpkg_contents WHERE data_type = 'features' LIMIT 1`).Scan(&table); err != nil {
		return fmt.Errorf("find feature table: %w", err)
	}
	var srs int
	if err := db.QueryRow(`SELECT srs_id FROM gpkg_geometry_columns WHERE table_name = ?`, table).Scan(&srs); err != nil {
		return fmt.Errorf("read srs: %w", err)
	}
	if srs != 4326 {
		return fmt.Errorf("srs %d, want 4326 (WGS 84)", srs)
	}

	rows, err := db.Query(fmt.Sprintf(`SELECT geometry, COALESCE(pass, '') FROM %q ORDER BY fid`, table))
	if err != nil {
		return err
	}
	defer rows.Close()

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	fmt.Fprint(w, `{"type":"FeatureCollection","features":[`)

	n, tagged := 0, 0
	for rows.Next() {
		var blob []byte
		var pass string
		if err := rows.Scan(&blob, &pass); err != nil {
			return err
		}
		coords, err := decodeLineString(blob)
		if err != nil {
			return fmt.Errorf("feature %d: %w", n+1, err)
		}
		if n > 0 {
			w.WriteByte(',')
		}
		props := "{}"
		if pass != "" {
			p, _ := json.Marshal(map[string]string{"pass": pass})
			props = string(p)
			tagged++
		}
		fmt.Fprintf(w, "\n{\"type\":\"Feature\",\"properties\":%s,\"geometry\":{\"type\":\"LineString\",\"coordinates\":[", props)
		for i, c := range coords {
			if i > 0 {
				w.WriteByte(',')
			}
			fmt.Fprintf(w, "[%s,%s]", num(c[0]), num(c[1]))
		}
		fmt.Fprint(w, "]}}")
		n++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	fmt.Fprint(w, "\n]}\n")
	if err := w.Flush(); err != nil {
		return err
	}
	log.Printf("wrote %s: %d lines, %d with a passage tag", out, n, tagged)
	return nil
}

// num prints a coordinate with 6 decimals (about 0.1 m), trimmed.
func num(v float64) string {
	return strconv.FormatFloat(math.Round(v*1e6)/1e6, 'f', -1, 64)
}

// decodeLineString reads a GeoPackage geometry blob holding a 2D LineString.
// Layout: "GP", version, flags, srs_id, optional envelope, then WKB.
func decodeLineString(b []byte) ([][2]float64, error) {
	if len(b) < 8 || b[0] != 'G' || b[1] != 'P' {
		return nil, errors.New("not a GeoPackage geometry")
	}
	flags := b[3]
	envelope := [...]int{0, 32, 48, 48, 64}
	ei := int(flags>>1) & 0x7
	if ei >= len(envelope) {
		return nil, fmt.Errorf("bad envelope indicator %d", ei)
	}
	wkb := b[8+envelope[ei]:]
	if len(wkb) < 9 {
		return nil, errors.New("short WKB")
	}
	var order binary.ByteOrder = binary.BigEndian
	if wkb[0] == 1 {
		order = binary.LittleEndian
	}
	if t := order.Uint32(wkb[1:5]); t != 2 {
		return nil, fmt.Errorf("WKB type %d, want 2 (LineString)", t)
	}
	n := int(order.Uint32(wkb[5:9]))
	if len(wkb) < 9+16*n {
		return nil, errors.New("short WKB coordinates")
	}
	coords := make([][2]float64, n)
	for i := range coords {
		off := 9 + 16*i
		coords[i][0] = math.Float64frombits(order.Uint64(wkb[off:]))
		coords[i][1] = math.Float64frombits(order.Uint64(wkb[off+8:]))
	}
	return coords, nil
}
