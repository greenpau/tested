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

package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/greenpau/tested/pkg/coverage"
	"github.com/greenpau/tested/pkg/report"
)

const (
	// DefaultOutputDir is tested's managed artifact directory.
	DefaultOutputDir = ".coverage"
	// DefaultMaximumEventBytes bounds one decoded JSON event without limiting
	// the byte-faithful raw event log.
	DefaultMaximumEventBytes int64 = 16 << 20
	// DefaultMaximumTestOutputBytes bounds one test's output in derived
	// reports without truncating the raw event log.
	DefaultMaximumTestOutputBytes int64 = 1 << 20
	// DefaultMaximumTotalOutputBytes bounds aggregate retained output across
	// all normalized scopes without truncating the raw event log.
	DefaultMaximumTotalOutputBytes int64 = 64 << 20
	// DefaultMaximumResultEntries bounds aggregate normalized packages,
	// occurrences, builds, metadata, and retained output chunks.
	DefaultMaximumResultEntries int64 = 1_000_000
	// DefaultMaximumNormalizedBytes bounds aggregate retained identity and
	// metadata strings independently from captured output text.
	DefaultMaximumNormalizedBytes int64 = 128 << 20
	// DefaultSlowestCount is the number of slow tests shown in summaries.
	DefaultSlowestCount = 10
)

// Command identifies one tested operation.
type Command string

const (
	// CommandRun executes go test and generates reports.
	CommandRun Command = "run"
	// CommandReport regenerates reports from an existing event stream.
	CommandReport Command = "report"
	// CommandVersion prints build identity.
	CommandVersion Command = "version"
	// CommandHelp prints command usage.
	CommandHelp Command = "help"
)

// ConsoleFormat controls the terminal summary projection.
type ConsoleFormat string

const (
	// FormatPlain renders a terminal-oriented text summary.
	FormatPlain ConsoleFormat = "plain"
	// FormatMarkdown renders a portable Markdown summary.
	FormatMarkdown ConsoleFormat = "markdown"
	// FormatJSON writes the generated semantic summary to standard output.
	FormatJSON ConsoleFormat = "json"
)

// ColorMode controls ANSI color use in plain output.
type ColorMode string

const (
	// ColorAuto enables colors only for a compatible terminal.
	ColorAuto ColorMode = "auto"
	// ColorAlways always emits ANSI colors.
	ColorAlways ColorMode = "always"
	// ColorNever never emits ANSI colors.
	ColorNever ColorMode = "never"
)

// Options contains validated command-line configuration.
type Options struct {
	Command                 Command
	WorkDir                 string
	OutputDir               string
	GoBinary                string
	Title                   string
	NoCoverage              bool
	Color                   ColorMode
	Format                  ConsoleFormat
	Quiet                   bool
	Slowest                 int
	MaximumEventBytes       int64
	MaximumOutputBytes      int64
	MaximumTotalOutputBytes int64
	MaximumResultEntries    int64
	MaximumNormalizedBytes  int64
	RedactPatterns          []string
	MinimumCoverage         string
	GoTestArgs              []string
	EventsFile              string
	CoverageProfileFile     string
	StderrFile              string
	RunMetadataFile         string
	AllowFailures           bool
}

// UsageError marks an invalid command line. Callers should print usage and
// return the conventional process exit code 2.
type UsageError struct {
	Err error
}

// Error returns the validation failure.
func (e *UsageError) Error() string {
	return e.Err.Error()
}

// Unwrap exposes the validation failure.
func (e *UsageError) Unwrap() error {
	return e.Err
}

// IsUsageError reports whether err is a command-line usage failure.
func IsUsageError(err error) bool {
	var target *UsageError
	return errors.As(err, &target)
}

// Parse parses args without writing diagnostics or usage text.
func Parse(args []string) (Options, error) {
	command, remaining := selectCommand(args)
	if command == CommandHelp || command == CommandVersion {
		if len(remaining) > 0 {
			return Options{}, usageErrorf("%s does not accept arguments", command)
		}
		return Options{Command: command}, nil
	}

	options := defaultOptions(command)
	var err error
	switch command {
	case CommandRun:
		err = parseRun(&options, remaining)
	case CommandReport:
		err = parseReport(&options, remaining)
	default:
		err = usageErrorf("unknown command %q", command)
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Options{Command: CommandHelp}, nil
		}
		return Options{}, err
	}
	if err := options.validate(); err != nil {
		return Options{}, err
	}
	return options, nil
}

