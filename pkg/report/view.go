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
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/greenpau/tested/pkg/coverage"
	"github.com/greenpau/tested/pkg/result"
)

type reportView struct {
	Title                string
	Finalized            bool
	Outcome              string
	Packages             []packageView
	Builds               []buildView
	UnattributedOutput   *unattributedOutputView
	Diagnostics          []diagnosticView
	DiagnosticCount      uint64
	DiagnosticsRetained  uint64
	DiagnosticsTruncated bool
	Coverage             *coverageView
	Slowest              []slowView
	Summary              result.Summary
	Metadata             result.RunMetadata
	Timing               result.Timing
	Assessment           *assessmentView
}

type packageView struct {
	Name                string
	Status              string
	StatusClass         string
	Duration            string
	DurationNanos       int64
	DurationKnown       bool
	DurationSource      string
	Cached              bool
	NoTests             bool
	FailedBuild         string
	IncompleteReason    string
	Output              string
	OutputBytes         int64
	OutputRetainedBytes int64
	OutputTruncated     bool
	Attributes          []attributeView
	Artifacts           []artifactView
	Tests               []occurrenceView
}

type occurrenceView struct {
	HTMLID              string
	ParentHTMLID        string
	Package             string
	Name                string
	Label               string
	Ordinal             uint64
	Kind                string
	Status              string
	StatusClass         string
	Duration            string
	DurationNanos       int64
	DurationKnown       bool
	DurationSource      string
	IncompleteReason    string
	Output              string
	OutputBytes         int64
	OutputRetainedBytes int64
	OutputTruncated     bool
	Attributes          []attributeView
	Artifacts           []artifactView
	Signals             result.Signals
}

type buildView struct {
	ImportPath          string
	Status              string
	StatusClass         string
	Output              string
	OutputBytes         int64
	OutputRetainedBytes int64
	OutputTruncated     bool
	Signals             result.Signals
}

type unattributedOutputView struct {
	Output              string
	OutputBytes         int64
	OutputRetainedBytes int64
	OutputTruncated     bool
}

type attributeView struct {
	Sequence uint64 `json:"sequence"`
	Line     uint64 `json:"line,omitempty"`
	Key      string `json:"key"`
	Value    string `json:"value"`
}

type artifactView struct {
	Sequence uint64 `json:"sequence"`
	Line     uint64 `json:"line,omitempty"`
	Path     string `json:"path"`
}

