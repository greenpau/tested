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
	"encoding/xml"
	"errors"
	"html"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/greenpau/tested/pkg/artifact"
	"github.com/greenpau/tested/pkg/coverage"
	"github.com/greenpau/tested/pkg/protocol"
	"github.com/greenpau/tested/pkg/result"
)

func TestRendererSecuritySemanticsAndDeterminism(t *testing.T) {
	renderer, err := New(Options{
		Title:                  `CI </title><script>alert("title")</script> secret=alpha`,
		RedactPatterns:         []string{`secret=[[:alnum:]]+`},
		Slowest:                5,
		MaxFailureExcerptBytes: 96,
	})
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	input := testInput()
	originalCommand := append([]string(nil), input.Result.Metadata.Command...)

	var first bytes.Buffer
	if err := renderer.RenderTestOutputHTML(&first, input); err != nil {
		t.Fatalf("RenderTestOutputHTML() unexpected error: %v", err)
	}
	var second bytes.Buffer
	if err := renderer.RenderTestOutputHTML(&second, input); err != nil {
		t.Fatalf("second RenderTestOutputHTML() unexpected error: %v", err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("RenderTestOutputHTML() is not byte deterministic")
	}
	if strings.Join(input.Result.Metadata.Command, "\x00") !=
		strings.Join(originalCommand, "\x00") {
		t.Fatal("renderer mutated input metadata command")
	}

	html := first.String()
	for _, forbidden := range []string{
		"secret=alpha",
		"secret=bravo",
		`<script>alert("payload")</script>`,
		"\x1b[31m",
		"https://",
		"http://",
	} {
		if strings.Contains(html, forbidden) {
			t.Errorf("HTML contains unsafe value %q", forbidden)
		}
	}
	for _, expected := range []string{
		`content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"`,
		"--background:",
		"@media (prefers-color-scheme: dark)",
		"[hidden] {",
		"details > * {",
		`class="summary-grid"`,
		`class="package-row filterable"`,
		`class="test-row filterable"`,
		`class="status-pill failed"`,
		`scope="col"`,
		`href="#report-content"`,
		`id="filter-package"`,
		`id="filter-test"`,
		`id="filter-output"`,
		`id="clear-filters"`,
		`const rows = Array.from(document.querySelectorAll(".filterable"));`,
		`data-status="failed"`,
		"TestRepeat [attempt 2]",
		"build-only/package",
		"incomplete",
		"70.00%",
		"Output truncated",
		"&lt;/script&gt;",
		redactionReplacement,
	} {
		if !strings.Contains(html, expected) {
			t.Errorf("HTML missing %q", expected)
		}
	}
	if strings.Index(html, "pkg/a") > strings.Index(html, "pkg/z") {
		t.Error("HTML package order is not deterministic lexical order")
	}
	pageHeaderEnd := strings.Index(html, "</header>")
	assessmentStart := strings.Index(
		html,
		`aria-labelledby="assessment-heading"`,
	)
	if pageHeaderEnd < 0 ||
		(assessmentStart >= 0 && pageHeaderEnd > assessmentStart) {
		t.Error("page header contains a nested assessment section header")
	}

	var summary bytes.Buffer
	if err := renderer.RenderSummaryJSON(&summary, input); err != nil {
		t.Fatalf("RenderSummaryJSON() unexpected error: %v", err)
	}
	if !json.Valid(summary.Bytes()) {
		t.Fatalf("summary JSON is invalid: %s", summary.String())
	}
	if strings.Contains(summary.String(), "PASSING-LOG") {
		t.Error("summary JSON retained ordinary passing output")
	}
	if strings.Contains(summary.String(), "secret=") {
		t.Error("summary JSON contains a configured secret")
	}
	var decoded summaryDocument
	if err := json.Unmarshal(summary.Bytes(), &decoded); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if decoded.Schema != SummarySchema || decoded.Outcome != "failed" {
		t.Fatalf("summary identity = (%q, %q)", decoded.Schema, decoded.Outcome)
	}
	if decoded.Run.Signal != "terminated" {
		t.Fatalf("summary signal = %q, want terminated", decoded.Run.Signal)
	}
	if decoded.Coverage == nil || decoded.Coverage.Statements != 10 ||
		decoded.Coverage.Covered != 7 {
		t.Fatalf("summary coverage = %#v", decoded.Coverage)
	}
	if len(decoded.Failures) < 3 {
		t.Fatalf("summary failures = %d, want test, incomplete, and build", len(decoded.Failures))
	}
	totalExcerpt := 0
	for _, failure := range decoded.Failures {
		totalExcerpt += len(failure.Excerpt)
	}
	if totalExcerpt > 96 {
		t.Fatalf("summary failure excerpts = %d bytes, want <= 96", totalExcerpt)
	}

	var junit bytes.Buffer
	if err := renderer.RenderJUnitXML(&junit, input); err != nil {
		t.Fatalf("RenderJUnitXML() unexpected error: %v", err)
	}
	var suites junitSuites
	if err := xml.Unmarshal(junit.Bytes(), &suites); err != nil {
		t.Fatalf("JUnit XML is invalid: %v\n%s", err, junit.String())
	}
	if suites.Failures == 0 || suites.Errors == 0 {
		t.Fatalf("JUnit outcomes = failures %d, errors %d", suites.Failures, suites.Errors)
	}
	for _, expected := range []string{
		"TestRepeat [occurrence 1]",
		"TestRepeat [occurrence 2]",
		"build-only/package",
		"tested.duration_source",
		"event_time_estimate",
	} {
		if !strings.Contains(junit.String(), expected) {
			t.Errorf("JUnit XML missing %q", expected)
		}
	}
	for _, forbidden := range []string{"secret=", "\x01", "\x1b"} {
		if strings.Contains(junit.String(), forbidden) {
			t.Errorf("JUnit XML contains unsafe value %q", forbidden)
		}
	}
}

func TestSummaryGolden(t *testing.T) {
	renderer, err := New(Options{Title: "golden"})
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	input := Input{Result: result.Result{
		Finalized: true,
		Metadata:  result.RunMetadata{ExitCode: 0},
	}}
	var output bytes.Buffer
	if err := renderer.RenderSummaryJSON(&output, input); err != nil {
		t.Fatalf("RenderSummaryJSON() unexpected error: %v", err)
	}
	expected, err := os.ReadFile(filepath.Join("testdata", "summary.golden.json"))
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}
	if !bytes.Equal(output.Bytes(), expected) {
		t.Fatalf("summary golden mismatch\n got:\n%s\nwant:\n%s", output.Bytes(), expected)
	}
}

