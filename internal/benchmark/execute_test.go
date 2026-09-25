package benchmark

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
	"github.com/Vesiro/vesiro-benchmarker/internal/query"
	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

// recordingPublisher counts lifecycle calls and can fail on demand.
type recordingPublisher struct {
	mu        sync.Mutex
	starts    int
	finishes  int
	published int

	startErr   error
	publishErr error
	finishErr  error
}

func (r *recordingPublisher) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts++
	return r.startErr
}

func (r *recordingPublisher) Finish() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishes++
	return r.finishErr
}

func (r *recordingPublisher) Publish(sample.Sample) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.published++
	return r.publishErr
}

func (r *recordingPublisher) counts() (starts, finishes, published int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.starts, r.finishes, r.published
}

func TestExecuteStartsPublishesAndFinishes(t *testing.T) {
	t.Parallel()

	srv, served := mockNode(t)
	rec := &recordingPublisher{}

	err := Execute(context.Background(), ExecutionConfig{
		RunConfig:  testRunConfig(srv.URL, countedOptions(3, 4, 1)),
		Publishers: []publisher.Publisher{rec},
	})

	require.NoError(t, err)
	starts, finishes, published := rec.counts()
	require.Equal(t, 1, starts)
	require.Equal(t, 1, finishes)
	require.Equal(t, 12, published)
	require.EqualValues(t, 12, served.Load())
}

func TestExecuteFansOutToEveryPublisher(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	first, second := &recordingPublisher{}, &recordingPublisher{}

	err := Execute(context.Background(), ExecutionConfig{
		RunConfig:  testRunConfig(srv.URL, countedOptions(1, 5, 1)),
		Publishers: []publisher.Publisher{first, second},
	})

	require.NoError(t, err)
	for _, rec := range []*recordingPublisher{first, second} {
		_, _, published := rec.counts()
		require.Equal(t, 5, published)
	}
}

func TestExecuteFinishesPublishersEvenWhenTheRunFails(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(srv.Close)

	rec := &recordingPublisher{}
	err := Execute(context.Background(), ExecutionConfig{
		RunConfig:  testRunConfig(srv.URL, countedOptions(1, 3, 1)),
		Publishers: []publisher.Publisher{rec},
	})

	require.Error(t, err)
	starts, finishes, _ := rec.counts()
	require.Equal(t, 1, starts)
	require.Equal(t, 1, finishes, "Finish must run even on failure so output is flushed")
}

func TestExecutePropagatesPublisherStartError(t *testing.T) {
	t.Parallel()

	srv, served := mockNode(t)
	sentinel := errors.New("cannot start")
	rec := &recordingPublisher{startErr: sentinel}

	err := Execute(context.Background(), ExecutionConfig{
		RunConfig:  testRunConfig(srv.URL, countedOptions(1, 3, 1)),
		Publishers: []publisher.Publisher{rec},
	})

	require.ErrorIs(t, err, sentinel)
	require.EqualValues(t, 0, served.Load(), "no traffic should be sent if a publisher cannot start")
}

func TestExecutePropagatesPublishError(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	sentinel := errors.New("cannot publish")
	rec := &recordingPublisher{publishErr: sentinel}

	err := Execute(context.Background(), ExecutionConfig{
		RunConfig:  testRunConfig(srv.URL, countedOptions(1, 3, 1)),
		Publishers: []publisher.Publisher{rec},
	})

	require.ErrorIs(t, err, sentinel)
}

func TestExecutePropagatesFinishError(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	sentinel := errors.New("cannot finish")
	rec := &recordingPublisher{finishErr: sentinel}

	err := Execute(context.Background(), ExecutionConfig{
		RunConfig:  testRunConfig(srv.URL, countedOptions(1, 2, 1)),
		Publishers: []publisher.Publisher{rec},
	})

	require.ErrorIs(t, err, sentinel)
}

func TestExecuteWithoutPublishersStillRuns(t *testing.T) {
	t.Parallel()

	srv, served := mockNode(t)

	err := Execute(context.Background(), ExecutionConfig{
		RunConfig: testRunConfig(srv.URL, countedOptions(2, 3, 1)),
	})

	require.NoError(t, err)
	require.EqualValues(t, 6, served.Load())
}

// phaseRecorder logs phase boundaries and samples in the order it hears them,
// collapsing a stretch of samples from one template into a single entry.
type phaseRecorder struct {
	mu      sync.Mutex
	events  []string
	phases  []publisher.Phase
	samples map[string]int

	// onPublish, when set, runs for every sample.
	onPublish func()
}

func (r *phaseRecorder) Start() error  { return nil }
func (r *phaseRecorder) Finish() error { return nil }

