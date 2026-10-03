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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/greenpau/tested/pkg/coverage"
	"github.com/greenpau/tested/pkg/result"
)

type summaryDocument struct {
	Schema               string                     `json:"schema"`
	Title                string                     `json:"title"`
	Finalized            bool                       `json:"finalized"`
	Outcome              string                     `json:"outcome"`
	Run                  summaryRun                 `json:"run"`
	Timing               summaryTiming              `json:"timing"`
	Counts               result.Summary             `json:"counts"`
	Coverage             *summaryCoverage           `json:"coverage,omitempty"`
	Packages             []summaryPackage           `json:"packages,omitempty"`
	Builds               []summaryBuild             `json:"builds,omitempty"`
	UnattributedOutput   *summaryUnattributedOutput `json:"unattributed_output,omitempty"`
	Failures             []summaryFailure           `json:"failures,omitempty"`
	Diagnostics          []diagnosticView           `json:"diagnostics,omitempty"`
	DiagnosticCount      uint64                     `json:"diagnostic_count,omitempty"`
	DiagnosticsRetained  uint64                     `json:"diagnostics_retained,omitempty"`
	DiagnosticsTruncated bool                       `json:"diagnostics_truncated,omitempty"`
	Slowest              []summarySlow              `json:"slowest,omitempty"`
	Signals              result.Signals             `json:"signals,omitempty"`
	Assessment           *Assessment                `json:"assessment,omitempty"`
}

type summaryRun struct {
	Command             []string `json:"command,omitempty"`
	WorkDir             string   `json:"work_dir,omitempty"`
	StartedAt           string   `json:"started_at,omitempty"`
	FinishedAt          string   `json:"finished_at,omitempty"`
	ExitCode            int      `json:"exit_code"`
	Signal              string   `json:"signal,omitempty"`
	Interrupted         bool     `json:"interrupted,omitempty"`
	Cancellation        string   `json:"cancellation,omitempty"`
	CancellationSignal  string   `json:"cancellation_signal,omitempty"`
	RecommendedExitCode int      `json:"recommended_exit_code,omitempty"`
}

type summaryTiming struct {
	WallStartedAt   string `json:"wall_started_at,omitempty"`
	WallFinishedAt  string `json:"wall_finished_at,omitempty"`
	WallDurationNS  int64  `json:"wall_duration_ns,omitempty"`
	WallMeasured    bool   `json:"wall_measured,omitempty"`
	EventStartedAt  string `json:"event_started_at,omitempty"`
	EventFinishedAt string `json:"event_finished_at,omitempty"`
	EventDurationNS int64  `json:"event_duration_ns,omitempty"`
	EventEstimated  bool   `json:"event_estimated,omitempty"`
	EventTimestamps uint64 `json:"event_timestamps,omitempty"`
}

type summaryCoverage struct {
	Mode         string                `json:"mode"`
	Statements   uint64                `json:"statements"`
	Covered      uint64                `json:"covered"`
	Percent      *float64              `json:"percent,omitempty"`
	PercentExact string                `json:"percent_exact,omitempty"`
	Files        []summaryCoverageFile `json:"files,omitempty"`
}

type summaryCoverageFile struct {
	Name         string   `json:"name"`
	Statements   uint64   `json:"statements"`
	Covered      uint64   `json:"covered"`
	Percent      *float64 `json:"percent,omitempty"`
	PercentExact string   `json:"percent_exact,omitempty"`
}

type summaryPackage struct {
	Name                string              `json:"name"`
	Status              string              `json:"status"`
	ElapsedNS           *int64              `json:"elapsed_ns,omitempty"`
	DurationSource      string              `json:"duration_source,omitempty"`
	Cached              bool                `json:"cached,omitempty"`
	NoTests             bool                `json:"no_tests,omitempty"`
	FailedBuild         string              `json:"failed_build,omitempty"`
	IncompleteReason    string              `json:"incomplete_reason,omitempty"`
	OutputBytes         int64               `json:"output_bytes,omitempty"`
	OutputRetainedBytes int64               `json:"output_retained_bytes,omitempty"`
	OutputTruncated     bool                `json:"output_truncated,omitempty"`
	Attributes          []attributeView     `json:"attributes,omitempty"`
	Artifacts           []artifactView      `json:"artifacts,omitempty"`
	Tests               []summaryOccurrence `json:"tests,omitempty"`
}

