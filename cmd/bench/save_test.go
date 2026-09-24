package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Vesiro/vesiro-benchmarker/internal/benchmark"
	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
	"github.com/Vesiro/vesiro-benchmarker/internal/query"
	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

func TestReportFileNameStartsWithTheTimeAndKeepsOnlySafeCharacters(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 9, 24, 14, 15, 2, 0, time.Local)
	report := publisher.Report{StartedAt: started, Command: "folder", Label: "before upgrade/v2"}

	require.Equal(t,
		"2026-09-24T14-15-02_folder_cc-wet_before-upgrade-v2.json",
		reportFileName(report, "cc-wet"))
}

func TestReportFileNameLeavesOutAMissingLabel(t *testing.T) {
	t.Parallel()

	report := publisher.Report{StartedAt: time.Date(2026, 9, 24, 14, 15, 2, 0, time.Local), Command: "run"}

	require.Equal(t, "2026-09-24T14-15-02_run_idx.json", reportFileName(report, "idx"))
}

func TestSaveReportNeverOverwritesAnEarlierReport(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "results")

	first, err := saveReport(dir, "run.json", publisher.Report{Label: "first"})
	require.NoError(t, err)
	second, err := saveReport(dir, "run.json", publisher.Report{Label: "second"})
	require.NoError(t, err)

	require.Equal(t, filepath.Join(dir, "run.json"), first)
	require.Equal(t, filepath.Join(dir, "run-2.json"), second)
	require.Equal(t, "first", readReport(t, first).Label)
	require.Equal(t, "second", readReport(t, second).Label)
}

func readReport(t *testing.T, path string) publisher.Report {
	t.Helper()

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var report publisher.Report
	require.NoError(t, json.Unmarshal(raw, &report))
	return report
}

// savedReports lists what executeAndSave wrote.
func savedReports(t *testing.T, dir string) []string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	require.NoError(t, err)
	return matches
}

func testNode(t *testing.T, body string) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// testExecution builds a small run against nodeURL with a summary that prints
// nowhere, plus any extra publishers.
func testExecution(nodeURL string, perClient int, extra ...publisher.Publisher) (benchmark.ExecutionConfig, *publisher.Summary) {
	opts := benchmark.RequestOptions{
		NumClients:        1,
		RequestsPerClient: &perClient,
		RepeatEachRequest: 1,
		Progress:          "none",
	}
	templates := []query.Template{testTemplate()}
	summary := newSummary("run", nil, opts, saveFlags{Label: "test"}, templates, 1)
	summary.Out = &bytes.Buffer{}
	summary.Diagnostics = &bytes.Buffer{}

	return benchmark.ExecutionConfig{
		RunConfig:  newRunConfig(targetFlags{NodeURL: nodeURL, IndexName: "idx"}, opts, templates, 1),
		Publishers: append([]publisher.Publisher{summary}, extra...),
	}, summary
}

const okBody = `{"took":3,"hits":{"total":{"value":1,"relation":"eq"},"hits":[]}}`

func TestExecuteAndSaveWritesTheReport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	config, summary := testExecution(testNode(t, okBody), 3)

	require.NoError(t, executeAndSave(context.Background(), config, summary, saveFlags{Save: true, ResultsDir: dir}))

	saved := savedReports(t, dir)
	require.Len(t, saved, 1)
	report := readReport(t, saved[0])
	require.Equal(t, 3, report.Results.Requests)
	require.Equal(t, "run", report.Command)
	require.Equal(t, "test", report.Label)
	require.False(t, report.Interrupted)
	require.Empty(t, report.Error)
}

func TestExecuteAndSaveWritesNothingWithNoSave(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	config, summary := testExecution(testNode(t, okBody), 3)

	require.NoError(t, executeAndSave(context.Background(), config, summary, saveFlags{Save: false, ResultsDir: dir}))

	require.Empty(t, savedReports(t, dir))
}

func TestExecuteAndSaveSkipsARunThatSentNothing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Nothing listens here, so the first request fails.
	config, summary := testExecution("http://127.0.0.1:1", 3)

	err := executeAndSave(context.Background(), config, summary, saveFlags{Save: true, ResultsDir: dir})

	require.Error(t, err)
	require.Empty(t, savedReports(t, dir), "an empty report is not worth keeping")
}

func TestExecuteAndSaveMarksAFailedRun(t *testing.T) {
	t.Parallel()

	// The first response is fine and the second is not JSON, which fails the
	// run partway through.
	responses := make(chan string, 2)
	responses <- okBody
	responses <- "not json"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, <-responses)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	config, summary := testExecution(srv.URL, 2)

	err := executeAndSave(context.Background(), config, summary, saveFlags{Save: true, ResultsDir: dir})

	require.Error(t, err)
	saved := savedReports(t, dir)
	require.Len(t, saved, 1)
	report := readReport(t, saved[0])
	require.Equal(t, 1, report.Results.Requests)
	require.Contains(t, report.Error, "not valid json")
}

// cancelOnFirstSample stands in for Ctrl-C arriving mid-run.
type cancelOnFirstSample struct{ cancel context.CancelFunc }

func (c cancelOnFirstSample) Start() error                { return nil }
func (c cancelOnFirstSample) Finish() error               { return nil }
func (c cancelOnFirstSample) Publish(sample.Sample) error { c.cancel(); return nil }

func TestExecuteAndSaveMarksAnInterruptedRun(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	config, summary := testExecution(testNode(t, okBody), 1000, cancelOnFirstSample{cancel})

	require.NoError(t, executeAndSave(ctx, config, summary, saveFlags{Save: true, ResultsDir: dir}),
		"an interrupt is a clean stop")

	saved := savedReports(t, dir)
	require.Len(t, saved, 1)
	report := readReport(t, saved[0])
	require.True(t, report.Interrupted)
	require.Less(t, report.Results.Requests, 1000)
}
