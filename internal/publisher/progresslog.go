package publisher

import (
	"fmt"
	"time"

	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

type ProgressLog struct {
	Interval      time.Duration
	SampleSize    *int
	BenchmarkTime *time.Duration
	runtime       time.Duration
	count         int
	delta         int
	done          chan struct{}
}

func (p *ProgressLog) Start() error {
	p.count = 0
	p.delta = 0
	p.done = make(chan struct{})

	var remainingSamples string
	if p.SampleSize != nil {
		remainingSamples = fmt.Sprintf("%d", *p.SampleSize)
	} else {
		remainingSamples = "-"
	}

	go func() {
		ticker := time.NewTicker(p.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.runtime += p.Interval
				var percentage float64
				if p.SampleSize == nil {
					percentage = p.runtime.Seconds() / p.BenchmarkTime.Seconds() * 100
				} else {
					percentage = float64(p.count) / float64(*p.SampleSize) * 100
				}
				fmt.Printf(
					"Progress: %.2f%% (%d/%s queries) (%.2f qps) \n",
					percentage,
					p.count,
					remainingSamples,
					float64(p.delta)/p.Interval.Seconds(),
				)
				p.delta = 0
			case <-p.done:
				return
			}
		}
	}()

	return nil
}

func (p *ProgressLog) Finish() error {
	close(p.done)
	return nil
}

func (p *ProgressLog) Publish(s sample.Sample) error {
	p.count++
	p.delta++
	return nil
}
