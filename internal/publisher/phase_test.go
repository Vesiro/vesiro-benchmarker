package publisher

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

// runPhases drives a report through one phase per entry, publishing that
// entry's samples, and returns the lines it printed.
func runPhases(t *testing.T, report *PhaseReport, phases map[string][]sample.Sample, order ...string) []string {
	t.Helper()

	var out bytes.Buffer
	report.Out = &out
	require.NoError(t, report.Start())
	for i, name := range order {
		phase := Phase{Number: i + 1, Count: len(order), Name: name}
		require.NoError(t, report.StartPhase(phase))
		for _, s := range phases[name] {
			require.NoError(t, report.Publish(s))
		}
		require.NoError(t, report.FinishPhase(phase))
	}
	require.NoError(t, report.Finish())

	return strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
}

func TestPhaseReportPrintsOneRowPerPhase(t *testing.T) {
	t.Parallel()

	lines := runPhases(t,
		&PhaseReport{Progress: ProgressConfig{Mode: "none"}, NameWidth: 17},
		map[string][]sample.Sample{
			"boolean/term.json": {
				newSample("boolean/term.json", 8, 20*time.Millisecond, 1),
				newErrorSample("boolean/term.json", http.StatusBadRequest, 40*time.Millisecond),
			},
			"terms/ids.json": {newSample("terms/ids.json", 8, 10*time.Millisecond, 1)},
		},
		"boolean/term.json", "terms/ids.json",
	)

	require.Len(t, lines, 2, "without a progress display, each phase is just its row")
	require.True(t, strings.HasPrefix(lines[0], "[1/2] boolean/term.json       2 req      1 err  "), lines[0])
	require.True(t, strings.HasSuffix(lines[0], " q/s    avg    30.00  p50    20.00  p95    40.00  p99    40.00 ms"), lines[0])
	require.True(t, strings.HasPrefix(lines[1], "[2/2] terms/ids.json          1 req      0 err  "),
		"each phase counts only its own samples: %s", lines[1])
}

func TestPhaseReportRowsLineUp(t *testing.T) {
	t.Parallel()

	order := []string{"a.json", "a-much-longer-name.json"}
	for i := 3; i <= 10; i++ {
		order = append(order, "a.json")
	}
	lines := runPhases(t,
		&PhaseReport{Progress: ProgressConfig{Mode: "none"}, NameWidth: len("a-much-longer-name.json")},
		map[string][]sample.Sample{
			"a.json":                  {newSample("a.json", 8, 5*time.Millisecond, 1)},
			"a-much-longer-name.json": {newSample("a-much-longer-name.json", 8, 4232100*time.Microsecond, 1)},
		},
		order...,
	)

	require.Len(t, lines, 10)
	require.True(t, strings.HasPrefix(lines[0], "[ 1/10] "), "counters pad to the widest: %s", lines[0])
	for _, column := range []string{" req", " err", " q/s", "avg ", "p50 ", "p95 ", "p99 ", " ms"} {
		want := strings.Index(lines[0], column)
		for _, line := range lines[1:] {
			require.Equal(t, want, strings.Index(line, column), "%q must line up:\n%s\n%s", column, lines[0], line)
		}
	}
}

func TestPhaseReportNamesThePhaseBeforeALog(t *testing.T) {
	t.Parallel()

	perClient := 1
	lines := runPhases(t,
		&PhaseReport{Progress: ProgressConfig{Mode: "log", TotalRequests: &perClient}, NameWidth: 6},
		map[string][]sample.Sample{"a.json": {newSample("a.json", 8, 5*time.Millisecond, 1)}},
		"a.json",
	)

	require.Len(t, lines, 2)
	require.Equal(t, "[1/1] Running a.json", lines[0])
	require.True(t, strings.HasPrefix(lines[1], "[1/1] a.json       1 req"), lines[1])
}

func TestPhaseReportMarksAPhaseCutShortAsStopped(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	report := &PhaseReport{Out: &out, Progress: ProgressConfig{Mode: "none"}}
	require.NoError(t, report.Start())

	require.NoError(t, report.StartPhase(Phase{Number: 1, Count: 3, Name: "a.json"}))
	require.NoError(t, report.Publish(newSample("a.json", 8, 20*time.Millisecond, 1)))
	require.NoError(t, report.Finish())

	require.True(t, strings.HasPrefix(out.String(), "[1/3] a.json       1 req"), out.String())
	require.True(t, strings.HasSuffix(out.String(), " ms  (stopped)\n"), out.String())
}

func TestPhaseReportFinishWithoutAnOpenPhasePrintsNothingMore(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	report := &PhaseReport{Out: &out, Progress: ProgressConfig{Mode: "none"}}
	require.NoError(t, report.Start())
	require.NoError(t, report.StartPhase(Phase{Number: 1, Count: 1, Name: "a.json"}))
	require.NoError(t, report.FinishPhase(Phase{Number: 1, Count: 1, Name: "a.json"}))
	printed := out.String()

	require.NoError(t, report.Finish())

	require.Equal(t, printed, out.String())
}

func TestPhaseReportRejectsAnInvalidProgressModeAtStart(t *testing.T) {
	t.Parallel()

	report := &PhaseReport{Out: &bytes.Buffer{}, Progress: ProgressConfig{Mode: "flashing-lights"}}

	require.ErrorContains(t, report.Start(), "invalid progress mode")
}

func TestNewProgressPassesThePrefixToTheBar(t *testing.T) {
	t.Parallel()

	total := 10
	seconds := 5 * time.Second

	counted, err := NewProgress(ProgressConfig{Mode: "bar", TotalRequests: &total, Prefix: "[1/2] a.json  "})
	require.NoError(t, err)
	require.Equal(t, "[1/2] a.json  ", counted.(*SampleProgressBar).Prefix)

	timed, err := NewProgress(ProgressConfig{Mode: "bar", BenchmarkTime: &seconds, Prefix: "[1/2] a.json  "})
	require.NoError(t, err)
	require.Equal(t, "[1/2] a.json  ", timed.(*TimedProgressBar).Prefix)
}
