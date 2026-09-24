package compare

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
)

func perQuery(name string, errors int, qps, avg, p50, p95, p99 float64) publisher.PerQueryResult {
	return publisher.PerQueryResult{
		Name:            name,
		Requests:        160,
		Errors:          errors,
		QPS:             qps,
		ClientLatencyMs: publisher.Latency{Avg: avg, P50: p50, P95: p95, P99: p99},
	}
}

// baselineRun and contenderRun differ in their index, seed and results, and
// each has one query the other lacks.
func baselineRun() Run {
	return Run{Path: "results/before.json", Report: publisher.Report{
		Version:   "v1.0.0",
		Command:   "folder",
		Label:     "before",
		StartedAt: time.Date(2026, 9, 24, 11, 30, 12, 0, time.Local),
		Config:    map[string]any{"IndexName": "cc-wet-small", "NumClients": float64(16), "Seed": float64(1), "Random": false},
		Results: publisher.Results{
			Requests: 480, Ok: 480, QPS: 812.40, TookAvgMs: 12,
			ClientLatencyMs: publisher.Latency{Avg: 19.63, P50: 18.02, P95: 31.44, P99: 40.10},
			PerQuery: []publisher.PerQueryResult{
				perQuery("boolean/conjunction.json", 0, 145.58, 26.36, 24.53, 52.18, 70.38),
				perQuery("boolean/exclusion.json", 0, 8.19, 472.79, 203.40, 1599.84, 4232.28),
				perQuery("terms/ids.json", 0, 352.63, 11.00, 9.37, 22.75, 37.63),
			},
		},
	}}
}

func contenderRun() Run {
	return Run{Path: "results/after.json", Report: publisher.Report{
		Version:   "v1.0.0",
		Command:   "folder",
		Label:     "after",
		StartedAt: time.Date(2026, 9, 24, 14, 15, 2, 0, time.Local),
		Config:    map[string]any{"IndexName": "cc-wet-large", "NumClients": float64(16), "Seed": float64(2), "Random": false},
		Results: publisher.Results{
			Requests: 480, Ok: 477, Errors: 3, ErrorRate: 3.0 / 480, QPS: 905.10, TookAvgMs: 11,
			ClientLatencyMs: publisher.Latency{Avg: 17.60, P50: 16.10, P95: 30.00, P99: 52.30},
			PerQuery: []publisher.PerQueryResult{
				perQuery("boolean/conjunction.json", 0, 160.02, 23.10, 22.10, 53.00, 81.20),
				perQuery("boolean/exclusion.json", 3, 4.50, 860.00, 390.10, 2900.00, 7100.00),
				perQuery("terms/terms.json", 0, 300.00, 12.00, 10.00, 25.00, 40.00),
			},
		},
	}}
}

func printed(baseline, contender Run, opts Options) string {
	var out bytes.Buffer
	Print(&out, baseline, contender, opts)
	return out.String()
}

func TestPrintSetsTheRunsSideBySide(t *testing.T) {
	t.Parallel()

	got := printed(baselineRun(), contenderRun(), Options{Threshold: 5})

	require.Equal(t, `Baseline:  results/before.json  (before, started 2026-09-24 11:30:12)
Contender: results/after.json  (after, started 2026-09-24 14:15:02)

Config differences:
  IndexName  "cc-wet-small" → "cc-wet-large"

                Baseline  Contender  Change
  Requests           480        480
  Errors       0 (0.00%)  3 (0.62%)
  q/s             812.40     905.10  +11.4%
  Took Avg ms      12.00      11.00   -8.3%
  Avg ms           19.63      17.60  -10.3%
  p50 ms           18.02      16.10  -10.7%
  p95 ms           31.44      30.00   -4.6%
  p99 ms           40.10      52.30  +30.4%

Per Query (baseline → contender, latency in ms):
  Query                     Errors  q/s                      Avg                      p50                      p95                        p99
  boolean/conjunction.json       0  145.58 → 160.02   +9.9%   26.36 →  23.10  -12.4%   24.53 →  22.10   -9.9%    52.18 →   53.00   +1.6%    70.38 →   81.20  +15.4%
  boolean/exclusion.json     0 → 3    8.19 →   4.50  -45.1%  472.79 → 860.00  +81.9%  203.40 → 390.10  +91.8%  1599.84 → 2900.00  +81.3%  4232.28 → 7100.00  +67.8%
Only in baseline:  terms/ids.json
Only in contender: terms/terms.json
`, got)
}

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestPrintInColorKeepsTheSameLayout(t *testing.T) {
	t.Parallel()

	plain := printed(baselineRun(), contenderRun(), Options{Threshold: 5})
	colored := printed(baselineRun(), contenderRun(), Options{Threshold: 5, Color: true})

	stripped := ansi.ReplaceAllString(colored, "")
	require.True(t, strings.HasPrefix(stripped, plain), "color must not move anything:\n%s", stripped)
	require.Contains(t, stripped, "green where the contender is better and red where it is worse")
}

