package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"
)

func TestRunWarmupFlags(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		want int
		err  string
	}{
		{name: "disabled by default"},
		{name: "seconds", args: []string{"--warmup=10"}, want: 10},
		{name: "zero", args: []string{"--warmup=0"}},
		{name: "negative", args: []string{"--warmup=-1"}, err: "warmup must be zero or greater"},
		{name: "client validation retained", args: []string{"--warmup=10", "--num-clients=0"}, err: "num clients must be greater than zero"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cli CLI
			parser, err := kong.New(&cli)
			require.NoError(t, err)
			args := []string{"run", "--node-url=http://localhost:9200", "--index-name=idx", "--query-template=q.json", "--requests-per-client=1"}
			_, err = parser.Parse(append(args, tc.args...))
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, cli.Run.Warmup)
		})
	}
}

func TestRunWarmupPrecedesMeasurementAndIsExcludedFromSavedResults(t *testing.T) {
	t.Parallel()
	var served atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served.Add(1)
		_, _ = io.WriteString(w, okBody)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	queryPath := filepath.Join(dir, "query.json")
	require.NoError(t, os.WriteFile(queryPath, []byte(`{"query":{"match_all":{}}}`), 0o644))
	resultsDir := filepath.Join(dir, "results")
	var cli CLI
	parser, err := kong.New(&cli)
	require.NoError(t, err)
	ctx, err := parser.Parse([]string{
		"run", "--node-url=" + srv.URL, "--index-name=idx", "--query-template=" + queryPath,
		"--num-clients=3", "--requests-per-client=2", "--warmup=1", "--qps=30",
		"--progress=none", "--results-dir=" + resultsDir,
	})
	require.NoError(t, err)
	started := time.Now()
	require.NoError(t, ctx.Run())

	files := savedReports(t, resultsDir)
	require.Len(t, files, 1)
	report := readReport(t, files[0])
	require.Equal(t, 6, report.Results.Requests, "all clients get their full measured request count")
	require.Greater(t, served.Load(), int64(6), "warmup sends additional requests")
	require.GreaterOrEqual(t, report.StartedAt.Sub(started), time.Second, "measurement starts after warmup")
	require.InDelta(t, report.FinishedAt.Sub(report.StartedAt).Seconds()*1000, report.Results.ExecutionTimeMs, 0.001)
	require.Equal(t, float64(1), report.Config.(map[string]any)["Warmup"])
}
