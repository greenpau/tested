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

package app

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/greenpau/tested/pkg/artifact"
	"github.com/greenpau/tested/pkg/cli"
	"github.com/greenpau/tested/pkg/report"
)

// Execute parses args, performs the requested operation, writes all user
// output, and returns the process exit code. It never closes stdout or stderr.
func Execute(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	buildInfo BuildInfo,
) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if ctx == nil {
		writeDiagnostic(stderr, "context must not be nil")
		return ExitInfrastructure
	}

	options, err := cli.Parse(args)
	if err != nil {
		writeDiagnostic(stderr, err.Error())
		if cli.IsUsageError(err) {
			writeDiagnostic(stderr, `run "tested help" for usage`)
		}
		return ExitInfrastructure
	}

	switch options.Command {
	case cli.CommandHelp:
		if _, err := io.WriteString(stdout, cli.Usage("tested")); err != nil {
			writeDiagnostic(stderr, fmt.Sprintf("write help: %v", err))
			return ExitInfrastructure
		}
		return ExitSuccess
	case cli.CommandVersion:
		if _, err := io.WriteString(
			stdout,
			report.SanitizeTerminalText(buildInfo.String()),
		); err != nil {
			writeDiagnostic(stderr, fmt.Sprintf("write version: %v", err))
			return ExitInfrastructure
		}
		return ExitSuccess
	}

	layout, err := artifact.Resolve(options.WorkDir, options.OutputDir)
	if err != nil {
		writeDiagnostic(stderr, err.Error())
		return ExitInfrastructure
	}
	title := options.Title
	if title == "" {
		title = filepath.Base(layout.WorkDir)
		if title == "." || title == string(filepath.Separator) || title == "" {
			title = "tested report"
		}
	}
	renderer, console, err := newPresentation(options, title, stdout)
	if err != nil {
		writeDiagnostic(stderr, err.Error())
		return ExitInfrastructure
	}

	var outcome executionOutcome
	switch options.Command {
	case cli.CommandRun:
		outcome = executeRun(ctx, options, layout, renderer, console)
	case cli.CommandReport:
		outcome = executeReport(ctx, options, layout, renderer, console)
	default:
		outcome.errors.add(fmt.Errorf("unsupported command %q", options.Command))
		outcome.state.InfrastructureErr = true
	}
	outcome.errors.write(stderr, renderer.SanitizeTerminalText)
	outcome.warnings.writeWarnings(stderr, renderer.SanitizeTerminalText)
	return ExitCode(outcome.state)
}

func newPresentation(
	options cli.Options,
	title string,
	stdout io.Writer,
) (*report.Renderer, *report.Console, error) {
	renderer, err := report.New(report.Options{
		Title:          title,
		RedactPatterns: options.RedactPatterns,
		Slowest:        options.Slowest,
	})
	if err != nil {
		return nil, nil, err
	}
	console, err := report.NewConsole(report.ConsoleOptions{
		Writer:         stdout,
		Title:          title,
		Format:         report.ConsoleFormat(options.Format),
		Color:          report.ColorMode(options.Color),
		Quiet:          options.Quiet,
		Slowest:        options.Slowest,
		RedactPatterns: options.RedactPatterns,
	})
	if err != nil {
		return nil, nil, err
	}
	return renderer, console, nil
}

func writeDiagnostic(writer io.Writer, message string) {
	_, _ = fmt.Fprintf(
		writer,
		"tested: %s\n",
		report.SanitizeTerminalText(message),
	)
}
