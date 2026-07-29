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
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/greenpau/tested/pkg/result"
)

func TestConsoleFormatsAndSafety(t *testing.T) {
	tests := []struct {
		name       string
		format     ConsoleFormat
		quiet      bool
		want       []string
		notWant    []string
		decodeJSON bool
		lookupEnv  func(string) (string, bool)
		isTerminal func(io.Writer) bool
	}{
		{
			name:   "plain NO_COLOR and controls",
			format: ConsolePlain,
			want: []string{
				"[FAILED]",
				"pkg/�[2J",
				"Child exit: 1 (signal terminated)",
				"Weighted profile coverage: 70.00%",
			},
			notWant: []string{
				"\x1b",
				"secret=alpha",
				"secret=bravo",
			},
			lookupEnv: func(string) (string, bool) { return "", true },
			isTerminal: func(io.Writer) bool {
				return true
			},
		},
		{
			name:    "markdown escaping",
			format:  ConsoleMarkdown,
			want:    []string{`\#`, `\|`, "## Failures", "70\\.00%"},
			notWant: []string{"\x1b", "secret="},
		},
		{
			name:       "JSON stream",
			format:     ConsoleJSON,
			want:       []string{`"schema":"tested/summary/v1"`},
			notWant:    []string{"\x1b", "secret="},
			decodeJSON: true,
		},
		{
			name:    "quiet suppresses live only",
			format:  ConsolePlain,
			quiet:   true,
			want:    []string{"Outcome:", "Tests:"},
			notWant: []string{"pkg/�[2J"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			console, err := NewConsole(ConsoleOptions{
				Writer:         &output,
				Title:          "# report|secret=alpha",
				Format:         test.format,
				Color:          ColorAuto,
				Quiet:          test.quiet,
				Slowest:        3,
				RedactPatterns: []string{`secret=[[:alnum:]]+`},
				LookupEnv:      test.lookupEnv,
				IsTerminal:     test.isTerminal,
			})
			if err != nil {
				t.Fatalf("NewConsole() unexpected error: %v", err)
			}
			pkg := result.Package{
				Name:   "pkg/\x1b[2J",
				Status: result.StatusFailed,
				Tests: []result.TestOccurrence{
					{Status: result.StatusFailed},
				},
			}
			if err := console.Package(pkg); err != nil {
				t.Fatalf("Package() unexpected error: %v", err)
			}
			if err := console.Final(testInput()); err != nil {
				t.Fatalf("Final() unexpected error: %v", err)
			}
			got := output.String()
			for _, expected := range test.want {
				if !strings.Contains(got, expected) {
					t.Errorf("console output missing %q:\n%s", expected, got)
				}
			}
			for _, forbidden := range test.notWant {
				if strings.Contains(got, forbidden) {
					t.Errorf("console output contains %q:\n%s", forbidden, got)
				}
			}
			if test.decodeJSON {
				decoder := json.NewDecoder(strings.NewReader(got))
				var summary map[string]any
				if err := decoder.Decode(&summary); err != nil {
					t.Fatalf("decode final JSON: %v", err)
				}
				if summary["schema"] != SummarySchema {
					t.Fatalf("decoded JSON value = %#v", summary)
				}
				var extra any
				if err := decoder.Decode(&extra); err != io.EOF {
					t.Fatalf("JSON console has an extra value: %#v, %v", extra, err)
				}
			}
		})
	}
}

func TestConsoleColorPolicy(t *testing.T) {
	tests := []struct {
		name      string
		mode      ColorMode
		terminal  bool
		noColor   bool
		wantColor bool
	}{
		{name: "auto terminal", mode: ColorAuto, terminal: true, wantColor: true},
		{name: "auto pipe", mode: ColorAuto},
		{name: "NO_COLOR", mode: ColorAuto, terminal: true, noColor: true},
		{name: "always overrides pipe", mode: ColorAlways, wantColor: true},
		{name: "never", mode: ColorNever, terminal: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			console, err := NewConsole(ConsoleOptions{
				Writer: &output,
				Format: ConsolePlain,
				Color:  test.mode,
				IsTerminal: func(io.Writer) bool {
					return test.terminal
				},
				LookupEnv: func(name string) (string, bool) {
					return "", name == "NO_COLOR" && test.noColor
				},
			})
			if err != nil {
				t.Fatalf("NewConsole() unexpected error: %v", err)
			}
			if err := console.Package(result.Package{
				Name:   "pkg",
				Status: result.StatusPassed,
			}); err != nil {
				t.Fatalf("Package() unexpected error: %v", err)
			}
			hasColor := strings.Contains(output.String(), "\x1b[32m")
			if hasColor != test.wantColor {
				t.Fatalf("color = %t, want %t; output %q", hasColor, test.wantColor, output.String())
			}
		})
	}
}

func TestConsoleErrors(t *testing.T) {
	if _, err := NewConsole(ConsoleOptions{}); err == nil {
		t.Fatal("NewConsole() accepted a nil writer")
	}
	if _, err := NewConsole(ConsoleOptions{
		Writer: io.Discard,
		Format: "yaml",
	}); err == nil {
		t.Fatal("NewConsole() accepted an unsupported format")
	}
	if _, err := NewConsole(ConsoleOptions{
		Writer:         io.Discard,
		RedactPatterns: []string{"z*"},
	}); err == nil || !strings.Contains(err.Error(), "must not match empty text") {
		t.Fatalf("NewConsole() error = %v, want empty-match rejection", err)
	}
	if err := (*Console)(nil).Final(Input{}); err == nil {
		t.Fatal("nil Console.Final() did not fail")
	}
}

func TestConsoleRedactionAmplificationIsBounded(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		value    string
	}{
		{
			name:     "contextual zero width",
			patterns: []string{`\b`},
			value:    "secret",
		},
		{
			name:     "repeated dot rules",
			patterns: []string{".", "."},
			value:    "a",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			console, err := NewConsole(ConsoleOptions{
				Writer:         &output,
				Format:         ConsolePlain,
				Color:          ColorNever,
				RedactPatterns: test.patterns,
			})
			if err != nil {
				t.Fatalf("NewConsole() unexpected error: %v", err)
			}
			if err := console.Package(result.Package{
				Name:   test.value,
				Status: result.StatusPassed,
			}); err != nil {
				t.Fatalf("Package() unexpected error: %v", err)
			}
			got := output.String()
			if !strings.Contains(got, redactionOmission) {
				t.Fatalf("console output lacks conservative omission marker: %q", got)
			}
			if len(got) > 256 {
				t.Fatalf("console output amplified unexpectedly to %d bytes", len(got))
			}
		})
	}
}
