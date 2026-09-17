package publisher

import (
	"fmt"
	"time"

	"github.com/cheggaaa/pb/v3"

	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

type SampleProgressBar struct {
	SampleSize int
	bar        *pb.ProgressBar
}

func (p *SampleProgressBar) Start() error {
	p.bar = pb.New(p.SampleSize).
		SetTemplate(pb.ProgressBarTemplate(`{{counters . }} {{bar . "[" "=" ">" " " "]"}} {{percent .}} | {{speed . }} | Elapsed: {{etime .}}`)).
		Start()
	return nil
}

func (p *SampleProgressBar) Finish() error {
	p.bar.Finish()
	return nil
}

func (p *SampleProgressBar) Publish(s sample.Sample) error {
	p.bar.Increment()
	return nil
}

type TimedProgressBar struct {
	Duration  time.Duration
	bar       *pb.ProgressBar
	startTime time.Time
	count     int64
}

func (p *TimedProgressBar) Start() error {
	total := int(p.Duration.Milliseconds())

	p.bar = pb.New(total).
		SetTemplate(pb.ProgressBarTemplate(
			`{{counters . }} ms {{bar . "[" "=" ">" " " "]"}} {{percent .}} | qps: {{string . "metric"}} | Elapsed: {{etime .}}`,
		)).
		Start()

	p.startTime = time.Now()

	// Background goroutine advances the bar based on real elapsed time
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			elapsed := time.Since(p.startTime)
			p.bar.SetCurrent(elapsed.Milliseconds())
			if elapsed >= p.Duration {
				return
			}
		}
	}()

	return nil
}

func (p *TimedProgressBar) Finish() error {
	p.bar.SetCurrent(p.Duration.Milliseconds())
	p.bar.Finish()
	return nil
}

func (p *TimedProgressBar) Publish(s sample.Sample) error {
	p.count++
	qps := float64(p.count) / time.Since(p.startTime).Seconds()
	p.bar.Set("metric", fmt.Sprintf("%.2f", qps))
	return nil
}