func TestAssessmentDrivesTruthfulSummaryAndJUnit(t *testing.T) {
	renderer, err := New(Options{Title: "assessment"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		input        Input
		wantOutcome  string
		wantFailures int
		wantErrors   int
	}{
		{
			name: "empty stream with failed child",
			input: Input{
				Result: result.Result{
					Finalized: true,
					Metadata:  result.RunMetadata{ExitCode: 7},
				},
				Assessment: &Assessment{
					ChildStarted:   true,
					ChildExitKnown: true,
				},
			},
			wantOutcome:  "failed",
			wantFailures: 1,
		},
		{
			name: "missing process status",
			input: Input{
				Result: result.Result{
					Finalized: true,
					Metadata:  result.RunMetadata{ExitCode: -1},
				},
				Assessment: &Assessment{},
			},
			wantOutcome: "incomplete",
			wantErrors:  1,
		},
		{
			name: "coverage policy failure",
			input: Input{
				Result: result.Result{
					Finalized: true,
					Metadata:  result.RunMetadata{ExitCode: 0},
				},
				Assessment: &Assessment{
					ChildStarted:   true,
					ChildExitKnown: true,
					CoveragePolicy: &CoveragePolicy{
						Minimum:    "100",
						Actual:     "99.999999999999999994578989",
						Covered:    ^uint64(0) - 1,
						Statements: ^uint64(0),
						Available:  true,
						Satisfied:  false,
					},
				},
			},
			wantOutcome:  "coverage_failed",
			wantFailures: 1,
		},
		{
			name: "successful child conflicts with failure event",
			input: Input{
				Result: result.Result{
					Finalized: true,
					Metadata:  result.RunMetadata{ExitCode: 0},
					Summary: result.Summary{
						Tests: result.OutcomeCounts{
							Total:  1,
							Failed: 1,
						},
					},
				},
				Assessment: &Assessment{
					ChildStarted:   true,
					ChildExitKnown: true,
					Issues: []Issue{{
						Kind:    "integrity",
						Message: "failure event conflicts with exit zero",
						Fatal:   true,
					}},
				},
			},
			wantOutcome: "incomplete",
			wantErrors:  1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var summary bytes.Buffer
			if err := renderer.RenderSummaryJSON(&summary, test.input); err != nil {
				t.Fatal(err)
			}
			var document summaryDocument
			if err := json.Unmarshal(summary.Bytes(), &document); err != nil {
				t.Fatal(err)
			}
			if document.Outcome != test.wantOutcome {
				t.Fatalf(
					"summary outcome = %q, want %q",
					document.Outcome,
					test.wantOutcome,
				)
			}
			if document.Assessment == nil {
				t.Fatal("summary assessment is missing")
			}

			var output bytes.Buffer
			if err := renderer.RenderJUnitXML(&output, test.input); err != nil {
				t.Fatal(err)
			}
			var junit junitSuites
			if err := xml.Unmarshal(output.Bytes(), &junit); err != nil {
				t.Fatal(err)
			}
			if junit.Failures < test.wantFailures ||
				junit.Errors < test.wantErrors {
				t.Fatalf(
					"JUnit failures/errors = %d/%d, want at least %d/%d\n%s",
					junit.Failures,
					junit.Errors,
					test.wantFailures,
					test.wantErrors,
					output.String(),
				)
			}
		})
	}
}

func TestDirectionalControlsAndTruncatedRedaction(t *testing.T) {
	renderer, err := New(Options{
		Title:          "safe\u202Etitle",
		RedactPatterns: []string{`token=\S+`},
	})
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Result: result.Result{
		Finalized: true,
		Metadata:  result.RunMetadata{ExitCode: 0},
		Packages: []result.Package{{
			Name:   "pkg/\u2066spoof",
			Status: result.StatusFailed,
			Tests: []result.TestOccurrence{{
				ID: result.OccurrenceID{
					Package: "pkg/\u2066spoof",
					Name:    "TestSecret",
					Ordinal: 1,
				},
				Status:              result.StatusFailed,
				Output:              testOutput("prefix token=partial"),
				OutputBytes:         200,
				OutputRetainedBytes: 20,
				OutputTruncated:     true,
			}},
		}},
		Summary: result.Summary{
			Packages: result.OutcomeCounts{Total: 1, Failed: 1},
			Tests:    result.OutcomeCounts{Total: 1, Failed: 1},
		},
	}}

	var html, summary, junit bytes.Buffer
	if err := renderer.RenderTestOutputHTML(&html, input); err != nil {
		t.Fatal(err)
	}
	if err := renderer.RenderSummaryJSON(&summary, input); err != nil {
		t.Fatal(err)
	}
	if err := renderer.RenderJUnitXML(&junit, input); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"html":    html.Bytes(),
		"summary": summary.Bytes(),
		"junit":   junit.Bytes(),
	} {
		for _, forbidden := range []string{
			"token=partial",
			"\u202E",
			"\u2066",
		} {
			if bytes.Contains(data, []byte(forbidden)) {
				t.Errorf("%s contains unsafe value %q", name, forbidden)
			}
		}
		if !bytes.Contains(data, []byte(truncatedRedaction)) {
			t.Errorf("%s lacks conservative truncated-output redaction", name)
		}
	}
	if got := SanitizeTerminalText("left\u202Eright"); got != `left\u202Eright` {
		t.Fatalf("SanitizeTerminalText() = %q", got)
	}
}

