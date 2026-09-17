package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Vesiro/vesiro-benchmarker/internal/benchmark"
	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
	"github.com/Vesiro/vesiro-benchmarker/internal/query"
	"github.com/Vesiro/vesiro-benchmarker/internal/version"
)

func testTemplate() query.Template {
	return query.Template{
		Name:     "t",
		Bindings: map[string]query.Binding{},
		Template: json.RawMessage(`{"query":{"match_all":{}}}`),
	}
}

func TestNewRunConfigProducesAValidConfig(t *testing.T) {
	t.Parallel()

	perClient := 25
	config := newRunConfig(
		targetFlags{NodeURL: "http://node:9200", IndexName: "cc-wet"},
		benchmark.RequestOptions{
			NumClients:        8,
			RequestsPerClient: &perClient,
			RepeatEachRequest: 1,
			Progress:          "none",
		},
		[]query.Template{testTemplate()},
		4232,
	)

	require.NoError(t, config.Validate())
	require.Equal(t, "http://node:9200", config.NodeURL)
	require.Equal(t, "cc-wet", config.IndexName)
	require.Equal(t, int64(4232), config.Seed)
	require.Len(t, config.Templates, 1)
	require.NotNil(t, config.HTTPClient, "a client must always be built")
	require.True(t, config.SkipQueryValidation)
	require.Equal(t, 8, config.NumClients)
}

func TestNewRunConfigWithoutTemplatesFailsValidation(t *testing.T) {
	t.Parallel()

	perClient := 1
	config := newRunConfig(
		targetFlags{NodeURL: "http://node:9200", IndexName: "cc-wet"},
		benchmark.RequestOptions{
			NumClients:        1,
			RequestsPerClient: &perClient,
			RepeatEachRequest: 1,
		},
		nil,
		1,
	)

	require.ErrorContains(t, config.Validate(), "at least one query template is required")
}

func TestAppendProgressAddsAPublisherWhenEnabled(t *testing.T) {
	t.Parallel()

	perClient := 10
	opts := benchmark.RequestOptions{
		NumClients:        2,
		RequestsPerClient: &perClient,
		RepeatEachRequest: 1,
		Progress:          "bar",
	}

	got, err := appendProgress(nil, opts)

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.IsType(t, &publisher.SampleProgressBar{}, got[0])
}

func TestAppendProgressLeavesTheListAloneWhenDisabled(t *testing.T) {
	t.Parallel()

	perClient := 10
	opts := benchmark.RequestOptions{
		NumClients:        2,
		RequestsPerClient: &perClient,
		RepeatEachRequest: 1,
		Progress:          "none",
	}
	existing := []publisher.Publisher{&publisher.Summary{}}

	got, err := appendProgress(existing, opts)

	require.NoError(t, err)
	require.Len(t, got, 1, "progress 'none' must not add a publisher")
	require.IsType(t, &publisher.Summary{}, got[0])
}

func TestAppendProgressSurfacesAnInvalidMode(t *testing.T) {
	t.Parallel()

	perClient := 10
	opts := benchmark.RequestOptions{
		NumClients:        2,
		RequestsPerClient: &perClient,
		RepeatEachRequest: 1,
		Progress:          "flashing-lights",
	}

	got, err := appendProgress(nil, opts)

	require.Nil(t, got)
	require.ErrorContains(t, err, "invalid progress mode")
}

func TestQueryFlagsBuildQueryConfig(t *testing.T) {
	t.Parallel()

	flags := queryFlags{ExtraFields: `{"profile":true}`}

	config := flags.queryConfig("some/template.json")

	require.Equal(t, "some/template.json", config.TemplatePath)
	require.Equal(t, `{"profile":true}`, config.ExtraFieldsJSON)
}

func TestNewSignalContextIsCancellable(t *testing.T) {
	t.Parallel()

	ctx, cancel := newSignalContext()
	require.NoError(t, ctx.Err())

	cancel()

	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestLogConfigOmitsBasicAuthCredentials(t *testing.T) {
	// log's output is global, so this one cannot run in parallel.
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	err := logConfig(RunCmd{
		targetFlags: targetFlags{
			NodeURL:   "http://node:9200",
			IndexName: "cc-wet",
			User:      "elastic:s3cr3t",
		},
	})

	require.NoError(t, err)
	require.NotContains(t, logged.String(), "s3cr3t")
	require.NotContains(t, logged.String(), "elastic")
	require.Contains(t, logged.String(), "cc-wet", "the rest of the config must still be logged")
}

func TestNewSummaryCarriesTheReportMetadata(t *testing.T) {
	t.Parallel()

	cmd := RunCmd{
		targetFlags: targetFlags{NodeURL: "http://node:9200", IndexName: "cc-wet", User: "elastic:s3cr3t"},
	}
	opts := benchmark.RequestOptions{NumClients: 8, Output: "json"}

	summary := newSummary(cmd, opts, []query.Template{{Name: "a.json"}, {Name: "b.json"}}, 4232)

	require.Equal(t, "json", summary.Format)
	require.Equal(t, version.Version, summary.Meta.Version)
	require.EqualValues(t, 4232, summary.Meta.Seed)
	require.Equal(t, []string{"a.json", "b.json"}, summary.Meta.Queries)

	raw, err := json.Marshal(summary.Meta.Config)
	require.NoError(t, err)
	require.Contains(t, string(raw), "cc-wet", "the effective config must be in the report")
	require.NotContains(t, string(raw), "s3cr3t", "credentials must never reach the report")
}

func TestAppendProgressStaysSilentForAJSONReport(t *testing.T) {
	t.Parallel()

	perClient := 10
	opts := benchmark.RequestOptions{
		NumClients:        2,
		RequestsPerClient: &perClient,
		RepeatEachRequest: 1,
		Progress:          "bar",
		Output:            "json",
	}

	got, err := appendProgress(nil, opts)

	require.NoError(t, err)
	require.Empty(t, got, "a progress bar would corrupt the json on stdout")
}
