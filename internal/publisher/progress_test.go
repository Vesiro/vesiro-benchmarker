package publisher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewProgressBarUsesSampleCountWhenTotalKnown(t *testing.T) {
	t.Parallel()

	total := 500
	got, err := NewProgress(ProgressConfig{Mode: "bar", TotalRequests: &total})

	require.NoError(t, err)
	bar, ok := got.(*SampleProgressBar)
	require.True(t, ok, "a known request total should give a sample-counting bar")
	require.Equal(t, 500, bar.SampleSize)
}

func TestNewProgressBarFallsBackToElapsedTimeWhenOnlyTimeoutKnown(t *testing.T) {
	t.Parallel()

	window := 90 * time.Second
	got, err := NewProgress(ProgressConfig{Mode: "bar", BenchmarkTime: &window})

	require.NoError(t, err)
	bar, ok := got.(*TimedProgressBar)
	require.True(t, ok, "a duration-bounded run should give a time-based bar")
	require.Equal(t, window, bar.Duration)
}

func TestNewProgressBarPrefersSampleCountWhenBothKnown(t *testing.T) {
	t.Parallel()

	total := 10
	window := time.Minute
	got, err := NewProgress(ProgressConfig{Mode: "bar", TotalRequests: &total, BenchmarkTime: &window})

	require.NoError(t, err)
	require.IsType(t, &SampleProgressBar{}, got)
}

func TestNewProgressBarWithoutAnyBoundErrors(t *testing.T) {
	t.Parallel()

	got, err := NewProgress(ProgressConfig{Mode: "bar"})

	require.Nil(t, got)
	require.ErrorContains(t, err, "requires either a request total or a benchmark timeout")
}

func TestNewProgressLogCarriesBothBounds(t *testing.T) {
	t.Parallel()

	total := 200
	window := 45 * time.Second
	got, err := NewProgress(ProgressConfig{Mode: "log", TotalRequests: &total, BenchmarkTime: &window})

	require.NoError(t, err)
	progressLog, ok := got.(*ProgressLog)
	require.True(t, ok)
	require.Equal(t, &total, progressLog.SampleSize)
	require.Equal(t, &window, progressLog.BenchmarkTime)
	require.Positive(t, progressLog.Interval)
}

func TestNewProgressNoneReturnsNoPublisher(t *testing.T) {
	t.Parallel()

	got, err := NewProgress(ProgressConfig{Mode: "none"})

	require.NoError(t, err)
	require.Nil(t, got, "callers skip a nil progress publisher rather than starting it")
}

func TestNewProgressRejectsUnknownMode(t *testing.T) {
	t.Parallel()

	got, err := NewProgress(ProgressConfig{Mode: "sparklines"})

	require.Nil(t, got)
	require.ErrorContains(t, err, "invalid progress mode: sparklines")
}
