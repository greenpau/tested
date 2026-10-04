// Copyright 2026 Paul Greenberg greenpau@outlook.com
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package report

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/greenpau/tested/pkg/result"
)

// ConsoleFormat selects a live/final console projection.
type ConsoleFormat string

const (
	// ConsolePlain writes terminal-oriented text.
	ConsolePlain ConsoleFormat = "plain"
	// ConsoleMarkdown writes portable Markdown.
	ConsoleMarkdown ConsoleFormat = "markdown"
	// ConsoleJSON writes one complete final summary and no live events.
	ConsoleJSON ConsoleFormat = "json"
)

// ColorMode selects trusted ANSI status coloring for plain output.
type ColorMode string

const (
	// ColorAuto enables color only for a terminal when NO_COLOR is absent.
	ColorAuto ColorMode = "auto"
	// ColorAlways explicitly enables trusted renderer color.
	ColorAlways ColorMode = "always"
	// ColorNever disables color.
	ColorNever ColorMode = "never"
)

// ConsoleOptions configures live and final result rendering.
type ConsoleOptions struct {
	Writer                 io.Writer
	Title                  string
	Format                 ConsoleFormat
	Color                  ColorMode
	Quiet                  bool
	Slowest                int
	RedactPatterns         []string
	MaxFailureExcerptBytes int
	IsTerminal             func(io.Writer) bool
	LookupEnv              func(string) (string, bool)
}

// Console renders concurrency-safe live and final result output.
type Console struct {
	mu             sync.Mutex
	writer         io.Writer
	format         ConsoleFormat
	color          bool
	quiet          bool
	renderer       *Renderer
	liveBytes      int
	liveLimited    bool
	logsOmitted    bool
	outputClipped  bool
	livePackage    string
	livePackageSet bool
}

