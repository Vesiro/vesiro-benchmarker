package publisher

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

// Phase is one stretch of a run, such as one template of a sequential folder
// run.
type Phase struct {
	// Number counts from 1 up to Count.
	Number int
	Count  int
	Name   string
}

// PhaseObserver is implemented by publishers that want to know where each
// phase of a run starts and ends. Every sample published between StartPhase
// and FinishPhase belongs to that phase.
type PhaseObserver interface {
	StartPhase(Phase) error
	FinishPhase(Phase) error
}

// PhaseReport shows a fresh progress display for each phase and prints a
// one-row result when it finishes. The rows line up, so a long run reads as a
// table.
type PhaseReport struct {
	// Out is where the rows are written. Nil means os.Stderr, which keeps
	// stdout to just the final report.
	Out io.Writer

	// Progress configures the display shown during each phase, with the limits
	// of a single phase. A bar is labelled with the phase, so it sits where the
	// phase's row will go; a log gets a line naming the phase before it starts.
	Progress ProgressConfig

	// NameWidth pads phase names to a common width, usually the longest one,
	// so the numbers after them line up.
	NameWidth int

	// progress, results and label are set only while a phase is running.
	progress Publisher
	results  *Summary
	label    string
}

func (p *PhaseReport) out() io.Writer {
	if p.Out != nil {
		return p.Out
	}
	return os.Stderr
}

// Start checks the progress config, so a bad mode fails before any traffic is
// sent rather than at the first phase.
func (p *PhaseReport) Start() error {
	_, err := NewProgress(p.Progress)
	return err
}

// Finish closes a phase that is still running, which happens when the run is
// interrupted or fails partway through it.
func (p *PhaseReport) Finish() error {
	if p.results == nil {
		return nil
	}
	return p.closePhase(true)
}

func (p *PhaseReport) Publish(s sample.Sample) error {
	if p.results == nil {
		return nil
	}
	if err := p.results.Publish(s); err != nil {
		return err
	}
	if p.progress != nil {
		return p.progress.Publish(s)
	}
	return nil
}

func (p *PhaseReport) StartPhase(phase Phase) error {
	counter := fmt.Sprintf("[%*d/%d]", len(strconv.Itoa(phase.Count)), phase.Number, phase.Count)
	p.label = fmt.Sprintf("%s %-*s", counter, p.NameWidth, phase.Name)

	config := p.Progress
	switch config.Mode {
	case "bar":
		config.Prefix = p.label + "  "
	case "log":
		_, _ = fmt.Fprintf(p.out(), "%s Running %s\n", counter, phase.Name)
	}

	// The final report already echoes failing responses, so this one stays
	// quiet rather than print each of them twice.
	p.results = &Summary{Diagnostics: io.Discard}
	if err := p.results.Start(); err != nil {
		return err
	}

	progress, err := NewProgress(config)
	if err != nil {
		return err
	}
	if progress != nil {
		if err := progress.Start(); err != nil {
			return err
		}
	}
	p.progress = progress
	return nil
}

func (p *PhaseReport) FinishPhase(Phase) error {
	return p.closePhase(false)
}

// closePhase prints the phase's row in place of its progress display. The
// field widths fit the usual range of values, so rows only drift out of line
// for extremes such as a p99 over 100 seconds.
func (p *PhaseReport) closePhase(stopped bool) error {
	var err error
	if p.progress != nil {
		err = p.progress.Finish()
	}

	p.results.T1 = time.Now().UTC()
	r := p.results.Calculate()
	l := r.ClientLatencyMs
	row := fmt.Sprintf("%s  %6d req  %5d err  %8.2f q/s    avg %8.2f  p50 %8.2f  p95 %8.2f  p99 %8.2f ms",
		p.label, r.Requests, r.Errors, r.QPS, l.Avg, l.P50, l.P95, l.P99)
	if stopped {
		row += "  (stopped)"
	}
	_, _ = fmt.Fprintln(p.out(), row)

	p.progress, p.results, p.label = nil, nil, ""
	return err
}
