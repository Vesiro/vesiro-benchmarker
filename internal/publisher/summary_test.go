package publisher

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Vesiro/vesiro-benchmarker/internal/query"
	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
	"github.com/stretchr/testify/require"
)

// newSample is a served query: 2xx with no error in the body.
func newSample(name string, took float64, duration time.Duration, hits int) sample.Sample {
	body, _ := json.Marshal(map[string]any{
		"took": took,
		"hits": map[string]any{
			"total": map[string]any{
				"value":    hits,
				"relation": "eq",
			},
		},
	})

	return sample.Sample{
		Query:          query.Query{Template: query.Template{Name: name}},
		ClientDuration: duration,
		Status:         http.StatusOK,
		Body:           body,
	}
}

// newErrorSample is a query the node rejected, reporting the failure in both
// the HTTP status and the body, as Elasticsearch does.
func newErrorSample(name string, status int, duration time.Duration) sample.Sample {
	body, _ := json.Marshal(map[string]any{
		"error":  map[string]any{"type": "search_phase_execution_exception"},
		"status": status,
	})

	return sample.Sample{
		Query:          query.Query{Template: query.Template{Name: name}},
		ClientDuration: duration,
		Status:         status,
		Body:           body,
	}
}

// summaryOver publishes the samples into a summary spanning a fixed window, so
// the derived rate is deterministic.
func summaryOver(t *testing.T, window time.Duration, samples ...sample.Sample) *Summary {
	t.Helper()

	summary := &Summary{Diagnostics: &bytes.Buffer{}}
	require.NoError(t, summary.Start())
	for _, s := range samples {
		require.NoError(t, summary.Publish(s))
	}
	summary.T0 = time.Unix(0, 0)
	summary.T1 = summary.T0.Add(window)

	return summary
}

func TestSummaryCalculateAggregatesTheRun(t *testing.T) {
	t.Parallel()

	summary := summaryOver(t, 3*time.Second,
		newSample("a.json", 10, 100*time.Millisecond, 1),
		newSample("b.json", 20, 300*time.Millisecond, 0),
		newSample("a.json", 30, 200*time.Millisecond, 2),
	)

	results := summary.Calculate()

	require.Equal(t, 3, results.Requests)
	require.Equal(t, 3, results.Ok)
	require.Zero(t, results.Errors)
	require.Zero(t, results.ErrorRate)
	require.Equal(t, 1.0, results.QPS)
	require.Equal(t, 3000.0, results.ExecutionTimeMs)
	require.Equal(t, map[int]int{200: 3}, results.StatusCounts)
	require.Equal(t, 2, results.HasHits)
	require.InDelta(t, 2.0/3.0, results.HitsRatio, 1e-9)
	require.Equal(t, 20.0, results.TookAvgMs)
	require.Equal(t, 200.0, results.ClientLatencyMs.Avg)
}

func TestSummaryCalculateReportsClientLatencyPercentiles(t *testing.T) {
	t.Parallel()

	// 100 requests of 1ms..100ms, so the nearest-rank percentiles land on
	// exactly the 50th, 95th and 99th values.
	samples := make([]sample.Sample, 0, 100)
	for i := 1; i <= 100; i++ {
		samples = append(samples, newSample("a.json", 1, time.Duration(i)*time.Millisecond, 1))
	}

	results := summaryOver(t, time.Second, samples...).Calculate()

	require.Equal(t, 50.5, results.ClientLatencyMs.Avg)
	require.Equal(t, 50.0, results.ClientLatencyMs.P50)
	require.Equal(t, 95.0, results.ClientLatencyMs.P95)
	require.Equal(t, 99.0, results.ClientLatencyMs.P99)
}

func TestSummaryCalculateReportsPercentilesPerQuery(t *testing.T) {
	t.Parallel()

	samples := make([]sample.Sample, 0, 200)
	for i := 1; i <= 100; i++ {
		samples = append(samples, newSample("fast.json", 1, time.Duration(i)*time.Millisecond, 1))
		samples = append(samples, newSample("slow.json", 1, time.Duration(10*i)*time.Millisecond, 1))
	}

	results := summaryOver(t, time.Second, samples...).Calculate()

	require.Len(t, results.PerQuery, 2)

	require.Equal(t, "fast.json", results.PerQuery[0].Name)
	require.Equal(t, 100, results.PerQuery[0].Requests)
	require.Equal(t, 95.0, results.PerQuery[0].ClientLatencyMs.P95)

	require.Equal(t, "slow.json", results.PerQuery[1].Name)
	require.Equal(t, 100, results.PerQuery[1].Requests)
	require.Equal(t, 950.0, results.PerQuery[1].ClientLatencyMs.P95)
}

func TestSummaryCalculateCountsStatusesAndErrorRate(t *testing.T) {
	t.Parallel()

	summary := summaryOver(t, 4*time.Second,
		newSample("a.json", 5, 10*time.Millisecond, 1),
		newErrorSample("a.json", http.StatusBadRequest, 10*time.Millisecond),
		newSample("a.json", 5, 10*time.Millisecond, 1),
		newErrorSample("a.json", http.StatusServiceUnavailable, 10*time.Millisecond),
	)

	results := summary.Calculate()

	require.Equal(t, 4, results.Requests)
	require.Equal(t, 2, results.Ok)
	require.Equal(t, 2, results.Errors)
	require.Equal(t, 0.5, results.ErrorRate)
	require.Equal(t, map[int]int{200: 2, 400: 1, 503: 1}, results.StatusCounts)
	// A failed query is still a request that was sent and timed.
	require.Equal(t, 4, results.PerQuery[0].Requests)
}

