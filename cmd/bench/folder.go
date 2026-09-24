package main

import (
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/Vesiro/vesiro-benchmarker/internal/benchmark"
	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
	"github.com/Vesiro/vesiro-benchmarker/internal/query"
)

type FolderCmd struct {
	benchmark.RequestOptions
	targetFlags
	queryFlags
	QueryFolder string `required:"" name:"query-folder" help:"Folder containing raw JSON query files or template files. Subfolders are included."`
	Random      bool   `default:"false" help:"Pick a query file at random for each request instead of running them one at a time. The request count or benchmark timeout then applies to the whole run, not to each file."`
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

	summary := newSummary(c, c.RequestOptions, templates, seed)

	var publishers []publisher.Publisher
	if c.Random {
		publishers, err = appendProgress(publishers, c.RequestOptions)
		if err != nil {
			return err
		}
		publishers = append(publishers, summary)
	} else {
		// Each template gets its own progress bar, wiped once its result line
		// is printed. The phase report goes last so it finishes first, which
		// puts the line for a template cut short above the final report.
		progress := progressConfig(c.RequestOptions)
		progress.CleanOnFinish = true
		publishers = append(publishers, summary, &publisher.PhaseReport{
			Progress:  progress,
			NameWidth: longestName(templates),
		})
	}

	ctx, cancel := newSignalContext()
	defer cancel()

	return execute(ctx, benchmark.ExecutionConfig{
		RunConfig:  newRunConfig(c.targetFlags, c.RequestOptions, templates, seed),
		Publishers: publishers,
		Sequential: !c.Random,
	})
}

func (c *FolderCmd) loadQueryFolder() ([]query.Template, error) {
	files, err := c.queryFiles(".")
	if err != nil {
		return nil, err
	}

	templates := make([]query.Template, 0, len(files))
	for _, rel := range files {
		tmpl, err := benchmark.PrepareQuery(c.queryConfig(filepath.Join(c.QueryFolder, rel)))
		if err != nil {
			return nil, fmt.Errorf("prepare %s: %w", rel, err)
		}

		// Name by path relative to the query folder, so same-named files in
		// different subfolders stay separate in the per-query report.
		tmpl.Name = filepath.ToSlash(rel)
		templates = append(templates, tmpl)
	}

	if len(templates) == 0 {
		return nil, fmt.Errorf("no query files found in %s", c.QueryFolder)
	}

	return templates, nil
}

// queryFiles lists the files in dir and all its subfolders, relative to the
// query folder. The order is fixed (by name, each subfolder in place), so a seed
// replays the same run.
func (c *FolderCmd) queryFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(c.QueryFolder, dir))
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		rel := filepath.Join(dir, entry.Name())
		if !entry.IsDir() {
			files = append(files, rel)
			continue
		}

		nested, err := c.queryFiles(rel)
		if err != nil {
			return nil, err
		}
		files = append(files, nested...)
	}

	return files, nil
}

func longestName(templates []query.Template) int {
	width := 0
	for _, tmpl := range templates {
		width = max(width, utf8.RuneCountInString(tmpl.Name))
	}
	return width
}
