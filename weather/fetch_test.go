package weather

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

var testRun = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

// fixtureServer serves the swh fixture as the wave message of run
// 2026-11-01 00Z step 24, with Range support. The oper index is empty.
func fixtureServer(t *testing.T, dataStatus int, operParam string) *httptest.Server {
	t.Helper()
	index, err := os.ReadFile("testdata/fixture.index")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/swh-20261101T00-024h.grib2")
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "-oper-fc.index"):
			if operParam != "" {
				fmt.Fprintf(w, `{"param": %q, "_offset": 0, "_length": %d}`+"\n", operParam, len(data))
			}
		case strings.HasSuffix(r.URL.Path, "-wave-fc.index"):
			w.Write(index)
		case strings.HasSuffix(r.URL.Path, ".grib2"):
			if dataStatus != 0 {
				http.Error(w, "boom", dataStatus)
				return
			}
			var a, b int
			if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &a, &b); err != nil || b >= len(data) {
				http.Error(w, "bad range", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(b-a+1))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(data[a : b+1])
		default:
			http.NotFound(w, r)
		}
	}))
}

func testFetcher(url string, steps []int) *Fetcher {
	return &Fetcher{BaseURL: url, Steps: steps, Workers: 2, backoff: []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}}
}

func TestGribURL(t *testing.T) {
	run := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	got := gribURL("https://x", run, "oper", 24, "index")
	want := "https://x/20261102/12z/ifs/0p25/oper/20261102120000-24h-oper-fc.index"
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestParseIndex(t *testing.T) {
	m, err := parseIndex(strings.NewReader("{\"param\":\"a\",\"_offset\":5,\"_length\":10}\n\n{\"param\":\"b\",\"_offset\":15,\"_length\":7}\n"))
	if err != nil || len(m) != 2 || m["b"] != (indexEntry{15, 7}) {
		t.Fatalf("got %v, %v", m, err)
	}
}

func TestParseIndexRejectsHugeLength(t *testing.T) {
	for _, line := range []string{
		`{"param":"a","_offset":0,"_length":17000000}`,
		`{"param":"a","_offset":0,"_length":0}`,
		`{"param":"a","_offset":-1,"_length":5}`,
		`not json`,
	} {
		if _, err := parseIndex(strings.NewReader(line + "\n")); err == nil {
			t.Errorf("%s: want error", line)
		}
	}
}

func TestParseIndexRejectsTooManyLines(t *testing.T) {
	line := `{"param":"a","_offset":0,"_length":5}` + "\n"
	if _, err := parseIndex(strings.NewReader(strings.Repeat(line, maxIndexLines+1))); err == nil {
		t.Fatal("want error")
	}
}

func TestWantedSkipsGustAtStep0(t *testing.T) {
	has := func(fs []FieldID, f FieldID) bool {
		for _, x := range fs {
			if x == f {
				return true
			}
		}
		return false
	}
	if has(wanted("oper", 0), Gust) || !has(wanted("oper", 3), Gust) {
		t.Error("Gust should be wanted only after step 0")
	}
	w := wanted("wave", 0)
	if len(w) != 3 || !has(w, SWH) || !has(w, MWD) || !has(w, MWP) {
		t.Errorf("wave fields %v", w)
	}
}

func TestFetchWritesRun(t *testing.T) {
	srv := fixtureServer(t, 0, "10u")
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "run.bin")
	if err := testFetcher(srv.URL, []int{24}).Fetch(context.Background(), testRun, path); err != nil {
		t.Fatal(err)
	}
	r, err := OpenRun(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	v := r.At(50, -20, testRun.Add(24*time.Hour))
	if d := math.Abs(float64(v[SWH]) - 2.285643); d > 0.005 {
		t.Errorf("SWH %v, want 2.2856", v[SWH])
	}
	if isNaN(v[U10]) {
		t.Error("U10 is NaN, want a value")
	}
	if !isNaN(v[V10]) {
		t.Errorf("V10 %v, want NaN", v[V10])
	}
}

func TestFetchEmptyStreamFails(t *testing.T) {
	srv := fixtureServer(t, 0, "")
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "run.bin")
	if err := testFetcher(srv.URL, []int{24}).Fetch(context.Background(), testRun, path); err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file left behind: %v", err)
	}
}

func TestLatestEmptyStepsUsesDefault(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	f := testFetcher(srv.URL, []int{})
	f.Now = func() time.Time { return testRun }
	if _, err := f.Latest(context.Background()); err == nil {
		t.Fatal("want not-ready error, not a panic")
	}
}

func TestFetchWrongValidTimeFails(t *testing.T) {
	srv := fixtureServer(t, 0, "10u")
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "run.bin")
	if err := testFetcher(srv.URL, []int{48}).Fetch(context.Background(), testRun, path); err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file left behind: %v", err)
	}
}

func TestFetchServerErrorRemovesFile(t *testing.T) {
	srv := fixtureServer(t, http.StatusInternalServerError, "10u")
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "run.bin")
	if err := testFetcher(srv.URL, []int{24}).Fetch(context.Background(), testRun, path); err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file left behind: %v", err)
	}
}

func TestLatestPicksNewestReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/20261102/00z/") && strings.HasSuffix(r.URL.Path, "-24h-oper-fc.index") ||
			strings.HasPrefix(r.URL.Path, "/20261102/00z/") && strings.HasSuffix(r.URL.Path, "-24h-wave-fc.index") {
			w.Write([]byte("{}\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	f := testFetcher(srv.URL, []int{0, 24})
	f.Now = func() time.Time { return time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC) }
	got, err := f.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	f.Now = func() time.Time { return time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC) }
	if _, err := f.Latest(context.Background()); err == nil {
		t.Error("want error when no run is ready")
	}
}
