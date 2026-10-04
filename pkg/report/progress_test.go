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
	"io"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/greenpau/tested/pkg/result"
)

func TestLiveProgressFormats(t *testing.T) {
	for _, format := range []ConsoleFormat{ConsolePlain, ConsoleMarkdown, ConsoleJSON} {
		for _, quiet := range []bool{false, true} {
			t.Run(string(format)+map[bool]string{true: " quiet", false: " live"}[quiet], func(t *testing.T) {
				var output bytes.Buffer
				console, err := NewConsole(ConsoleOptions{Writer: &output, Format: format, Quiet: quiet, Color: ColorNever})
				if err != nil {
					t.Fatal(err)
				}
				id := &result.OccurrenceID{Package: "p", Name: "TestParent/child", Ordinal: 2}
				for _, action := range []string{"start", "run", "pause", "cont", "pass", "fail", "skip", "bench", "build-fail", "attr", "artifacts"} {
					if _, err := console.Event(result.Progress{Action: action, Scope: result.OutputTest, Package: "p", Test: id, Status: result.StatusRunning}); err != nil {
						t.Fatal(err)
					}
				}
				_, err = console.Event(result.Progress{Action: "output", Scope: result.OutputTest, Package: "p", Test: id, Output: "first\n# [link](url) <script>\x1b[2J\r\u202E", OutputTruncated: true, Diagnostics: 1})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = console.Stderr("compiler message\n"); err != nil {
					t.Fatal(err)
				}
				if err = console.Stage("Publishing reports"); err != nil {
					t.Fatal(err)
				}
				got := output.String()
				if quiet || format == ConsoleJSON {
					if got != "" {
						t.Fatalf("suppressed live output: %q", got)
					}
					return
				}
				for _, want := range []string{"TestParent/child", "occurrence 2", "first", "compiler message", "Publishing reports", "integrity diagnostics", "truncated"} {
					if !strings.Contains(got, want) {
						t.Fatalf("missing %q in %q", want, got)
					}
				}
				if strings.ContainsAny(got, "\x1b\r\u202E") {
					t.Fatalf("unsafe controls: %q", got)
				}
				if format == ConsoleMarkdown && (strings.Contains(got, "<script>") || strings.Contains(got, "[link](url)")) {
					t.Fatalf("unsafe Markdown: %q", got)
				}
			})
		}
	}
}

