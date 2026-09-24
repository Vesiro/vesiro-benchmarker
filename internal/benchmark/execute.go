package benchmark

import (
	"context"

	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
	"github.com/Vesiro/vesiro-benchmarker/internal/query"
	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

type ExecutionConfig struct {
	RunConfig  RunConfig
	Publishers []publisher.Publisher

	// Runs the templates sequentially, one after another, instead of randomly.
	Sequential bool
}

func Execute(ctx context.Context, config ExecutionConfig) (err error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	startedPublishers := make([]publisher.Publisher, 0, len(config.Publishers))
	defer func() {
		for i := len(startedPublishers) - 1; i >= 0; i-- {
			if finishErr := startedPublishers[i].Finish(); err == nil && finishErr != nil {
				err = finishErr
			}
		}
	}()
	for _, p := range config.Publishers {
		if err := p.Start(); err != nil {
			return err
		}
		startedPublishers = append(startedPublishers, p)
	}

	if !config.Sequential {
		return publishRun(runCtx, config.RunConfig, startedPublishers)
	}

	// The loop below would quietly do nothing without templates.
	if err := config.RunConfig.Validate(); err != nil {
		return err
	}

	templates := config.RunConfig.Templates
	for i, tmpl := range templates {
		phase := publisher.Phase{Number: i + 1, Count: len(templates), Name: tmpl.Name}
		phaseConfig := config.RunConfig
		phaseConfig.Templates = []query.Template{tmpl}

		if err := forEachPhaseObserver(startedPublishers, func(o publisher.PhaseObserver) error {
			return o.StartPhase(phase)
		}); err != nil {
			return err
		}
		if err := publishRun(runCtx, phaseConfig, startedPublishers); err != nil {
			return err
		}
		if err := forEachPhaseObserver(startedPublishers, func(o publisher.PhaseObserver) error {
			return o.FinishPhase(phase)
		}); err != nil {
			return err
		}
	}

	return nil
}

// publishRun makes one run and hands each of its samples to the publishers.
// It returns once every sample is published, so nothing from this run can
// arrive after it.
func publishRun(ctx context.Context, config RunConfig, publishers []publisher.Publisher) error {
	samples := make(chan sample.Sample, config.NumClients)
	done, err := Run(ctx, config, samples)
	if err != nil {
		return err
	}

	for s := range samples {
		if err := distributeSample(s, publishers); err != nil {
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
