package weather

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"time"
)

const (
	runMagic       = "SRWX1\n"
	maxHeaderBytes = 64 << 10
	maxGridDim     = 4000
	maxRunSteps    = 200
	runSource      = "ECMWF IFS open data"
	quantizedLimit = 32767
)

var noValueBits = uint16(0x8000) // NoValue as stored

// runHeader is the JSON line that follows the magic. Scale and Offset are
// listed per field, in the same order as Fields.
type runHeader struct {
	Source string    `json:"source"`
	Run    string    `json:"run"`
	Grid   Grid      `json:"grid"`
	Fields []string  `json:"fields"`
	Steps  []int     `json:"steps"`
	Scale  []float64 `json:"scale"`
	Offset []float64 `json:"offset"`
	Credit string    `json:"credit"`
}

// slotValues is the number of int16 values in one (field, step) slot.
func slotValues(g Grid) int64 { return int64(g.NX) * int64(g.NY) }

// slotOffset is the file offset of the first value of (field, step).
func slotOffset(dataStart int64, g Grid, nSteps int, f FieldID, step int) int64 {
	return dataStart + (int64(f)*int64(nSteps)+int64(step))*slotValues(g)*2
}

func fieldParams() (params []string, scale, offset []float64) {
	for _, s := range fieldSpecs {
		params = append(params, s.Param)
		scale = append(scale, s.Scale)
		offset = append(offset, s.Offset)
	}
	return
}

// RunWriter fills a new run file. Fields may be written in any order; slots
// never written read back as NoValue after Close.
type RunWriter struct {
	f         *os.File
	path      string
	g         Grid
	nSteps    int
	dataStart int64
	written   []bool
	buf       []byte
}

// CreateRun creates (or replaces) the run file at path, sized for every field
// and step. The caller must finish with Close or Abort.
func CreateRun(path string, ref time.Time, g Grid, steps []int) (*RunWriter, error) {
	if g.NX <= 0 || g.NY <= 0 || g.NX > maxGridDim || g.NY > maxGridDim {
		return nil, fmt.Errorf("weather: bad grid size %dx%d", g.NX, g.NY)
	}
	if len(steps) == 0 || len(steps) > maxRunSteps {
		return nil, fmt.Errorf("weather: %d steps, want 1..%d", len(steps), maxRunSteps)
	}
	params, scale, offset := fieldParams()
	hdr, err := json.Marshal(runHeader{
		Source: runSource, Run: ref.UTC().Format(time.RFC3339), Grid: g,
		Fields: params, Steps: steps, Scale: scale, Offset: offset, Credit: Credit,
	})
	if err != nil {
		return nil, err
	}
	hdr = append(hdr, '\n')
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := &RunWriter{
		f: f, path: path, g: g, nSteps: len(steps),
		dataStart: int64(len(runMagic) + len(hdr)),
		written:   make([]bool, int(NumFields)*len(steps)),
		buf:       make([]byte, 2*slotValues(g)),
	}
	size := w.dataStart + int64(len(w.written))*slotValues(g)*2
	if _, err := f.WriteAt(append([]byte(runMagic), hdr...), 0); err != nil {
		w.Abort()
		return nil, err
	}
	if err := f.Truncate(size); err != nil {
		w.Abort()
		return nil, err
	}
	return w, nil
}

// Put quantizes one field at one step (len(vals) must be NX*NY, natural
// order) and writes it. NaN becomes NoValue; out-of-range values are clamped.
func (w *RunWriter) Put(f FieldID, stepIdx int, vals []float32) error {
	if f < 0 || f >= NumFields || stepIdx < 0 || stepIdx >= w.nSteps {
		return fmt.Errorf("weather: Put field %d step %d out of range", f, stepIdx)
	}
	if int64(len(vals)) != slotValues(w.g) {
		return fmt.Errorf("weather: Put got %d values, want %d", len(vals), slotValues(w.g))
	}
	sp := fieldSpecs[f]
	for i, v := range vals {
		q := NoValue
		if !math.IsNaN(float64(v)) {
			r := math.Round((float64(v) - sp.Offset) / sp.Scale)
			q = int16(math.Max(-quantizedLimit, math.Min(quantizedLimit, r)))
		}
		binary.LittleEndian.PutUint16(w.buf[2*i:], uint16(q))
	}
	if _, err := w.f.WriteAt(w.buf, slotOffset(w.dataStart, w.g, w.nSteps, f, stepIdx)); err != nil {
		return err
	}
	w.written[int(f)*w.nSteps+stepIdx] = true
	return nil
}

// Close fills every slot that was never written with NoValue, syncs and
// closes the file.
func (w *RunWriter) Close() error {
	for i := 0; i < len(w.buf); i += 2 {
		binary.LittleEndian.PutUint16(w.buf[i:], noValueBits)
	}
	for i, done := range w.written {
		if done {
			continue
		}
		off := slotOffset(w.dataStart, w.g, w.nSteps, FieldID(i/w.nSteps), i%w.nSteps)
		if _, err := w.f.WriteAt(w.buf, off); err != nil {
			w.f.Close()
			return err
		}
	}
	if err := w.f.Sync(); err != nil {
		w.f.Close()
		return err
	}
	return w.f.Close()
}

