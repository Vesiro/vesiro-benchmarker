// Package compare sets two saved benchmark reports side by side.
package compare

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Vesiro/vesiro-benchmarker/internal/publisher"
)

// Run is a saved report and where it was read from.
type Run struct {
	Path   string
	Report publisher.Report
}

// Load reads a report saved by a benchmark run, or written by --output=json.
func Load(path string) (Run, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Run{}, err
	}

	var report publisher.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		return Run{}, fmt.Errorf("%s is not a benchmark report: %w", path, err)
	}
	return Run{Path: path, Report: report}, nil
}

type Options struct {
	// Threshold is the change, in percent, below which a difference counts as
	// run-to-run noise and is not highlighted.
	Threshold float64

	// Color highlights changes beyond the threshold, green where the
	// contender is better and red where it is worse.
	Color bool
}

const (
	colorRed   = "\x1b[31m"
	colorGreen = "\x1b[32m"
	colorReset = "\x1b[0m"
)

// ignoredConfig are settings that do not change the load, so a difference in
// them does not make two runs any less comparable.
var ignoredConfig = map[string]bool{"Seed": true, "Progress": true, "Output": true}

// Print writes the comparison of contender against baseline.
func Print(w io.Writer, baseline, contender Run, opts Options) {
	printRun(w, "Baseline: ", baseline)
	printRun(w, "Contender:", contender)
	_, _ = fmt.Fprintln(w)

	warned := printWarnings(w, "baseline", baseline.Report)
	warned = printWarnings(w, "contender", contender.Report) || warned
	if warned {
		_, _ = fmt.Fprintln(w)
	}

	printDifferences(w, baseline.Report, contender.Report)
	_, _ = fmt.Fprintln(w)

	printHeadline(w, baseline.Report.Results, contender.Report.Results, opts)
	_, _ = fmt.Fprintln(w)

	printPerQuery(w, baseline.Report.Results.PerQuery, contender.Report.Results.PerQuery, opts)

	if opts.Color {
		_, _ = fmt.Fprintf(w, "\nChanges beyond ±%g%% are marked %sgreen%s where the contender is better and %sred%s where it is worse.\n",
			opts.Threshold, colorGreen, colorReset, colorRed, colorReset)
	}
}

func printRun(w io.Writer, role string, run Run) {
	var details []string
	if run.Report.Label != "" {
		details = append(details, run.Report.Label)
	}
	if !run.Report.StartedAt.IsZero() {
		details = append(details, "started "+run.Report.StartedAt.Local().Format("2006-01-02 15:04:05"))
	}

	line := role + " " + run.Path
	if len(details) > 0 {
		line += "  (" + strings.Join(details, ", ") + ")"
	}
	_, _ = fmt.Fprintln(w, line)
}

func printWarnings(w io.Writer, role string, report publisher.Report) bool {
	warned := false
	if report.Interrupted {
		_, _ = fmt.Fprintf(w, "Warning: the %s was interrupted, so it covers only part of its run.\n", role)
		warned = true
	}
	if report.Error != "" {
		_, _ = fmt.Fprintf(w, "Warning: the %s stopped early with an error: %s\n", role, report.Error)
		warned = true
	}
	return warned
}

