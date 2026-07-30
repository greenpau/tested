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
	"reflect"
	"strings"
	"testing"
)

func TestParseRun(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    Options
		wantErr string
	}{
		{
			name: "default",
			want: Options{
				Command:                 CommandRun,
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
				GoTestArgs:              []string{"./..."},
			},
		},
		{
			name: "explicit pass through",
			args: []string{
				"run", "-C", "../target", "-o", "artifacts",
				"--go", "/opt/go/bin/go", "--title", "Target",
				"--format", "markdown", "--color", "never",
				"--slowest", "3", "--max-event-bytes", "4096",
				"--max-test-output-bytes", "8192",
				"--max-total-output-bytes", "16384",
				"--max-result-entries", "1234",
				"--max-normalized-bytes", "32768",
				"--minimum-coverage", "82.5",
				"--coverage-diff-base", "origin/main",
				"--redact", `secret=\S+`, "--redact", `token=\S+`,
				"--", "-race", "-count=1", "./...",
			},
			want: Options{
				Command:                 CommandRun,
				WorkDir:                 "../target",
				OutputDir:               "artifacts",
				GoBinary:                "/opt/go/bin/go",
				Title:                   "Target",
				Color:                   ColorNever,
				Format:                  FormatMarkdown,
				Slowest:                 3,
				MaximumEventBytes:       4096,
				MaximumOutputBytes:      8192,
				MaximumTotalOutputBytes: 16384,
				MaximumResultEntries:    1234,
				MaximumNormalizedBytes:  32768,
				MinimumCoverage:         "82.5",
				CoverageDiffBase:        "origin/main",
				RedactPatterns:          []string{`secret=\S+`, `token=\S+`},
				GoTestArgs:              []string{"-race", "-count=1", "./..."},
				EventsFile:              "",
				CoverageProfileFile:     "",
			},
		},
		{
			name: "implicit run with flags after package",
			args: []string{"./...", "-run", "TestLogin"},
			want: Options{
				Command:                 CommandRun,
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
				GoTestArgs:              []string{"./...", "-run", "TestLogin"},
			},
		},
		{
			name:    "managed json",
			args:    []string{"run", "--", "-json", "./..."},
			wantErr: "managed by tested",
		},
		{
			name:    "managed cover profile assignment",
			args:    []string{"run", "--", "-coverprofile=other.out", "./..."},
			wantErr: "managed by tested",
		},
		{
			name:    "compile only",
			args:    []string{"run", "--", "-c"},
			wantErr: "incompatible with tested",
		},
		{
			name:    "compile only assignment",
			args:    []string{"run", "--", "-c=true"},
			wantErr: "incompatible with tested",
		},
		{
			name: "managed-looking test binary arg is allowed",
			args: []string{"run", "--", "./...", "-args", "-json"},
			want: Options{
				Command:                 CommandRun,
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
				GoTestArgs:              []string{"./...", "-args", "-json"},
			},
		},
		{
			name:    "coverage threshold with no coverage",
			args:    []string{"run", "--no-coverage", "--minimum-coverage", "80"},
			wantErr: "cannot be combined",
		},
		{
			name:    "coverage diff with no coverage",
			args:    []string{"run", "--no-coverage", "--coverage-diff-base", "HEAD"},
			wantErr: "cannot be combined",
		},
		{
			name:    "coverage diff option shaped revision",
			args:    []string{"run", "--coverage-diff-base=-HEAD"},
			wantErr: "cannot begin with '-'",
		},
		{
			name:    "coverage diff control character",
			args:    []string{"run", "--coverage-diff-base", "main\nHEAD"},
			wantErr: "control character",
		},
		{
			name: "coverage diff revision limit",
			args: []string{
				"run",
				"--coverage-diff-base",
				strings.Repeat("a", 1025),
			},
			wantErr: "exceeds 1024 bytes",
		},
		{
			name:    "non-finite coverage threshold",
			args:    []string{"run", "--minimum-coverage", "NaN"},
			wantErr: "plain decimal percentage",
		},
		{
			name:    "bad redaction",
			args:    []string{"run", "--redact", "["},
			wantErr: "invalid --redact",
		},
		{
			name:    "redaction matches empty input",
			args:    []string{"run", "--redact", "z*"},
			wantErr: "must not match empty text",
		},
		{
			name:    "report redaction matches empty input",
			args:    []string{"report", "--redact", "^"},
			wantErr: "must not match empty text",
		},
		{
			name:    "small event limit",
			args:    []string{"run", "--max-event-bytes", "100"},
			wantErr: "zero or at least 1024",
		},
		{
			name:    "small aggregate output limit",
			args:    []string{"run", "--max-total-output-bytes", "100"},
			wantErr: "zero or at least 1024",
		},
		{
			name:    "negative result entry limit",
			args:    []string{"run", "--max-result-entries", "-1"},
			wantErr: "zero or greater",
		},
		{
			name:    "small normalized byte limit",
			args:    []string{"run", "--max-normalized-bytes", "100"},
			wantErr: "zero or at least 1024",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Parse(test.args)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Parse() error = %v, want substring %q", err, test.wantErr)
				}
				if !IsUsageError(err) {
					t.Fatalf("Parse() error type = %T, want usage error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Parse() mismatch\n got: %#v\nwant: %#v", got, test.want)
			}
		})
	}
}

