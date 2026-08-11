// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Distributed under the MIT License.

package rushenv

import (
	"time"

	"rush-hour/internal/rushlog"
)

// Recorder writes an agent's play as a results file with the same columns a
// participant's session produces.
//
// That is what makes the two comparable: an agent's episode is a trial, its
// action is a click, and the rows say so in the same words. What it cannot
// fake is a hesitation — an agent produces no rows between decisions — so
// timing columns from an agent run mean "when the request arrived", nothing
// more.
type Recorder struct {
	writer *rushlog.Writer

	// mousePoint, when set, gives the screen position a participant would have
	// had to click to produce this action. Only the windowed build can know it.
	mousePoint func(st State) (x, y float32)

	trials map[int]int
	onset  map[int]time.Time
	// ended marks trials whose trial_end row is already written. A solved board
	// stays solved, and a client is free to keep stepping it, so without this
	// every later step would append another end row.
	ended map[int]bool
}

// SetMousePoint installs the click-position function. It is a method rather
// than an exported field so that it is a no-op on the nil Recorder that stands
// for "no results file wanted", as every other method here is.
func (r *Recorder) SetMousePoint(f func(st State) (x, y float32)) {
	if r == nil {
		return
	}
	r.mousePoint = f
}

// NewRecorder returns nil when path is empty, so "no results file" needs no
// special case at the call site.
func NewRecorder(path string, subjectID int, comments []string) (*Recorder, error) {
	w, err := rushlog.NewWriter(path, subjectID, comments)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, nil
	}
	return &Recorder{
		writer: w,
		trials: map[int]int{},
		onset:  map[int]time.Time{},
		ended:  map[int]bool{},
	}, nil
}

// Observe is the Server.Observer callback.
func (r *Recorder) Observe(envID int, kind string, st State) {
	if r == nil {
		return
	}
	if kind == kindReset {
		r.trials[envID]++
		r.onset[envID] = time.Now()
		r.ended[envID] = false
		_ = r.writer.Add(rushlog.NewRow(
			r.trials[envID], st.Puzzle, st.MinMoves, rushlog.EventTrialStart, 0))
		return
	}

	elapsed := time.Since(r.onset[envID]).Milliseconds()
	row := rushlog.NewRow(r.trials[envID], st.Puzzle, st.MinMoves, rushlog.EventClickEmpty, elapsed)

	if st.Slot >= 0 {
		// A real vehicle was addressed: the row says which, and whether it went
		// anywhere. A padding slot addresses no vehicle at all, which is the
		// closest an agent comes to clicking empty space.
		row.Kind = rushlog.EventClickBlocked
		if st.Moved {
			row.Kind = rushlog.EventClickMove
		}
		row.Car = st.Label
		row.Orient = "V"
		if st.Cars[st.Slot][3] == 1 {
			row.Orient = "H"
		}
		row.FromR, row.FromC = st.From[0], st.From[1]
		row.ToR, row.ToC = st.To[0], st.To[1]
	}
	if r.mousePoint != nil {
		row.MouseX, row.MouseY = r.mousePoint(st)
	}
	_ = r.writer.Add(row)

	if st.Solved && !r.ended[envID] {
		end := rushlog.NewRow(r.trials[envID], st.Puzzle, st.MinMoves, rushlog.EventTrialEnd, elapsed)
		end.NMoves = st.NSlides
		end.Solved = true
		end.TrialMS = elapsed
		_ = r.writer.Add(end)
		r.ended[envID] = true
		// Flushed per episode: a training run that is killed keeps its trace.
		_ = r.writer.Flush()
	}
}

// Close finishes the file.
func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}
	return r.writer.Close()
}