func selectCommand(args []string) (Command, []string) {
	if len(args) == 0 {
		return CommandRun, nil
	}
	switch args[0] {
	case "run":
		return CommandRun, args[1:]
	case "report":
		return CommandReport, args[1:]
	case "version", "--version", "-version":
		return CommandVersion, args[1:]
	case "help", "--help", "-h", "-help":
		return CommandHelp, args[1:]
	default:
		return CommandRun, args
	}
}

func defaultOptions(command Command) Options {
	return Options{
		Command:                 command,
		WorkDir:                 ".",
		OutputDir:               DefaultOutputDir,
		GoBinary:                "go",
		Color:                   ColorAuto,
		Format:                  FormatPlain,
		Slowest:                 DefaultSlowestCount,
		MaximumEventBytes:       DefaultMaximumEventBytes,
		MaximumOutputBytes:      DefaultMaximumTestOutputBytes,
		MaximumTotalOutputBytes: DefaultMaximumTotalOutputBytes,
		MaximumResultEntries:    DefaultMaximumResultEntries,
		MaximumNormalizedBytes:  DefaultMaximumNormalizedBytes,
	}
}

func parseRun(options *Options, args []string) error {
	flags := newFlagSet("run")
	redactions := redactionList{}
	registerCommonFlags(flags, options, &redactions)
	flags.BoolVar(&options.NoCoverage, "no-coverage", false, "")
	flags.Var(
		&thresholdValue{destination: &options.MinimumCoverage},
		"minimum-coverage",
		"",
	)
	if err := flags.Parse(args); err != nil {
		return usageError(err)
	}
	options.RedactPatterns = redactions.Values()
	options.GoTestArgs = append([]string(nil), flags.Args()...)
	if len(options.GoTestArgs) == 0 {
		options.GoTestArgs = []string{"./..."}
	}
	if err := validateManagedArguments(options.GoTestArgs); err != nil {
		return usageError(err)
	}
	return nil
}

func parseReport(options *Options, args []string) error {
	flags := newFlagSet("report")
	redactions := redactionList{}
	registerCommonFlags(flags, options, &redactions)
	flags.BoolVar(&options.NoCoverage, "no-coverage", false, "")
	flags.StringVar(&options.EventsFile, "events", "", "")
	flags.StringVar(&options.CoverageProfileFile, "coverprofile", "", "")
	flags.StringVar(&options.StderrFile, "stderr", "", "")
	flags.StringVar(&options.RunMetadataFile, "run-metadata", "", "")
	flags.BoolVar(&options.AllowFailures, "allow-failures", false, "")
	if err := flags.Parse(args); err != nil {
		return usageError(err)
	}
	options.RedactPatterns = redactions.Values()
	positionals := flags.Args()
	if len(positionals) > 1 {
		return usageErrorf("report accepts at most one positional event-log path")
	}
	if len(positionals) == 1 {
		if options.EventsFile != "" {
			return usageErrorf("event log was provided both positionally and with --events")
		}
		options.EventsFile = positionals[0]
	}
	return nil
}

func registerCommonFlags(
	flags *flag.FlagSet,
	options *Options,
	redactions *redactionList,
) {
	flags.StringVar(&options.WorkDir, "work-dir", ".", "")
	flags.StringVar(&options.WorkDir, "C", ".", "")
	flags.StringVar(&options.OutputDir, "output-dir", DefaultOutputDir, "")
	flags.StringVar(&options.OutputDir, "o", DefaultOutputDir, "")
	flags.StringVar(&options.GoBinary, "go", "go", "")
	flags.StringVar(&options.Title, "title", "", "")
	flags.Var(redactions, "redact", "")
	flags.Var((*colorValue)(&options.Color), "color", "")
	flags.Var((*formatValue)(&options.Format), "format", "")
	flags.BoolVar(&options.Quiet, "quiet", false, "")
	flags.IntVar(&options.Slowest, "slowest", DefaultSlowestCount, "")
	flags.Int64Var(&options.MaximumEventBytes, "max-event-bytes", DefaultMaximumEventBytes, "")
	flags.Int64Var(
		&options.MaximumOutputBytes,
		"max-test-output-bytes",
		DefaultMaximumTestOutputBytes,
		"",
	)
	flags.Int64Var(
		&options.MaximumTotalOutputBytes,
		"max-total-output-bytes",
		DefaultMaximumTotalOutputBytes,
		"",
	)
	flags.Int64Var(
		&options.MaximumResultEntries,
		"max-result-entries",
		DefaultMaximumResultEntries,
		"",
	)
	flags.Int64Var(
		&options.MaximumNormalizedBytes,
		"max-normalized-bytes",
		DefaultMaximumNormalizedBytes,
		"",
	)
}

func newFlagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}
	return flags
}

func (options Options) validate() error {
	if strings.TrimSpace(options.WorkDir) == "" {
		return usageErrorf("--work-dir must not be empty")
	}
	if strings.TrimSpace(options.OutputDir) == "" {
		return usageErrorf("--output-dir must not be empty")
	}
	if strings.TrimSpace(options.GoBinary) == "" {
		return usageErrorf("--go must not be empty")
	}
	if options.Slowest < 0 {
		return usageErrorf("--slowest must be zero or greater")
	}
	if options.MaximumEventBytes != 0 && options.MaximumEventBytes < 1024 {
		return usageErrorf("--max-event-bytes must be zero or at least 1024")
	}
	if options.MaximumEventBytes > int64(^uint(0)>>1) {
		return usageErrorf("--max-event-bytes exceeds this platform's integer limit")
	}
	if options.MaximumOutputBytes != 0 && options.MaximumOutputBytes < 1024 {
		return usageErrorf("--max-test-output-bytes must be zero or at least 1024")
	}
	if options.MaximumTotalOutputBytes != 0 &&
		options.MaximumTotalOutputBytes < 1024 {
		return usageErrorf(
			"--max-total-output-bytes must be zero or at least 1024",
		)
	}
	if options.MaximumResultEntries < 0 {
		return usageErrorf("--max-result-entries must be zero or greater")
	}
	if options.MaximumNormalizedBytes != 0 &&
		options.MaximumNormalizedBytes < 1024 {
		return usageErrorf(
			"--max-normalized-bytes must be zero or at least 1024",
		)
	}
	if options.MinimumCoverage != "" {
		if _, err := coverage.ParseThreshold(options.MinimumCoverage); err != nil {
			return usageErrorf("--minimum-coverage: %v", err)
		}
	}
	if options.NoCoverage && options.MinimumCoverage != "" {
		return usageErrorf("--minimum-coverage cannot be combined with --no-coverage")
	}
	if options.Command == CommandReport && options.NoCoverage &&
		options.CoverageProfileFile != "" {
		return usageErrorf("--coverprofile cannot be combined with --no-coverage")
	}
	if err := report.ValidateRedactPatterns(options.RedactPatterns); err != nil {
		return usageErrorf("invalid --redact configuration: %v", err)
	}
	return nil
}

func validateManagedArguments(args []string) error {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "-args" || arg == "--args" {
			break
		}
		switch {
		case arg == "-json" || arg == "--json" ||
			strings.HasPrefix(arg, "-json=") ||
			strings.HasPrefix(arg, "--json="):
			return fmt.Errorf("%q is managed by tested and must be removed", arg)
		case arg == "-coverprofile" || arg == "--coverprofile":
			return fmt.Errorf("%q is managed by tested and must be removed", arg)
		case strings.HasPrefix(arg, "-coverprofile=") ||
			strings.HasPrefix(arg, "--coverprofile="):
			return fmt.Errorf("%q is managed by tested and must be removed", arg)
		case arg == "-c" || arg == "--c" ||
			strings.HasPrefix(arg, "-c=") ||
			strings.HasPrefix(arg, "--c="):
			return fmt.Errorf("%q compiles a test binary without running it and is incompatible with tested", arg)
		}
	}
	return nil
}

func usageError(err error) error {
	return &UsageError{Err: err}
}

func usageErrorf(format string, args ...any) error {
	return usageError(fmt.Errorf(format, args...))
}

type redactionList struct {
	patterns   []string
	totalBytes int
}

func (values *redactionList) String() string {
	if values == nil {
		return ""
	}
	return strings.Join(values.patterns, ",")
}

type thresholdValue struct {
	destination *string
}

func (value *thresholdValue) String() string {
	if value == nil || value.destination == nil {
		return ""
	}
	return *value.destination
}

