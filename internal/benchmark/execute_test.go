package benchmark

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
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
