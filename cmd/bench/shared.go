package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/Vesiro/vesiro-benchmarker/internal/benchmark"
	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
	"github.com/Vesiro/vesiro-benchmarker/internal/query"
	"github.com/Vesiro/vesiro-benchmarker/internal/version"
)

// targetFlags are the flags needed to reach a single node and index.
type targetFlags struct {
	NodeURL   string `required:"" name:"node-url" help:"Target node URL to which requests will be sent."`
	IndexName string `required:"" help:"Name of the index to target when sending requests."`
	// json:"-" keeps the password out of logConfig.
	User                  string `default:"" json:"-" help:"Optional basic auth credentials as user:password."`
	InsecureSkipTLSVerify bool   `default:"false" help:"Skip TLS certificate verification. Only for targets using self-signed certificates."`
}

// httpClient builds the client for reaching this target, sized to the number
// of workers that will share it.
func (t targetFlags) httpClient(numClients int) *http.Client {
	return benchmark.HTTPClientConfig{
		NumClients:            numClients,
		Credentials:           t.User,
		InsecureSkipTLSVerify: t.InsecureSkipTLSVerify,
	}.Build()
}

// queryFlags are the flags that shape the generated query bodies.
type queryFlags struct {
	Seed        *int64 `help:"Seed for random number generation. If unset, current time is used."`
	ExtraFields string `default:"" help:"Extra fields to add to each query JSON object."`
}

func (q queryFlags) queryConfig(templatePath string) benchmark.QueryConfig {
	return benchmark.QueryConfig{
		TemplatePath:    templatePath,
		ExtraFieldsJSON: q.ExtraFields,
	}
}

// newRunConfig assembles the engine config every traffic-sending command needs,
// including the shared HTTP client sized to the client count.
func newRunConfig(
	target targetFlags,
	opts benchmark.RequestOptions,
	templates []query.Template,
	seed int64,
) benchmark.RunConfig {
	return benchmark.RunConfig{
		RequestOptions:      opts,
		HTTPClient:          target.httpClient(opts.NumClients),
		NodeURL:             target.NodeURL,
		IndexName:           target.IndexName,
		Templates:           templates,
		Seed:                seed,
		SkipQueryValidation: true,
	}
}

// newSummary builds the end-of-run report publisher.
func newSummary(config any, opts benchmark.RequestOptions, templates []query.Template, seed int64) *publisher.Summary {
	names := make([]string, 0, len(templates))
	for _, tmpl := range templates {
		names = append(names, tmpl.Name)
	}

	return &publisher.Summary{
		Format: opts.Output,
		Meta: publisher.ReportMeta{
			Version: version.Version,
			Seed:    seed,
			Config:  config,
			Queries: names,
		},
	}
}

// newProgressPublisher returns the configured progress publisher, or nil when
// progress display is turned off.
func newProgressPublisher(opts benchmark.RequestOptions) (publisher.Publisher, error) {
	mode := opts.Progress
	if opts.Output == publisher.OutputJSON {
		mode = "none"
	}

	return publisher.NewProgress(publisher.ProgressConfig{
		Mode:          mode,
		TotalRequests: opts.TotalRequests(),
		BenchmarkTime: opts.BenchmarkTimeoutDuration(),
	})
}

// appendProgress adds the progress publisher when one is configured.
func appendProgress(publishers []publisher.Publisher, opts benchmark.RequestOptions) ([]publisher.Publisher, error) {
	progress, err := newProgressPublisher(opts)
	if err != nil {
		return nil, err
	}
	if progress == nil {
		return publishers, nil
	}
	return append(publishers, progress), nil
}

func logConfig(cnf any) error {
	raw, err := json.MarshalIndent(cnf, "", "  ")
	if err != nil {
		return err
	}
	log.Println("Config", string(raw))
	return nil
}

// newSignalContext gives a context that is cancelled on Ctrl-C or SIGTERM, so
// an interrupted benchmark still gets to flush its publishers.
func newSignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

// execute runs the benchmark, treating interruption as a clean shutdown rather
// than an error.
func execute(ctx context.Context, config benchmark.ExecutionConfig) error {
	if err := benchmark.Execute(ctx, config); err != nil {
		if errors.Is(err, context.Canceled) {
			log.Println("Benchmark interrupted, shutting down gracefully...")
			return nil
		}
		return err
	}
	return nil
}