type summaryOccurrence struct {
	Name                string          `json:"name"`
	Ordinal             uint64          `json:"ordinal"`
	Kind                string          `json:"kind,omitempty"`
	Status              string          `json:"status"`
	ElapsedNS           *int64          `json:"elapsed_ns,omitempty"`
	DurationSource      string          `json:"duration_source,omitempty"`
	IncompleteReason    string          `json:"incomplete_reason,omitempty"`
	OutputBytes         int64           `json:"output_bytes,omitempty"`
	OutputRetainedBytes int64           `json:"output_retained_bytes,omitempty"`
	OutputTruncated     bool            `json:"output_truncated,omitempty"`
	Attributes          []attributeView `json:"attributes,omitempty"`
	Artifacts           []artifactView  `json:"artifacts,omitempty"`
	Signals             result.Signals  `json:"signals,omitempty"`
}

type summaryUnattributedOutput struct {
	Text                string `json:"text,omitempty"`
	OutputBytes         int64  `json:"output_bytes,omitempty"`
	OutputRetainedBytes int64  `json:"output_retained_bytes,omitempty"`
	OutputTruncated     bool   `json:"output_truncated,omitempty"`
}

type summaryBuild struct {
	ImportPath          string         `json:"import_path"`
	Status              string         `json:"status"`
	OutputBytes         int64          `json:"output_bytes,omitempty"`
	OutputRetainedBytes int64          `json:"output_retained_bytes,omitempty"`
	OutputTruncated     bool           `json:"output_truncated,omitempty"`
	Signals             result.Signals `json:"signals,omitempty"`
}

type summaryFailure struct {
	Scope               string `json:"scope"`
	Package             string `json:"package,omitempty"`
	Name                string `json:"name,omitempty"`
	Ordinal             uint64 `json:"ordinal,omitempty"`
	Status              string `json:"status"`
	Reason              string `json:"reason,omitempty"`
	Excerpt             string `json:"excerpt,omitempty"`
	OriginalOutputBytes int64  `json:"original_output_bytes,omitempty"`
	RetainedOutputBytes int64  `json:"retained_output_bytes,omitempty"`
	ExcerptTruncated    bool   `json:"excerpt_truncated,omitempty"`
}

type summarySlow struct {
	Package        string `json:"package"`
	Name           string `json:"name"`
	Ordinal        uint64 `json:"ordinal"`
	Status         string `json:"status"`
	ElapsedNS      int64  `json:"elapsed_ns"`
	DurationSource string `json:"duration_source,omitempty"`
}

// RenderSummaryJSON writes the deterministic tested/summary/v1 document.
func (r *Renderer) RenderSummaryJSON(writer io.Writer, input Input) error {
	if r == nil {
		return errors.New("render summary JSON: renderer is nil")
	}
	if writer == nil {
		return errors.New("render summary JSON: writer is nil")
	}
	document := r.buildSummary(input)
	data, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("render summary JSON: encode document: %w", err)
	}
	data = append(data, '\n')
	if _, err := writer.Write(data); err != nil {
		return fmt.Errorf("render summary JSON: write document: %w", err)
	}
	return nil
}

