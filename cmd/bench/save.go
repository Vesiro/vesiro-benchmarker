package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Vesiro/vesiro-benchmarker/internal/benchmark"
	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
)

// saveFlags decide where a run's report is saved. json:"-" keeps them out of
// the logged config, which describes the load rather than where results go.
type saveFlags struct {
	Save       bool   `default:"true" negatable:"" json:"-" help:"Save the report as JSON in --results-dir. Turn off with --no-save."`
	ResultsDir string `default:"results" json:"-" help:"Folder that saved reports are written to."`
	Label      string `default:"" json:"-" help:"Name for this run, added to the saved report and its file name."`
}

// executeAndSave runs the benchmark, then saves its report. A run cut short by
// an interrupt or a failure is saved too, marked as such, as long as it sent
// at least one request.
func executeAndSave(ctx context.Context, config benchmark.ExecutionConfig, summary *publisher.Summary, save saveFlags) error {
	runErr := execute(ctx, config)
	if !save.Save {
		return runErr
	}

	report := summary.Report()
	if report.Results.Requests == 0 {
		return runErr
	}
	report.Interrupted = ctx.Err() != nil
	if runErr != nil {
		report.Error = runErr.Error()
	}

	path, err := saveReport(save.ResultsDir, reportFileName(report, config.RunConfig.IndexName), report)
	if err != nil {
		return errors.Join(runErr, fmt.Errorf("save report: %w", err))
	}
	log.Printf("Saved report to %s", path)
	return runErr
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// reportFileName names a report by when it started, so a folder of them lists
// oldest first, followed by what it measured.
func reportFileName(report publisher.Report, index string) string {
	parts := []string{report.StartedAt.Local().Format("2006-01-02T15-04-05"), report.Command, index}
	if report.Label != "" {
		parts = append(parts, report.Label)
	}

	var kept []string
	for _, part := range parts {
		if part = strings.Trim(unsafeFileChars.ReplaceAllString(part, "-"), "-"); part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, "_") + ".json"
}

// saveReport writes the report into dir under name, creating dir if needed.
// It never overwrites an earlier report: a clash gets a numbered name instead.
func saveReport(dir, name string, report publisher.Report) (string, error) {
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	base := strings.TrimSuffix(name, ".json")
	for n := 1; ; n++ {
		path := filepath.Join(dir, base+".json")
		if n > 1 {
			path = filepath.Join(dir, fmt.Sprintf("%s-%d.json", base, n))
		}

		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}

		_, writeErr := f.Write(append(raw, '\n'))
		return path, errors.Join(writeErr, f.Close())
	}
}