func TestRendererPublishPreservesEvidence(t *testing.T) {
	workDir := t.TempDir()
	layout, err := artifact.Resolve(workDir, ".coverage")
	if err != nil {
		t.Fatalf("artifact.Resolve() unexpected error: %v", err)
	}
	if err := layout.PrepareRun(); err != nil {
		t.Fatalf("PrepareRun() unexpected error: %v", err)
	}
	raw := []byte("{\"Action\":\"fail\",\"Output\":\"secret=bravo\"}\n")
	evidence, err := layout.CreateTestOutput()
	if err != nil {
		t.Fatalf("CreateTestOutput() unexpected error: %v", err)
	}
	if _, err := evidence.Write(raw); err != nil {
		t.Fatalf("write evidence: %v", err)
	}
	if err := evidence.Close(); err != nil {
		t.Fatalf("close evidence: %v", err)
	}
	stderr, err := layout.CreateStderrLog()
	if err != nil {
		t.Fatalf("CreateStderrLog() unexpected error: %v", err)
	}
	if _, err := stderr.WriteString("raw secret=bravo\n"); err != nil {
		t.Fatalf("write stderr evidence: %v", err)
	}
	if err := stderr.Close(); err != nil {
		t.Fatalf("close stderr evidence: %v", err)
	}

	renderer, err := New(Options{
		Title:          "publish",
		RedactPatterns: []string{`secret=[[:alnum:]]+`},
		Slowest:        2,
	})
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	if err := renderer.Publish(layout, testInput()); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	gotRaw, err := os.ReadFile(layout.TestOutputJSONL)
	if err != nil {
		t.Fatalf("read raw evidence: %v", err)
	}
	if !bytes.Equal(gotRaw, raw) {
		t.Fatalf("raw evidence changed\n got: %q\nwant: %q", gotRaw, raw)
	}
	for _, path := range []string{
		layout.TestOutputHTML,
		layout.SummaryJSON,
		layout.JUnitXML,
		layout.IndexHTML,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("stat published artifact %s: %v", path, err)
			continue
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Errorf("mode for %s = %o, want 600", path, info.Mode().Perm())
		}
	}
	if _, err := os.Stat(layout.ManifestJSON); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("report renderer unexpectedly published manifest: %v", err)
	}
	index, err := os.ReadFile(layout.IndexHTML)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	for _, link := range []string{
		`href="test_output.html"`,
		`href="summary.json"`,
		`href="junit.xml"`,
		`href="test_output.jsonl"`,
		`href="stderr.log"`,
	} {
		if !bytes.Contains(index, []byte(link)) {
			t.Errorf("index missing %s", link)
		}
	}
	if bytes.Contains(index, []byte(`href="coverage.html"`)) {
		t.Error("index linked a missing coverage report")
	}
}

func TestRenderIndexFiltersUnmanagedLinks(t *testing.T) {
	renderer, err := New(Options{Title: "index"})
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	var output bytes.Buffer
	if err := renderer.RenderIndexHTML(
		&output,
		testInput(),
		[]artifact.Name{artifact.SummaryJSON, artifact.Name("../outside")},
	); err != nil {
		t.Fatalf("RenderIndexHTML() unexpected error: %v", err)
	}
	if !strings.Contains(output.String(), `href="summary.json"`) {
		t.Error("index omitted available fixed link")
	}
	if strings.Contains(output.String(), "outside") {
		t.Error("index included unmanaged link")
	}
}

