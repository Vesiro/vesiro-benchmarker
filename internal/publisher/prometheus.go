package publisher

import (
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

// Prometheus exposes a run's samples as Prometheus metrics. Counters and the
// latency histogram update with every sample; the qps gauge is recomputed once
// per Interval, so it shows the current rate rather than the run's average.
type Prometheus struct {
	Interval time.Duration

	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	qps      prometheus.Gauge
	running  prometheus.Gauge

	count atomic.Int64
	done  chan struct{}
	wg    sync.WaitGroup
}

func NewPrometheus(interval time.Duration) *Prometheus {
	p := &Prometheus{
		Interval: interval,
		registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bench_requests_total",
			Help: "Search requests that got a response, by query and HTTP status.",
		}, []string{"query", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bench_request_duration_seconds",
			Help:    "Client latency of search requests, network trip included.",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
		}, []string{"query"}),
		qps: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "bench_qps",
			Help: "Requests per second over the last update interval.",
		}),
		running: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "bench_running",
			Help: "1 while a measured run is in progress, otherwise 0.",
		}),
	}
	p.registry.MustRegister(p.requests, p.duration, p.qps, p.running)
	return p
}

// Handler serves the metrics for scraping.
func (p *Prometheus) Handler() http.Handler {
	return promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{})
}

func (p *Prometheus) Start() error {
	p.count.Store(0)
	p.done = make(chan struct{})
	p.running.Set(1)

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(p.Interval)
		defer ticker.Stop()

		last := time.Now()
		for {
			select {
			case now := <-ticker.C:
				// Divide by the real elapsed time, since ticks can be late.
				p.qps.Set(float64(p.count.Swap(0)) / now.Sub(last).Seconds())
				last = now
			case <-p.done:
				return
			}
		}
	}()

	return nil
}

func (p *Prometheus) Finish() error {
	close(p.done)
	p.wg.Wait()
	p.qps.Set(0)
	p.running.Set(0)
	return nil
}

func (p *Prometheus) Publish(s sample.Sample) error {
	query := s.Query.Template.Name
	if query == "" {
		query = "<unnamed>"
	}

	p.count.Add(1)
	p.requests.WithLabelValues(query, strconv.Itoa(s.Status)).Inc()
	p.duration.WithLabelValues(query).Observe(s.ClientDuration.Seconds())
	return nil
}
