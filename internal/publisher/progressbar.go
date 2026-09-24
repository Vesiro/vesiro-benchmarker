package publisher

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cheggaaa/pb/v3"

	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

type SampleProgressBar struct {
	SampleSize    int
	Prefix        string
	CleanOnFinish bool
	bar           *pb.ProgressBar
}

func (p *SampleProgressBar) Start() error {
	p.bar = pb.New(p.SampleSize).
		SetTemplate(pb.ProgressBarTemplate(`{{string . "prefix"}}{{counters . }} {{bar . "[" "=" ">" " " "]"}} {{percent .}} | {{speed . }} | Elapsed: {{etime .}}`)).
		Set("prefix", p.Prefix).
		Set(pb.CleanOnFinish, p.CleanOnFinish).
		Start()
	return nil
}

func (p *SampleProgressBar) Finish() error {
	p.bar.Finish()
	eraseCleanedBar(p.bar)
	return nil
}

func (p *SampleProgressBar) Publish(s sample.Sample) error {
	p.bar.Increment()
	return nil
}

type TimedProgressBar struct {
	Duration      time.Duration
	Prefix        string
	CleanOnFinish bool
	bar           *pb.ProgressBar
	startTime     time.Time
	count         int64
}

func (p *TimedProgressBar) Start() error {
	total := int(p.Duration.Milliseconds())

	p.bar = pb.New(total).
		SetTemplate(pb.ProgressBarTemplate(
			`{{string . "prefix"}}{{counters . }} ms {{bar . "[" "=" ">" " " "]"}} {{percent .}} | qps: {{string . "metric"}} | Elapsed: {{etime .}}`,
		)).
		Set("prefix", p.Prefix).
		Set(pb.CleanOnFinish, p.CleanOnFinish).
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
	eraseCleanedBar(p.bar)
	return nil
}

func (p *TimedProgressBar) Publish(s sample.Sample) error {
	p.count++
	qps := float64(p.count) / time.Since(p.startTime).Seconds()
	p.bar.Set("metric", fmt.Sprintf("%.2f", qps))
	return nil
}

// eraseCleanedBar empties the line a finished bar was wiped from. pb wipes by
// overwriting the bar with spaces, which the terminal keeps, so any text
// printed there later would be copied out with a tail of them. Only a terminal
// gets the escape code, and pb draws on stderr.
func eraseCleanedBar(bar *pb.ProgressBar) {
	if bar.GetBool(pb.CleanOnFinish) && bar.GetBool(pb.Terminal) {
		_, _ = io.WriteString(os.Stderr, "\r\x1b[2K")
	}
}