func TestSummaryCountsA2xxWithABodyErrorAsAFailure(t *testing.T) {
	t.Parallel()

	// Elasticsearch can answer 200 while reporting the failure in the body.
	body, _ := json.Marshal(map[string]any{"status": 500, "error": "boom"})
	s := sample.Sample{
		Query:          query.Query{Template: query.Template{Name: "a.json"}},
		ClientDuration: time.Millisecond,
		Status:         http.StatusOK,
		Body:           body,
	}

	results := summaryOver(t, time.Second, s).Calculate()

	require.Equal(t, 1, results.Requests)
	require.Zero(t, results.Ok)
	require.Equal(t, 1, results.Errors)
	require.Equal(t, 1.0, results.ErrorRate)
}

func TestSummaryCalculateHandlesZeroSamples(t *testing.T) {
	t.Parallel()

	summary := &Summary{T0: time.Unix(0, 0), T1: time.Unix(2, 0)}

	results := summary.Calculate()

	require.Zero(t, results.Requests)
	require.Zero(t, results.Ok)
	require.Zero(t, results.Errors)
	require.Zero(t, results.ErrorRate)
	require.Zero(t, results.QPS)
	require.Zero(t, results.TookAvgMs)
	require.Zero(t, results.ClientLatencyMs)
	require.Empty(t, results.PerQuery)
	require.Empty(t, results.StatusCounts)
}

func TestSummaryPublishEchoesFailuresToDiagnosticsNotTheReport(t *testing.T) {
	t.Parallel()

	var report, diagnostics bytes.Buffer
	summary := &Summary{Out: &report, Diagnostics: &diagnostics}
	require.NoError(t, summary.Start())

	require.NoError(t, summary.Publish(newErrorSample("a.json", http.StatusBadRequest, time.Millisecond)))

	require.Contains(t, diagnostics.String(), "search_phase_execution_exception")
	require.Empty(t, report.String(), "the report stream must stay clean for redirection")
}

func TestSummaryPrintReportsEveryHeadlineNumber(t *testing.T) {
	t.Parallel()

	var report bytes.Buffer
	summary := summaryOver(t, 2*time.Second,
		newSample("a.json", 8, 20*time.Millisecond, 1),
		newErrorSample("a.json", http.StatusServiceUnavailable, 40*time.Millisecond),
	)
	summary.Out = &report

	summary.Print()

	printed := report.String()
	for _, want := range []string{
		"Requests:        2",
		"Ok:              1",
		"Errors:          1 (50.00%)",
		"Statuses:        200: 1, 503: 1",
		"q/s:             1.00",
		"Took Avg:        4.00ms",
		"Client Latency:  avg 30.00ms  p50 20.00ms  p95 40.00ms  p99 40.00ms",
		"HasHits:         1 (50.0%)",
		"Per Query:",
		"a.json: 2 requests  avg 30.00ms",
	} {
		require.Contains(t, printed, want)
	}
}

func TestSummaryFinishWritesJSONWhenAsked(t *testing.T) {
	t.Parallel()

	var report bytes.Buffer
	summary := summaryOver(t, 2*time.Second,
		newSample("a.json", 8, 20*time.Millisecond, 1),
		newErrorSample("b.json", http.StatusServiceUnavailable, 40*time.Millisecond),
	)
	summary.Out = &report
	summary.Format = OutputJSON
	summary.Meta = ReportMeta{
		Version: "1.2.3",
		Seed:    4232,
		Config:  map[string]any{"numClients": 16},
		Queries: []string{"a.json", "b.json"},
	}

	require.NoError(t, summary.Finish())

	var got Report
	require.NoError(t, json.Unmarshal(report.Bytes(), &got), "the report must be one valid JSON document")

	require.Equal(t, "1.2.3", got.Version)
	require.EqualValues(t, 4232, got.Seed)
	require.Equal(t, []string{"a.json", "b.json"}, got.Queries)
	require.Equal(t, map[string]any{"numClients": float64(16)}, got.Config)
	require.False(t, got.FinishedAt.Before(got.StartedAt))

	require.Equal(t, 2, got.Results.Requests)
	require.Equal(t, 1, got.Results.Ok)
	require.Equal(t, 1, got.Results.Errors)
	require.Equal(t, 0.5, got.Results.ErrorRate)
	require.Equal(t, map[int]int{200: 1, 503: 1}, got.Results.StatusCounts)
	require.Equal(t, 4.0, got.Results.TookAvgMs)
	require.Equal(t, 30.0, got.Results.ClientLatencyMs.Avg)
	require.Equal(t, 20.0, got.Results.ClientLatencyMs.P50)
	require.Len(t, got.Results.PerQuery, 2)
}

func TestSummaryStartRejectsAnUnknownFormat(t *testing.T) {
	t.Parallel()

	require.ErrorContains(t, (&Summary{Format: "yaml"}).Start(), "invalid output format")
	require.NoError(t, (&Summary{Format: ""}).Start())
	require.NoError(t, (&Summary{Format: OutputText}).Start())
	require.NoError(t, (&Summary{Format: OutputJSON}).Start())
}