func (r *phaseRecorder) Publish(s sample.Sample) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.samples == nil {
		r.samples = map[string]int{}
	}
	r.samples[s.Query.Template.Name]++
	event := "sample " + s.Query.Template.Name
	if len(r.events) == 0 || r.events[len(r.events)-1] != event {
		r.events = append(r.events, event)
	}
	if r.onPublish != nil {
		r.onPublish()
	}
	return nil
}

func (r *phaseRecorder) StartPhase(p publisher.Phase) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.phases = append(r.phases, p)
	r.events = append(r.events, "start "+p.Name)
	return nil
}

func (r *phaseRecorder) FinishPhase(p publisher.Phase) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, "finish "+p.Name)
	return nil
}

func TestExecuteSequentialRunsEachTemplateInTurnWithTheFullCount(t *testing.T) {
	t.Parallel()

	srv, served := mockNode(t)
	rec := &phaseRecorder{}

	err := Execute(context.Background(), ExecutionConfig{
		RunConfig:  testRunConfig(srv.URL, countedOptions(2, 5, 1), testTemplate("a"), testTemplate("b"), testTemplate("c")),
		Publishers: []publisher.Publisher{rec},
		Sequential: true,
	})

	require.NoError(t, err)
	require.EqualValues(t, 30, served.Load())
	require.Equal(t, map[string]int{"a": 10, "b": 10, "c": 10}, rec.samples,
		"every template gets clients * requests-per-client")
	require.Equal(t, []string{
		"start a", "sample a", "finish a",
		"start b", "sample b", "finish b",
		"start c", "sample c", "finish c",
	}, rec.events, "every sample of a template must land between its start and finish")
	require.Equal(t, []publisher.Phase{
		{Number: 1, Count: 3, Name: "a"},
		{Number: 2, Count: 3, Name: "b"},
		{Number: 3, Count: 3, Name: "c"},
	}, rec.phases)
}

func TestExecuteSequentialGivesEachTemplateTheFullTimeout(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	rec := &phaseRecorder{}

	start := time.Now()
	err := Execute(context.Background(), ExecutionConfig{
		RunConfig:  testRunConfig(srv.URL, timedOptions(2, 1), testTemplate("a"), testTemplate("b")),
		Publishers: []publisher.Publisher{rec},
		Sequential: true,
	})

	require.NoError(t, err)
	require.GreaterOrEqual(t, time.Since(start), 2*time.Second, "one second per template")
	require.Equal(t, []string{"start a", "sample a", "finish a", "start b", "sample b", "finish b"}, rec.events)
}

func TestExecuteSequentialStopsAtCancellationWithoutStartingTheRest(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := &phaseRecorder{onPublish: cancel}
	lifecycle := &recordingPublisher{}

	err := Execute(ctx, ExecutionConfig{
		RunConfig:  testRunConfig(srv.URL, timedOptions(1, 60), testTemplate("a"), testTemplate("b")),
		Publishers: []publisher.Publisher{rec, lifecycle},
		Sequential: true,
	})

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []string{"start a", "sample a"}, rec.events,
		"an interrupted template never finishes and the next one never starts")
	_, finishes, _ := lifecycle.counts()
	require.Equal(t, 1, finishes, "publishers still finish so the report is printed")
}

func TestExecuteSequentialStopsAtTheFirstRequestError(t *testing.T) {
	t.Parallel()

	var served atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served.Add(1)
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(srv.Close)
	rec := &phaseRecorder{}

	err := Execute(context.Background(), ExecutionConfig{
		RunConfig:  testRunConfig(srv.URL, countedOptions(1, 1, 1), testTemplate("a"), testTemplate("b")),
		Publishers: []publisher.Publisher{rec},
		Sequential: true,
	})

	require.Error(t, err)
	require.EqualValues(t, 1, served.Load(), "the second template must not run after a failure")
	require.Equal(t, []string{"start a"}, rec.events)
}

func TestExecuteSequentialRejectsAnInvalidConfig(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	config := testRunConfig(srv.URL, countedOptions(1, 1, 1))
	config.Templates = []query.Template{}

	err := Execute(context.Background(), ExecutionConfig{RunConfig: config, Sequential: true})

	require.ErrorContains(t, err, "at least one query template is required")
}

func TestExecuteMixedRunHasNoPhases(t *testing.T) {
	t.Parallel()

	srv, _ := mockNode(t)
	rec := &phaseRecorder{}

	err := Execute(context.Background(), ExecutionConfig{
		RunConfig:  testRunConfig(srv.URL, countedOptions(1, 20, 1), testTemplate("a"), testTemplate("b")),
		Publishers: []publisher.Publisher{rec},
	})

	require.NoError(t, err)
	require.Empty(t, rec.phases)
	require.Equal(t, 20, rec.samples["a"]+rec.samples["b"], "the count covers the whole run")
}

