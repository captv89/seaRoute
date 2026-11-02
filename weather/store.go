package weather

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	runFilePrefix = "ecmwf-"
	runFileSuffix = ".bin"
	tmpSuffix     = ".tmp"
)

// Status describes the store for the API: whether weather is available, which
// run is loaded, and how the last refresh went.
type Status struct {
	Enabled     bool       `json:"enabled"`
	Run         string     `json:"run,omitempty"`
	RunTime     *time.Time `json:"run_time,omitempty"`
	End         *time.Time `json:"end,omitempty"`
	LastAttempt *time.Time `json:"last_attempt,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
}

// Store keeps the newest forecast run open and replaces it atomically when a
// newer one has been downloaded. It is safe for concurrent use.
//
// The run before the current one stays open, because a request that fetched
// it just before a swap may still be reading from it.
type Store struct {
	dir       string
	refreshMu sync.Mutex // one Refresh at a time

	mu          sync.Mutex
	current     *Run
	previous    *Run
	lastAttempt time.Time
	lastError   string
}

func runFileName(id string) string { return runFilePrefix + id + runFileSuffix }

// runFileID extracts the run id and time from a file name such as
// "ecmwf-2026110200.bin". ok is false for any other name.
func runFileID(name string) (id string, t time.Time, ok bool) {
	if !strings.HasPrefix(name, runFilePrefix) || !strings.HasSuffix(name, runFileSuffix) {
		return "", time.Time{}, false
	}
	id = strings.TrimSuffix(strings.TrimPrefix(name, runFilePrefix), runFileSuffix)
	t, err := time.Parse("2006010215", id)
	if err != nil || RunID(t) != id {
		return "", time.Time{}, false
	}
	return id, t, true
}

// NewStore prepares dir (creating it if needed), removes leftover partial
// downloads and opens the newest valid run file. Invalid run files are logged
// and deleted. Unrelated files are left alone.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir}
	tmps, err := filepath.Glob(filepath.Join(dir, runFilePrefix+"*"+runFileSuffix+tmpSuffix))
	if err != nil {
		return nil, err
	}
	for _, p := range tmps {
		os.Remove(p)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type cand struct {
		name, id string
		t        time.Time
	}
	var cands []cand
	for _, e := range entries {
		if id, t, ok := runFileID(e.Name()); ok && e.Type().IsRegular() {
			cands = append(cands, cand{e.Name(), id, t})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].t.After(cands[j].t) })
	for _, c := range cands {
		if s.current != nil {
			break
		}
		path := filepath.Join(dir, c.name)
		r, err := OpenRun(path)
		if err == nil && r.ID != c.id {
			r.Close()
			err = fmt.Errorf("header run %s does not match file name", r.ID)
		}
		if err != nil {
			log.Printf("weather: dropping invalid run file %s: %v", c.name, err)
			os.Remove(path)
			continue
		}
		s.current = r
	}
	s.cleanup()
	return s, nil
}

// Current returns the newest run, or nil when none is loaded. The run stays
// valid for reading until two newer runs have been installed.
func (s *Store) Current() *Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

// Status reports the loaded run and the outcome of the last refresh.
func (s *Store) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{Enabled: true, LastError: s.lastError}
	if s.current != nil {
		ref, end := s.current.Ref, s.current.End()
		st.Run, st.RunTime, st.End = s.current.ID, &ref, &end
	}
	if !s.lastAttempt.IsZero() {
		at := s.lastAttempt
		st.LastAttempt = &at
	}
	return st
}

// Install opens the finished run file at path, makes it current, closes the
// run from the swap before last and deletes the files of runs that are
// neither current nor previous.
func (s *Store) Install(path string) error {
	r, err := OpenRun(path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.current != nil && s.current.ID == r.ID {
		s.mu.Unlock()
		r.Close()
		return nil
	}
	old := s.previous
	s.previous, s.current = s.current, r
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	s.cleanup()
	return nil
}

// cleanup deletes run files that are neither current nor previous.
func (s *Store) cleanup() {
	s.mu.Lock()
	keep := map[string]bool{}
	for _, r := range []*Run{s.current, s.previous} {
		if r != nil {
			keep[runFileName(r.ID)] = true
		}
	}
	s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if _, _, ok := runFileID(e.Name()); ok && !keep[e.Name()] {
			os.Remove(filepath.Join(s.dir, e.Name()))
		}
	}
}

// Refresh downloads the latest forecast run if it is newer than the current
// one and installs it. The outcome is recorded in Status. On failure the
// current run stays in place and no partial file is left behind.
func (s *Store) Refresh(ctx context.Context, f *Fetcher) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	err := s.refresh(ctx, f)
	s.mu.Lock()
	s.lastAttempt = time.Now().UTC()
	s.lastError = ""
	if err != nil {
		s.lastError = err.Error()
	}
	s.mu.Unlock()
	return err
}

func (s *Store) refresh(ctx context.Context, f *Fetcher) error {
	latest, err := f.Latest(ctx)
	if err != nil {
		return err
	}
	if cur := s.Current(); cur != nil && !latest.After(cur.Ref) {
		return nil
	}
	id := RunID(latest)
	final := filepath.Join(s.dir, runFileName(id))
	tmp := final + tmpSuffix
	if err := f.Fetch(ctx, latest, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := s.Install(final); err != nil {
		os.Remove(final)
		return err
	}
	return nil
}

// Loop refreshes immediately and then every interval until ctx is done.
// Errors are logged and retried at the next tick.
func (s *Store) Loop(ctx context.Context, f *Fetcher, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := s.Refresh(ctx, f); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("weather: refresh: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