func TestLiveProgressGroupsPackageContext(t *testing.T) {
	for _, format := range []ConsoleFormat{ConsolePlain, ConsoleMarkdown} {
		t.Run(string(format), func(t *testing.T) {
			var output bytes.Buffer
			console, err := NewConsole(ConsoleOptions{Writer: &output, Format: format})
			if err != nil {
				t.Fatal(err)
			}
			event := func(pkg, action, value string) {
				t.Helper()
				_, err := console.Event(result.Progress{
					Action: action, Scope: result.OutputTest, Package: pkg,
					Test:   &result.OccurrenceID{Package: pkg, Name: "TestShared/child", Ordinal: 2},
					Output: value,
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			event("example.com/p", "run", "")
			event("example.com/p", "output", "first\nsecond\n")
			event("example.com/p", "pass", "")
			if written, err := console.Event(result.Progress{Action: "silent", Package: "example.com/q"}); written || err != nil {
				t.Fatalf("silent update wrote context: %t, %v", written, err)
			}
			event("example.com/p", "run", "")
			event("example.com/q", "run", "")
			event("example.com/p", "pass", "")
			if _, err := console.Stderr("compiler stderr\n"); err != nil {
				t.Fatal(err)
			}
			event("example.com/p", "run", "")
			if err := console.Stage("Still running"); err != nil {
				t.Fatal(err)
			}
			event("example.com/p", "pass", "")
			if _, err := console.Event(result.Progress{Scope: result.OutputUnattributed, Output: "global\n"}); err != nil {
				t.Fatal(err)
			}
			event("example.com/p", "run", "")
			got := output.String()
			for pkg, want := range map[string]int{"example.com/p": 5, "example.com/q": 1} {
				visible := pkg
				if format == ConsoleMarkdown {
					visible = strings.ReplaceAll(pkg, ".", `\.`)
				}
				if count := strings.Count(got, visible); count != want {
					t.Errorf("%s appears %d times, want %d context headings: %s", pkg, count, want, got)
				}
			}
			for _, line := range strings.Split(got, "\n") {
				if strings.Contains(line, "example") && !strings.Contains(line, "[package]") && !strings.Contains(line, "**package**") {
					t.Errorf("package repeated outside a context heading: %s", line)
				}
			}
			if strings.Count(got, "TestShared/child") != 10 || !strings.Contains(got, "occurrence 2") {
				t.Fatalf("test identities or log lines lost: %s", got)
			}
		})
	}
}

func TestLiveProgressPackageContextUsesUnredactedIdentity(t *testing.T) {
	var output bytes.Buffer
	console, err := NewConsole(ConsoleOptions{Writer: &output, RedactPatterns: []string{"secret-(one|two)"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"example/secret-one", "example/secret-one", "example/secret-two"} {
		if _, err := console.Event(result.Progress{
			Action: "run", Scope: result.OutputTest, Package: pkg,
			Test: &result.OccurrenceID{Name: "TestShared", Ordinal: 1},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(output.String(), "secret-") || strings.Count(output.String(), "[package]") != 2 {
		t.Fatalf("redaction merged package contexts or exposed identity: %s", output.String())
	}
}

func TestLiveProgressRedactsFragmentedLogs(t *testing.T) {
	var output bytes.Buffer
	console, err := NewConsole(ConsoleOptions{Writer: &output, RedactPatterns: []string{`secret=abcdef`}})
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range []string{"secret=abc", "def\n"} {
		if _, err := console.Event(result.Progress{Action: "output", Output: chunk}); err != nil {
			t.Fatal(err)
		}
		if _, err := console.Stderr(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := console.Stage("working secret=abcdef"); err != nil {
		t.Fatal(err)
	}
	if _, err := console.Event(result.Progress{Action: "run", Package: "secret=abcdef"}); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "secret=") || strings.Contains(got, "def") || strings.Count(got, "Live log text omitted") != 1 || !strings.Contains(got, "[run]") {
		t.Fatalf("redaction stream: %q", got)
	}
}

func TestLiveProgressBudgetsAndConcurrency(t *testing.T) {
	var output bytes.Buffer
	console, err := NewConsole(ConsoleOptions{Writer: &output})
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 500 {
				_, _ = console.Stderr(strings.Repeat("界\x1b", 5000))
			}
		}()
	}
	workers.Wait()
	if !utf8.Valid(output.Bytes()) || output.Len() > liveDetailLimit+liveLineLimit || strings.Count(output.String(), "Live detail limit reached") != 1 {
		t.Fatalf("unbounded or invalid output: %d bytes", output.Len())
	}
	for _, line := range strings.Split(output.String(), "\n") {
		if len(line) > liveLineLimit {
			t.Fatalf("long live line: %d", len(line))
		}
	}
	before := output.Len()
	if written, err := console.Event(result.Progress{Action: "run", Package: "p"}); err != nil || written {
		t.Fatalf("budget not sticky: %v, %v", written, err)
	}
	if err := console.Stage("Still working"); err != nil {
		t.Fatal(err)
	}
	if err := console.Final(Input{}); err != nil {
		t.Fatal(err)
	}
	if output.Len() <= before || !strings.Contains(output.String()[before:], "Outcome:") {
		t.Fatal("budget suppressed final status")
	}
}

type progressShortWriter struct{}

func (progressShortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

type retryProgressWriter struct {
	bytes.Buffer
	fail bool
}

func (w *retryProgressWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if w.fail {
		w.fail = false
		return n, io.ErrClosedPipe
	}
	return n, err
}

func (w *retryProgressWriter) WriteString(value string) (int, error) {
	return w.Write([]byte(value))
}

func TestLiveProgressRestoresContextAfterWriteFailure(t *testing.T) {
	var output retryProgressWriter
	console, err := NewConsole(ConsoleOptions{Writer: &output})
	if err != nil {
		t.Fatal(err)
	}
	for i, pkg := range []string{"p", "q", "p"} {
		before := output.Len()
		output.fail = i == 1
		_, err := console.Event(result.Progress{Action: "run", Scope: result.OutputPackage, Package: pkg})
		if i == 1 {
			if !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("lost write error: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(output.String()[before:], "[package] "+pkg+"\n") {
			t.Fatalf("missing context after a possibly partial write: %s", output.String()[before:])
		}
	}
}

func TestLiveProgressShortWrites(t *testing.T) {
	console, err := NewConsole(ConsoleOptions{Writer: progressShortWriter{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := console.Stage("work"); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write lost: %v", err)
	}
	if _, err := console.Event(result.Progress{Action: "run", Package: "p"}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short event/context write lost: %v", err)
	}
	var missing *Console
	if err := missing.Stage("work"); err == nil {
		t.Fatal("nil console stage succeeded")
	}
	if _, err := missing.Event(result.Progress{}); err == nil {
		t.Fatal("nil console event succeeded")
	}
	if _, err := missing.Stderr("log"); err == nil {
		t.Fatal("nil console stderr succeeded")
	}
}

func TestLiveProgressRedactsIdentitiesBeforeComposition(t *testing.T) {
	for _, format := range []ConsoleFormat{ConsolePlain, ConsoleMarkdown} {
		t.Run(string(format), func(t *testing.T) {
			var output bytes.Buffer
			console, err := NewConsole(ConsoleOptions{Writer: &output, Format: format, RedactPatterns: []string{`^secret`, `private$`}})
			if err != nil {
				t.Fatal(err)
			}
			for _, action := range []string{"run", "pass", "attr", "artifacts"} {
				_, err := console.Event(result.Progress{Action: action, Scope: result.OutputTest, Package: "secret/pkg", Test: &result.OccurrenceID{Name: "secret/Test_private", Ordinal: 1}, Status: result.StatusPassed})
				if err != nil {
					t.Fatal(err)
				}
			}
			if strings.Contains(output.String(), "secret") || strings.Contains(output.String(), "private") || !strings.Contains(output.String(), "REDACTED") {
				t.Fatalf("anchored rules bypassed: %s", output.String())
			}
		})
	}
}