func (value *thresholdValue) Set(input string) error {
	threshold, err := coverage.ParseThreshold(input)
	if err != nil {
		return err
	}
	if value.destination == nil {
		return errors.New("minimum coverage destination is nil")
	}
	*value.destination = threshold.Canonical()
	return nil
}

func (values *redactionList) Set(value string) error {
	if values == nil {
		return errors.New("redaction destination is nil")
	}
	if len(values.patterns) >= report.MaximumRedactionPatterns {
		return fmt.Errorf(
			"redaction pattern count %d exceeds limit %d",
			len(values.patterns)+1,
			report.MaximumRedactionPatterns,
		)
	}
	if len(value) > report.MaximumRedactionPatternBytes {
		return fmt.Errorf(
			"redaction size %d exceeds %d-byte limit",
			len(value),
			report.MaximumRedactionPatternBytes,
		)
	}
	if len(value) > report.MaximumTotalRedactionPatternBytes-values.totalBytes {
		return fmt.Errorf(
			"redaction patterns exceed %d-byte aggregate limit",
			report.MaximumTotalRedactionPatternBytes,
		)
	}
	values.patterns = append(values.patterns, value)
	values.totalBytes += len(value)
	return nil
}

func (values *redactionList) Values() []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values.patterns...)
}

type colorValue ColorMode

func (value *colorValue) String() string {
	return string(*value)
}

func (value *colorValue) Set(input string) error {
	mode := ColorMode(strings.ToLower(input))
	switch mode {
	case ColorAuto, ColorAlways, ColorNever:
		*value = colorValue(mode)
		return nil
	default:
		return fmt.Errorf("color must be auto, always, or never")
	}
}

type formatValue ConsoleFormat

func (value *formatValue) String() string {
	return string(*value)
}

func (value *formatValue) Set(input string) error {
	format := ConsoleFormat(strings.ToLower(input))
	switch format {
	case FormatPlain, FormatMarkdown, FormatJSON:
		*value = formatValue(format)
		return nil
	default:
		return fmt.Errorf("format must be plain, markdown, or json")
	}
}

// Usage returns the complete user-facing command reference.
func Usage(program string) string {
	if program == "" {
		program = "tested"
	}
	return fmt.Sprintf(`%[1]s runs Go tests once and produces coverage and test reports.

Usage:
  %[1]s [run] [options] [-- go-test-arguments...]
  %[1]s report [options] [event-log]
  %[1]s version
  %[1]s help

Run options:
  -C, --work-dir DIR          run in DIR (default ".")
  -o, --output-dir DIR        write artifacts below DIR (default ".coverage")
      --go PATH               Go command to execute (default "go")
      --title TEXT            report title (default: project directory)
      --no-coverage           run tests without a coverage profile
      --minimum-coverage PCT  fail when weighted statement coverage is below PCT
      --format FORMAT         plain, markdown, or json (default "plain")
      --color MODE            auto, always, or never (default "auto")
      --quiet                 suppress live package progress
      --slowest N             include N slowest test occurrences (default 10)
      --max-event-bytes N     maximum JSON event/assembled benchmark line size;
                              0 disables the byte limit
      --max-test-output-bytes N
                              per-test output limit in derived reports
      --max-total-output-bytes N
                              aggregate retained-output limit; 0 is unlimited
      --max-result-entries N  normalized entry limit; 0 is unlimited
      --max-normalized-bytes N
                              normalized string-byte limit; 0 is unlimited
      --redact REGEXP         redact matching text in derived reports; repeatable
                              must consume text; max 32 rules, 4096 bytes each,
                              and 32 KiB total

Report options:
      --events FILE           event log (default: DIR/test_output.jsonl)
      --coverprofile FILE     profile (default: DIR/coverage.out)
      --stderr FILE           captured stderr (default: DIR/stderr.log)
      --run-metadata FILE     tested/run/v1 status (default: DIR/run.json)
      --allow-failures        return success after reporting failed test evidence
      --no-coverage           do not read or render a coverage profile
  Common run options for directory, title, output, color, format, limits, and
  redaction also apply.

Examples:
  %[1]s
  %[1]s run -- -race -count=1 ./...
  %[1]s run -C ../project -o .coverage -- -run TestLogin ./...
  %[1]s report -C ../project .coverage/test_output.jsonl

tested owns -json and -coverprofile. Put Go test flags after "--" so they are
passed through without being interpreted as tested options.
`, program)
}
