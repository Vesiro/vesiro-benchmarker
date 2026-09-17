package main

import (
	"github.com/Vesiro/vesiro-benchmarker/internal/benchmark"
	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
	"github.com/Vesiro/vesiro-benchmarker/internal/query"
)

type RunCmd struct {
	benchmark.RequestOptions
	targetFlags
	queryFlags
	QueryTemplate string `required:"" help:"Template string used to generate query bodies."`
}

func (c *RunCmd) Run() error {
	seed := benchmark.ResolveSeed(c.Seed)
	c.Seed = &seed

	if err := logConfig(c); err != nil {
		return err
	}

	template, err := benchmark.PrepareQuery(c.queryConfig(c.QueryTemplate))
	if err != nil {
		return err
	}

	var publishers []publisher.Publisher
	publishers, err = appendProgress(publishers, c.RequestOptions)
	if err != nil {
		return err
	}
	publishers = append(publishers, newSummary(c, c.RequestOptions, []query.Template{template}, seed))

	ctx, cancel := newSignalContext()
	defer cancel()

	return execute(ctx, benchmark.ExecutionConfig{
		RunConfig:  newRunConfig(c.targetFlags, c.RequestOptions, []query.Template{template}, seed),
		Publishers: publishers,
	})
}