// Abort closes and deletes the partly written file.
func (w *RunWriter) Abort() {
	w.f.Close()
	os.Remove(w.path)
}

// Run is a read-only view of one run file. Rows are read on demand with
// ReadAt, so a run never has to fit in memory.
type Run struct {
	ID        string
	Ref       time.Time
	Grid      Grid
	Steps     []int
	f         *os.File
	dataStart int64
	scale     [NumFields]float64
	offset    [NumFields]float64
}

// OpenRun opens a run file. The file is untrusted: the header is bounded and
// checked, and the size must match the header exactly.
func OpenRun(path string) (*Run, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r, err := parseRun(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("weather: %s: %w", path, err)
	}
	return r, nil
}

func parseRun(f *os.File) (*Run, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	br := bufio.NewReaderSize(io.NewSectionReader(f, 0, st.Size()), 4096)
	magic := make([]byte, len(runMagic))
	if _, err := io.ReadFull(br, magic); err != nil || string(magic) != runMagic {
		return nil, errors.New("bad magic")
	}
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > maxHeaderBytes {
			return nil, errors.New("header too long")
		}
		if err == nil {
			break
		}
		if err != bufio.ErrBufferFull {
			return nil, errors.New("unterminated header")
		}
	}
	var h runHeader
	dec := json.NewDecoder(bytes.NewReader(line))
	if err := dec.Decode(&h); err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	g := h.Grid
	if g.NX <= 0 || g.NY <= 0 || g.NX > maxGridDim || g.NY > maxGridDim {
		return nil, fmt.Errorf("bad grid size %dx%d", g.NX, g.NY)
	}
	if len(h.Steps) == 0 || len(h.Steps) > maxRunSteps {
		return nil, fmt.Errorf("%d steps", len(h.Steps))
	}
	params, _, _ := fieldParams()
	if len(h.Fields) != len(params) || len(h.Scale) != len(params) || len(h.Offset) != len(params) {
		return nil, errors.New("fields do not match")
	}
	r := &Run{Grid: g, Steps: h.Steps, f: f, dataStart: int64(len(runMagic) + len(line))}
	for i, p := range params {
		if h.Fields[i] != p {
			return nil, errors.New("fields do not match")
		}
		r.scale[i], r.offset[i] = h.Scale[i], h.Offset[i]
		if r.scale[i] <= 0 || math.IsNaN(r.scale[i]) || math.IsInf(r.scale[i], 0) || math.IsNaN(r.offset[i]) || math.IsInf(r.offset[i], 0) {
			return nil, errors.New("bad scale or offset")
		}
	}
	ref, err := time.Parse(time.RFC3339, h.Run)
	if err != nil {
		return nil, fmt.Errorf("run time: %w", err)
	}
	r.Ref = ref.UTC()
	r.ID = RunID(r.Ref)
	want := r.dataStart + int64(NumFields)*int64(len(h.Steps))*slotValues(g)*2
	if st.Size() != want {
		return nil, fmt.Errorf("size %d, want %d", st.Size(), want)
	}
	return r, nil
}

// Close releases the file.
func (r *Run) Close() error { return r.f.Close() }

// ValidTime is the time forecast step stepIdx applies to.
func (r *Run) ValidTime(stepIdx int) time.Time {
	return r.Ref.Add(time.Duration(r.Steps[stepIdx]) * time.Hour)
}

// End is the valid time of the last step.
func (r *Run) End() time.Time { return r.ValidTime(len(r.Steps) - 1) }

// readRow reads one grid row (NX quantized values) of a field at a step into
// dst, which must hold at least NX values.
func (r *Run) readRow(f FieldID, stepIdx, row int, dst []int16) error {
	g := r.Grid
	if f < 0 || f >= NumFields || stepIdx < 0 || stepIdx >= len(r.Steps) ||
		row < 0 || row >= g.NY || len(dst) < g.NX {
		return fmt.Errorf("weather: readRow field %d step %d row %d out of range", f, stepIdx, row)
	}
	buf := make([]byte, 2*g.NX)
	off := slotOffset(r.dataStart, g, len(r.Steps), f, stepIdx) + int64(row)*int64(g.NX)*2
	if _, err := r.f.ReadAt(buf, off); err != nil {
		return err
	}
	for i := 0; i < g.NX; i++ {
		dst[i] = int16(binary.LittleEndian.Uint16(buf[2*i:]))
	}
	return nil
}

// decode turns a stored value back into physical units; NoValue gives NaN.
func (r *Run) decode(f FieldID, q int16) float32 {
	if q == NoValue {
		return float32(math.NaN())
	}
	return float32(float64(q)*r.scale[f] + r.offset[f])
}
