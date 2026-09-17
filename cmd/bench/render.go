package main

import (
	"fmt"

	"github.com/Vesiro/vesiro-benchmarker/internal/benchmark"
)

type RenderCmd struct {
	queryFlags
	QueryTemplate string `required:"" help:"Template to render into a concrete query body."`
	Count         int    `default:"1" help:"Number of queries to render, to show how bindings vary."`
}

func (c *RenderCmd) Run() error {
	if c.Count <= 0 {
		return fmt.Errorf("count must be greater than zero")
	}

	// Render through the same path a benchmark uses, so the output is exactly
	// what would go over the wire.
	template, err := benchmark.PrepareQuery(c.queryConfig(c.QueryTemplate))
	if err != nil {
		return fmt.Errorf("prepare query template: %w", err)
	}

	resolver, err := benchmark.NewResolver(template, benchmark.ResolveSeed(c.Seed), false)
	if err != nil {
		return fmt.Errorf("create resolver: %w", err)
	}

	for i := 0; i < c.Count; i++ {
		query := resolver.Query()
		raw, err := query.Json()
		if err != nil {
			return fmt.Errorf("render query %d: %w", i+1, err)
		}
		if c.Count > 1 {
			fmt.Printf("--- query %d ---\n", i+1)
		}
		fmt.Println(query.Vars)
		fmt.Println(string(raw))
	}

	return nil
}