type diagnosticView struct {
	Kind      string `json:"kind"`
	Sequence  uint64 `json:"sequence"`
	Line      uint64 `json:"line"`
	Bytes     int64  `json:"bytes"`
	Preview   string `json:"preview,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Message   string `json:"message"`
}

type coverageView struct {
	Mode       string
	Statements uint64
	Covered    uint64
	Percentage string
	Available  bool
	Files      []coverageFileView
}

type coverageFileView struct {
	Name       string
	Statements uint64
	Covered    uint64
	Percentage string
	Available  bool
}

type slowView struct {
	Package        string
	Name           string
	Label          string
	Ordinal        uint64
	Duration       string
	DurationNanos  int64
	DurationSource string
	Status         string
	StatusClass    string
}

func (r *Renderer) buildView(input Input) reportView {
	view := reportView{
		Title:                r.redact(r.title),
		Finalized:            input.Result.Finalized,
		Outcome:              outcome(input),
		Summary:              input.Result.Summary,
		Metadata:             redactMetadata(input.Result.Metadata, r.redact),
		Timing:               input.Result.Timing,
		Assessment:           r.buildAssessmentView(input.Assessment),
		DiagnosticCount:      input.Result.DiagnosticCount,
		DiagnosticsTruncated: input.Result.DiagnosticsTruncated,
	}
	if view.DiagnosticCount == 0 {
		view.DiagnosticCount = input.Result.Summary.Diagnostics
	}

	packages := append([]result.Package(nil), input.Result.Packages...)
	sort.SliceStable(packages, func(i, j int) bool {
		return packages[i].Name < packages[j].Name
	})
	for _, pkg := range packages {
		packageOutput := sortedOutput(pkg.Output)
		durationKnown := knownDuration(pkg.Elapsed, pkg.DurationSource)
		pkgView := packageView{
			Name:                r.redact(pkg.Name),
			Status:              string(pkg.Status),
			StatusClass:         statusClass(pkg.Status),
			Duration:            formatDuration(pkg.Elapsed, durationKnown),
			DurationNanos:       int64(pkg.Elapsed),
			DurationKnown:       durationKnown,
			DurationSource:      string(pkg.DurationSource),
			Cached:              pkg.Cached,
			NoTests:             pkg.NoTests,
			FailedBuild:         r.redact(pkg.FailedBuild),
			IncompleteReason:    r.redact(pkg.IncompleteReason),
			Output:              r.redactOutput(joinOutput(packageOutput), pkg.OutputTruncated),
			OutputBytes:         pkg.OutputBytes,
			OutputRetainedBytes: pkg.OutputRetainedBytes,
			OutputTruncated:     pkg.OutputTruncated,
			Attributes:          r.buildAttributeViews(pkg.Attributes),
			Artifacts:           r.buildArtifactViews(pkg.Artifacts),
		}

		tests := append([]result.TestOccurrence(nil), pkg.Tests...)
		sort.SliceStable(tests, func(i, j int) bool {
			left, right := tests[i].ID, tests[j].ID
			if left.Name != right.Name {
				return left.Name < right.Name
			}
			return left.Ordinal < right.Ordinal
		})
		for _, test := range tests {
			output := sortedOutput(test.Output)
			durationKnown := knownDuration(test.Elapsed, test.DurationSource)
			testView := occurrenceView{
				Package:             r.redact(test.ID.Package),
				Name:                r.redact(test.ID.Name),
				Label:               r.redact(occurrenceLabel(test.ID.Name, test.ID.Ordinal)),
				Ordinal:             test.ID.Ordinal,
				Kind:                string(test.Kind),
				Status:              string(test.Status),
				StatusClass:         statusClass(test.Status),
				Duration:            formatDuration(test.Elapsed, durationKnown),
				DurationNanos:       int64(test.Elapsed),
				DurationKnown:       durationKnown,
				DurationSource:      string(test.DurationSource),
				IncompleteReason:    r.redact(test.IncompleteReason),
				Output:              r.redactOutput(joinOutput(output), test.OutputTruncated),
				OutputBytes:         test.OutputBytes,
				OutputRetainedBytes: test.OutputRetainedBytes,
				OutputTruncated:     test.OutputTruncated,
				Attributes:          r.buildAttributeViews(test.Attributes),
				Artifacts:           r.buildArtifactViews(test.Artifacts),
				Signals:             test.Signals,
			}
			pkgView.Tests = append(pkgView.Tests, testView)
			if test.Elapsed > 0 {
				view.Slowest = append(view.Slowest, slowView{
					Package:        testView.Package,
					Name:           testView.Name,
					Label:          testView.Label,
					Ordinal:        testView.Ordinal,
					Duration:       testView.Duration,
					DurationNanos:  int64(test.Elapsed),
					DurationSource: testView.DurationSource,
					Status:         testView.Status,
					StatusClass:    testView.StatusClass,
				})
			}
		}
		bindHTMLHierarchy(tests, pkgView.Tests, len(view.Packages))
		view.Packages = append(view.Packages, pkgView)
	}

	builds := append([]result.Build(nil), input.Result.Builds...)
	sort.SliceStable(builds, func(i, j int) bool {
		return builds[i].ImportPath < builds[j].ImportPath
	})
	for _, build := range builds {
		view.Builds = append(view.Builds, buildView{
			ImportPath:          r.redact(build.ImportPath),
			Status:              string(build.Status),
			StatusClass:         statusClass(build.Status),
			Output:              r.redactOutput(joinOutput(sortedOutput(build.Output)), build.OutputTruncated),
			OutputBytes:         build.OutputBytes,
			OutputRetainedBytes: build.OutputRetainedBytes,
			OutputTruncated:     build.OutputTruncated,
			Signals:             build.Signals,
		})
	}

	if len(input.Result.UnattributedOutput) > 0 ||
		input.Result.OutputBytes != 0 ||
		input.Result.OutputRetainedBytes != 0 ||
		input.Result.OutputTruncated {
		view.UnattributedOutput = &unattributedOutputView{
			Output: r.redactOutput(
				joinOutput(sortedOutput(input.Result.UnattributedOutput)),
				input.Result.OutputTruncated,
			),
			OutputBytes:         input.Result.OutputBytes,
			OutputRetainedBytes: input.Result.OutputRetainedBytes,
			OutputTruncated:     input.Result.OutputTruncated,
		}
	}

	diagnostics := append([]result.Diagnostic(nil), input.Result.Diagnostics...)
	sort.SliceStable(diagnostics, func(i, j int) bool {
		if diagnostics[i].Sequence != diagnostics[j].Sequence {
			return diagnostics[i].Sequence < diagnostics[j].Sequence
		}
		return diagnostics[i].Line < diagnostics[j].Line
	})
	for _, diagnostic := range diagnostics {
		view.Diagnostics = append(view.Diagnostics, diagnosticView{
			Kind:      string(diagnostic.Kind),
			Sequence:  diagnostic.Sequence,
			Line:      diagnostic.Line,
			Bytes:     diagnostic.Bytes,
			Preview:   r.redactOutput(diagnostic.Preview, diagnostic.Truncated),
			Truncated: diagnostic.Truncated,
			Message:   r.redact(diagnostic.Message),
		})
	}
	view.DiagnosticsRetained = uint64(len(view.Diagnostics))
	if view.DiagnosticCount < view.DiagnosticsRetained {
		view.DiagnosticCount = view.DiagnosticsRetained
	}

	view.Coverage = r.buildCoverageView(input.Coverage)
	sort.SliceStable(view.Slowest, func(i, j int) bool {
		left, right := view.Slowest[i], view.Slowest[j]
		if left.DurationNanos != right.DurationNanos {
			return left.DurationNanos > right.DurationNanos
		}
		if left.Package != right.Package {
			return left.Package < right.Package
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.Ordinal < right.Ordinal
	})
	if len(view.Slowest) > r.slowest {
		view.Slowest = view.Slowest[:r.slowest]
	}
	return view
}

func (r *Renderer) buildAttributeViews(
	attributes []result.Attribute,
) []attributeView {
	if len(attributes) == 0 {
		return nil
	}
	view := make([]attributeView, 0, len(attributes))
	for _, attribute := range attributes {
		view = append(view, attributeView{
			Sequence: attribute.Sequence,
			Line:     attribute.Line,
			Key:      r.redact(attribute.Key),
			Value:    r.redact(attribute.Value),
		})
	}
	return view
}

func (r *Renderer) buildArtifactViews(
	artifacts []result.Artifact,
) []artifactView {
	if len(artifacts) == 0 {
		return nil
	}
	view := make([]artifactView, 0, len(artifacts))
	for _, artifact := range artifacts {
		view = append(view, artifactView{
			Sequence: artifact.Sequence,
			Line:     artifact.Line,
			Path:     r.redact(artifact.Path),
		})
	}
	return view
}

func (r *Renderer) buildCoverageView(profile *coverage.Profile) *coverageView {
	view := buildCoverageTotalView(profile)
	if view == nil {
		return nil
	}
	files := append([]coverage.FileSummary(nil), profile.Files...)
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})
	for _, file := range files {
		filePercentage, fileAvailable := formatCoveragePercentage(file.Totals)
		view.Files = append(view.Files, coverageFileView{
			Name:       r.redact(file.Name),
			Statements: file.Totals.Statements,
			Covered:    file.Totals.Covered,
			Percentage: filePercentage,
			Available:  fileAvailable,
		})
	}
	return view
}

func buildCoverageTotalView(profile *coverage.Profile) *coverageView {
	if profile == nil {
		return nil
	}
	percentage, available := formatCoveragePercentage(profile.Total)
	return &coverageView{
		Mode:       string(profile.Mode),
		Statements: profile.Total.Statements,
		Covered:    profile.Total.Covered,
		Percentage: percentage,
		Available:  available,
	}
}

func outcome(input Input) string {
	snapshot := input.Result
	if !snapshot.Finalized {
		return "incomplete"
	}
	if snapshot.Metadata.Interrupted {
		return "interrupted"
	}
	if input.Assessment != nil && input.Assessment.ChildExitKnown &&
		snapshot.Metadata.ExitCode != 0 {
		return "failed"
	}
	if input.Assessment != nil {
		assessment := input.Assessment
		if assessment.InfrastructureFailed || assessment.EvidenceIncomplete ||
			assessment.StreamCorrupt || !assessment.ChildExitKnown {
			return "incomplete"
		}
		if !assessment.ReportFailed {
			for _, issue := range assessment.Issues {
				if issue.Fatal {
					return "incomplete"
				}
			}
		}
	}
	if snapshot.Metadata.ExitCode != 0 {
		return "failed"
	}
	if snapshot.Incomplete || snapshot.Summary.Incomplete {
		return "incomplete"
	}
	semanticFailed := snapshot.Summary.Tests.Failed > 0 ||
		snapshot.Summary.Packages.Failed > 0 ||
		snapshot.Summary.BuildFailures > 0
	if semanticFailed {
		if input.Assessment != nil && input.Assessment.ChildExitKnown {
			return "incomplete"
		}
		return "failed"
	}
	if snapshot.Summary.Tests.Incomplete > 0 ||
		snapshot.Summary.Packages.Incomplete > 0 ||
		snapshot.Summary.Diagnostics > 0 {
		return "incomplete"
	}
	if input.Assessment != nil &&
		input.Assessment.CoveragePolicy != nil &&
		input.Assessment.CoveragePolicy.Available &&
		!input.Assessment.CoveragePolicy.Satisfied {
		return "coverage_failed"
	}
	if input.Assessment != nil {
		if input.Assessment.ReportFailed {
			return "incomplete"
		}
		for _, issue := range input.Assessment.Issues {
			if issue.Fatal {
				return "incomplete"
			}
		}
	}
	return "passed"
}

func statusClass(status result.Status) string {
	switch status {
	case result.StatusPassed, result.StatusBenchmarked:
		return "passed"
	case result.StatusFailed:
		return "failed"
	case result.StatusSkipped:
		return "skipped"
	case result.StatusIncomplete, result.StatusRunning, result.StatusPaused:
		return "incomplete"
	default:
		return "unknown"
	}
}

func occurrenceLabel(name string, ordinal uint64) string {
	if ordinal <= 1 {
		return name
	}
	return fmt.Sprintf("%s [attempt %d]", name, ordinal)
}

func sortedOutput(output []result.Output) []result.Output {
	cloned := append([]result.Output(nil), output...)
	sort.SliceStable(cloned, func(i, j int) bool {
		if cloned[i].Sequence != cloned[j].Sequence {
			return cloned[i].Sequence < cloned[j].Sequence
		}
		if cloned[i].Line != cloned[j].Line {
			return cloned[i].Line < cloned[j].Line
		}
		return false
	})
	return cloned
}

func joinOutput(output []result.Output) string {
	var builder strings.Builder
	for _, item := range output {
		builder.WriteString(item.Text)
	}
	return builder.String()
}

func redactMetadata(
	metadata result.RunMetadata,
	redact func(string) string,
) result.RunMetadata {
	metadata.WorkDir = redact(metadata.WorkDir)
	metadata.Signal = redact(metadata.Signal)
	metadata.Cancellation = redact(metadata.Cancellation)
	metadata.CancellationSignal = redact(metadata.CancellationSignal)
	metadata.Command = append([]string(nil), metadata.Command...)
	for index := range metadata.Command {
		metadata.Command[index] = redact(metadata.Command[index])
	}
	return metadata
}

func knownDuration(
	duration time.Duration,
	source result.DurationSource,
) bool {
	if duration != 0 {
		return true
	}
	switch source {
	case result.DurationGoElapsed, result.DurationEventEstimate:
		return true
	default:
		return false
	}
}

func formatDuration(duration time.Duration, known bool) string {
	if !known {
		return ""
	}
	return duration.String()
}

func durationSeconds(duration time.Duration, known bool) string {
	if !known {
		return ""
	}
	value := strconv.FormatFloat(duration.Seconds(), 'f', 9, 64)
	value = strings.TrimRight(value, "0")
	value = strings.TrimRight(value, ".")
	if value == "" {
		return "0"
	}
	return value
}

// formatCoveragePercentage rounds half up to two decimal places without
// implying zero or complete coverage when the exact ratio is between them.
func formatCoveragePercentage(totals coverage.Totals) (string, bool) {
	value, err := totals.FormatPercentage(2)
	if err != nil {
		return "unavailable", false
	}
	switch {
	case value == "100.00" && totals.Covered < totals.Statements:
		return ">99.99%", true
	case value == "0.00" && totals.Covered > 0:
		return "<0.01%", true
	}
	return value + "%", true
}

func truncateUTF8(value string, maximum int) (string, bool) {
	if maximum < 0 || len(value) <= maximum {
		return value, false
	}
	if maximum == 0 {
		return "", len(value) > 0
	}
	end := maximum
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end], true
}
