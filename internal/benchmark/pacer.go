package benchmark

import (
	"context"
	"sync/atomic"
	"time"
)

// pacer holds the combined request rate of all clients at a fixed target.
//
// Requests are numbered from one counter shared by every client, and request n
// is due at start + n/qps. At 100 qps that puts the deadlines at start+0ms,
// start+10ms, start+20ms and so on, and wait sleeps until the deadline of the
// request that called it. Sharing the counter is what makes the target a rate
// for the run as a whole rather than one for each client.
//
// Deadlines are counted from the run's start, not from the previous request.
// Sleeping 1/qps after each response would be simpler, but every sleep
// overshoots a little and the errors compound, so the achieved rate drifts
// below the target over a long run. Counting from a fixed origin leaves no
// per-request error to accumulate.
//
// A request whose deadline has already passed sends without sleeping, so if the
// run stalls for 50ms at 100 qps the next five requests go out back to back and
// the average recovers. The target is still only a ceiling, since each client
// holds one request at a time: 16 clients waiting 50ms for every response top
// out at 320 requests per second, however high qps is set. The summary reports
// the rate actually achieved.
type pacer struct {
	start time.Time
	qps   float64
	next  atomic.Int64
}

// newPacer returns nil when qps is not positive. A nil pacer is the unpaced
// case: its wait never blocks.
func newPacer(qps float64, start time.Time) *pacer {
	if qps <= 0 {
		return nil
	}
	return &pacer{start: start, qps: qps}
}

// wait blocks until this request's deadline, or until ctx is cancelled. A nil
// pacer never blocks.
func (p *pacer) wait(ctx context.Context) error {
	if p == nil {
		return nil
	}

	n := p.next.Add(1) - 1
	offset := time.Duration(float64(n) / p.qps * float64(time.Second))
	delay := time.Until(p.start.Add(offset))
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