// cellColor finds the color the value was printed in, on the first line
// starting with prefix.
func cellColor(t *testing.T, output, prefix, value string) string {
	t.Helper()

	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		for _, c := range []string{colorRed, colorGreen} {
			if strings.Contains(line, c+value+colorReset) {
				return c
			}
		}
		require.Contains(t, line, value)
		return ""
	}
	t.Fatalf("no line starts with %q", prefix)
	return ""
}

func TestPrintColorsEachChangeByWhetherItIsBetter(t *testing.T) {
	t.Parallel()

	got := printed(baselineRun(), contenderRun(), Options{Threshold: 5, Color: true})

	require.Equal(t, colorGreen, cellColor(t, got, "  q/s", "+11.4%"), "more throughput is better")
	require.Equal(t, colorGreen, cellColor(t, got, "  p50 ms", "-10.7%"), "lower latency is better")
	require.Equal(t, colorRed, cellColor(t, got, "  p99 ms", "+30.4%"), "higher latency is worse")
	require.Equal(t, "", cellColor(t, got, "  p95 ms", "-4.6%"), "a change within the threshold is noise")
	require.Equal(t, colorRed, cellColor(t, got, "  Errors", "3 (0.62%)"))
	require.Equal(t, colorRed, cellColor(t, got, "  boolean/exclusion.json", "-45.1%"))
	require.Equal(t, colorRed, cellColor(t, got, "  boolean/exclusion.json", "0 → 3"))
}

func TestPrintShowsADashForARateThatWasNotMeasured(t *testing.T) {
	t.Parallel()

	baseline, contender := baselineRun(), contenderRun()
	// A mixed run gives its queries no rate of their own.
	contender.Report.Results.PerQuery[0].QPS = 0

	got := printed(baseline, contender, Options{Threshold: 5})

	require.Contains(t, got, "  boolean/conjunction.json       0  145.58 →    -       -   26.36 →  23.10  -12.4%")
}

func TestPrintLeavesOutTheRateColumnWhenNeitherRunHasOne(t *testing.T) {
	t.Parallel()

	baseline, contender := baselineRun(), contenderRun()
	for _, results := range []*publisher.Results{&baseline.Report.Results, &contender.Report.Results} {
		for i := range results.PerQuery {
			results.PerQuery[i].QPS = 0
		}
	}

	got := printed(baseline, contender, Options{Threshold: 5})

	require.Contains(t, got, "  Query                     Errors  Avg                      p50")
	require.Contains(t, got, "  boolean/conjunction.json       0   26.36 →  23.10  -12.4%")
}

func TestPrintWarnsAboutARunThatStoppedEarly(t *testing.T) {
	t.Parallel()

	baseline, contender := baselineRun(), contenderRun()
	baseline.Report.Error = "response body is not valid json"
	contender.Report.Interrupted = true

	got := printed(baseline, contender, Options{Threshold: 5})

	require.Contains(t, got, "Warning: the baseline stopped early with an error: response body is not valid json\n")
	require.Contains(t, got, "Warning: the contender was interrupted, so it covers only part of its run.\n")
}

func TestPrintSaysSoWhenTheConfigMatches(t *testing.T) {
	t.Parallel()

	baseline, contender := baselineRun(), baselineRun()
	contender.Report.Config = map[string]any{
		"IndexName": "cc-wet-small", "NumClients": float64(16), "Random": false,
		"Seed": float64(99), "Progress": "none", "Output": "json",
	}

	got := printed(baseline, contender, Options{Threshold: 5})

	require.Contains(t, got, "Config differences: none\n", "seed and display settings do not change the load")
}

func TestPrintListsSettingsSetInOnlyOneRun(t *testing.T) {
	t.Parallel()

	baseline, contender := baselineRun(), contenderRun()
	contender.Report.Config.(map[string]any)["QPS"] = float64(200)
	contender.Report.Command = "run"

	got := printed(baseline, contender, Options{Threshold: 5})

	require.Contains(t, got, "  command    folder → run\n")
	require.Contains(t, got, "  QPS        (unset) → 200\n")
}

func TestLoadReadsASavedReport(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "report.json")
	raw, err := json.Marshal(contenderRun().Report)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o644))

	run, err := Load(path)

	require.NoError(t, err)
	require.Equal(t, path, run.Path)
	require.Equal(t, "after", run.Report.Label)
	require.Equal(t, 3, run.Report.Results.PerQuery[1].Errors)
}

func TestLoadRejectsAFileThatIsNotAReport(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "notes.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

	_, err := Load(path)

	require.ErrorContains(t, err, "is not a benchmark report")
}
