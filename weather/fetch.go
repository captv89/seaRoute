package weather

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// DefaultBaseURL is the ECMWF open data site.
const DefaultBaseURL = "https://data.ecmwf.int/forecasts"

const (
	maxMessageBytes = 16 << 20 // largest GRIB message we accept
	maxIndexLine    = 1 << 20
	maxIndexLines   = 10000
	maxIndexBytes   = 16 << 20
	requestTimeout  = 60 * time.Second
	defaultWorkers  = 4
	maxCandidates   = 4
)

// defaultBackoff is the wait before each retry of a failed request.
var defaultBackoff = []time.Duration{time.Second, 4 * time.Second, 16 * time.Second}

// ecmwfGrid is the grid every 0.25 degree open data message must have.
var ecmwfGrid = Grid{NX: 1440, NY: 721, Lat0: 90, Lon0: 180, DLat: -0.25, DLon: 0.25}

// Fetcher downloads forecast runs from ECMWF open data. Zero values pick
// defaults: DefaultBaseURL, a client with a 60 s timeout, DefaultSteps, 4
// workers and time.Now.
type Fetcher struct {
	BaseURL string
	Client  *http.Client
	Steps   []int
	Workers int
	Now     func() time.Time

	backoff []time.Duration // nil means defaultBackoff; tests shorten it
}

type indexEntry struct{ Offset, Length int64 }

// gribURL builds the URL of a run's index or data file; ext is "index" or "grib2".
func gribURL(base string, run time.Time, stream string, step int, ext string) string {
	run = run.UTC()
	return fmt.Sprintf("%s/%s/%02dz/ifs/0p25/%s/%s0000-%dh-%s-fc.%s",
		base, run.Format("20060102"), run.Hour(), stream, run.Format("2006010215"), step, stream, ext)
}

// parseIndex reads an ECMWF index (one JSON object per line) into the byte
// range of each parameter. The input is untrusted: lines, count and message
// sizes are bounded. The first entry of a repeated parameter wins.
func parseIndex(r io.Reader) (map[string]indexEntry, error) {
	sc := bufio.NewScanner(io.LimitReader(r, maxIndexBytes))
	sc.Buffer(make([]byte, 0, 4096), maxIndexLine)
	out := map[string]indexEntry{}
	n := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if n++; n > maxIndexLines {
			return nil, errors.New("weather: index has too many lines")
		}
		var e struct {
			Param  string `json:"param"`
			Offset int64  `json:"_offset"`
			Length int64  `json:"_length"`
		}
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("weather: bad index line: %w", err)
		}
		if e.Length <= 0 || e.Length > maxMessageBytes || e.Offset < 0 {
			return nil, fmt.Errorf("weather: index entry %q has bad range", e.Param)
		}
		if _, dup := out[e.Param]; !dup {
			out[e.Param] = indexEntry{e.Offset, e.Length}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("weather: reading index: %w", err)
	}
	return out, nil
}

// wanted lists the fields of one stream to download at a step. The step 0
// gust is only a placeholder in the data, so it is skipped.
func wanted(stream string, step int) []FieldID {
	var out []FieldID
	for f := FieldID(0); f < NumFields; f++ {
		if fieldSpecs[f].Stream != stream || (f == Gust && step == 0) {
			continue
		}
		out = append(out, f)
	}
	return out
}

var streams = []string{"oper", "wave"}

func (f *Fetcher) base() string {
	if f.BaseURL != "" {
		return f.BaseURL
	}
	return DefaultBaseURL
}

func (f *Fetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Timeout: requestTimeout}
}

func (f *Fetcher) steps() []int {
	if f.Steps != nil {
		return f.Steps
	}
	return DefaultSteps()
}

func (f *Fetcher) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *Fetcher) waits() []time.Duration {
	if f.backoff != nil {
		return f.backoff
	}
	return defaultBackoff
}

// statusError is an unexpected HTTP status.
type statusError struct {
	url  string
	code int
}

func (e *statusError) Error() string { return fmt.Sprintf("weather: GET %s: status %d", e.url, e.code) }

// retryable reports whether a failed request is worth repeating: network
// errors, server errors and 429, but not other client errors or cancellation.
func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var se *statusError
	if errors.As(err, &se) {
		return se.code >= 500 || se.code == http.StatusTooManyRequests
	}
	return true
}