func (r *Renderer) buildSummary(input Input) summaryDocument {
	view := r.buildView(input)
	document := summaryDocument{
		Schema:               SummarySchema,
		Title:                view.Title,
		Finalized:            view.Finalized,
		Outcome:              view.Outcome,
		Run:                  makeSummaryRun(view.Metadata),
		Timing:               makeSummaryTiming(view.Timing),
		Counts:               view.Summary,
		Coverage:             makeSummaryCoverage(view.Coverage),
		Signals:              input.Result.Signals,
		Assessment:           makeSummaryAssessment(view.Assessment),
		DiagnosticCount:      view.DiagnosticCount,
		DiagnosticsRetained:  view.DiagnosticsRetained,
		DiagnosticsTruncated: view.DiagnosticsTruncated,
	}
	if view.UnattributedOutput != nil {
		document.UnattributedOutput = &summaryUnattributedOutput{
			Text:                view.UnattributedOutput.Output,
			OutputBytes:         view.UnattributedOutput.OutputBytes,
			OutputRetainedBytes: view.UnattributedOutput.OutputRetainedBytes,
			OutputTruncated:     view.UnattributedOutput.OutputTruncated,
		}
	}

	remaining := r.maxFailureExcerptBytes
	for _, pkg := range view.Packages {
		packageSummary := summaryPackage{
			Name:                pkg.Name,
			Status:              pkg.Status,
			ElapsedNS:           durationPointer(pkg.DurationNanos, pkg.DurationKnown),
			DurationSource:      pkg.DurationSource,
			Cached:              pkg.Cached,
			NoTests:             pkg.NoTests,
			FailedBuild:         pkg.FailedBuild,
			IncompleteReason:    pkg.IncompleteReason,
			OutputBytes:         pkg.OutputBytes,
			OutputRetainedBytes: pkg.OutputRetainedBytes,
			OutputTruncated:     pkg.OutputTruncated,
			Attributes:          append([]attributeView(nil), pkg.Attributes...),
			Artifacts:           append([]artifactView(nil), pkg.Artifacts...),
		}
		for _, test := range pkg.Tests {
			testSummary := summaryOccurrence{
				Name:                test.Name,
				Ordinal:             test.Ordinal,
				Kind:                test.Kind,
				Status:              test.Status,
				ElapsedNS:           durationPointer(test.DurationNanos, test.DurationKnown),
				DurationSource:      test.DurationSource,
				IncompleteReason:    test.IncompleteReason,
				OutputBytes:         test.OutputBytes,
				OutputRetainedBytes: test.OutputRetainedBytes,
				OutputTruncated:     test.OutputTruncated,
				Attributes:          append([]attributeView(nil), test.Attributes...),
				Artifacts:           append([]artifactView(nil), test.Artifacts...),
				Signals:             test.Signals,
			}
			packageSummary.Tests = append(packageSummary.Tests, testSummary)

			if test.Status == string(result.StatusFailed) ||
				test.Status == string(result.StatusIncomplete) {
				excerpt, truncated := takeExcerpt(test.Output, &remaining)
				document.Failures = append(document.Failures, summaryFailure{
					Scope:               "test",
					Package:             test.Package,
					Name:                test.Name,
					Ordinal:             test.Ordinal,
					Status:              test.Status,
					Reason:              test.IncompleteReason,
					Excerpt:             excerpt,
					OriginalOutputBytes: test.OutputBytes,
					RetainedOutputBytes: test.OutputRetainedBytes,
					ExcerptTruncated:    truncated || test.OutputTruncated,
				})
			}
		}
		document.Packages = append(document.Packages, packageSummary)
		if failureStatus(pkg.Status) &&
			(pkg.Output != "" ||
				pkg.FailedBuild != "" ||
				pkg.IncompleteReason != "" ||
				pkg.OutputTruncated) {
			excerpt, truncated := takeExcerpt(pkg.Output, &remaining)
			document.Failures = append(document.Failures, summaryFailure{
				Scope:               "package",
				Package:             pkg.Name,
				Status:              pkg.Status,
				Reason:              packageFailureReason(pkg),
				Excerpt:             excerpt,
				OriginalOutputBytes: pkg.OutputBytes,
				RetainedOutputBytes: pkg.OutputRetainedBytes,
				ExcerptTruncated:    truncated || pkg.OutputTruncated,
			})
		}
	}

	for _, build := range view.Builds {
		document.Builds = append(document.Builds, summaryBuild{
			ImportPath:          build.ImportPath,
			Status:              build.Status,
			OutputBytes:         build.OutputBytes,
			OutputRetainedBytes: build.OutputRetainedBytes,
			OutputTruncated:     build.OutputTruncated,
			Signals:             build.Signals,
		})
		if build.Status == string(result.StatusFailed) {
			excerpt, truncated := takeExcerpt(build.Output, &remaining)
			document.Failures = append(document.Failures, summaryFailure{
				Scope:               "build",
				Name:                build.ImportPath,
				Status:              build.Status,
				Excerpt:             excerpt,
				OriginalOutputBytes: build.OutputBytes,
				RetainedOutputBytes: build.OutputRetainedBytes,
				ExcerptTruncated:    truncated || build.OutputTruncated,
			})
		}
	}
	document.Diagnostics = view.Diagnostics
	for _, slow := range view.Slowest {
		document.Slowest = append(document.Slowest, summarySlow{
			Package:        slow.Package,
			Name:           slow.Name,
			Ordinal:        slow.Ordinal,
			Status:         slow.Status,
			ElapsedNS:      slow.DurationNanos,
			DurationSource: slow.DurationSource,
		})
	}
	return document
}

func durationPointer(nanoseconds int64, known bool) *int64 {
	if !known {
		return nil
	}
	value := nanoseconds
	return &value
}

func packageFailureReason(pkg packageView) string {
	switch {
	case pkg.IncompleteReason == "":
		return pkg.FailedBuild
	case pkg.FailedBuild == "":
		return pkg.IncompleteReason
	default:
		return pkg.IncompleteReason + "; failed build: " + pkg.FailedBuild
	}
}

