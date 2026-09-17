package publisher

import (
	"math"
	"time"
)

// percentile returns the value at the given percentile of sorted, which must be
// in ascending order. It uses the nearest-rank method, so the result is always
// one of the observed values rather than an interpolation between two of them.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

func millis(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