// NewConsole validates options and creates a console renderer.
func NewConsole(options ConsoleOptions) (*Console, error) {
	if options.Writer == nil {
		return nil, errors.New("create report console: writer is nil")
	}
	format := options.Format
	if format == "" {
		format = ConsolePlain
	}
	switch format {
	case ConsolePlain, ConsoleMarkdown, ConsoleJSON:
	default:
		return nil, fmt.Errorf("create report console: unsupported format %q", format)
	}
	colorMode := options.Color
	if colorMode == "" {
		colorMode = ColorAuto
	}
	switch colorMode {
	case ColorAuto, ColorAlways, ColorNever:
	default:
		return nil, fmt.Errorf("create report console: unsupported color mode %q", colorMode)
	}

	renderer, err := New(Options{
		Title:                  options.Title,
		RedactPatterns:         options.RedactPatterns,
		Slowest:                options.Slowest,
		MaxFailureExcerptBytes: options.MaxFailureExcerptBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("create report console: %w", err)
	}

	isTerminal := options.IsTerminal
	if isTerminal == nil {
		isTerminal = defaultIsTerminal
	}
	lookupEnv := options.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	_, noColor := lookupEnv("NO_COLOR")
	color := colorMode == ColorAlways ||
		(colorMode == ColorAuto && !noColor && isTerminal(options.Writer))
	if format != ConsolePlain {
		color = false
	}
	return &Console{
		writer:   options.Writer,
		format:   format,
		color:    color,
		quiet:    options.Quiet,
		renderer: renderer,
	}, nil
}

// Package writes one live package completion projection. Quiet and ConsoleJSON
// suppress live package output but never suppress Final.
func (c *Console) Package(pkg result.Package) error {
	if c == nil {
		return errors.New("render console package: console is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.quiet || c.format == ConsoleJSON {
		return nil
	}
	c.livePackageSet = false

	name := c.renderer.redact(pkg.Name)
	status := string(pkg.Status)
	counts := countTests(pkg.Tests)
	switch c.format {
	case ConsoleMarkdown:
		line := fmt.Sprintf(
			"- **%s** %s%s — %d tests, %d failed, %d skipped, %d benchmarked, %d incomplete\n",
			escapeMarkdown(strings.ToUpper(status)),
			escapeMarkdown(name),
			markdownDuration(pkg),
			counts.Total,
			counts.Failed,
			counts.Skipped,
			counts.Benchmarked,
			counts.Incomplete,
		)
		return writeConsole(c.writer, line)
	default:
		duration := " duration unavailable"
		if knownDuration(pkg.Elapsed, pkg.DurationSource) {
			duration = " " + pkg.Elapsed.String()
			if pkg.DurationSource != "" {
				duration += " (" + neutralizeTerminal(string(pkg.DurationSource)) + ")"
			}
		} else if pkg.DurationSource != "" {
			duration += " (" + neutralizeTerminal(string(pkg.DurationSource)) + ")"
		}
		line := fmt.Sprintf(
			"[%s] %s%s — %d tests, %d failed, %d skipped, %d benchmarked, %d incomplete\n",
			c.statusText(status),
			neutralizeTerminalInline(name),
			duration,
			counts.Total,
			counts.Failed,
			counts.Skipped,
			counts.Benchmarked,
			counts.Incomplete,
		)
		return writeConsole(c.writer, line)
	}
}

// Final writes the final plain, Markdown, or summary JSON projection.
func (c *Console) Final(input Input) error {
	if c == nil {
		return errors.New("render final console: console is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.livePackageSet = false
	if c.format == ConsoleJSON {
		return c.renderer.RenderSummaryJSON(c.writer, input)
	}
	view := c.renderer.buildView(input)
	if c.format == ConsoleMarkdown {
		return c.writeMarkdownFinal(view, input)
	}
	return c.writePlainFinal(view, input)
}

func (c *Console) writePlainFinal(view reportView, input Input) error {
	var output bytes.Buffer
	fmt.Fprintf(&output, "%s\n", neutralizeTerminalInline(view.Title))
	fmt.Fprintf(&output, "Outcome: %s\n", c.statusText(view.Outcome))
	output.WriteString("Child exit: ")
	if view.Assessment != nil && !view.Assessment.ChildExitKnown {
		output.WriteString("unavailable")
	} else {
		fmt.Fprintf(&output, "%d", view.Metadata.ExitCode)
		if view.Assessment != nil && view.Assessment.ChildExitInferred {
			output.WriteString(" (inferred)")
		}
	}
	if view.Metadata.Signal != "" {
		fmt.Fprintf(
			&output,
			" (signal %s)",
			neutralizeTerminalInline(view.Metadata.Signal),
		)
	}
	if view.Metadata.Interrupted {
		output.WriteString(" (cancelled")
		if view.Metadata.Cancellation != "" {
			fmt.Fprintf(
				&output,
				": %s",
				neutralizeTerminalInline(view.Metadata.Cancellation),
			)
			if view.Metadata.CancellationSignal != "" {
				fmt.Fprintf(
					&output,
					" %s",
					neutralizeTerminalInline(view.Metadata.CancellationSignal),
				)
			}
			if view.Metadata.RecommendedExitCode > 0 {
				fmt.Fprintf(
					&output,
					", projected exit %d",
					view.Metadata.RecommendedExitCode,
				)
			}
		}
		output.WriteByte(')')
	}
	output.WriteByte('\n')
	fmt.Fprintf(
		&output,
		"Tests: %d total, %d passed, %d failed, %d skipped, %d benchmarked, %d incomplete\n",
		view.Summary.Tests.Total,
		view.Summary.Tests.Passed,
		view.Summary.Tests.Failed,
		view.Summary.Tests.Skipped,
		view.Summary.Tests.Benchmarked,
		view.Summary.Tests.Incomplete,
	)
	if view.Coverage == nil {
		output.WriteString("Weighted profile coverage: unavailable\n")
	} else {
		fmt.Fprintf(
			&output,
			"Weighted profile coverage: %s (%d/%d statements, mode %s)\n",
			view.Coverage.Percentage,
			view.Coverage.Covered,
			view.Coverage.Statements,
			neutralizeTerminal(view.Coverage.Mode),
		)
	}
	writePlainResourceUsage(&output, view.Summary)
	writePlainAssessment(&output, view.Assessment)
	writePlainUnattributedOutput(&output, view.UnattributedOutput)
	writePlainDiagnostics(&output, view)
	document := c.renderer.buildSummary(input)
	if len(document.Failures) > 0 {
		output.WriteString("Failures:\n")
		for _, failure := range document.Failures {
			fmt.Fprintf(
				&output,
				"  - %s %s: %s\n",
				neutralizeTerminalInline(failure.Scope),
				neutralizeTerminalInline(failureLabel(failure)),
				neutralizeTerminalInline(failure.Status),
			)
			if failure.Reason != "" {
				fmt.Fprintf(&output, "    %s\n", neutralizeTerminal(failure.Reason))
			}
			if failure.Excerpt != "" {
				fmt.Fprintf(&output, "    %s\n", indentText(neutralizeTerminal(failure.Excerpt), "    "))
			}
			if failure.ExcerptTruncated {
				output.WriteString("    [output truncated; inspect test_output.jsonl]\n")
			}
		}
	}
	writePlainSlowest(&output, view.Slowest)
	return writeConsole(c.writer, output.String())
}

func (c *Console) writeMarkdownFinal(view reportView, input Input) error {
	var output bytes.Buffer
	fmt.Fprintf(&output, "# %s\n\n", escapeMarkdown(view.Title))
	fmt.Fprintf(&output, "Outcome: **%s**\n\n", escapeMarkdown(view.Outcome))
	output.WriteString("| Metric | Value |\n| --- | ---: |\n")
	if view.Assessment != nil && !view.Assessment.ChildExitKnown {
		output.WriteString("| Child exit | unavailable |\n")
	} else {
		exit := strconv.Itoa(view.Metadata.ExitCode)
		if view.Assessment != nil && view.Assessment.ChildExitInferred {
			exit += " (inferred)"
		}
		fmt.Fprintf(&output, "| Child exit | %s |\n", escapeMarkdown(exit))
	}
	if view.Metadata.Signal != "" {
		fmt.Fprintf(&output, "| Signal | %s |\n", escapeMarkdown(view.Metadata.Signal))
	}
	if view.Metadata.Interrupted {
		output.WriteString("| Interrupted | yes |\n")
		if view.Metadata.Cancellation != "" {
			fmt.Fprintf(
				&output,
				"| Cancellation | %s |\n",
				escapeMarkdown(view.Metadata.Cancellation),
			)
		}
		if view.Metadata.CancellationSignal != "" {
			fmt.Fprintf(
				&output,
				"| Cancellation signal | %s |\n",
				escapeMarkdown(view.Metadata.CancellationSignal),
			)
		}
		if view.Metadata.RecommendedExitCode > 0 {
			fmt.Fprintf(
				&output,
				"| Projected exit | %d |\n",
				view.Metadata.RecommendedExitCode,
			)
		}
	}
	fmt.Fprintf(&output, "| Tests | %d |\n", view.Summary.Tests.Total)
	fmt.Fprintf(&output, "| Passed | %d |\n", view.Summary.Tests.Passed)
	fmt.Fprintf(&output, "| Failed | %d |\n", view.Summary.Tests.Failed)
	fmt.Fprintf(&output, "| Skipped | %d |\n", view.Summary.Tests.Skipped)
	fmt.Fprintf(&output, "| Benchmarked | %d |\n", view.Summary.Tests.Benchmarked)
	fmt.Fprintf(&output, "| Incomplete | %d |\n", view.Summary.Tests.Incomplete)
	if view.Coverage == nil {
		output.WriteString("| Weighted profile coverage | unavailable |\n")
	} else {
		fmt.Fprintf(
			&output,
			"| Weighted profile coverage | %s \\(%d/%d statements\\) |\n",
			escapeMarkdown(view.Coverage.Percentage),
			view.Coverage.Covered,
			view.Coverage.Statements,
		)
	}
	writeMarkdownResourceUsage(&output, view.Summary)
	writeMarkdownAssessment(&output, view.Assessment)
	writeMarkdownUnattributedOutput(&output, view.UnattributedOutput)
	writeMarkdownDiagnostics(&output, view)
	document := c.renderer.buildSummary(input)
	if len(document.Failures) > 0 {
		output.WriteString("\n## Failures\n\n")
		for _, failure := range document.Failures {
			fmt.Fprintf(
				&output,
				"- **%s** %s — %s\n",
				escapeMarkdown(failure.Scope),
				escapeMarkdown(failureLabel(failure)),
				escapeMarkdown(failure.Status),
			)
			if failure.Reason != "" {
				fmt.Fprintf(&output, "  - %s\n", escapeMarkdown(failure.Reason))
			}
			if failure.Excerpt != "" {
				output.WriteByte('\n')
				lines := strings.Split(failure.Excerpt, "\n")
				for _, line := range lines {
					if line == "" {
						output.WriteString("  >\n")
						continue
					}
					fmt.Fprintf(&output, "  > %s\n", escapeMarkdown(line))
				}
			}
			if failure.ExcerptTruncated {
				output.WriteString("  - Output truncated; inspect `test_output.jsonl`.\n")
			}
		}
	}
	if len(view.Slowest) > 0 {
		output.WriteString("\n## Slowest occurrences\n\n")
		output.WriteString("| Package | Occurrence | Duration | Source |\n| --- | --- | ---: | --- |\n")
		var previousPackage string
		for i, slow := range view.Slowest {
			name := escapeMarkdown(slow.Package)
			if i > 0 && slow.Package == previousPackage {
				name = "↳"
			}
			fmt.Fprintf(
				&output,
				"| %s | %s | %s | %s |\n",
				name,
				escapeMarkdown(slow.Label),
				escapeMarkdown(slow.Duration),
				escapeMarkdown(slow.DurationSource),
			)
			previousPackage = slow.Package
		}
	}
	return writeConsole(c.writer, output.String())
}

func writePlainResourceUsage(output *bytes.Buffer, summary result.Summary) {
	if summary.TotalOutputTruncated {
		fmt.Fprintf(
			output,
			"Derived output: retained %d of %d bytes (truncated; raw JSONL is authoritative)\n",
			summary.TotalOutputRetainedBytes,
			summary.TotalOutputBytes,
		)
	}
	if summary.NormalizedEntriesTruncated {
		meaning := "presentation entries omitted; raw JSONL is authoritative"
		if summary.Incomplete {
			meaning = "semantic result incomplete"
		}
		fmt.Fprintf(
			output,
			"Normalized entries: retained %d of %d (%s)\n",
			summary.NormalizedEntriesRetained,
			summary.NormalizedEntries,
			meaning,
		)
	}
	if summary.NormalizedBytesTruncated {
		meaning := "presentation strings omitted; raw JSONL is authoritative"
		if summary.Incomplete {
			meaning = "semantic result incomplete"
		}
		fmt.Fprintf(
			output,
			"Normalized strings: retained %d of %d bytes (%s)\n",
			summary.NormalizedBytesRetained,
			summary.NormalizedBytes,
			meaning,
		)
	}
}

func writeMarkdownResourceUsage(output *bytes.Buffer, summary result.Summary) {
	if summary.TotalOutputTruncated {
		fmt.Fprintf(
			output,
			"| Derived output bytes | %d of %d retained; raw JSONL is authoritative |\n",
			summary.TotalOutputRetainedBytes,
			summary.TotalOutputBytes,
		)
	}
	if summary.NormalizedEntriesTruncated {
		meaning := "presentation entries omitted; raw JSONL is authoritative"
		if summary.Incomplete {
			meaning = "semantic result incomplete"
		}
		fmt.Fprintf(
			output,
			"| Normalized entries | %d of %d retained; %s |\n",
			summary.NormalizedEntriesRetained,
			summary.NormalizedEntries,
			meaning,
		)
	}
	if summary.NormalizedBytesTruncated {
		meaning := "presentation strings omitted; raw JSONL is authoritative"
		if summary.Incomplete {
			meaning = "semantic result incomplete"
		}
		fmt.Fprintf(
			output,
			"| Normalized string bytes | %d of %d retained; %s |\n",
			summary.NormalizedBytesRetained,
			summary.NormalizedBytes,
			meaning,
		)
	}
}

func (c *Console) statusText(status string) string {
	text := strings.ToUpper(neutralizeTerminal(status))
	if !c.color {
		return text
	}
	switch status {
	case string(result.StatusPassed), string(result.StatusBenchmarked):
		return "\x1b[32m" + text + "\x1b[0m"
	case string(result.StatusFailed), "coverage_failed":
		return "\x1b[31m" + text + "\x1b[0m"
	case string(result.StatusSkipped):
		return "\x1b[33m" + text + "\x1b[0m"
	default:
		return "\x1b[36m" + text + "\x1b[0m"
	}
}

func writePlainAssessment(output *bytes.Buffer, assessment *assessmentView) {
	if output == nil || assessment == nil {
		return
	}
	if assessment.CoveragePolicy != nil {
		policy := assessment.CoveragePolicy
		status := "not satisfied"
		if policy.Satisfied {
			status = "satisfied"
		}
		fmt.Fprintf(
			output,
			"Coverage policy: minimum %s%%, actual %s (%s)\n",
			neutralizeTerminalInline(policy.Minimum),
			neutralizeTerminalInline(policy.ActualPercentage),
			status,
		)
	}
	if len(assessment.Issues) > 0 {
		output.WriteString("Run issues:\n")
		for _, issue := range assessment.Issues {
			suffix := ""
			if issue.Fatal {
				suffix = " [fatal]"
			}
			fmt.Fprintf(
				output,
				"  - %s: %s%s\n",
				neutralizeTerminalInline(issue.Kind),
				neutralizeTerminalInline(issue.Message),
				suffix,
			)
		}
	}
}

func writeMarkdownAssessment(output *bytes.Buffer, assessment *assessmentView) {
	if output == nil || assessment == nil {
		return
	}
	if assessment.CoveragePolicy != nil {
		policy := assessment.CoveragePolicy
		status := "not satisfied"
		if policy.Satisfied {
			status = "satisfied"
		}
		fmt.Fprintf(
			output,
			"| Coverage policy | minimum %s%%; actual %s; %s |\n",
			escapeMarkdown(policy.Minimum),
			escapeMarkdown(policy.ActualPercentage),
			status,
		)
	}
	if len(assessment.Issues) > 0 {
		output.WriteString("\n## Run issues\n\n")
		for _, issue := range assessment.Issues {
			fatal := ""
			if issue.Fatal {
				fatal = " (fatal)"
			}
			fmt.Fprintf(
				output,
				"- **%s%s:** %s\n",
				escapeMarkdown(issue.Kind),
				fatal,
				escapeMarkdown(issue.Message),
			)
		}
	}
}

func countTests(tests []result.TestOccurrence) result.OutcomeCounts {
	var counts result.OutcomeCounts
	for _, test := range tests {
		counts.Total++
		switch test.Status {
		case result.StatusPassed:
			counts.Passed++
		case result.StatusFailed:
			counts.Failed++
		case result.StatusSkipped:
			counts.Skipped++
		case result.StatusBenchmarked:
			counts.Benchmarked++
		case result.StatusIncomplete:
			counts.Incomplete++
		case result.StatusRunning:
			counts.Running++
		case result.StatusPaused:
			counts.Paused++
		default:
			counts.Unknown++
		}
	}
	return counts
}

func markdownDuration(pkg result.Package) string {
	if !knownDuration(pkg.Elapsed, pkg.DurationSource) {
		value := " · duration unavailable"
		if pkg.DurationSource != "" {
			value += " \\(" + escapeMarkdown(string(pkg.DurationSource)) + "\\)"
		}
		return value
	}
	value := " · " + escapeMarkdown(pkg.Elapsed.String())
	if pkg.DurationSource != "" {
		value += " \\(" + escapeMarkdown(string(pkg.DurationSource)) + "\\)"
	}
	return value
}

func writePlainUnattributedOutput(
	output *bytes.Buffer,
	unattributed *unattributedOutputView,
) {
	if output == nil || unattributed == nil {
		return
	}
	output.WriteString("Unattributed test output:\n")
	if unattributed.Output != "" {
		fmt.Fprintf(
			output,
			"  %s\n",
			indentText(neutralizeTerminal(unattributed.Output), "  "),
		)
	}
	fmt.Fprintf(
		output,
		"  [retained %d of %d bytes",
		unattributed.OutputRetainedBytes,
		unattributed.OutputBytes,
	)
	if unattributed.OutputTruncated {
		output.WriteString("; truncated, inspect test_output.jsonl")
	}
	output.WriteString("]\n")
}

func writePlainDiagnostics(output *bytes.Buffer, view reportView) {
	if output == nil ||
		(view.DiagnosticCount == 0 &&
			len(view.Diagnostics) == 0 &&
			!view.DiagnosticsTruncated) {
		return
	}
	fmt.Fprintf(
		output,
		"Integrity diagnostics: %d retained of %d",
		view.DiagnosticsRetained,
		view.DiagnosticCount,
	)
	if view.DiagnosticsTruncated {
		output.WriteString(" [diagnostic list truncated]")
	}
	output.WriteByte('\n')
	for _, diagnostic := range view.Diagnostics {
		fmt.Fprintf(
			output,
			"  - %s record %d, line %d: %s\n",
			neutralizeTerminalInline(diagnostic.Kind),
			diagnostic.Sequence,
			diagnostic.Line,
			neutralizeTerminalInline(diagnostic.Message),
		)
		if diagnostic.Preview != "" {
			fmt.Fprintf(
				output,
				"    %s\n",
				indentText(neutralizeTerminal(diagnostic.Preview), "    "),
			)
		}
	}
}

func writeMarkdownUnattributedOutput(
	output *bytes.Buffer,
	unattributed *unattributedOutputView,
) {
	if output == nil || unattributed == nil {
		return
	}
	output.WriteString("\n## Unattributed test output\n\n")
	if unattributed.Output != "" {
		for _, line := range strings.Split(unattributed.Output, "\n") {
			if line == "" {
				output.WriteString(">\n")
				continue
			}
			fmt.Fprintf(output, "> %s\n", escapeMarkdown(line))
		}
		output.WriteByte('\n')
	}
	fmt.Fprintf(
		output,
		"Retained %d of %d bytes",
		unattributed.OutputRetainedBytes,
		unattributed.OutputBytes,
	)
	if unattributed.OutputTruncated {
		output.WriteString("; output truncated, inspect `test_output.jsonl`")
	}
	output.WriteString(".\n")
}

func writeMarkdownDiagnostics(output *bytes.Buffer, view reportView) {
	if output == nil ||
		(view.DiagnosticCount == 0 &&
			len(view.Diagnostics) == 0 &&
			!view.DiagnosticsTruncated) {
		return
	}
	output.WriteString("\n## Integrity diagnostics\n\n")
	fmt.Fprintf(
		output,
		"Retained %d of %d diagnostics",
		view.DiagnosticsRetained,
		view.DiagnosticCount,
	)
	if view.DiagnosticsTruncated {
		output.WriteString("; the diagnostic list is truncated")
	}
	output.WriteString(".\n\n")
	for _, diagnostic := range view.Diagnostics {
		fmt.Fprintf(
			output,
			"- **%s record %d, line %d:** %s\n",
			escapeMarkdown(diagnostic.Kind),
			diagnostic.Sequence,
			diagnostic.Line,
			escapeMarkdown(diagnostic.Message),
		)
		if diagnostic.Preview == "" {
			continue
		}
		for _, line := range strings.Split(diagnostic.Preview, "\n") {
			if line == "" {
				output.WriteString("  >\n")
				continue
			}
			fmt.Fprintf(output, "  > %s\n", escapeMarkdown(line))
		}
	}
}

func writePlainSlowest(output *bytes.Buffer, slowest []slowView) {
	if len(slowest) == 0 {
		return
	}
	output.WriteString("Slowest occurrences:\n")
	var previousPackage string
	for i, slow := range slowest {
		if i == 0 || slow.Package != previousPackage {
			fmt.Fprintf(output, "  %s\n", neutralizeTerminalInline(slow.Package))
		}
		fmt.Fprintf(
			output,
			"    - %s: %s (%s)\n",
			neutralizeTerminalInline(slow.Label),
			neutralizeTerminalInline(slow.Duration),
			neutralizeTerminalInline(slow.DurationSource),
		)
		previousPackage = slow.Package
	}
}

func failureLabel(failure summaryFailure) string {
	label := failure.Name
	if failure.Package != "" {
		if label == "" {
			label = failure.Package
		} else {
			label = failure.Package + "::" + label
		}
	}
	if failure.Ordinal > 0 {
		label += fmt.Sprintf(" [occurrence %d]", failure.Ordinal)
	}
	return label
}

func indentText(value, prefix string) string {
	return strings.ReplaceAll(value, "\n", "\n"+prefix)
}

func writeConsole(writer io.Writer, value string) error {
	n, err := io.WriteString(writer, value)
	if err == nil && n != len(value) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return fmt.Errorf("write report console: %w", err)
	}
	return nil
}

func defaultIsTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
