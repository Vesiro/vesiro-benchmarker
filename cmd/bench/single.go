package main

import (
	"fmt"

	"github.com/Vesiro/vesiro-benchmarker/internal/benchmark"
)

type SingleCmd struct {
	targetFlags
	queryFlags
	QueryTemplate string `required:"" help:"Template string used to generate query bodies."`
}

func (c *SingleCmd) Run() error {
	template, err := benchmark.PrepareQuery(c.queryConfig(c.QueryTemplate))
	if err != nil {
		return fmt.Errorf("prepare query template: %w", err)
	}

	resolver, err := benchmark.NewResolver(template, benchmark.ResolveSeed(c.Seed), true)
	if err != nil {
		return fmt.Errorf("create resolver: %w", err)
	}

	client := benchmark.Client{
		HTTPClient: c.httpClient(1),
		NodeURL:    c.NodeURL,
		Index:      c.IndexName,
	}

	query := resolver.Query()

	fmt.Println("Performing single request...")
	queryJSON, err := query.Json()
	if err != nil {
		return fmt.Errorf("generate query JSON: %w", err)
	}
	fmt.Println("Query JSON:", string(queryJSON))

	ctx, cancel := newSignalContext()
	defer cancel()

	s, err := client.Search(ctx, query)
	if err != nil {
		return fmt.Errorf("perform request: %w", err)
	}

	fmt.Println(s.Query.Vars, s.ClientDuration, s.Status, string(s.Body))
	return nil
}