func TestParseRedactionLimits(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name: "pattern count",
			args: append(
				[]string{"run"},
				repeatFlag("--redact", "a", 33)...,
			),
			wantErr: "pattern count 33 exceeds limit 32",
		},
		{
			name: "single pattern bytes",
			args: []string{
				"run",
				"--redact",
				strings.Repeat("a", 4097),
			},
			wantErr: "size 4097 exceeds 4096-byte limit",
		},
		{
			name: "aggregate pattern bytes",
			args: append(
				[]string{"run"},
				repeatFlag("--redact", strings.Repeat("a", 4096), 9)...,
			),
			wantErr: "exceed 32768-byte aggregate limit",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(test.args)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Parse() error = %v, want substring %q", err, test.wantErr)
			}
			if !IsUsageError(err) {
				t.Fatalf("Parse() error type = %T, want usage error", err)
			}
		})
	}
}

func repeatFlag(name, value string, count int) []string {
	args := make([]string, 0, count*2)
	for range count {
		args = append(args, name, value)
	}
	return args
}

func TestParseReport(t *testing.T) {
	got, err := Parse([]string{
		"report", "-C", "/project", "-o", "reports",
		"--coverprofile", "profile.out", "--stderr", "stderr.txt",
		"--run-metadata", "run.json",
		"--coverage-diff-base", "release-1",
		"--allow-failures", "events.jsonl",
	})
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if got.Command != CommandReport || got.WorkDir != "/project" ||
		got.OutputDir != "reports" || got.EventsFile != "events.jsonl" ||
		got.CoverageProfileFile != "profile.out" ||
		got.StderrFile != "stderr.txt" ||
		got.RunMetadataFile != "run.json" ||
		got.CoverageDiffBase != "release-1" ||
		!got.AllowFailures {
		t.Fatalf("Parse() = %#v", got)
	}

	_, err = Parse([]string{"report", "--events", "one", "two"})
	if err == nil || !IsUsageError(err) {
		t.Fatalf("Parse() error = %v, want usage error", err)
	}

	_, err = Parse([]string{
		"report", "--no-coverage", "--coverprofile", "coverage.out",
	})
	if err == nil || !IsUsageError(err) {
		t.Fatalf("Parse(contradictory coverage flags) error = %v, want usage error", err)
	}

	_, err = Parse([]string{
		"report", "--no-coverage", "--coverage-diff-base", "HEAD",
	})
	if err == nil || !IsUsageError(err) {
		t.Fatalf("Parse(coverage diff without coverage) error = %v, want usage error", err)
	}

	got, err = Parse([]string{
		"run", "--no-coverage", "--coverage-diff-base", "",
	})
	if err != nil {
		t.Fatalf("Parse(empty coverage diff base) error = %v", err)
	}
	if got.CoverageDiffBase != "" || !got.NoCoverage {
		t.Fatalf("Parse(empty coverage diff base) = %#v", got)
	}

	got, err = Parse([]string{
		"run", "--minimum-coverage", "082.5000",
	})
	if err != nil {
		t.Fatalf("Parse(exact threshold) error = %v", err)
	}
	if got.MinimumCoverage != "82.5" {
		t.Fatalf("MinimumCoverage = %q, want canonical 82.5", got.MinimumCoverage)
	}
}

func TestParseHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"help"},
		{"run", "--help"},
		{"report", "-h"},
		{"--version"},
		{"version"},
	} {
		got, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse(%q): %v", args, err)
		}
		if got.Command != CommandHelp && got.Command != CommandVersion {
			t.Fatalf("Parse(%q) command = %q", args, got.Command)
		}
	}

	_, err := Parse([]string{"version", "extra"})
	var usage *UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("Parse() error = %v, want *UsageError", err)
	}
}

func TestUsage(t *testing.T) {
	output := Usage("custom-tested")
	for _, expected := range []string{
		"custom-tested runs Go tests",
		"test_output.jsonl",
		"--minimum-coverage",
		"--coverage-diff-base",
		"no revision inference or fetch",
		"baseline/deleted source may be sensitive",
		"--max-normalized-bytes",
		"--stderr",
		"must consume text",
		"32 KiB total",
		"tested owns -json and -coverprofile",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("Usage() missing %q", expected)
		}
	}
}
