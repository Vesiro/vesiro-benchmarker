package publisher

import (
	"fmt"
	"time"
)

// ProgressConfig selects and configures a progress publisher. Exactly one of
// TotalRequests or BenchmarkTime must be set for the "bar" mode, since a bar
// needs a known total to measure against.
type ProgressConfig struct {
	Mode          string
	TotalRequests *int
	BenchmarkTime *time.Duration
}

// NewProgress builds the progress publisher for the configured mode. It returns
// a nil Publisher for mode "none", which callers should skip rather than start.
func NewProgress(config ProgressConfig) (Publisher, error) {
	switch config.Mode {
	case "bar":
		if config.TotalRequests != nil {
			return &SampleProgressBar{SampleSize: *config.TotalRequests}, nil
		}
		if config.BenchmarkTime != nil {
			return &TimedProgressBar{Duration: *config.BenchmarkTime}, nil
		}
		return nil, fmt.Errorf("progress mode 'bar' requires either a request total or a benchmark timeout")
	case "log":
		return &ProgressLog{
			Interval:      3 * time.Second,
			SampleSize:    config.TotalRequests,
			BenchmarkTime: config.BenchmarkTime,
		}, nil
	case "none":
		return nil, nil
	default:
		return nil, fmt.Errorf(
			"invalid progress mode: %s. valid options are: 'bar', 'log', 'none'",
			config.Mode,
		)
	}
}
