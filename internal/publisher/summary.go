package publisher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

// Output formats a Summary can render at the end of a run.
const (
	OutputText = "text"
	OutputJSON = "json"
)

// ReportMeta is what a run knows about itself that the samples cannot say.
type ReportMeta struct {
	Version string
	Seed    int64
	// Config is the effective configuration, marshalled as given. It must not
	// carry credentials.
	Config  any
	Queries []string
}

type Summary struct {
	// Out is where the report is written. Nil means os.Stdout.
	Out io.Writer

	// Diagnostics is where failing response bodies are echoed. Nil means
	// os.Stderr, which keeps Out to just the report.
	Diagnostics io.Writer

	// Format is OutputText (the default) or OutputJSON.
	Format string

	Meta ReportMeta

	T0 time.Time
	T1 time.Time

	queries         int
	ok              int
	hasHits         int
	tookSum         float64
	clientDurations []time.Duration
	statusCounts    map[int]int
	perQuery        map[string]*perQueryStats
}

type perQueryStats struct {
	clientDurations []time.Duration
}

func (p *Summary) out() io.Writer {
	if p.Out != nil {
		return p.Out
	}
	return os.Stdout
}

func (p *Summary) diagnostics() io.Writer {
	if p.Diagnostics != nil {
		return p.Diagnostics
	}
	return os.Stderr
}

func (p *Summary) Start() error {
	if p.Format != "" && p.Format != OutputText && p.Format != OutputJSON {
		return fmt.Errorf("invalid output format %q: want %q or %q", p.Format, OutputText, OutputJSON)
	}
	p.T0 = time.Now().UTC()
	return nil
}

func (p *Summary) Finish() error {
	p.T1 = time.Now().UTC()
	if p.Format == OutputJSON {
		return p.WriteJSON()
	}
	p.Print()
	return nil
}

func (p *Summary) Publish(s sample.Sample) error {
	var body struct {
		Took   float64 `json:"took"`
		Status int     `json:"status,omitempty"`
		Hits   struct {
			Total struct {
				Value    int    `json:"value"`
				Relation string `json:"relation"`
			} `json:"total"`
		} `json:"hits,omitempty"`
	}
	if err := json.Unmarshal(s.Body, &body); err != nil {
		return err
	}

	if p.statusCounts == nil {
		p.statusCounts = make(map[int]int)
	}
	if p.perQuery == nil {
		p.perQuery = make(map[string]*perQueryStats)
	}

	p.queries++
	p.tookSum += body.Took
	p.clientDurations = append(p.clientDurations, s.ClientDuration)
	p.statusCounts[s.Status]++

	queryName := s.Query.Template.Name
	if queryName == "" {
		queryName = "<unnamed>"
	}
	stats := p.perQuery[queryName]
	if stats == nil {
		stats = &perQueryStats{}
		p.perQuery[queryName] = stats
	}
	stats.clientDurations = append(stats.clientDurations, s.ClientDuration)

	// A node that answers 2xx with no error in the body served the query.
	if s.Status >= 200 && s.Status <= 299 && body.Status == 0 {
		p.ok++
		if body.Hits.Total.Value > 0 {
			p.hasHits++
		}
		return nil
	}

	// Echo the failing response so the operator can see why it failed, but
	// never fail the run over a formatting problem.
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, s.Body, "", "  "); err != nil {
		_, _ = fmt.Fprintln(p.diagnostics(), "error:", err)
		return nil
	}
	_, _ = fmt.Fprintln(p.diagnostics(), pretty.String())

	return nil
}

// Latency is a distribution of client latencies in milliseconds.
type Latency struct {
	Avg float64 `json:"avg"`
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
}

type PerQueryResult struct {
	Name            string  `json:"name"`
	Requests        int     `json:"requests"`
	ClientLatencyMs Latency `json:"clientLatencyMs"`
}

type Results struct {
	Requests        int              `json:"requests"`
	Ok              int              `json:"ok"`
	Errors          int              `json:"errors"`
	ErrorRate       float64          `json:"errorRate"`
	QPS             float64          `json:"qps"`
	ExecutionTimeMs float64          `json:"executionTimeMs"`
	StatusCounts    map[int]int      `json:"statusCounts"`
	HasHits         int              `json:"hasHits"`
	HitsRatio       float64          `json:"hitsRatio"`
	TookAvgMs       float64          `json:"tookAvgMs"`
	ClientLatencyMs Latency          `json:"clientLatencyMs"`
	PerQuery        []PerQueryResult `json:"perQuery"`
}

