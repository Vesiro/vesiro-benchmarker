package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Vesiro/vesiro-benchmarker/internal/benchmark"
	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
	"github.com/Vesiro/vesiro-benchmarker/internal/query"
)

type FolderCmd struct {
	benchmark.RequestOptions
	targetFlags
	queryFlags
	QueryFolder string `required:"" name:"query-folder" help:"Folder containing raw JSON query files or template files."`
}

func (c *FolderCmd) Run() error {
	templates, err := c.loadQueryFolder()
	if err != nil {
		return fmt.Errorf("prepare query folder: %w", err)
	}

	// Resolve the seed before logging, so the logged config is the one the run
	// actually used and can be replayed.
	seed := benchmark.ResolveSeed(c.Seed)
	c.Seed = &seed

	if err := logConfig(c); err != nil {
		return err
	}

	var publishers []publisher.Publisher
	publishers, err = appendProgress(publishers, c.RequestOptions)
	if err != nil {
		return err
	}
	publishers = append(publishers, newSummary(c, c.RequestOptions, templates, seed))

	ctx, cancel := newSignalContext()
	defer cancel()

	return execute(ctx, benchmark.ExecutionConfig{
		RunConfig:  newRunConfig(c.targetFlags, c.RequestOptions, templates, seed),
		Publishers: publishers,
	})
}

func (c *FolderCmd) loadQueryFolder() ([]query.Template, error) {
	entries, err := os.ReadDir(c.QueryFolder)
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	templates := make([]query.Template, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filePath := filepath.Join(c.QueryFolder, entry.Name())
		tmpl, err := benchmark.PrepareQuery(c.queryConfig(filePath))
		if err != nil {
			return nil, fmt.Errorf("prepare %s: %w", entry.Name(), err)
		}

		tmpl.Name = entry.Name()
		templates = append(templates, tmpl)
	}

	if len(templates) == 0 {
		return nil, fmt.Errorf("no query files found in %s", c.QueryFolder)
	}

	return templates, nil
}