// printDifferences lists the settings that differ between the runs, which is
// what decides whether their numbers can be compared at all.
func printDifferences(w io.Writer, baseline, contender publisher.Report) {
	type difference struct{ name, from, to string }
	var diffs []difference

	if baseline.Command != contender.Command {
		diffs = append(diffs, difference{"command", baseline.Command, contender.Command})
	}
	if baseline.Version != contender.Version {
		diffs = append(diffs, difference{"version", baseline.Version, contender.Version})
	}

	from, to := configValues(baseline.Config), configValues(contender.Config)
	keys := make([]string, 0, len(from)+len(to))
	for key := range from {
		keys = append(keys, key)
	}
	for key := range to {
		if _, ok := from[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		if ignoredConfig[key] || from[key] == to[key] {
			continue
		}
		diffs = append(diffs, difference{key, orUnset(from[key]), orUnset(to[key])})
	}

	if len(diffs) == 0 {
		_, _ = fmt.Fprintln(w, "Config differences: none")
		return
	}

	_, _ = fmt.Fprintln(w, "Config differences:")
	rows := make([][]cell, 0, len(diffs))
	for _, d := range diffs {
		rows = append(rows, []cell{{text: d.name}, {text: d.from + " → " + d.to, left: true}})
	}
	writeTable(w, rows, false)
}

// configValues flattens a report's config to one JSON-encoded value per
// setting, so values of any type compare as text.
func configValues(config any) map[string]string {
	fields, _ := config.(map[string]any)
	values := make(map[string]string, len(fields))
	for key, value := range fields {
		raw, _ := json.Marshal(value)
		values[key] = string(raw)
	}
	return values
}

func orUnset(value string) string {
	if value == "" {
		return "(unset)"
	}
	return value
}

func printHeadline(w io.Writer, baseline, contender publisher.Results, opts Options) {
	type metric struct {
		name   string
		from   float64
		to     float64
		better direction
	}
	metrics := []metric{
		{"q/s", baseline.QPS, contender.QPS, higherIsBetter},
		{"Took Avg ms", baseline.TookAvgMs, contender.TookAvgMs, lowerIsBetter},
		{"Avg ms", baseline.ClientLatencyMs.Avg, contender.ClientLatencyMs.Avg, lowerIsBetter},
		{"p50 ms", baseline.ClientLatencyMs.P50, contender.ClientLatencyMs.P50, lowerIsBetter},
		{"p95 ms", baseline.ClientLatencyMs.P95, contender.ClientLatencyMs.P95, lowerIsBetter},
		{"p99 ms", baseline.ClientLatencyMs.P99, contender.ClientLatencyMs.P99, lowerIsBetter},
	}

	rows := [][]cell{
		{{}, {text: "Baseline"}, {text: "Contender"}, {text: "Change"}},
		{{text: "Requests"}, {text: fmt.Sprint(baseline.Requests)}, {text: fmt.Sprint(contender.Requests)}, {}},
		{
			{text: "Errors"},
			{text: fmt.Sprintf("%d (%.2f%%)", baseline.Errors, baseline.ErrorRate*100)},
			{text: fmt.Sprintf("%d (%.2f%%)", contender.Errors, contender.ErrorRate*100), color: errorColor(baseline.ErrorRate, contender.ErrorRate)},
			{},
		},
	}
	for _, m := range metrics {
		rows = append(rows, []cell{
			{text: m.name},
			{text: fmt.Sprintf("%.2f", m.from)},
			{text: fmt.Sprintf("%.2f", m.to)},
			changeCell(m.from, m.to, m.better, opts),
		})
	}
	writeTable(w, rows, opts.Color)
}

// printPerQuery shows each query's values in both runs and how much they
// changed. Every metric is one column of "baseline → contender  change", with
// its parts lined up across rows so a long list can be scanned for what moved.
func printPerQuery(w io.Writer, baseline, contender []publisher.PerQueryResult, opts Options) {
	contenderByName := make(map[string]publisher.PerQueryResult, len(contender))
	for _, q := range contender {
		contenderByName[q.Name] = q
	}

	type pair struct{ from, to publisher.PerQueryResult }
	var pairs []pair
	var onlyBaseline []string
	matched := make(map[string]bool, len(baseline))
	for _, from := range baseline {
		to, ok := contenderByName[from.Name]
		if !ok {
			onlyBaseline = append(onlyBaseline, from.Name)
			continue
		}
		matched[from.Name] = true
		pairs = append(pairs, pair{from, to})
	}

	var onlyContender []string
	for _, to := range contender {
		if !matched[to.Name] {
			onlyContender = append(onlyContender, to.Name)
		}
	}

	type metric struct {
		name   string
		better direction
		value  func(publisher.PerQueryResult) float64
	}
	metrics := []metric{
		{"q/s", higherIsBetter, func(q publisher.PerQueryResult) float64 { return q.QPS }},
		{"Avg", lowerIsBetter, func(q publisher.PerQueryResult) float64 { return q.ClientLatencyMs.Avg }},
		{"p50", lowerIsBetter, func(q publisher.PerQueryResult) float64 { return q.ClientLatencyMs.P50 }},
		{"p95", lowerIsBetter, func(q publisher.PerQueryResult) float64 { return q.ClientLatencyMs.P95 }},
		{"p99", lowerIsBetter, func(q publisher.PerQueryResult) float64 { return q.ClientLatencyMs.P99 }},
	}
	// Queries in a mixed run have no rate of their own. When neither run has
	// any, a column of dashes would only take up room.
	hasRate := false
	for _, p := range pairs {
		hasRate = hasRate || p.from.QPS > 0 || p.to.QPS > 0
	}
	if !hasRate {
		metrics = metrics[1:]
	}

	// Measure every part first, so each metric's parts can line up.
	type value struct {
		from, to string
		change   cell
	}
	values := make([][]value, len(pairs))
	widths := make([][3]int, len(metrics))
	for i, p := range pairs {
		for j, m := range metrics {
			from, to := m.value(p.from), m.value(p.to)
			v := value{formatValue(from), formatValue(to), changeCell(from, to, m.better, opts)}
			values[i] = append(values[i], v)
			widths[j] = [3]int{
				max(widths[j][0], utf8.RuneCountInString(v.from)),
				max(widths[j][1], utf8.RuneCountInString(v.to)),
				max(widths[j][2], v.change.width()),
			}
		}
	}

	header := []cell{{text: "Query"}, {text: "Errors"}}
	for _, m := range metrics {
		header = append(header, cell{text: m.name, left: true})
	}
	rows := [][]cell{header}
	for i, p := range pairs {
		row := []cell{{text: p.from.Name}, errorCountCell(p.from.Errors, p.to.Errors)}
		for j, v := range values[i] {
			width := widths[j]
			row = append(row, cell{parts: []cell{
				{text: fmt.Sprintf("%*s → %*s  %*s", width[0], v.from, width[1], v.to, width[2]-v.change.width(), "")},
				v.change,
			}})
		}
		rows = append(rows, row)
	}

	if len(pairs) > 0 {
		_, _ = fmt.Fprintln(w, "Per Query (baseline → contender, latency in ms):")
		writeTable(w, rows, opts.Color)
	} else {
		_, _ = fmt.Fprintln(w, "Per Query: no queries in common")
	}
	if len(onlyBaseline) > 0 {
		_, _ = fmt.Fprintf(w, "Only in baseline:  %s\n", strings.Join(onlyBaseline, ", "))
	}
	if len(onlyContender) > 0 {
		_, _ = fmt.Fprintf(w, "Only in contender: %s\n", strings.Join(onlyContender, ", "))
	}
}

// formatValue shows a measured value, or a dash for one that was not measured.
func formatValue(v float64) string {
	if v <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.2f", v)
}

func errorCountCell(from, to int) cell {
	if from == to {
		return cell{text: fmt.Sprint(from)}
	}
	return cell{
		text:  fmt.Sprintf("%d → %d", from, to),
		color: errorColor(float64(from), float64(to)),
	}
}

type direction int

const (
	lowerIsBetter direction = iota
	higherIsBetter
)

// changeCell shows the change from one value to the next as a percentage. A
// value of zero means it was not measured, such as the rate of a query that
// shared its run with others, so there is no change to show.
func changeCell(from, to float64, better direction, opts Options) cell {
	if from <= 0 || to <= 0 {
		return cell{text: "-"}
	}

	change := (to - from) / from * 100
	c := cell{text: fmt.Sprintf("%+.1f%%", change)}
	if math.Abs(change) < 0.05 {
		// Too small to show, and "-0.0%" would suggest a change anyway.
		c.text = "0.0%"
	}
	if math.Abs(change) >= opts.Threshold {
		if (change > 0) == (better == higherIsBetter) {
			c.color = colorGreen
		} else {
			c.color = colorRed
		}
	}
	return c
}

func errorColor(from, to float64) string {
	switch {
	case to > from:
		return colorRed
	case to < from:
		return colorGreen
	default:
		return ""
	}
}

type cell struct {
	text  string
	color string
	// left aligns the cell to the left. The first column always is.
	left bool
	// parts, when set, stand in for text and color: the cell is these pieces
	// side by side, each in its own color.
	parts []cell
}

func (c cell) width() int {
	if c.parts == nil {
		return utf8.RuneCountInString(c.text)
	}
	width := 0
	for _, part := range c.parts {
		width += part.width()
	}
	return width
}

func (c cell) render(useColor bool) string {
	if c.parts != nil {
		var out strings.Builder
		for _, part := range c.parts {
			out.WriteString(part.render(useColor))
		}
		return out.String()
	}
	if useColor && c.color != "" {
		return c.color + c.text + colorReset
	}
	return c.text
}

// writeTable prints rows indented by two spaces, with columns two spaces
// apart. Widths are measured on the plain text, so color codes never push the
// columns out of line.
func writeTable(w io.Writer, rows [][]cell, useColor bool) {
	var widths []int
	for _, row := range rows {
		for i, c := range row {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], c.width())
		}
	}

	for _, row := range rows {
		var line strings.Builder
		line.WriteString("  ")
		for i, c := range row {
			if i > 0 {
				line.WriteString("  ")
			}
			pad := strings.Repeat(" ", widths[i]-c.width())
			text := c.render(useColor)
			if i == 0 || c.left {
				line.WriteString(text + pad)
			} else {
				line.WriteString(pad + text)
			}
		}
		_, _ = fmt.Fprintln(w, strings.TrimRight(line.String(), " "))
	}
}
