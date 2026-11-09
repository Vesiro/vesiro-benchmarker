package benchmark

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
	"github.com/Vesiro/vesiro-benchmarker/internal/query"
	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

type ExecutionConfig struct {
	Control    *Control
	RunConfig  RunConfig
	Publishers []publisher.Publisher

	// Runs the templates sequentially, one after another, instead of randomly.
	Sequential bool
	// Warmup is the number of seconds to run each template before its measured
	// phase, or once before a normal or random run. Samples are discarded and
	// the measured request count does not apply.
	Warmup int
}

func Execute(ctx context.Context, config ExecutionConfig) (err error) {
	if config.Warmup < 0 {
		return fmt.Errorf("warmup must be zero or greater")
	}
	if err := config.RunConfig.Validate(); err != nil {
		return err
	}
	if config.Control != nil && !config.Sequential && len(config.RunConfig.Templates) != 1 {
		return fmt.Errorf("measurement control requires sequential or single-template execution")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start a normal or random run's publishers after warmup so progress and report
	// clocks cover only measurement. Sequential runs time each phase instead.
	if !config.Sequential && config.Warmup > 0 {
		name := "query mix"
		if len(config.RunConfig.Templates) == 1 {
			name = config.RunConfig.Templates[0].Name
		}
		if err := warmupRun(runCtx, config.RunConfig, config.Warmup, name); err != nil {
			return err
		}
		if err := runCtx.Err(); err != nil {
			return err
		}
	}

	activeName := ""
	startedPublishers := make([]publisher.Publisher, 0, len(config.Publishers))
	defer func() {
		// Final reports must observe errors from every other publisher.
		var reports []publisher.Publisher
		for i := len(startedPublishers) - 1; i >= 0; i-- {
			p := startedPublishers[i]
			if _, ok := p.(interface{ ExecutionOutcome(bool, error) }); ok {
				reports = append(reports, p)
				continue
			}
			err = errors.Join(err, p.Finish())
		}
		if activeName != "" && config.Control != nil {
			for _, p := range reports {
				if timing, ok := p.(interface{ StopTiming() }); ok {
					timing.StopTiming()
				}
			}
			cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err = errors.Join(err, config.Control.exchange(cleanup, "aborted", activeName, err, 0))
			cancel()
		}
		for _, p := range reports {
			p.(interface{ ExecutionOutcome(bool, error) }).ExecutionOutcome(ctx.Err() != nil, err)
			err = errors.Join(err, p.Finish())
		}
	}()
	if !config.Sequential && config.Control != nil {
		name := config.RunConfig.Templates[0].Name
		if err := config.Control.exchange(runCtx, "ready", name, nil, 0); err != nil {
			return err
		}
		activeName = name
	}
	for _, p := range config.Publishers {
		if err := p.Start(); err != nil {
			return err
		}
		startedPublishers = append(startedPublishers, p)
	}

	if !config.Sequential {
		name := config.RunConfig.Templates[0].Name
		start := time.Now()
		phaseErr := publishRun(runCtx, config.RunConfig, startedPublishers)
		elapsed := time.Since(start)
		if config.Control != nil {
			// Close the summary clock before completion waits.
			for _, p := range startedPublishers {
				if o, ok := p.(interface{ StopTiming() }); ok {
					o.StopTiming()
				}
			}
		}
		outcome := "finished"
		if phaseErr != nil {
			outcome = "aborted"
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		controlErr := config.Control.exchange(cleanup, outcome, name, phaseErr, elapsed)
		activeName = ""
		return errors.Join(phaseErr, controlErr)
	}

	templates := config.RunConfig.Templates
	for i, tmpl := range templates {
		phase := publisher.Phase{Number: i + 1, Count: len(templates), Name: tmpl.Name}
		phaseConfig := config.RunConfig
		phaseConfig.Templates = []query.Template{tmpl}

		if config.Warmup > 0 {
			if err := warmupRun(runCtx, phaseConfig, config.Warmup, phase.Name); err != nil {
				return err
			}
		}
		if err := runCtx.Err(); err != nil {
			return err
		}

		if err := config.Control.exchange(runCtx, "ready", phase.Name, nil, 0); err != nil {
			return err
		}
		if config.Control != nil {
			activeName = phase.Name
		}
		if err := forEachPhaseObserver(startedPublishers, func(o publisher.PhaseObserver) error {
			return o.StartPhase(phase)
		}); err != nil {
			return err
		}
		start := time.Now()
		phaseErr := publishRun(runCtx, phaseConfig, startedPublishers)
		elapsed := time.Since(start)
		if phaseErr != nil && config.Control == nil {
			return phaseErr
		}
		var finishErr error
		if phaseErr == nil {
			finishErr = forEachPhaseObserver(startedPublishers, func(o publisher.PhaseObserver) error { return o.FinishPhase(phase) })
		} else {
			for _, p := range startedPublishers {
				if o, ok := p.(interface{ StopTiming() }); ok {
					o.StopTiming()
				}
			}
		}
		outcome := "finished"
		if phaseErr != nil || finishErr != nil {
			outcome = "aborted"
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		controlErr := config.Control.exchange(cleanup, outcome, phase.Name, errors.Join(phaseErr, finishErr), elapsed)
		activeName = ""
		cancel()
		if err := errors.Join(phaseErr, finishErr, controlErr); err != nil {
			return err
		}
	}

	return nil
}

// warmupRun uses the same clients, templates and load settings as measurement,
// replacing its limits with a dedicated timer and discarding all samples.
func warmupRun(ctx context.Context, config RunConfig, seconds int, name string) error {
	config.RequestsPerClient = nil
	config.BenchmarkTimeout = &seconds
	if config.Progress != "none" {
		log.Printf("Warming up %s for %d seconds with %d clients", name, seconds, config.NumClients)
	}
	if err := publishRun(ctx, config, nil); err != nil {
		return fmt.Errorf("warmup %s: %w", name, err)
	}
	return nil
}

// publishRun makes one run and hands each of its samples to the publishers.
// It returns once every sample is published, so nothing from this run can
// arrive after it.
func publishRun(ctx context.Context, config RunConfig, publishers []publisher.Publisher) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	samples := make(chan sample.Sample, config.NumClients)
	done, err := Run(runCtx, config, samples)
	if err != nil {
		return err
	}

	for s := range samples {
		if err := distributeSample(s, publishers); err != nil {
			cancel()
			for range samples {
			}
			<-done
			return err
		}
	}

	return <-done
}

func distributeSample(s sample.Sample, publishers []publisher.Publisher) error {
	for _, p := range publishers {
		if err := p.Publish(s); err != nil {
			return err
		}
	}
	return nil
}

func forEachPhaseObserver(publishers []publisher.Publisher, fn func(publisher.PhaseObserver) error) error {
	for _, p := range publishers {
		if o, ok := p.(publisher.PhaseObserver); ok {
			if err := fn(o); err != nil {
				return err
			}
		}
	}
	return nil
}
