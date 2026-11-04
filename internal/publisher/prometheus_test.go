package publisher

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestPrometheusCountsRequestsByQueryAndStatus(t *testing.T) {
	t.Parallel()

	p := NewPrometheus(time.Hour)
	require.NoError(t, p.Start())
	require.NoError(t, p.Publish(newSample("a.json", 3, 5*time.Millisecond, 1)))
	require.NoError(t, p.Publish(newSample("a.json", 3, 5*time.Millisecond, 1)))
	require.NoError(t, p.Publish(newErrorSample("b.json", 429, time.Millisecond)))

	require.Equal(t, 2.0, testutil.ToFloat64(p.requests.WithLabelValues("a.json", "200")))
	require.Equal(t, 1.0, testutil.ToFloat64(p.requests.WithLabelValues("b.json", "429")))
	require.Equal(t, 2, testutil.CollectAndCount(p.duration))
	require.Equal(t, 1.0, testutil.ToFloat64(p.running))

	require.NoError(t, p.Finish())
	require.Equal(t, 0.0, testutil.ToFloat64(p.running))
}

func TestPrometheusUpdatesQPSEveryInterval(t *testing.T) {
	t.Parallel()

	p := NewPrometheus(50 * time.Millisecond)
	require.NoError(t, p.Start())
	t.Cleanup(func() { _ = p.Finish() })

	for range 10 {
		require.NoError(t, p.Publish(newSample("a.json", 3, time.Millisecond, 1)))
	}

	require.Eventually(t, func() bool {
		return testutil.ToFloat64(p.qps) > 0
	}, time.Second, 10*time.Millisecond, "qps should be set once the first interval passes")
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(p.qps) == 0
	}, time.Second, 10*time.Millisecond, "qps should drop back once requests stop")
}

func TestPrometheusFinishResetsQPS(t *testing.T) {
	t.Parallel()

	p := NewPrometheus(10 * time.Millisecond)
	require.NoError(t, p.Start())
	require.NoError(t, p.Publish(newSample("a.json", 3, time.Millisecond, 1)))
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(p.qps) > 0
	}, time.Second, 5*time.Millisecond)

	require.NoError(t, p.Finish())

	require.Equal(t, 0.0, testutil.ToFloat64(p.qps), "a finished run sends no traffic")
}

func TestPrometheusReportsTheQueryOfASingleTemplateRun(t *testing.T) {
	t.Parallel()

	p := NewPrometheus(time.Hour)
	p.Query = "match_all.json"
	require.NoError(t, p.Start())

	require.Equal(t, 1.0, testutil.ToFloat64(p.currentQuery.WithLabelValues("match_all.json")))

	require.NoError(t, p.Finish())
	require.Equal(t, 0, testutil.CollectAndCount(p.currentQuery), "a finished run has no current query")
}

func TestPrometheusReportsTheQueryOfEachPhase(t *testing.T) {
	t.Parallel()

	p := NewPrometheus(time.Hour)
	require.NoError(t, p.Start())
	t.Cleanup(func() { _ = p.Finish() })
	require.Equal(t, 0, testutil.CollectAndCount(p.currentQuery), "no query before the first phase")

	first := Phase{Number: 1, Count: 2, Name: "a.json"}
	second := Phase{Number: 2, Count: 2, Name: "b.json"}

	require.NoError(t, p.StartPhase(first))
	require.Equal(t, 1, testutil.CollectAndCount(p.currentQuery))
	require.Equal(t, 1.0, testutil.ToFloat64(p.currentQuery.WithLabelValues("a.json")))

	require.NoError(t, p.FinishPhase(first))
	require.Equal(t, 0, testutil.CollectAndCount(p.currentQuery), "no query between phases")

	require.NoError(t, p.StartPhase(second))
	require.Equal(t, 1, testutil.CollectAndCount(p.currentQuery), "only one query is current at a time")
	require.Equal(t, 1.0, testutil.ToFloat64(p.currentQuery.WithLabelValues("b.json")))
}

func TestPrometheusHandlerServesTheMetrics(t *testing.T) {
	t.Parallel()

	p := NewPrometheus(time.Hour)
	p.Query = "a.json"
	require.NoError(t, p.Start())
	t.Cleanup(func() { _ = p.Finish() })
	require.NoError(t, p.Publish(newSample("a.json", 3, 5*time.Millisecond, 1)))

	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)
	response, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	for _, name := range []string{"bench_requests_total", "bench_request_duration_seconds", "bench_qps", "bench_running", "bench_current_query"} {
		require.True(t, strings.Contains(string(body), name), "missing %s", name)
	}
}
