package benchmark

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewPacerIsNilWhenRateNotSet(t *testing.T) {
	t.Parallel()

	require.Nil(t, newPacer(0, time.Now()))
	require.Nil(t, newPacer(-1, time.Now()))
}

func TestNilPacerNeverBlocks(t *testing.T) {
	t.Parallel()

	var unpaced *pacer

	start := time.Now()
	for i := 0; i < 1000; i++ {
		require.NoError(t, unpaced.wait(context.Background()))
	}

	require.Less(t, time.Since(start), 50*time.Millisecond)
}

func TestPacerFirstRequestIsDueImmediately(t *testing.T) {
	t.Parallel()

	p := newPacer(10, time.Now())

	start := time.Now()
	require.NoError(t, p.wait(context.Background()))

	require.Less(t, time.Since(start), 20*time.Millisecond)
}

func TestPacerSpacesRequestsAtTheTargetRate(t *testing.T) {
	t.Parallel()

	// 200 qps puts 5ms between deadlines, so 20 requests span 19 gaps: 95ms.
	const qps = 200.0
	const requests = 20

	p := newPacer(qps, time.Now())

	start := time.Now()
	for i := 0; i < requests; i++ {
		require.NoError(t, p.wait(context.Background()))
	}
	elapsed := time.Since(start)

	expected := time.Duration(float64(requests-1) / qps * float64(time.Second))
	require.GreaterOrEqual(t, elapsed, expected, "must not run faster than the target rate")
	// Generous upper bound: timer granularity varies by machine and CI load.
	require.Less(t, elapsed, expected+250*time.Millisecond)
}

func TestPacerDeadlinesAreAbsoluteNotIncremental(t *testing.T) {
	t.Parallel()

	// Starting a second in the past leaves the first 100 deadlines already
	// overdue, so 50 requests go out without sleeping. That is what lets a run
	// make up for a slow response.
	p := newPacer(100, time.Now().Add(-time.Second))

	start := time.Now()
	for i := 0; i < 50; i++ {
		require.NoError(t, p.wait(context.Background()))
	}

	require.Less(t, time.Since(start), 50*time.Millisecond)
}

func TestPacerSharesOneCounterAcrossClients(t *testing.T) {
	t.Parallel()

	// The counter is shared, so 4 clients sending 5 requests each get the same
	// 20 deadlines one client sending 20 would have got.
	const qps = 200.0
	const clients, each = 4, 5

	p := newPacer(qps, time.Now())

	var wg sync.WaitGroup
	wg.Add(clients)
	start := time.Now()
	for i := 0; i < clients; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < each; j++ {
				_ = p.wait(context.Background())
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	expected := time.Duration(float64(clients*each-1) / qps * float64(time.Second))
	require.GreaterOrEqual(t, elapsed, expected, "clients must share one rate, not get one each")
}

func TestPacerHandsOutEachNumberOnce(t *testing.T) {
	t.Parallel()

	p := newPacer(1_000_000, time.Now())

	var wg sync.WaitGroup
	wg.Add(8)
	for i := 0; i < 8; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = p.wait(context.Background())
			}
		}()
	}
	wg.Wait()

	require.EqualValues(t, 800, p.next.Load(), "every request must take exactly one number")
}

func TestPacerWaitReturnsWhenContextCancelled(t *testing.T) {
	t.Parallel()

	// One request per 10s, so the second wait would outlast the test by far.
	p := newPacer(0.1, time.Now())
	require.NoError(t, p.wait(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := p.wait(ctx)

	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, time.Since(start), time.Second, "cancellation must not wait for the deadline")
}
