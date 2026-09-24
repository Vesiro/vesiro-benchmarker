package main

import (
	"fmt"
	"os"

	"github.com/mattn/go-isatty"

	"github.com/Vesiro/vesiro-benchmarker/internal/compare"
)

type CompareCmd struct {
	Baseline  string  `required:"" help:"Saved report to compare against."`
	Contender string  `required:"" help:"Saved report to compare with the baseline."`
	Threshold float64 `default:"5" help:"Changes smaller than this percentage are treated as noise and not highlighted."`
}

func (c *CompareCmd) Run() error {
	if c.Threshold < 0 {
		return fmt.Errorf("threshold must be zero or greater")
	}

	baseline, err := compare.Load(c.Baseline)
	if err != nil {
		return err
	}
	contender, err := compare.Load(c.Contender)
	if err != nil {
		return err
	}

	compare.Print(os.Stdout, baseline, contender, compare.Options{
		Threshold: c.Threshold,
		Color:     useColor(os.Stdout),
	})
	return nil
}

// useColor reports whether f is a terminal, unless the NO_COLOR convention
// (https://no-color.org) asks for plain output.
func useColor(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isatty.IsTerminal(f.Fd())
}
