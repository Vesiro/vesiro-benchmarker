package benchmark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

// drain collects every sample the run produces and returns the run's error.
func drain(t *testing.T, ctx context.Context, config RunConfig) ([]sample.Sample, error) {
	t.Helper()

	samples := make(chan sample.Sample, config.NumClients)
	done, err := Run(ctx, config, samples)
	require.NoError(t, err)

	collected := make([]sample.Sample, 0, 32)
	for s := range samples {
		collected = append(collected, s)
	}
	return collected, <-done
}

func TestRunSendsClientsTimesRequestsPerClient(t *testing.T) {
	t.Parallel()

	srv, served := mockNode(t)
	config := testRunConfig(srv.URL, countedOptions(4, 5, 1))

	collected, err := drain(t, context.Background(), config)

	require.NoError(t, err)
	require.Len(t, collected, 20)
	require.EqualValues(t, 20, served.Load())
}

func TestRunAppliesRepeatEachRequest(t *testing.T) {
	t.Parallel()

	srv, served := mockNode(t)
	// 1 client * 2 requests * 3 repeats
	config := testRunConfig(srv.URL, countedOptions(1, 2, 3))

	collected, err := drain(t, context.Background(), config)

	require.NoError(t, err)
	require.Len(t, collected, 6)
	require.EqualValues(t, 6, served.Load())
}

func TestRunPopulatesSamplesWithTemplateAndStatus(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	config := testRunConfig(srv.URL, countedOptions(1, 1, 1))

	collected, err := drain(t, context.Background(), config)

	require.NoError(t, err)
	require.Len(t, collected, 1)
	require.Equal(t, http.StatusOK, collected[0].Status)
	require.Equal(t, "t", collected[0].Query.Template.Name)
	require.Positive(t, collected[0].ClientDuration)
	require.JSONEq(t, okResponse, string(collected[0].Body))
}

func TestRunCancelsAnInFlightRequestAtTheBenchmarkTimeout(t *testing.T) {
	t.Parallel()

	// The node never answers, so the run can only end by cancelling the request
	// it is waiting on. That cancellation is the run finishing, not a failure.
	config := testRunConfig(hangingNode(t).URL, timedOptions(1, 1))

	start := time.Now()
	collected, err := drain(t, context.Background(), config)

	require.NoError(t, err)
	require.Empty(t, collected)
	require.Less(t, time.Since(start), 5*time.Second)
}

func TestRunFailsWhenARequestExceedsTheRequestTimeout(t *testing.T) {
	t.Parallel()

	opts := countedOptions(1, 1, 1)
	opts.RequestTimeout = ptr(1)
	config := testRunConfig(hangingNode(t).URL, opts)

	_, err := drain(t, context.Background(), config)

	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRunStopsAtBenchmarkTimeoutAndReportsNoError(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	// No request count, so only the timeout can end the run.
	config := testRunConfig(srv.URL, timedOptions(2, 1))

	collected, err := drain(t, context.Background(), config)

	// A timeout is the expected way for a duration-bounded run to finish, so it
	// must not surface as an error.
	require.NoError(t, err)
	require.NotEmpty(t, collected)
}

func TestRunReturnsContextErrorWhenCancelled(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	config := testRunConfig(srv.URL, timedOptions(2, 60))

	ctx, cancel := context.WithCancel(context.Background())
	samples := make(chan sample.Sample, config.NumClients)
	done, err := Run(ctx, config, samples)
	require.NoError(t, err)

	// Cancel once traffic is definitely flowing.
	<-samples
	cancel()
	for range samples { //nolint:revive // drain until the producers stop
	}

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestRunSurfacesRequestErrors(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("this is not json"))
	}))
	t.Cleanup(srv.Close)

	config := testRunConfig(srv.URL, countedOptions(2, 5, 1))

	_, err := drain(t, context.Background(), config)

	require.Error(t, err)
}

func TestRunRejectsNilSampleChannel(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	config := testRunConfig(srv.URL, countedOptions(1, 1, 1))

	_, err := Run(context.Background(), config, nil)

	require.ErrorContains(t, err, "samples channel is required")
}

func TestRunRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	config := testRunConfig(srv.URL, countedOptions(1, 1, 1))
	config.Templates = nil

	samples := make(chan sample.Sample, 1)
	_, err := Run(context.Background(), config, samples)

	require.ErrorContains(t, err, "at least one query template is required")
}

func TestRunWithUniformRequestsGivesEveryClientTheSameQueries(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	opts := countedOptions(2, 1, 1)
	opts.UniformRequests = true
	config := testRunConfig(
		srv.URL,
		opts,
		testTemplateWithTerms("varied", "alpha", "beta", "gamma", "delta", "epsilon"),
	)

	collected, err := drain(t, context.Background(), config)

	require.NoError(t, err)
	require.Len(t, collected, 2)
	// Both clients seed from the same value, so their first query must match.
	require.Equal(t, collected[0].Query.Vars, collected[1].Query.Vars)
}

func TestRunSpreadsRequestsAcrossMultipleTemplates(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	config := testRunConfig(
		srv.URL,
		countedOptions(1, 50, 1),
		testTemplate("first"),
		testTemplate("second"),
	)

	collected, err := drain(t, context.Background(), config)
	require.NoError(t, err)
	require.Len(t, collected, 50)

	names := map[string]int{}
	for _, s := range collected {
		names[s.Query.Template.Name]++
	}
	require.Len(t, names, 2, "both templates should be exercised across 50 requests")
}

func TestRunRejectsOptionsWithNeitherLimit(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	config := testRunConfig(srv.URL, RequestOptions{
		NumClients:        1,
		RepeatEachRequest: 1,
	})

	samples := make(chan sample.Sample, 1)
	_, err := Run(context.Background(), config, samples)

	require.ErrorContains(t, err, "either benchmark-timeout or num-of-requests-per-client must be set")
}

func TestRunWithQPSHoldsTheTargetRate(t *testing.T) {
	t.Parallel()

	srv, served := mockNode(t)
	// 8 clients would finish 40 requests almost instantly unpaced; at 100 qps
	// the 40th request is scheduled 390ms in.
	opts := countedOptions(8, 5, 1)
	opts.QPS = 100
	config := testRunConfig(srv.URL, opts)

	start := time.Now()
	collected, err := drain(t, context.Background(), config)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.Len(t, collected, 40)
	require.EqualValues(t, 40, served.Load())

	expected := time.Duration(float64(39) / 100.0 * float64(time.Second))
	require.GreaterOrEqual(t, elapsed, expected, "the run must not outpace the requested rate")
}

func TestRunWithoutQPSIsNotRateLimited(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	config := testRunConfig(srv.URL, countedOptions(4, 25, 1))

	start := time.Now()
	collected, err := drain(t, context.Background(), config)

	require.NoError(t, err)
	require.Len(t, collected, 100)
	require.Less(t, time.Since(start), 2*time.Second, "the default must stay closed-loop")
}

func TestRunWithQPSStillStopsAtTheTimeout(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	// 50 qps for 1s, so roughly 50 requests rather than the unpaced thousands.
	opts := timedOptions(4, 1)
	opts.QPS = 50
	config := testRunConfig(srv.URL, opts)

	collected, err := drain(t, context.Background(), config)

	require.NoError(t, err)
	require.NotEmpty(t, collected)
	require.Less(t, len(collected), 200, "pacing must bound the request count within the timeout")
}

func TestRunWithQPSPacesRepeatsToo(t *testing.T) {
	t.Parallel()

	srv, served := mockNode(t)
	// 2 clients * 3 requests * 2 repeats = 12 HTTP requests, all paced.
	opts := countedOptions(2, 3, 2)
	opts.QPS = 100
	config := testRunConfig(srv.URL, opts)

	start := time.Now()
	collected, err := drain(t, context.Background(), config)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.Len(t, collected, 12)
	require.EqualValues(t, 12, served.Load())

	expected := time.Duration(float64(11) / 100.0 * float64(time.Second))
	require.GreaterOrEqual(t, elapsed, expected, "repeats count against the rate")
}

func TestRunRejectsNegativeQPS(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	opts := countedOptions(1, 1, 1)
	opts.QPS = -5
	config := testRunConfig(srv.URL, opts)

	samples := make(chan sample.Sample, 1)
	_, err := Run(context.Background(), config, samples)

	require.ErrorContains(t, err, "qps must be zero or greater")
}
