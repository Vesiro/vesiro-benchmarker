package benchmark

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vesiro/vesiro-benchmarker/internal/query"
	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

func Run(ctx context.Context, config RunConfig, samples chan<- sample.Sample) (<-chan error, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if samples == nil {
		return nil, fmt.Errorf("samples channel is required")
	}

	done := make(chan error, 1)
	go func() {
		err := run(ctx, config, samples)
		close(samples)
		done <- err
		close(done)
	}()

	return done, nil
}

func run(ctx context.Context, config RunConfig, samples chan<- sample.Sample) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	templates := config.Templates

	var timedOut atomic.Bool
	if config.BenchmarkTimeout != nil {
		timer := time.NewTimer(*config.BenchmarkTimeoutDuration())
		defer timer.Stop()

		go func() {
			select {
			case <-timer.C:
				timedOut.Store(true)
				cancel()
			case <-runCtx.Done():
			}
		}()
	}

	errs := make(chan error, 1)

	// Take the origin that request deadlines count from before any client
	// starts, so they all pace against the same one.
	pace := newPacer(config.QPS, time.Now())

	var seedCounter atomic.Int64
	seedCounter.Store(config.Seed)

	var wg sync.WaitGroup
	wg.Add(config.NumClients)
	for i := 0; i < config.NumClients; i++ {
		go func() {
			defer wg.Done()

			seed := seedCounter.Load()
			if !config.UniformRequests {
				seed = seedCounter.Add(1)
			}

			requestRNG := rand.New(rand.NewSource(seed))
			resolvers := make([]query.Resolver, 0, len(templates))
			for _, tmpl := range templates {
				resolver, err := NewResolver(
					tmpl,
					requestRNG.Int63(),
					config.SkipQueryValidation,
				)
				if err != nil {
					sendError(errs, cancel, err)
					return
				}
				resolvers = append(resolvers, resolver)
			}

			client := Client{
				HTTPClient: config.HTTPClient,
				NodeURL:    config.NodeURL,
				Index:      config.IndexName,
				Timeout:    ResolveTimeout(config.RequestTimeout),
			}

			for requestIndex := 0; config.RequestsPerClient == nil || requestIndex < *config.RequestsPerClient; requestIndex++ {
				select {
				case <-runCtx.Done():
					return
				default:
				}

				query := resolvers[requestRNG.Intn(len(resolvers))].Query()
				for repeatIndex := 0; repeatIndex < config.RepeatEachRequest; repeatIndex++ {
					// Every request counts against the rate, repeats included.
					if err := pace.wait(runCtx); err != nil {
						return
					}

					s, err := client.Search(runCtx, query)
					if err != nil {
						sendError(errs, cancel, err)
						return
					}

					select {
					case <-runCtx.Done():
						return
					case samples <- s:
					}
				}
			}
		}()
	}

	wg.Wait()

	if timedOut.Load() {
		return nil
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	select {
	case err := <-errs:
		if err != nil {
			return err
		}
	default:
	}

	return nil
}

func sendError(errs chan<- error, cancel context.CancelFunc, err error) {
	select {
	case errs <- err:
	default:
	}
	cancel()
}
