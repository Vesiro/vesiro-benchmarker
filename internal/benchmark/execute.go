package benchmark

import (
	"context"

	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
	"github.com/Vesiro/vesiro-benchmarker/internal/sample"
)

type ExecutionConfig struct {
	RunConfig  RunConfig
	Publishers []publisher.Publisher
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

	samples := make(chan sample.Sample, config.RunConfig.NumClients)
	done, err := Run(runCtx, config.RunConfig, samples)
	if err != nil {
		return err
	}

	for s := range samples {
		if err := distributeSample(s, startedPublishers); err != nil {
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