func makeSummaryAssessment(view *assessmentView) *Assessment {
	if view == nil {
		return nil
	}
	assessment := &Assessment{
		ChildStarted:         view.ChildStarted,
		ChildExitKnown:       view.ChildExitKnown,
		ChildExitInferred:    view.ChildExitInferred,
		Cancellation:         view.Cancellation,
		CancellationSignal:   view.CancellationSignal,
		RecommendedExitCode:  view.RecommendedExitCode,
		InfrastructureFailed: view.InfrastructureFailed,
		ReportFailed:         view.ReportFailed,
		EvidenceIncomplete:   view.EvidenceIncomplete,
		StreamCorrupt:        view.StreamCorrupt,
		Issues:               append([]Issue(nil), view.Issues...),
	}
	if view.CoveragePolicy != nil {
		policy := view.CoveragePolicy.CoveragePolicy
		assessment.CoveragePolicy = &policy
	}
	return assessment
}

func makeSummaryRun(metadata result.RunMetadata) summaryRun {
	return summaryRun{
		Command:             append([]string(nil), metadata.Command...),
		WorkDir:             metadata.WorkDir,
		StartedAt:           formatTime(metadata.StartedAt),
		FinishedAt:          formatTime(metadata.FinishedAt),
		ExitCode:            metadata.ExitCode,
		Signal:              metadata.Signal,
		Interrupted:         metadata.Interrupted,
		Cancellation:        metadata.Cancellation,
		CancellationSignal:  metadata.CancellationSignal,
		RecommendedExitCode: metadata.RecommendedExitCode,
	}
}

func makeSummaryTiming(timing result.Timing) summaryTiming {
	return summaryTiming{
		WallStartedAt:   formatTimePointer(timing.WallStartedAt),
		WallFinishedAt:  formatTimePointer(timing.WallFinishedAt),
		WallDurationNS:  int64(timing.WallDuration),
		WallMeasured:    timing.WallMeasured,
		EventStartedAt:  formatTimePointer(timing.EventStartedAt),
		EventFinishedAt: formatTimePointer(timing.EventFinishedAt),
		EventDurationNS: int64(timing.EventDuration),
		EventEstimated:  timing.EventEstimated,
		EventTimestamps: timing.EventTimestamps,
	}
}

func makeSummaryCoverage(view *coverageView) *summaryCoverage {
	if view == nil {
		return nil
	}
	coverageSummary := &summaryCoverage{
		Mode:         view.Mode,
		Statements:   view.Statements,
		Covered:      view.Covered,
		PercentExact: exactPercentage(view),
	}
	if view.Available && view.Statements > 0 {
		value := float64(view.Covered) * 100 / float64(view.Statements)
		coverageSummary.Percent = &value
	}
	for _, file := range view.Files {
		item := summaryCoverageFile{
			Name:         file.Name,
			Statements:   file.Statements,
			Covered:      file.Covered,
			PercentExact: exactFilePercentage(file),
		}
		if file.Available && file.Statements > 0 {
			value := float64(file.Covered) * 100 / float64(file.Statements)
			item.Percent = &value
		}
		coverageSummary.Files = append(coverageSummary.Files, item)
	}
	return coverageSummary
}

func exactPercentage(view *coverageView) string {
	if view == nil || !view.Available {
		return ""
	}
	return exactCoveragePercentage(view.Covered, view.Statements)
}

func exactFilePercentage(view coverageFileView) string {
	if !view.Available {
		return ""
	}
	return exactCoveragePercentage(view.Covered, view.Statements)
}

// Keep the machine-readable decimal projection independent of display bounds.
func exactCoveragePercentage(covered, statements uint64) string {
	totals := coverage.Totals{Covered: covered, Statements: statements}
	value, err := totals.FormatPercentage(2)
	if err != nil {
		return ""
	}
	if (value == "100.00" && covered < statements) ||
		(value == "0.00" && covered > 0) {
		value, err = totals.FormatPercentage(24)
		if err != nil {
			return ""
		}
		value = strings.TrimRight(value, "0")
		value = strings.TrimRight(value, ".")
	}
	return value
}

func takeExcerpt(value string, remaining *int) (string, bool) {
	if remaining == nil || *remaining <= 0 {
		return "", len(value) > 0
	}
	excerpt, truncated := truncateUTF8(value, *remaining)
	*remaining -= len(excerpt)
	return excerpt, truncated
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func formatTimePointer(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatTime(*value)
}