func TestRenderIndexThemeSecurityAndDeterminism(t *testing.T) {
	renderer, err := New(Options{
		Title: `index </title><script>alert("index")</script>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	available := []artifact.Name{
		artifact.TestOutputHTML,
		artifact.CoverageHTML,
		artifact.SummaryJSON,
		artifact.JUnitXML,
		artifact.TestOutputJSONL,
		artifact.StderrLog,
		artifact.CoverageProfile,
	}
	var first, second bytes.Buffer
	if err := renderer.RenderIndexHTML(
		&first,
		testInput(),
		available,
	); err != nil {
		t.Fatal(err)
	}
	if err := renderer.RenderIndexHTML(
		&second,
		testInput(),
		available,
	); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("RenderIndexHTML() is not byte deterministic")
	}
	for _, expected := range []string{
		`content="default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"`,
		"--background:",
		"@media (prefers-color-scheme: dark)",
		`class="index-metrics"`,
		`class="artifact-grid"`,
		`class="artifact-card"`,
		`class="status-pill failed"`,
		`<span class="artifact-kind" aria-hidden="true">HTML</span>`,
		`href="test_output.html"`,
		`href="coverage.html"`,
	} {
		if !strings.Contains(first.String(), expected) {
			t.Errorf("index HTML missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		`<script>alert("index")</script>`,
		"@import",
		"url(",
		"http://",
		"https://",
	} {
		if strings.Contains(first.String(), forbidden) {
			t.Errorf("index HTML contains unsafe value %q", forbidden)
		}
	}
}

func TestRendererErrors(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "invalid redaction",
			run: func() error {
				_, err := New(Options{RedactPatterns: []string{"["}})
				return err
			},
		},
		{
			name: "negative slowest",
			run: func() error {
				_, err := New(Options{Slowest: -1})
				return err
			},
		},
		{
			name: "nil HTML writer",
			run: func() error {
				renderer, _ := New(Options{})
				return renderer.RenderTestOutputHTML(nil, Input{})
			},
		},
		{
			name: "writer failure",
			run: func() error {
				renderer, _ := New(Options{})
				return renderer.RenderSummaryJSON(errorWriter{}, Input{})
			},
		},
		{
			name: "nil layout",
			run: func() error {
				renderer, _ := New(Options{})
				return renderer.Publish(nil, Input{})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestSanitizers(t *testing.T) {
	tests := []struct {
		name string
		run  func(string) string
		in   string
		want string
	}{
		{
			name: "terminal ANSI",
			run:  neutralizeTerminal,
			in:   "\x1b[31mred\r",
			want: `\u001B[31mred\r`,
		},
		{
			name: "XML control",
			run:  sanitizeXML,
			in:   "a\x01b",
			want: "a\uFFFDb",
		},
		{
			name: "Markdown structure",
			run:  escapeMarkdown,
			in:   "# [x]|y",
			want: `\# \[x\]\|y`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.run(test.in); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestReportPreservesPackageAndTopLevelEvidence(t *testing.T) {
	renderer, err := New(Options{
		Title:          "evidence",
		RedactPatterns: []string{`secret=[[:alpha:]]+`},
	})
	if err != nil {
		t.Fatal(err)
	}
	input := Input{
		Result: result.Result{
			Finalized: true,
			Metadata:  result.RunMetadata{ExitCode: 1},
			Packages: []result.Package{{
				Name:                "pkg/failing",
				Status:              result.StatusFailed,
				IncompleteReason:    "package secret=alpha terminal contradiction",
				Output:              testOutput("package output secret=bravo\n"),
				OutputBytes:         100,
				OutputRetainedBytes: 28,
				OutputTruncated:     true,
				Attributes: []result.Attribute{{
					Sequence: 4,
					Line:     2,
					Key:      "owner secret=foxtrot",
					Value:    "team secret=golf",
				}},
				Artifacts: []result.Artifact{{
					Sequence: 5,
					Line:     3,
					Path:     "https://artifacts/secret=hotel",
				}},
				Tests: []result.TestOccurrence{{
					ID: result.OccurrenceID{
						Package: "pkg/failing",
						Name:    "TestIncomplete",
						Ordinal: 1,
					},
					Status:           result.StatusIncomplete,
					IncompleteReason: "test stream ended",
					Attributes: []result.Attribute{{
						Sequence: 6,
						Key:      "test-key",
						Value:    "secret=india",
					}},
					Artifacts: []result.Artifact{{
						Sequence: 7,
						Path:     "/tmp/secret=juliet",
					}},
				}},
			}},
			UnattributedOutput: []result.Output{
				{
					Sequence:      7,
					Line:          3,
					Scope:         result.OutputUnattributed,
					Text:          "<global> secret=charlie\n",
					OriginalBytes: 24,
				},
			},
			OutputBytes:         24,
			OutputRetainedBytes: 24,
			Diagnostics: []result.Diagnostic{{
				Kind:     protocol.DiagnosticMalformed,
				Sequence: 8,
				Line:     4,
				Bytes:    9,
				Preview:  "bad secret=delta",
				Message:  "malformed secret=echo",
			}},
			DiagnosticCount:      3,
			DiagnosticsTruncated: true,
			Summary: result.Summary{
				Packages: result.OutcomeCounts{Total: 1, Failed: 1},
				Tests: result.OutcomeCounts{
					Total:       3,
					Benchmarked: 2,
					Incomplete:  1,
				},
				Diagnostics:         3,
				DiagnosticsRetained: 1,
			},
		},
		Assessment: &Assessment{
			ChildStarted:   true,
			ChildExitKnown: true,
		},
	}

	var summaryOutput bytes.Buffer
	if err := renderer.RenderSummaryJSON(&summaryOutput, input); err != nil {
		t.Fatal(err)
	}
	var summary summaryDocument
	if err := json.Unmarshal(summaryOutput.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Packages) != 1 {
		t.Fatalf("summary packages = %d, want 1", len(summary.Packages))
	}
	pkg := summary.Packages[0]
	if pkg.IncompleteReason !=
		"package [REDACTED] terminal contradiction" {
		t.Fatalf("package incomplete reason = %q", pkg.IncompleteReason)
	}
	if pkg.OutputBytes != 100 ||
		pkg.OutputRetainedBytes != 28 ||
		!pkg.OutputTruncated {
		t.Fatalf("package output facts = %#v", pkg)
	}
	if len(pkg.Attributes) != 1 ||
		pkg.Attributes[0].Key != "owner [REDACTED]" ||
		pkg.Attributes[0].Value != "team [REDACTED]" ||
		len(pkg.Artifacts) != 1 ||
		pkg.Artifacts[0].Path != "https://artifacts/[REDACTED]" {
		t.Fatalf("package metadata = %#v / %#v", pkg.Attributes, pkg.Artifacts)
	}
	if len(pkg.Tests) != 1 ||
		len(pkg.Tests[0].Attributes) != 1 ||
		pkg.Tests[0].Attributes[0].Value != "[REDACTED]" ||
		len(pkg.Tests[0].Artifacts) != 1 ||
		pkg.Tests[0].Artifacts[0].Path != "/tmp/[REDACTED]" {
		t.Fatalf("occurrence metadata = %#v", pkg.Tests)
	}
	if summary.UnattributedOutput == nil {
		t.Fatal("summary omitted unattributed output")
	}
	if strings.Contains(summary.UnattributedOutput.Text, "secret=") ||
		!strings.Contains(summary.UnattributedOutput.Text, "[REDACTED]") {
		t.Fatalf(
			"summary unattributed output was not redacted: %q",
			summary.UnattributedOutput.Text,
		)
	}
	if summary.UnattributedOutput.OutputBytes != 24 ||
		summary.UnattributedOutput.OutputRetainedBytes != 24 {
		t.Fatalf(
			"summary unattributed output facts = %#v",
			summary.UnattributedOutput,
		)
	}
	if summary.DiagnosticCount != 3 ||
		summary.DiagnosticsRetained != 1 ||
		!summary.DiagnosticsTruncated {
		t.Fatalf(
			"summary diagnostic state = %d/%d truncated=%t",
			summary.DiagnosticsRetained,
			summary.DiagnosticCount,
			summary.DiagnosticsTruncated,
		)
	}
	foundPackageFailure := false
	for _, failure := range summary.Failures {
		if failure.Scope != "package" {
			continue
		}
		foundPackageFailure = true
		if failure.Reason !=
			"package [REDACTED] terminal contradiction" {
			t.Fatalf("package failure reason = %q", failure.Reason)
		}
	}
	if !foundPackageFailure {
		t.Fatal("summary omitted package failure details")
	}

	var htmlOutput bytes.Buffer
	if err := renderer.RenderTestOutputHTML(&htmlOutput, input); err != nil {
		t.Fatal(err)
	}
	html := htmlOutput.String()
	for _, expected := range []string{
		"package [REDACTED] terminal contradiction",
		`<dt>Benchmarks</dt><dd class="passed">2</dd>`,
		"Unattributed test output",
		"&lt;global&gt; [REDACTED]",
		"Retained 24 of 24 bytes",
		"Retained 1 of 3 diagnostics",
		"diagnostic list is truncated",
		"Package metadata",
		"Occurrence metadata",
		"https://artifacts/[REDACTED]",
	} {
		if !strings.Contains(html, expected) {
			t.Errorf("HTML missing %q:\n%s", expected, html)
		}
	}
	if strings.Contains(html, "secret=") {
		t.Fatal("HTML contains a configured secret")
	}
	if strings.Contains(html, `href="https://artifacts/`) {
		t.Fatal("HTML made an untrusted artifact path clickable")
	}

	var junitOutput bytes.Buffer
	if err := renderer.RenderJUnitXML(&junitOutput, input); err != nil {
		t.Fatal(err)
	}
	var junit junitSuites
	if err := xml.Unmarshal(junitOutput.Bytes(), &junit); err != nil {
		t.Fatal(err)
	}
	packageSuite := findJUnitSuite(junit.Suites, "pkg/failing")
	if packageSuite == nil {
		t.Fatal("JUnit omitted failing package suite")
	}
	if packageSuite.Failures != 1 || packageSuite.Errors != 1 {
		t.Fatalf(
			"package JUnit failures/errors = %d/%d, want 1/1\n%s",
			packageSuite.Failures,
			packageSuite.Errors,
			junitOutput.String(),
		)
	}
	if got := findJUnitProperty(
		packageSuite.Properties,
		"tested.attribute.1.key",
	); got != "owner [REDACTED]" {
		t.Fatalf("JUnit package attribute key = %q", got)
	}
	if got := findJUnitProperty(
		packageSuite.Properties,
		"tested.artifact.1.path",
	); got != "https://artifacts/[REDACTED]" {
		t.Fatalf("JUnit package artifact path = %q", got)
	}
	if got := findJUnitProperty(
		packageSuite.Cases[0].Properties,
		"tested.artifact.1.path",
	); got != "/tmp/[REDACTED]" {
		t.Fatalf("JUnit occurrence artifact path = %q", got)
	}
	outputSuite := findJUnitSuite(
		junit.Suites,
		"tested unattributed output",
	)
	if outputSuite == nil || len(outputSuite.Cases) != 1 {
		t.Fatalf("JUnit unattributed output suite = %#v", outputSuite)
	}
	outputCase := outputSuite.Cases[0]
	if strings.Contains(outputCase.SystemOut, "secret=") ||
		!strings.Contains(outputCase.SystemOut, "[REDACTED]") {
		t.Fatalf("JUnit unattributed output = %q", outputCase.SystemOut)
	}
	if got := findJUnitProperty(
		outputCase.Properties,
		"tested.output_bytes",
	); got != "24" {
		t.Fatalf("JUnit output bytes = %q, want 24", got)
	}
	diagnosticSuite := findJUnitSuite(
		junit.Suites,
		"tested stream integrity",
	)
	if diagnosticSuite == nil {
		t.Fatal("JUnit omitted diagnostic state")
	}
	if got := findJUnitProperty(
		diagnosticSuite.Properties,
		"tested.diagnostic_count",
	); got != "3" {
		t.Fatalf("JUnit diagnostic count = %q, want 3", got)
	}
	foundOmitted := false
	for _, testCase := range diagnosticSuite.Cases {
		if testCase.Name == "[additional diagnostics omitted]" &&
			testCase.Error != nil {
			foundOmitted = true
		}
	}
	if !foundOmitted {
		t.Fatal("JUnit omitted the deterministic diagnostic truncation case")
	}

	var consoleOutput bytes.Buffer
	console, err := NewConsole(ConsoleOptions{
		Writer:         &consoleOutput,
		Format:         ConsolePlain,
		Color:          ColorNever,
		RedactPatterns: []string{`secret=[[:alpha:]]+`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := console.Final(input); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"2 benchmarked",
		"package [REDACTED] terminal contradiction",
		"Unattributed test output:",
		"Integrity diagnostics: 1 retained of 3",
		"[diagnostic list truncated]",
	} {
		if !strings.Contains(consoleOutput.String(), expected) {
			t.Errorf(
				"console missing %q:\n%s",
				expected,
				consoleOutput.String(),
			)
		}
	}
	if strings.Contains(consoleOutput.String(), "secret=") {
		t.Fatal("console contains a configured secret")
	}
}

func TestKnownZeroDurationRemainsDistinctFromUnknown(t *testing.T) {
	renderer, err := New(Options{Title: "duration"})
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Result: result.Result{
		Finalized: true,
		Metadata:  result.RunMetadata{ExitCode: 0},
		Timing: result.Timing{
			WallMeasured: true,
		},
		Packages: []result.Package{
			{
				Name:           "pkg/known",
				Status:         result.StatusPassed,
				Elapsed:        0,
				DurationSource: result.DurationGoElapsed,
				Tests: []result.TestOccurrence{{
					ID: result.OccurrenceID{
						Package: "pkg/known",
						Name:    "BenchmarkZero",
						Ordinal: 1,
					},
					Status:         result.StatusBenchmarked,
					Kind:           result.TestKindBenchmark,
					Elapsed:        0,
					DurationSource: result.DurationGoElapsed,
				}},
			},
			{
				Name:           "pkg/unknown",
				Status:         result.StatusPassed,
				DurationSource: result.DurationUnknown,
				Tests: []result.TestOccurrence{{
					ID: result.OccurrenceID{
						Package: "pkg/unknown",
						Name:    "TestUnknownDuration",
						Ordinal: 1,
					},
					Status:         result.StatusPassed,
					Kind:           result.TestKindTest,
					DurationSource: result.DurationUnknown,
				}},
			},
		},
		Summary: result.Summary{
			Packages: result.OutcomeCounts{Total: 2, Passed: 2},
			Tests: result.OutcomeCounts{
				Total:       2,
				Passed:      1,
				Benchmarked: 1,
			},
		},
	}}

	summary := renderer.buildSummary(input)
	if len(summary.Packages) != 2 {
		t.Fatalf("summary packages = %d, want 2", len(summary.Packages))
	}
	if summary.Packages[0].ElapsedNS == nil ||
		*summary.Packages[0].ElapsedNS != 0 {
		t.Fatalf(
			"known package elapsed = %#v, want pointer to zero",
			summary.Packages[0].ElapsedNS,
		)
	}
	if summary.Packages[0].Tests[0].ElapsedNS == nil ||
		*summary.Packages[0].Tests[0].ElapsedNS != 0 {
		t.Fatalf(
			"known occurrence elapsed = %#v, want pointer to zero",
			summary.Packages[0].Tests[0].ElapsedNS,
		)
	}
	if summary.Packages[1].ElapsedNS != nil ||
		summary.Packages[1].Tests[0].ElapsedNS != nil {
		t.Fatal("unknown duration was serialized as a numeric duration")
	}

	var htmlOutput bytes.Buffer
	if err := renderer.RenderTestOutputHTML(&htmlOutput, input); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`data-duration="0"`,
		`title="Duration source: go_elapsed">Duration 0s`,
		"duration unavailable (unknown)",
		`<dt>Benchmarks</dt><dd class="passed">1</dd>`,
	} {
		if !strings.Contains(htmlOutput.String(), expected) {
			t.Errorf("HTML missing %q:\n%s", expected, htmlOutput.String())
		}
	}

	var junitOutput bytes.Buffer
	if err := renderer.RenderJUnitXML(&junitOutput, input); err != nil {
		t.Fatal(err)
	}
	var junit junitSuites
	if err := xml.Unmarshal(junitOutput.Bytes(), &junit); err != nil {
		t.Fatal(err)
	}
	if junit.Time != "0" {
		t.Fatalf("measured zero JUnit wall duration = %q, want 0", junit.Time)
	}
	knownSuite := findJUnitSuite(junit.Suites, "pkg/known")
	unknownSuite := findJUnitSuite(junit.Suites, "pkg/unknown")
	if knownSuite == nil || unknownSuite == nil {
		t.Fatalf("JUnit suites missing: %#v", junit.Suites)
	}
	if knownSuite.Time != "0" || knownSuite.Cases[0].Time != "0" {
		t.Fatalf(
			"known JUnit times = %q/%q, want 0/0",
			knownSuite.Time,
			knownSuite.Cases[0].Time,
		)
	}
	if unknownSuite.Time != "" || unknownSuite.Cases[0].Time != "" {
		t.Fatalf(
			"unknown JUnit times = %q/%q, want omitted",
			unknownSuite.Time,
			unknownSuite.Cases[0].Time,
		)
	}
	if got := findJUnitProperty(
		knownSuite.Cases[0].Properties,
		"tested.duration_known",
	); got != "true" {
		t.Fatalf("known JUnit duration property = %q", got)
	}
	if got := findJUnitProperty(
		unknownSuite.Cases[0].Properties,
		"tested.duration_known",
	); got != "false" {
		t.Fatalf("unknown JUnit duration property = %q", got)
	}

	var consoleOutput bytes.Buffer
	console, err := NewConsole(ConsoleOptions{
		Writer: &consoleOutput,
		Format: ConsolePlain,
		Color:  ColorNever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := console.Package(input.Result.Packages[0]); err != nil {
		t.Fatal(err)
	}
	if err := console.Package(input.Result.Packages[1]); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"pkg/known 0s (go_elapsed)",
		"pkg/unknown duration unavailable (unknown)",
		"1 benchmarked",
	} {
		if !strings.Contains(consoleOutput.String(), expected) {
			t.Errorf(
				"live console missing %q:\n%s",
				expected,
				consoleOutput.String(),
			)
		}
	}
}

func TestCoveragePresentationUsesExactUint64Arithmetic(t *testing.T) {
	renderer, err := New(Options{Title: "exact coverage"})
	if err != nil {
		t.Fatal(err)
	}
	input := Input{
		Result: result.Result{
			Finalized: true,
			Metadata:  result.RunMetadata{ExitCode: 0},
		},
		Coverage: &coverage.Profile{
			Mode: coverage.ModeCount,
			Total: coverage.Totals{
				Statements: ^uint64(0),
				Covered:    ^uint64(0) - 1,
			},
		},
	}
	const expected = "99.999999999999999994578989"
	view := renderer.buildView(input)
	if view.Coverage == nil ||
		view.Coverage.Percentage != ">99.99%" {
		t.Fatalf("coverage view = %#v", view.Coverage)
	}
	summary := renderer.buildSummary(input)
	if summary.Coverage == nil ||
		summary.Coverage.PercentExact != expected {
		t.Fatalf("summary exact coverage = %#v", summary.Coverage)
	}
	if summary.Coverage.Percent == nil {
		t.Fatal("summary omitted backward-compatible numeric coverage")
	}
}

func TestCoveragePercentagesStayConciseAcrossPresentations(t *testing.T) {
	renderer, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		covered    uint64
		statements uint64
		minimum    string
		actual     string
		want       string
		exact      string
		satisfied  bool
	}{
		{
			name: "reported policy", covered: 5789, statements: 7195,
			minimum: "1", actual: "80.458651841556636553161918",
			want: "80.46%", exact: "80.46", satisfied: true,
		},
		{
			name: "repeating below minimum", covered: 2, statements: 3,
			minimum: "66.67", actual: "66.666666666666666666666667",
			want: "66.67%", exact: "66.67",
		},
		{
			name: "half rounds up", covered: 1, statements: 32,
			minimum: "100", actual: "3.125", want: "3.13%", exact: "3.13",
		},
		{
			name: "positive rounds to zero", covered: 1, statements: ^uint64(0),
			minimum: "100", actual: "0.000000000000000005421011",
			want: "<0.01%", exact: "0.000000000000000005421011",
		},
		{
			name: "partial rounds to full", covered: ^uint64(0) - 1, statements: ^uint64(0),
			minimum: "100", actual: "99.999999999999999994578989",
			want: ">99.99%", exact: "99.999999999999999994578989",
		},
		{
			name: "zero", statements: 10, minimum: "100", actual: "0",
			want: "0.00%", exact: "0.00",
		},
		{
			name: "full", covered: 10, statements: 10, minimum: "100", actual: "100",
			want: "100.00%", exact: "100.00", satisfied: true,
		},
		{
			name: "unavailable", minimum: "1", want: "unavailable",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			totals := coverage.Totals{Covered: test.covered, Statements: test.statements}
			policy := CoveragePolicy{
				Minimum: test.minimum, Actual: test.actual,
				Covered: test.covered, Statements: test.statements,
				Available: test.statements > 0, Satisfied: test.satisfied,
			}
			input := Input{
				Result: result.Result{Finalized: true},
				Coverage: &coverage.Profile{
					Mode: coverage.ModeCount, Total: totals,
					Files: []coverage.FileSummary{{Name: "sample.go", Totals: totals}},
				},
				Assessment: &Assessment{
					ChildStarted: true, ChildExitKnown: true, CoveragePolicy: &policy,
				},
			}
			var testHTML, indexHTML bytes.Buffer
			if err := renderer.RenderTestOutputHTML(&testHTML, input); err != nil {
				t.Fatal(err)
			}
			if err := renderer.RenderIndexHTML(&indexHTML, input, nil); err != nil {
				t.Fatal(err)
			}
			// The test page includes aggregate, per-file, and policy percentages.
			if got := strings.Count(testHTML.String(), html.EscapeString(test.want)); got < 4 {
				t.Errorf("test HTML contains %q %d times, want at least 4", test.want, got)
			}
			if !strings.Contains(indexHTML.String(), html.EscapeString(test.want)) {
				t.Errorf("index HTML missing %q", test.want)
			}
			for _, format := range []ConsoleFormat{ConsolePlain, ConsoleMarkdown} {
				var output bytes.Buffer
				console, err := NewConsole(ConsoleOptions{Writer: &output, Format: format})
				if err != nil {
					t.Fatal(err)
				}
				if err := console.Final(input); err != nil {
					t.Fatal(err)
				}
				want := test.want
				if format == ConsoleMarkdown {
					want = escapeMarkdown(want)
				}
				if !strings.Contains(output.String(), "actual "+want) ||
					strings.Count(output.String(), want) < 2 {
					t.Errorf("%s console missing concise coverage %q: %s", format, want, &output)
				}
			}
			var junit bytes.Buffer
			if err := renderer.RenderJUnitXML(&junit, input); err != nil {
				t.Fatal(err)
			}
			var suites junitSuites
			if err := xml.Unmarshal(junit.Bytes(), &suites); err != nil {
				t.Fatal(err)
			}
			if !test.satisfied {
				suite := findJUnitSuite(suites.Suites, "tested coverage policy")
				if suite == nil || len(suite.Cases) != 1 {
					t.Fatalf("missing JUnit policy case: %s", &junit)
				}
				if policy.Available {
					want := "weighted statement coverage " + test.want +
						" is below minimum " + test.minimum + "%"
					if suite.Failures != 1 || suite.Cases[0].Failure == nil ||
						suite.Cases[0].Failure.Message != want {
						t.Errorf("JUnit policy failure = %#v, want %q", suite.Cases[0].Failure, want)
					}
				} else if suite.Errors != 1 {
					t.Errorf("unavailable JUnit policy errors = %d, want 1", suite.Errors)
				}
			}
			var summary bytes.Buffer
			if err := renderer.RenderSummaryJSON(&summary, input); err != nil {
				t.Fatal(err)
			}
			var document summaryDocument
			if err := json.Unmarshal(summary.Bytes(), &document); err != nil {
				t.Fatal(err)
			}
			if document.Coverage.PercentExact != test.exact ||
				document.Coverage.Files[0].PercentExact != test.exact {
				t.Errorf("JSON decimals changed: %#v", document.Coverage)
			}
			if got := document.Assessment.CoveragePolicy; *got != policy {
				t.Errorf("JSON policy changed: %#v, want %#v", got, policy)
			}
			if policy.Actual != test.actual || policy.Satisfied != test.satisfied {
				t.Errorf("rendering mutated the input policy: %#v", policy)
			}
		})
	}
}

func TestJUnitAddsNonPassingOutcomeSentinel(t *testing.T) {
	renderer, err := New(Options{Title: "sentinel"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		input       Input
		wantOutcome string
		wantFailure bool
	}{
		{
			name:        "unfinalized snapshot",
			input:       Input{},
			wantOutcome: "incomplete",
		},
		{
			name: "report failure without issue",
			input: Input{
				Result: result.Result{
					Finalized: true,
					Metadata:  result.RunMetadata{ExitCode: 0},
				},
				Assessment: &Assessment{
					ChildStarted:   true,
					ChildExitKnown: true,
					ReportFailed:   true,
				},
			},
			wantOutcome: "incomplete",
		},
		{
			name: "unassessed nonzero child exit",
			input: Input{
				Result: result.Result{
					Finalized: true,
					Metadata:  result.RunMetadata{ExitCode: 9},
				},
			},
			wantOutcome: "failed",
			wantFailure: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := renderer.RenderJUnitXML(&output, test.input); err != nil {
				t.Fatal(err)
			}
			var junit junitSuites
			if err := xml.Unmarshal(output.Bytes(), &junit); err != nil {
				t.Fatal(err)
			}
			if junit.Failures+junit.Errors == 0 {
				t.Fatalf(
					"non-passing outcome produced false-green JUnit:\n%s",
					output.String(),
				)
			}
			suite := findJUnitSuite(junit.Suites, "tested outcome")
			if suite == nil || len(suite.Cases) != 1 {
				t.Fatalf("outcome sentinel suite = %#v", suite)
			}
			testCase := suite.Cases[0]
			if test.wantFailure && testCase.Failure == nil {
				t.Fatalf("failed outcome sentinel is not a failure: %#v", testCase)
			}
			if !test.wantFailure && testCase.Error == nil {
				t.Fatalf(
					"%s outcome sentinel is not an error: %#v",
					test.wantOutcome,
					testCase,
				)
			}
			if got := findJUnitProperty(
				testCase.Properties,
				"tested.outcome",
			); got != test.wantOutcome {
				t.Fatalf("sentinel outcome = %q, want %q", got, test.wantOutcome)
			}
		})
	}
}

func TestResourceBudgetsProjectWithoutFalseGreen(t *testing.T) {
	renderer, err := New(Options{Title: "resource budgets"})
	if err != nil {
		t.Fatal(err)
	}
	summary := result.Summary{
		TotalOutputBytes:           4096,
		TotalOutputRetainedBytes:   1024,
		TotalOutputTruncated:       true,
		NormalizedEntries:          12,
		NormalizedEntriesRetained:  10,
		NormalizedEntriesTruncated: true,
		NormalizedBytes:            8192,
		NormalizedBytesRetained:    2048,
		NormalizedBytesTruncated:   true,
		Incomplete:                 true,
	}
	input := Input{Result: result.Result{
		Finalized:  true,
		Incomplete: true,
		Metadata:   result.RunMetadata{ExitCode: 0},
		Summary:    summary,
	}}
	if got := renderer.buildView(input).Outcome; got != "incomplete" {
		t.Fatalf("resource-limited outcome = %q, want incomplete", got)
	}

	var htmlOutput, summaryOutput, junitOutput bytes.Buffer
	if err := renderer.RenderTestOutputHTML(&htmlOutput, input); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(
		htmlOutput.String(),
		"Projection resource limits",
	) || !strings.Contains(
		htmlOutput.String(),
		"retained 10 of 12",
	) {
		t.Fatalf("HTML omitted resource facts:\n%s", htmlOutput.String())
	}
	if err := renderer.RenderSummaryJSON(&summaryOutput, input); err != nil {
		t.Fatal(err)
	}
	var document summaryDocument
	if err := json.Unmarshal(summaryOutput.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if !document.Counts.Incomplete ||
		document.Counts.NormalizedEntries != 12 ||
		document.Counts.NormalizedBytesRetained != 2048 {
		t.Fatalf("summary resource facts = %#v", document.Counts)
	}
	if err := renderer.RenderJUnitXML(&junitOutput, input); err != nil {
		t.Fatal(err)
	}
	var junit junitSuites
	if err := xml.Unmarshal(junitOutput.Bytes(), &junit); err != nil {
		t.Fatal(err)
	}
	suite := findJUnitSuite(junit.Suites, "tested resource limits")
	if suite == nil || suite.Errors != 1 || len(suite.Cases) != 1 {
		t.Fatalf("resource JUnit suite = %#v\n%s", suite, junitOutput.String())
	}
	if got := findJUnitProperty(
		suite.Cases[0].Properties,
		"tested.normalized_entries_retained",
	); got != "10" {
		t.Fatalf("retained-entry JUnit property = %q, want 10", got)
	}

	var consoleOutput bytes.Buffer
	console, err := NewConsole(ConsoleOptions{
		Writer: &consoleOutput,
		Title:  "resource budgets",
		Format: ConsolePlain,
		Color:  ColorNever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := console.Final(input); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Derived output: retained 1024 of 4096 bytes",
		"Normalized entries: retained 10 of 12",
		"Normalized strings: retained 2048 of 8192 bytes",
	} {
		if !strings.Contains(consoleOutput.String(), expected) {
			t.Errorf(
				"console omitted %q:\n%s",
				expected,
				consoleOutput.String(),
			)
		}
	}
}

func TestOutputBudgetClippingIsNotSemanticFailure(t *testing.T) {
	renderer, err := New(Options{Title: "output clipping"})
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Result: result.Result{
		Finalized: true,
		Metadata:  result.RunMetadata{ExitCode: 0},
		Summary: result.Summary{
			TotalOutputBytes:           4096,
			TotalOutputRetainedBytes:   1024,
			TotalOutputTruncated:       true,
			NormalizedEntries:          2,
			NormalizedEntriesRetained:  1,
			NormalizedEntriesTruncated: true,
			NormalizedBytes:            5,
			NormalizedBytesRetained:    3,
			NormalizedBytesTruncated:   true,
		},
	}}
	if got := renderer.buildView(input).Outcome; got != "passed" {
		t.Fatalf("output-clipped outcome = %q, want passed", got)
	}
	var output bytes.Buffer
	if err := renderer.RenderJUnitXML(&output, input); err != nil {
		t.Fatal(err)
	}
	var junit junitSuites
	if err := xml.Unmarshal(output.Bytes(), &junit); err != nil {
		t.Fatal(err)
	}
	suite := findJUnitSuite(junit.Suites, "tested resource limits")
	if suite == nil || suite.Errors != 0 || suite.Failures != 0 {
		t.Fatalf("output-only resource JUnit suite = %#v\n%s", suite, output.String())
	}
}

func TestOutputTiesPreserveSourceOrderAndMarkdownEscapesTildeFence(
	t *testing.T,
) {
	output := []result.Output{
		{Sequence: 4, Line: 2, Text: "z-first\n"},
		{Sequence: 4, Line: 2, Text: "a-second\n"},
	}
	if got := joinOutput(sortedOutput(output)); got != "z-first\na-second\n" {
		t.Fatalf("tied output order = %q", got)
	}
	if got := escapeMarkdown("~~~html"); strings.Contains(got, "~~~") ||
		got != `\~\~\~html` {
		t.Fatalf("escaped tilde fence = %q", got)
	}
}

func findJUnitSuite(
	suites []junitSuite,
	name string,
) *junitSuite {
	for index := range suites {
		if suites[index].Name == name {
			return &suites[index]
		}
	}
	return nil
}

func findJUnitProperty(
	properties []junitProp,
	name string,
) string {
	for _, property := range properties {
		if property.Name == name {
			return property.Value
		}
	}
	return ""
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func testInput() Input {
	started := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	finished := started.Add(4 * time.Second)
	eventStarted := started.Add(100 * time.Millisecond)
	eventFinished := started.Add(3900 * time.Millisecond)
	return Input{
		Result: result.Result{
			Finalized: true,
			Metadata: result.RunMetadata{
				Command:    []string{"go", "test", "./...", "secret=alpha"},
				WorkDir:    "/workspace/secret=alpha",
				StartedAt:  started,
				FinishedAt: finished,
				ExitCode:   1,
				Signal:     "terminated",
			},
			Timing: result.Timing{
				WallStartedAt:   &started,
				WallFinishedAt:  &finished,
				WallDuration:    4 * time.Second,
				WallMeasured:    true,
				EventStartedAt:  &eventStarted,
				EventFinishedAt: &eventFinished,
				EventDuration:   3800 * time.Millisecond,
				EventEstimated:  true,
				EventTimestamps: 8,
			},
			Packages: []result.Package{
				{
					Name:           "pkg/z",
					Status:         result.StatusFailed,
					Elapsed:        3 * time.Second,
					DurationSource: result.DurationGoElapsed,
					Tests: []result.TestOccurrence{
						{
							ID: result.OccurrenceID{
								Package: "pkg/z",
								Name:    "TestRepeat",
								Ordinal: 2,
							},
							Kind:                result.TestKindTest,
							Status:              result.StatusFailed,
							Elapsed:             1500 * time.Millisecond,
							DurationSource:      result.DurationEventEstimate,
							Output:              testOutput(`failure secret=bravo </script><script>alert("payload")</script> ` + "\x1b[31m\x01"),
							OutputBytes:         200,
							OutputRetainedBytes: 90,
							OutputTruncated:     true,
							Signals:             result.Signals{Panic: true},
						},
						{
							ID: result.OccurrenceID{
								Package: "pkg/z",
								Name:    "TestRepeat",
								Ordinal: 1,
							},
							Kind:                result.TestKindTest,
							Status:              result.StatusPassed,
							Elapsed:             2 * time.Second,
							DurationSource:      result.DurationGoElapsed,
							Output:              testOutput("PASSING-LOG secret=bravo\n"),
							OutputBytes:         25,
							OutputRetainedBytes: 25,
						},
					},
				},
				{
					Name:             "pkg/a",
					Status:           result.StatusIncomplete,
					IncompleteReason: "missing package terminal event",
					Tests: []result.TestOccurrence{
						{
							ID: result.OccurrenceID{
								Package: "pkg/a",
								Name:    "TestInterrupted",
								Ordinal: 1,
							},
							Kind:             result.TestKindTest,
							Status:           result.StatusIncomplete,
							IncompleteReason: "stream ended before terminal event",
							Output:           testOutput("partial secret=bravo\n"),
							OutputBytes:      21,
						},
					},
				},
			},
			Builds: []result.Build{
				{
					ImportPath:          "build-only/package",
					Status:              result.StatusFailed,
					Output:              testOutput("compile \x01 secret=bravo\n"),
					OutputBytes:         24,
					OutputRetainedBytes: 24,
				},
			},
			Diagnostics: []result.Diagnostic{
				{
					Kind:      protocol.DiagnosticMalformed,
					Sequence:  99,
					Line:      99,
					Bytes:     5,
					Preview:   "\x01oops secret=bravo",
					Truncated: true,
					Message:   "malformed secret=bravo",
				},
			},
			DiagnosticCount: 1,
			Signals:         result.Signals{Panic: true},
			Summary: result.Summary{
				Packages: result.OutcomeCounts{
					Total:      2,
					Failed:     1,
					Incomplete: 1,
				},
				Tests: result.OutcomeCounts{
					Total:      3,
					Passed:     1,
					Failed:     1,
					Incomplete: 1,
				},
				Builds: result.OutcomeCounts{
					Total:  1,
					Failed: 1,
				},
				BuildFailures:       1,
				Diagnostics:         1,
				DiagnosticsRetained: 1,
				Panic:               true,
			},
		},
		Coverage: &coverage.Profile{
			Mode: coverage.ModeCount,
			Files: []coverage.FileSummary{
				{
					Name:   "pkg/z/z.go",
					Totals: coverage.Totals{Statements: 6, Covered: 3},
				},
				{
					Name:   "pkg/a/a.go",
					Totals: coverage.Totals{Statements: 4, Covered: 4},
				},
			},
			Total: coverage.Totals{Statements: 10, Covered: 7},
		},
	}
}

func testOutput(text string) []result.Output {
	return []result.Output{
		{
			Sequence:      1,
			Line:          1,
			Scope:         result.OutputTest,
			Text:          text,
			OriginalBytes: int64(len(text)),
		},
	}
}
