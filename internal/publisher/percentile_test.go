package publisher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPercentileOfSortedDurations(t *testing.T) {
	t.Parallel()

	sorted := make([]time.Duration, 0, 100)
	for i := 1; i <= 100; i++ {
		sorted = append(sorted, time.Duration(i)*time.Millisecond)
	}

	require.Equal(t, 1*time.Millisecond, percentile(sorted, 0))
	require.Equal(t, 50*time.Millisecond, percentile(sorted, 50))
	require.Equal(t, 95*time.Millisecond, percentile(sorted, 95))
	require.Equal(t, 99*time.Millisecond, percentile(sorted, 99))
	require.Equal(t, 100*time.Millisecond, percentile(sorted, 100))
}

func TestPercentileOfEmptyIsZero(t *testing.T) {
	t.Parallel()

	require.Zero(t, percentile(nil, 95))
}

func TestPercentileOfOneValueIsThatValue(t *testing.T) {
	t.Parallel()

	only := []time.Duration{7 * time.Millisecond}

	require.Equal(t, 7*time.Millisecond, percentile(only, 50))
	require.Equal(t, 7*time.Millisecond, percentile(only, 99))
}

func TestPercentileRoundsUpToAnObservedValue(t *testing.T) {
	t.Parallel()

	// Three values: p50 sits at rank 2, and p99 at rank 3, never between them.
	sorted := []time.Duration{time.Millisecond, 2 * time.Millisecond, 30 * time.Millisecond}

	require.Equal(t, 2*time.Millisecond, percentile(sorted, 50))
	require.Equal(t, 30*time.Millisecond, percentile(sorted, 95))
	require.Equal(t, 30*time.Millisecond, percentile(sorted, 99))
}
