package main

import (
	"fmt"

	"github.com/Vesiro/vesiro-benchmarker/internal/benchmark"
	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
	"github.com/Vesiro/vesiro-benchmarker/internal/query"
)

type RunCmd struct {
	benchmark.RequestOptions
	targetFlags
	queryFlags
	saveFlags
	QueryTemplate string `required:"" help:"Template string used to generate query bodies."`
	Warmup        int    `default:"0" help:"Warm up for this many seconds before measuring, using the configured clients. Zero disables warmup."`
}

func (c *RunCmd) Validate() error {
	if err := c.RequestOptions.Validate(); err != nil {
		return err
	}
	if c.Warmup < 0 {
		return fmt.Errorf("warmup must be zero or greater")
	}
	return nil
}

func (c *RunCmd) Run() error {
	if err := c.Validate(); err != nil {
		return err
	}
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
	summary := newSummary("run", c, c.RequestOptions, c.saveFlags, []query.Template{template}, seed)
	publishers = append(publishers, summary)

	ctx, cancel := newSignalContext()
	defer cancel()

	return executeAndSave(ctx, benchmark.ExecutionConfig{
		RunConfig:  newRunConfig(c.targetFlags, c.RequestOptions, []query.Template{template}, seed),
		Publishers: publishers,
		Warmup:     c.Warmup,
	}, summary, c.saveFlags)
}