type Report struct {
	Version    string    `json:"version"`
	Seed       int64     `json:"seed"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	Config     any       `json:"config,omitempty"`
	Queries    []string  `json:"queries,omitempty"`
	Results    Results   `json:"results"`
}

// latency sorts durations in place and reduces them to a distribution.
func latency(durations []time.Duration) Latency {
	if len(durations) == 0 {
		return Latency{}
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	var sum time.Duration
	for _, d := range durations {
		sum += d
	}

	return Latency{
		Avg: millis(sum) / float64(len(durations)),
		P50: millis(percentile(durations, 50)),
		P95: millis(percentile(durations, 95)),
		P99: millis(percentile(durations, 99)),
	}
}

func (p *Summary) Calculate() Results {
	executionTime := p.T1.Sub(p.T0)

	perQuery := make([]PerQueryResult, 0, len(p.perQuery))
	for name, stats := range p.perQuery {
		perQuery = append(perQuery, PerQueryResult{
			Name:            name,
			Requests:        len(stats.clientDurations),
			ClientLatencyMs: latency(stats.clientDurations),
		})
	}
	sort.Slice(perQuery, func(i, j int) bool { return perQuery[i].Name < perQuery[j].Name })

	statusCounts := make(map[int]int, len(p.statusCounts))
	for status, count := range p.statusCounts {
		statusCounts[status] = count
	}

	results := Results{
		ExecutionTimeMs: millis(executionTime),
		StatusCounts:    statusCounts,
		PerQuery:        perQuery,
	}
	if p.queries == 0 {
		return results
	}

	errors := p.queries - p.ok
	results.Requests = p.queries
	results.Ok = p.ok
	results.Errors = errors
	results.ErrorRate = float64(errors) / float64(p.queries)
	results.HasHits = p.hasHits
	results.HitsRatio = float64(p.hasHits) / float64(p.queries)
	results.TookAvgMs = p.tookSum / float64(p.queries)
	results.ClientLatencyMs = latency(p.clientDurations)
	if executionTime > 0 {
		results.QPS = float64(p.queries) / executionTime.Seconds()
	}

	return results
}

func (p *Summary) Report() Report {
	return Report{
		Version:    p.Meta.Version,
		Seed:       p.Meta.Seed,
		StartedAt:  p.T0,
		FinishedAt: p.T1,
		Config:     p.Meta.Config,
		Queries:    p.Meta.Queries,
		Results:    p.Calculate(),
	}
}

func (p *Summary) WriteJSON() error {
	raw, err := json.MarshalIndent(p.Report(), "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(p.out(), string(raw))
	return err
}

func (p *Summary) Print() {
	w := p.out()
	s := p.Calculate()

	_, _ = fmt.Fprintf(w, "Requests:        %d\n", s.Requests)
	_, _ = fmt.Fprintf(w, "Ok:              %d\n", s.Ok)
	_, _ = fmt.Fprintf(w, "Errors:          %d (%.2f%%)\n", s.Errors, s.ErrorRate*100)
	_, _ = fmt.Fprintf(w, "Statuses:        %s\n", formatStatusCounts(s.StatusCounts))
	_, _ = fmt.Fprintf(w, "q/s:             %.2f\n", s.QPS)
	_, _ = fmt.Fprintf(w, "Execution Time:  %v\n", p.T1.Sub(p.T0))
	_, _ = fmt.Fprintf(w, "Took Avg:        %.2fms\n", s.TookAvgMs)
	_, _ = fmt.Fprintf(w, "Client Latency:  %s\n", formatLatency(s.ClientLatencyMs))
	_, _ = fmt.Fprintf(w, "HasHits:         %d (%.1f%%)\n", s.HasHits, s.HitsRatio*100)
	if len(s.PerQuery) > 0 {
		_, _ = fmt.Fprintln(w, "Per Query:")
		for _, perQuery := range s.PerQuery {
			_, _ = fmt.Fprintf(w, "  %s: %d requests  %s\n", perQuery.Name, perQuery.Requests, formatLatency(perQuery.ClientLatencyMs))
		}
	}
}

func formatLatency(l Latency) string {
	return fmt.Sprintf("avg %.2fms  p50 %.2fms  p95 %.2fms  p99 %.2fms", l.Avg, l.P50, l.P95, l.P99)
}

// formatStatusCounts renders the counts in ascending status order, so repeated
// runs of the same benchmark print the same line.
func formatStatusCounts(counts map[int]int) string {
	if len(counts) == 0 {
		return "-"
	}

	statuses := make([]int, 0, len(counts))
	for status := range counts {
		statuses = append(statuses, status)
	}
	sort.Ints(statuses)

	parts := make([]string, 0, len(statuses))
	for _, status := range statuses {
		parts = append(parts, strconv.Itoa(status)+": "+strconv.Itoa(counts[status]))
	}
	return strings.Join(parts, ", ")
}