// warmupRecorder inspects traffic just before measurement starts.
type warmupRecorder struct {
	phaseRecorder
	onStart func(publisher.Phase)
}

func (r *warmupRecorder) StartPhase(p publisher.Phase) error {
	r.onStart(p)
	return r.phaseRecorder.StartPhase(p)
}

type warmupTransport func(*http.Request) (*http.Response, error)

func (f warmupTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExecuteWarmsEachQueryWithAllClientsBeforeMeasuring(t *testing.T) {
	t.Parallel()

	var active, peak, served atomic.Int64
	config := testRunConfig("http://node", countedOptions(3, 2, 2), testTemplate("a"), testTemplate("b"))
	config.HTTPClient.Transport = warmupTransport(func(r *http.Request) (*http.Response, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); current > old; old = peak.Load() {
			if peak.CompareAndSwap(old, current) {
				break
			}
		}
		served.Add(1)
		timer := time.NewTimer(10 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-timer.C:
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(okResponse)), Header: make(http.Header)}, nil
		}
	})
	summary := &publisher.Summary{Out: io.Discard, PhaseTiming: true}
	lastStart := time.Now()
	var previousServed int64
	rec := &warmupRecorder{onStart: func(p publisher.Phase) {
		require.GreaterOrEqual(t, time.Since(lastStart), time.Second, "each query gets a full warmup")
		require.Greater(t, served.Load()-previousServed, int64(12), "warmup ignores the measured request count")
		require.EqualValues(t, 3, peak.Load(), "warmup uses the configured client count")
		require.Zero(t, active.Load(), "all warmup requests finish before measurement")
		previousServed = served.Load()
		lastStart = time.Now()
	}}
	require.NoError(t, Execute(context.Background(), ExecutionConfig{
		RunConfig: config, Publishers: []publisher.Publisher{rec, summary}, Sequential: true, Warmup: 1,
	}))
	require.Equal(t, map[string]int{"a": 12, "b": 12}, rec.samples)
	require.Equal(t, []string{"start a", "sample a", "finish a", "start b", "sample b", "finish b"}, rec.events)
	results := summary.Calculate()
	require.Equal(t, 24, results.Requests)
	wallTime := summary.T1.Sub(summary.T0).Seconds()
	require.LessOrEqual(t, results.ExecutionTimeMs/1000, wallTime-2, "warmup time is excluded")
	require.Greater(t, results.QPS, 24/wallTime)
}

func TestExecuteWarmupStopsBeforeMeasurementOnCancellationOrFailure(t *testing.T) {
	t.Parallel()
	for _, interrupted := range []bool{false, true} {
		t.Run(fmt.Sprint(interrupted), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var served atomic.Int64
			config := testRunConfig("http://node", countedOptions(1, 1, 1), testTemplate("a"), testTemplate("b"))
			sentinel := errors.New("warmup request failed")
			config.HTTPClient.Transport = warmupTransport(func(r *http.Request) (*http.Response, error) {
				served.Add(1)
				if interrupted {
					cancel()
					return nil, ctx.Err()
				}
				return nil, sentinel
			})
			rec := &phaseRecorder{}
			summary := &publisher.Summary{Out: io.Discard, PhaseTiming: true}
			err := Execute(ctx, ExecutionConfig{RunConfig: config, Publishers: []publisher.Publisher{rec, summary}, Sequential: true, Warmup: 1})
			if interrupted {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, sentinel)
			}
			require.EqualValues(t, 1, served.Load())
			require.Empty(t, rec.events)
			require.Zero(t, summary.Calculate().Requests)
			require.Zero(t, summary.Calculate().ExecutionTimeMs)
		})
	}
}

func TestExecuteRandomWarmsTheMixOnceBeforeTheFullMeasuredTimeout(t *testing.T) {
	t.Parallel()

	srv, served := mockNode(t)
	config := testRunConfig(srv.URL, timedOptions(3, 1), testTemplate("a"), testTemplate("b"))
	config.QPS = 30
	summary := &publisher.Summary{Out: io.Discard}
	rec := &phaseRecorder{}
	started := time.Now()
	err := Execute(context.Background(), ExecutionConfig{
		RunConfig: config, Publishers: []publisher.Publisher{summary, rec}, Warmup: 1,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, summary.T0.Sub(started), time.Second, "measurement starts after one warmup")
	require.GreaterOrEqual(t, summary.T1.Sub(summary.T0), time.Second, "measurement gets its own full timeout")
	require.GreaterOrEqual(t, served.Load()-int64(summary.Calculate().Requests), int64(20), "warmup traffic is not reported")
	require.InDelta(t, 30, summary.Calculate().Requests, 3, "the configured rate still applies")
	require.NotZero(t, rec.samples["a"])
	require.NotZero(t, rec.samples["b"])
	require.Empty(t, rec.phases, "a random mix is one run without per-query phases")
}
