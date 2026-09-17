package benchmark

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Vesiro/vesiro-benchmarker/internal/query"
)

// RequestOptions are the load-shaping knobs shared by every command that sends
// traffic. The struct tags are read by kong when it builds the CLI.
type RequestOptions struct {
	NumClients        int    `default:"16" help:"Number of concurrent clients (goroutines) sending requests."`
	RequestsPerClient *int   `help:"Number of requests each client will send (total = NumClients * RequestsPerClient)."`
	RepeatEachRequest int    `default:"1" help:"Total times each request is sent. Defaults to 1 (no repeats)."`
	BenchmarkTimeout  *int   `help:"Maximum benchmark duration in seconds."`
	RequestTimeout    *int   `help:"Maximum duration of a single request in seconds. Unset means a request is bounded only by the run itself."`
	UniformRequests   bool   `default:"false" help:"All clients sends the same requests"`
	Progress          string `default:"bar" help:"Progress display mode. Options: 'bar', 'none', 'log'."`
	Output            string `default:"text" help:"Final report format. Options: 'text', 'json'."`

	QPS float64 `default:"0" name:"qps" help:"Target aggregate requests per second across all clients. Unset or 0 sends as fast as possible. Cannot exceed num-clients divided by the mean response time."`
}

func (o RequestOptions) Validate() error {
	switch {
	case o.BenchmarkTimeout != nil && *o.BenchmarkTimeout < 0:
		return fmt.Errorf("timeout must be zero or greater")
	case o.RequestsPerClient == nil && o.BenchmarkTimeout == nil:
		return fmt.Errorf("either benchmark-timeout or num-of-requests-per-client must be set")
	case o.NumClients <= 0:
		return fmt.Errorf("num clients must be greater than zero")
	case o.RequestsPerClient != nil && *o.RequestsPerClient < 0:
		return fmt.Errorf("requests per client must be zero or greater")
	case o.RepeatEachRequest <= 0:
		return fmt.Errorf("repeat each request must be greater than zero")
	case o.RequestTimeout != nil && *o.RequestTimeout < 0:
		return fmt.Errorf("request timeout must be zero or greater")
	case o.QPS < 0:
		return fmt.Errorf("qps must be zero or greater")
	}
	return nil
}

func (o RequestOptions) BenchmarkTimeoutDuration() *time.Duration {
	if o.BenchmarkTimeout == nil {
		return nil
	}
	duration := time.Duration(*o.BenchmarkTimeout) * time.Second
	return &duration
}

// TotalRequests is the number of requests a run will send, or nil when the run
// is bounded by duration instead of by count.
func (o RequestOptions) TotalRequests() *int {
	if o.RequestsPerClient == nil {
		return nil
	}
	total := o.NumClients * (*o.RequestsPerClient) * o.RepeatEachRequest
	return &total
}

// RunConfig is everything Run needs to generate and send traffic.
type RunConfig struct {
	RequestOptions
	HTTPClient          *http.Client
	NodeURL             string
	IndexName           string
	Templates           []query.Template
	Seed                int64
	SkipQueryValidation bool
}

func (c RunConfig) Validate() error {
	if err := c.RequestOptions.Validate(); err != nil {
		return err
	}
	switch {
	case c.HTTPClient == nil:
		return fmt.Errorf("http client is required")
	case c.NodeURL == "":
		return fmt.Errorf("node url is required")
	case c.IndexName == "":
		return fmt.Errorf("index name is required")
	case len(c.Templates) == 0:
		return fmt.Errorf("at least one query template is required")
	default:
		return nil
	}
}

func ResolveSeed(seed *int64) int64 {
	if seed != nil {
		return *seed
	}
	return time.Now().UnixNano()
}

func ResolveTimeout(timeoutSeconds *int) time.Duration {
	if timeoutSeconds == nil {
		return 0
	}
	return time.Duration(*timeoutSeconds) * time.Second
}
