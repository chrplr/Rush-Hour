// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the MIT License.

package rushlog

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// Writer produces a results file for an agent run, in the format
// goxpyriment's DataFile produces for a participant: a subject_id column in
// front, comma separated, strings quoted and numbers bare.
//
// The formatting is duplicated rather than shared because the two writers have
// nothing else in common — the experiment's goes through SDL-bearing library
// code that a headless agent must not link. What is shared, and what matters,
// is the column list in row.go.
type Writer struct {
	file      *os.File
	buf       *bufio.Writer
	subjectID int
}

// NewWriter creates the file and writes its header. Passing an empty path
// returns nil, so callers can treat "no results file wanted" as a Writer that
// is simply never written to.
func NewWriter(path string, subjectID int, comments []string) (*Writer, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := &Writer{file: f, buf: bufio.NewWriter(f), subjectID: subjectID}

	for _, c := range comments {
		fmt.Fprintf(w.buf, "# %s\n", c)
	}
	fmt.Fprintf(w.buf, "subject_id,%s\n", strings.Join(Columns, ","))
	return w, w.buf.Flush()
}

// Add appends one row. A nil Writer discards it, which is what makes the
// results file optional at the call site.
func (w *Writer) Add(r Row) error {
	if w == nil {
		return nil
	}
	parts := make([]string, 0, len(Columns)+1)
	parts = append(parts, fmt.Sprint(w.subjectID))
	for _, v := range r.Values() {
		parts = append(parts, field(v))
	}
	_, err := fmt.Fprintln(w.buf, strings.Join(parts, ","))
	return err
}

// field formats one value: numbers and booleans bare, everything else quoted
// with internal quotes doubled, as in RFC 4180.
func field(v any) string {
	s := fmt.Sprint(v)
	switch v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64, bool:
		return s
	default:
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
}

// Flush pushes buffered rows to disk. Worth calling at the end of every
// episode: a training run that is killed should not lose its trace.
func (w *Writer) Flush() error {
	if w == nil {
		return nil
	}
	return w.buf.Flush()
}

// Close flushes and closes the file.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	if err := w.buf.Flush(); err != nil {
		w.file.Close()
		return err
	}
	return w.file.Close()
}

var _ io.Closer = (*Writer)(nil)