// get issues a GET (with an optional Range header), requires wantStatus, and
// returns at most limit bytes of body. exact < 0 skips the exact length check;
// otherwise the body must be exactly exact bytes. Failures are retried with
// backoff.
func (f *Fetcher) get(ctx context.Context, url, rng string, wantStatus int, exact int64, limit int64) ([]byte, error) {
	var err error
	waits := f.waits()
	for attempt := 0; ; attempt++ {
		var body []byte
		if body, err = f.getOnce(ctx, url, rng, wantStatus, exact, limit); err == nil {
			return body, nil
		}
		if attempt >= len(waits) || !retryable(ctx, err) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(waits[attempt]):
		}
	}
}

func (f *Fetcher) getOnce(ctx context.Context, url, rng string, wantStatus int, exact, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		return nil, &statusError{url, resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("weather: GET %s: body over %d bytes", url, limit)
	}
	if exact >= 0 && int64(len(body)) != exact {
		return nil, fmt.Errorf("weather: GET %s: got %d bytes, want %d", url, len(body), exact)
	}
	return body, nil
}

// Latest returns the newest run (00z or 12z, among the last four at or
// before Now) whose index for the last step exists in both streams.
func (f *Fetcher) Latest(ctx context.Context) (time.Time, error) {
	steps := f.steps()
	last := steps[len(steps)-1]
	t := f.now().UTC().Truncate(12 * time.Hour)
	for i := 0; i < maxCandidates; i++ {
		run := t.Add(-time.Duration(i) * 12 * time.Hour)
		ready := true
		for _, s := range streams {
			_, err := f.get(ctx, gribURL(f.base(), run, s, last, "index"), "", http.StatusOK, -1, maxIndexBytes)
			var se *statusError
			if errors.As(err, &se) && se.code == http.StatusNotFound {
				ready = false
				break
			}
			if err != nil {
				return time.Time{}, err
			}
		}
		if ready {
			return run, nil
		}
	}
	return time.Time{}, errors.New("weather: no ready forecast run found")
}

// Fetch downloads the run into a new run file at path. On any error the
// partial file is removed.
func (f *Fetcher) Fetch(ctx context.Context, run time.Time, path string) error {
	run = run.UTC()
	steps := f.steps()
	w, err := CreateRun(path, run, ecmwfGrid, steps)
	if err != nil {
		return err
	}
	defer w.Abort()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type job struct {
		idx    int
		stream string
	}
	jobs := make(chan job)
	var (
		wg      sync.WaitGroup
		putMu   sync.Mutex // RunWriter.Put updates shared bookkeeping
		errOnce sync.Once
		firstEr error
	)
	fail := func(err error) {
		errOnce.Do(func() { firstEr = err; cancel() })
	}
	workers := f.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					continue
				}
				if err := f.fetchStep(ctx, run, steps[j.idx], j.idx, j.stream, w, &putMu); err != nil {
					fail(err)
				}
			}
		}()
	}
feed:
	for i := range steps {
		for _, s := range streams {
			select {
			case jobs <- job{i, s}:
			case <-ctx.Done():
				break feed
			}
		}
	}
	close(jobs)
	wg.Wait()
	if firstEr != nil {
		return firstEr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return w.Close()
}

// fetchStep downloads every wanted field of one stream at one step.
func (f *Fetcher) fetchStep(ctx context.Context, run time.Time, step, idx int, stream string, w *RunWriter, putMu *sync.Mutex) error {
	body, err := f.get(ctx, gribURL(f.base(), run, stream, step, "index"), "", http.StatusOK, -1, maxIndexBytes)
	if err != nil {
		return err
	}
	index, err := parseIndex(bytes.NewReader(body))
	if err != nil {
		return err
	}
	dataURL := gribURL(f.base(), run, stream, step, "grib2")
	for _, fid := range wanted(stream, step) {
		e, ok := index[fieldSpecs[fid].Param]
		if !ok {
			continue
		}
		rng := fmt.Sprintf("bytes=%d-%d", e.Offset, e.Offset+e.Length-1)
		msg, err := f.get(ctx, dataURL, rng, http.StatusPartialContent, e.Length, maxMessageBytes)
		if err != nil {
			return err
		}
		fld, err := DecodeField(msg)
		if err != nil {
			return fmt.Errorf("weather: %s step %d %s: %w", stream, step, fieldSpecs[fid].Param, err)
		}
		if fld.Grid != ecmwfGrid {
			return fmt.Errorf("weather: %s step %d %s: unexpected grid %+v", stream, step, fieldSpecs[fid].Param, fld.Grid)
		}
		if !fld.Ref.Equal(run) || !fld.Valid.Equal(run.Add(time.Duration(step)*time.Hour)) {
			return fmt.Errorf("weather: %s step %d %s: message is for run %v valid %v", stream, step, fieldSpecs[fid].Param, fld.Ref, fld.Valid)
		}
		putMu.Lock()
		err = w.Put(fid, idx, fld.Values)
		putMu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
