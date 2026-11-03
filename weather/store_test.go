package weather

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// writeRunFile writes a small valid run file for ref into dir under name.
func writeRunFile(t *testing.T, dir, name string, ref time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	w, err := CreateRun(path, ref, testGrid, []int{0, 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func runAt(day int) time.Time { return time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC) }

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestNewStoreOpensNewest(t *testing.T) {
	dir := t.TempDir()
	writeRunFile(t, dir, runFileName(RunID(runAt(1))), runAt(1))
	writeRunFile(t, dir, runFileName(RunID(runAt(2))), runAt(2))
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	cur := s.Current()
	if cur == nil || cur.ID != RunID(runAt(2)) {
		t.Fatalf("current %v, want %s", cur, RunID(runAt(2)))
	}
	if exists(filepath.Join(dir, runFileName(RunID(runAt(1))))) {
		t.Error("older run file should be deleted")
	}
	st := s.Status()
	if !st.Enabled || st.Run != cur.ID || st.RunTime == nil || !st.RunTime.Equal(runAt(2)) {
		t.Errorf("status %+v", st)
	}
}

func TestNewStoreCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	s, err := NewStore(dir)
	if err != nil || s.Current() != nil {
		t.Fatalf("err %v, current %v", err, s.Current())
	}
}

func TestNewStoreDropsTmpAndBad(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, runFileName(RunID(runAt(3)))+".tmp")
	bad := filepath.Join(dir, runFileName(RunID(runAt(2))))
	mismatch := writeRunFile(t, dir, runFileName(RunID(runAt(4))), runAt(1)) // header says day 1
	other := filepath.Join(dir, "notes.txt")
	for _, p := range []string{tmp, bad, other} {
		if err := os.WriteFile(p, []byte("junk"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Current() != nil {
		t.Errorf("current %v, want nil", s.Current())
	}
	for _, p := range []string{tmp, bad, mismatch} {
		if exists(p) {
			t.Errorf("%s should be removed", p)
		}
	}
	if !exists(other) {
		t.Error("unrelated file must stay")
	}
}

func TestInstallKeepsPreviousOpen(t *testing.T) {
	dir := t.TempDir()
	p1 := writeRunFile(t, dir, runFileName(RunID(runAt(1))), runAt(1))
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	r1 := s.Current()
	p2 := writeRunFile(t, dir, runFileName(RunID(runAt(2))), runAt(2))
	if err := s.Install(p2); err != nil {
		t.Fatal(err)
	}
	if s.Current().ID != RunID(runAt(2)) {
		t.Fatal("run 2 should be current")
	}
	r1.At(0, 0, runAt(1)) // still readable
	if _, err := r1.f.Stat(); err != nil {
		t.Errorf("previous run closed too early: %v", err)
	}
	if !exists(p1) {
		t.Error("previous run file must stay")
	}
	p3 := writeRunFile(t, dir, runFileName(RunID(runAt(3))), runAt(3))
	if err := s.Install(p3); err != nil {
		t.Fatal(err)
	}
	if _, err := r1.f.Stat(); err == nil {
		t.Error("run 1 should be closed")
	}
	if exists(p1) {
		t.Error("run 1 file should be deleted")
	}
	if !exists(p2) || !exists(p3) {
		t.Error("runs 2 and 3 files must stay")
	}
}

func TestRefreshFailureKeepsCurrent(t *testing.T) {
	dir := t.TempDir()
	writeRunFile(t, dir, runFileName(RunID(runAt(1))), runAt(1))
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	cur := s.Current()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if err := s.Refresh(context.Background(), testFetcher(srv.URL, []int{24})); err == nil {
		t.Fatal("want error")
	}
	if s.Current() != cur {
		t.Error("current changed")
	}
	st := s.Status()
	if st.LastError == "" || st.LastAttempt == nil {
		t.Errorf("status %+v", st)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(left) != 0 {
		t.Errorf("leftover tmp files %v", left)
	}
}

func TestRefreshSkipsWhenUpToDate(t *testing.T) {
	dir := t.TempDir()
	writeRunFile(t, dir, runFileName(RunID(testRun)), testRun)
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	var data atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".grib2") {
			data.Add(1)
		}
		_, _ = w.Write([]byte("{}\n"))
	}))
	defer srv.Close()
	f := testFetcher(srv.URL, []int{24})
	f.Now = func() time.Time { return testRun.Add(time.Hour) }
	if err := s.Refresh(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if data.Load() != 0 {
		t.Errorf("%d data requests, want 0", data.Load())
	}
	if st := s.Status(); st.LastError != "" || st.LastAttempt == nil {
		t.Errorf("status %+v", st)
	}
}

func TestRefreshInstallsNewRun(t *testing.T) {
	srv := fixtureServer(t, 0, "10u")
	defer srv.Close()
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := testFetcher(srv.URL, []int{24})
	f.Now = func() time.Time { return testRun.Add(time.Hour) }
	if err := s.Refresh(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	cur := s.Current()
	if cur == nil || cur.ID != RunID(testRun) {
		t.Fatalf("current %v", cur)
	}
	if !exists(filepath.Join(dir, runFileName(cur.ID))) || exists(filepath.Join(dir, runFileName(cur.ID)+".tmp")) {
		t.Error("run file not in place")
	}
}
