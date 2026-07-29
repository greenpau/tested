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
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/greenpau/tested/pkg/result"
)

type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Name     string       `xml:"name,attr"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Errors   int          `xml:"errors,attr"`
	Skipped  int          `xml:"skipped,attr"`
	Time     string       `xml:"time,attr,omitempty"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name       string      `xml:"name,attr"`
	Tests      int         `xml:"tests,attr"`
	Failures   int         `xml:"failures,attr"`
	Errors     int         `xml:"errors,attr"`
	Skipped    int         `xml:"skipped,attr"`
	Time       string      `xml:"time,attr,omitempty"`
	Properties []junitProp `xml:"properties>property,omitempty"`
	Cases      []junitCase `xml:"testcase"`
}

type junitCase struct {
	Classname  string      `xml:"classname,attr"`
	Name       string      `xml:"name,attr"`
	Time       string      `xml:"time,attr,omitempty"`
	Properties []junitProp `xml:"properties>property,omitempty"`
	Failure    *junitIssue `xml:"failure,omitempty"`
	Error      *junitIssue `xml:"error,omitempty"`
	Skipped    *junitIssue `xml:"skipped,omitempty"`
	SystemOut  string      `xml:"system-out,omitempty"`
}

type junitProp struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type junitIssue struct {
	Message string `xml:"message,attr,omitempty"`
	Type    string `xml:"type,attr,omitempty"`
	Body    string `xml:",chardata"`
}

// RenderJUnitXML writes one deterministic testcase per occurrence plus
// synthetic cases for package, build, and stream failures without occurrences.
func (r *Renderer) RenderJUnitXML(writer io.Writer, input Input) error {
	if r == nil {
		return errors.New("render JUnit XML: renderer is nil")
	}
	if writer == nil {
		return errors.New("render JUnit XML: writer is nil")
	}
	document := r.buildJUnit(input)
	data, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("render JUnit XML: encode document: %w", err)
	}
	if _, err := io.WriteString(writer, xml.Header); err != nil {
		return fmt.Errorf("render JUnit XML: write header: %w", err)
	}
	if _, err := writer.Write(data); err != nil {
		return fmt.Errorf("render JUnit XML: write document: %w", err)
	}
	if _, err := io.WriteString(writer, "\n"); err != nil {
		return fmt.Errorf("render JUnit XML: write newline: %w", err)
	}
	return nil
}

func (r *Renderer) buildJUnit(input Input) junitSuites {
	view := r.buildView(input)
	document := junitSuites{Name: sanitizeXML(view.Title)}
	for _, pkg := range view.Packages {
		suite := junitSuite{
			Name: sanitizeXML(pkg.Name),
			Time: durationSeconds(
				timeDuration(pkg.DurationNanos),
				pkg.DurationKnown,
			),
		}
		suite.Properties = append(suite.Properties, junitProp{
			Name:  "tested.duration_known",
			Value: strconv.FormatBool(pkg.DurationKnown),
		})
		if pkg.DurationSource != "" {
			suite.Properties = append(suite.Properties, junitProp{
				Name:  "tested.duration_source",
				Value: sanitizeXML(pkg.DurationSource),
			})
		}
		if pkg.IncompleteReason != "" {
			suite.Properties = append(suite.Properties, junitProp{
				Name:  "tested.incomplete_reason",
				Value: sanitizeXML(pkg.IncompleteReason),
			})
		}
		suite.Properties = append(
			suite.Properties,
			metadataProperties(pkg.Attributes, pkg.Artifacts)...,
		)
		for _, occurrence := range pkg.Tests {
			testCase := junitCase{
				Classname: sanitizeXML(occurrence.Package),
				Name: sanitizeXML(fmt.Sprintf(
					"%s [occurrence %d]",
					occurrence.Name,
					occurrence.Ordinal,
				)),
				Time: durationSeconds(
					timeDuration(occurrence.DurationNanos),
					occurrence.DurationKnown,
				),
				SystemOut: sanitizeXML(occurrence.Output),
			}
			testCase.Properties = append(testCase.Properties,
				junitProp{Name: "tested.occurrence", Value: strconv.FormatUint(occurrence.Ordinal, 10)},
				junitProp{Name: "tested.status", Value: sanitizeXML(occurrence.Status)},
				junitProp{
					Name:  "tested.duration_known",
					Value: strconv.FormatBool(occurrence.DurationKnown),
				},
			)
			if occurrence.DurationSource != "" {
				testCase.Properties = append(testCase.Properties, junitProp{
					Name:  "tested.duration_source",
					Value: sanitizeXML(occurrence.DurationSource),
				})
			}
			testCase.Properties = append(
				testCase.Properties,
				metadataProperties(
					occurrence.Attributes,
					occurrence.Artifacts,
				)...,
			)
			if occurrence.OutputTruncated {
				testCase.Properties = append(testCase.Properties,
					junitProp{Name: "tested.output_truncated", Value: "true"},
					junitProp{
						Name:  "tested.output_bytes",
						Value: strconv.FormatInt(occurrence.OutputBytes, 10),
					},
					junitProp{
						Name:  "tested.output_retained_bytes",
						Value: strconv.FormatInt(occurrence.OutputRetainedBytes, 10),
					},
				)
			}
			switch occurrence.Status {
			case string(result.StatusFailed):
				testCase.Failure = &junitIssue{
					Message: "test failed",
					Type:    "test",
					Body:    sanitizeXML(occurrence.Output),
				}
			case string(result.StatusSkipped):
				testCase.Skipped = &junitIssue{
					Message: "test skipped",
					Type:    "skip",
				}
			case string(result.StatusIncomplete), string(result.StatusRunning),
				string(result.StatusPaused), string(result.StatusUnknown):
				message := occurrence.IncompleteReason
				if message == "" {
					message = "test evidence is incomplete"
				}
				testCase.Error = &junitIssue{
					Message: sanitizeXML(message),
					Type:    "incomplete",
					Body:    sanitizeXML(occurrence.Output),
				}
			}
			suite.Cases = append(suite.Cases, testCase)
		}

		hasFailure := false
		for _, testCase := range suite.Cases {
			if testCase.Failure != nil {
				hasFailure = true
				break
			}
		}
		if failureStatus(pkg.Status) &&
			(len(suite.Cases) == 0 ||
				pkg.Status != string(result.StatusFailed) ||
				!hasFailure) {
			issue := &junitIssue{
				Message: sanitizeXML(packageFailureReason(pkg)),
				Type:    "package",
				Body:    sanitizeXML(pkg.Output),
			}
			if issue.Message == "" {
				issue.Message = "package did not complete successfully"
			}
			testCase := junitCase{
				Classname: sanitizeXML(pkg.Name),
				Name:      "[package setup]",
				SystemOut: sanitizeXML(pkg.Output),
			}
			if pkg.OutputTruncated {
				testCase.Properties = append(
					testCase.Properties,
					truncationProperties(pkg.OutputBytes, pkg.OutputRetainedBytes)...,
				)
			}
			if pkg.Status == string(result.StatusFailed) {
				testCase.Failure = issue
			} else {
				testCase.Error = issue
			}
			suite.Cases = append(suite.Cases, testCase)
		}
		finalizeJUnitSuite(&suite)
		if suite.Tests > 0 {
			document.Suites = append(document.Suites, suite)
		}
	}

	var buildSuite junitSuite
	buildSuite.Name = "tested build events"
	for _, build := range view.Builds {
		if !failureStatus(build.Status) {
			continue
		}
		testCase := junitCase{
			Classname: "tested.build",
			Name:      sanitizeXML(build.ImportPath),
			SystemOut: sanitizeXML(build.Output),
		}
		if build.OutputTruncated {
			testCase.Properties = append(
				testCase.Properties,
				truncationProperties(build.OutputBytes, build.OutputRetainedBytes)...,
			)
		}
		issue := &junitIssue{
			Message: "package build failed",
			Type:    "build",
			Body:    sanitizeXML(build.Output),
		}
		if build.Status == string(result.StatusFailed) {
			testCase.Failure = issue
		} else {
			testCase.Error = issue
		}
		buildSuite.Cases = append(buildSuite.Cases, testCase)
	}
	finalizeJUnitSuite(&buildSuite)
	if buildSuite.Tests > 0 {
		document.Suites = append(document.Suites, buildSuite)
	}

	if view.UnattributedOutput != nil {
		output := view.UnattributedOutput
		outputSuite := junitSuite{
			Name: "tested unattributed output",
			Cases: []junitCase{{
				Classname: "tested.output",
				Name:      "[unattributed test output]",
				Properties: outputFactProperties(
					output.OutputBytes,
					output.OutputRetainedBytes,
					output.OutputTruncated,
				),
				SystemOut: sanitizeXML(output.Output),
			}},
		}
		finalizeJUnitSuite(&outputSuite)
		document.Suites = append(document.Suites, outputSuite)
	}

	if resourceSuite := buildResourceJUnit(view); resourceSuite != nil {
		document.Suites = append(document.Suites, *resourceSuite)
	}

	if len(view.Diagnostics) > 0 ||
		view.DiagnosticCount > 0 ||
		view.DiagnosticsTruncated {
		diagnosticSuite := junitSuite{
			Name: "tested stream integrity",
			Properties: []junitProp{
				{
					Name:  "tested.diagnostic_count",
					Value: strconv.FormatUint(view.DiagnosticCount, 10),
				},
				{
					Name:  "tested.diagnostics_retained",
					Value: strconv.FormatUint(view.DiagnosticsRetained, 10),
				},
				{
					Name:  "tested.diagnostics_truncated",
					Value: strconv.FormatBool(view.DiagnosticsTruncated),
				},
			},
		}
		for _, diagnostic := range view.Diagnostics {
			diagnosticSuite.Cases = append(diagnosticSuite.Cases, junitCase{
				Classname: "tested.protocol",
				Name: sanitizeXML(fmt.Sprintf(
					"%s record %d",
					diagnostic.Kind,
					diagnostic.Sequence,
				)),
				Properties: []junitProp{
					{
						Name:  "tested.diagnostic_bytes",
						Value: strconv.FormatInt(diagnostic.Bytes, 10),
					},
					{
						Name:  "tested.preview_truncated",
						Value: strconv.FormatBool(diagnostic.Truncated),
					},
				},
				Error: &junitIssue{
					Message: sanitizeXML(diagnostic.Message),
					Type:    "stream",
					Body:    sanitizeXML(diagnostic.Preview),
				},
			})
		}
		if view.DiagnosticsTruncated ||
			view.DiagnosticCount > view.DiagnosticsRetained {
			diagnosticSuite.Cases = append(diagnosticSuite.Cases, junitCase{
				Classname: "tested.protocol",
				Name:      "[additional diagnostics omitted]",
				Error: &junitIssue{
					Message: sanitizeXML(fmt.Sprintf(
						"retained %d of %d integrity diagnostics",
						view.DiagnosticsRetained,
						view.DiagnosticCount,
					)),
					Type: "stream",
				},
			})
		}
		finalizeJUnitSuite(&diagnosticSuite)
		document.Suites = append(document.Suites, diagnosticSuite)
	}

	if executionSuite := buildExecutionJUnit(view); executionSuite != nil {
		document.Suites = append(document.Suites, *executionSuite)
	}
	if policySuite := buildCoveragePolicyJUnit(view); policySuite != nil {
		document.Suites = append(document.Suites, *policySuite)
	}

	finalizeJUnitDocument(&document)
	if view.Outcome != "passed" &&
		document.Failures == 0 &&
		document.Errors == 0 {
		sentinel := buildOutcomeSentinelJUnit(view.Outcome)
		document.Suites = append(document.Suites, sentinel)
		finalizeJUnitDocument(&document)
	}
	document.Time = durationSeconds(
		input.Result.Timing.WallDuration,
		input.Result.Timing.WallMeasured ||
			input.Result.Timing.WallDuration != 0,
	)
	return document
}

func buildResourceJUnit(view reportView) *junitSuite {
	summary := view.Summary
	if !summary.Incomplete &&
		!summary.TotalOutputTruncated &&
		!summary.NormalizedEntriesTruncated &&
		!summary.NormalizedBytesTruncated {
		return nil
	}
	testCase := junitCase{
		Classname: "tested.resources",
		Name:      "[normalized projection resource usage]",
		Properties: []junitProp{
			{
				Name:  "tested.total_output_bytes",
				Value: strconv.FormatInt(summary.TotalOutputBytes, 10),
			},
			{
				Name: "tested.total_output_retained_bytes",
				Value: strconv.FormatInt(
					summary.TotalOutputRetainedBytes,
					10,
				),
			},
			{
				Name: "tested.total_output_truncated",
				Value: strconv.FormatBool(
					summary.TotalOutputTruncated,
				),
			},
			{
				Name: "tested.normalized_entries",
				Value: strconv.FormatUint(
					summary.NormalizedEntries,
					10,
				),
			},
			{
				Name: "tested.normalized_entries_retained",
				Value: strconv.FormatUint(
					summary.NormalizedEntriesRetained,
					10,
				),
			},
			{
				Name: "tested.normalized_entries_truncated",
				Value: strconv.FormatBool(
					summary.NormalizedEntriesTruncated,
				),
			},
			{
				Name:  "tested.normalized_bytes",
				Value: strconv.FormatInt(summary.NormalizedBytes, 10),
			},
			{
				Name: "tested.normalized_bytes_retained",
				Value: strconv.FormatInt(
					summary.NormalizedBytesRetained,
					10,
				),
			},
			{
				Name: "tested.normalized_bytes_truncated",
				Value: strconv.FormatBool(
					summary.NormalizedBytesTruncated,
				),
			},
		},
	}
	if summary.Incomplete {
		testCase.Error = &junitIssue{
			Message: "normalized result exceeded its semantic resource budget",
			Type:    "resource_capacity",
			Body: "Raw test_output.jsonl evidence remains authoritative; " +
				"the normalized semantic projection is incomplete.",
		}
	}
	suite := &junitSuite{
		Name:  "tested resource limits",
		Cases: []junitCase{testCase},
	}
	finalizeJUnitSuite(suite)
	return suite
}

func buildOutcomeSentinelJUnit(outcome string) junitSuite {
	testCase := junitCase{
		Classname: "tested.outcome",
		Name:      "[non-passing run outcome]",
		Properties: []junitProp{{
			Name:  "tested.outcome",
			Value: sanitizeXML(outcome),
		}},
	}
	issue := &junitIssue{
		Message: sanitizeXML("tested run outcome is " + outcome),
		Type:    "outcome",
	}
	switch outcome {
	case "failed", "coverage_failed":
		testCase.Failure = issue
	default:
		testCase.Error = issue
	}
	suite := junitSuite{
		Name:  "tested outcome",
		Cases: []junitCase{testCase},
	}
	finalizeJUnitSuite(&suite)
	return suite
}

func buildExecutionJUnit(view reportView) *junitSuite {
	if view.Assessment == nil {
		return nil
	}
	assessment := view.Assessment
	suite := &junitSuite{Name: "tested execution"}
	var issueBody strings.Builder
	for _, issue := range assessment.Issues {
		if !issue.Fatal {
			continue
		}
		fmt.Fprintf(
			&issueBody,
			"%s: %s\n",
			sanitizeXML(issue.Kind),
			sanitizeXML(issue.Message),
		)
	}
	if issueBody.Len() > 0 {
		suite.Cases = append(suite.Cases, junitCase{
			Classname: "tested.execution",
			Name:      "[run infrastructure]",
			Error: &junitIssue{
				Message: "tested could not complete the run reliably",
				Type:    "infrastructure",
				Body:    issueBody.String(),
			},
		})
	}
	if view.Metadata.Interrupted {
		message := "tested run was cancelled"
		if view.Metadata.Cancellation != "" {
			message += " (" + view.Metadata.Cancellation
			if view.Metadata.CancellationSignal != "" {
				message += ": " + view.Metadata.CancellationSignal
			}
			message += ")"
		}
		suite.Cases = append(suite.Cases, junitCase{
			Classname: "tested.execution",
			Name:      "[run cancellation]",
			Error: &junitIssue{
				Message: sanitizeXML(message),
				Type:    "cancellation",
			},
		})
	}
	if !assessment.ChildExitKnown {
		suite.Cases = append(suite.Cases, junitCase{
			Classname: "tested.execution",
			Name:      "[child process status]",
			Error: &junitIssue{
				Message: "authoritative child exit status is unavailable",
				Type:    "process",
			},
		})
	} else if view.Metadata.ExitCode != 0 &&
		view.Summary.Tests.Failed == 0 &&
		view.Summary.Packages.Failed == 0 &&
		view.Summary.BuildFailures == 0 {
		message := fmt.Sprintf(
			"child process exited with status %d",
			view.Metadata.ExitCode,
		)
		if view.Metadata.Signal != "" {
			message += " after signal " + view.Metadata.Signal
		}
		suite.Cases = append(suite.Cases, junitCase{
			Classname: "tested.execution",
			Name:      "[child process]",
			Failure: &junitIssue{
				Message: sanitizeXML(message),
				Type:    "process",
			},
		})
	}
	if assessment.EvidenceIncomplete && issueBody.Len() == 0 {
		suite.Cases = append(suite.Cases, junitCase{
			Classname: "tested.execution",
			Name:      "[raw evidence capture]",
			Error: &junitIssue{
				Message: "raw test evidence is incomplete",
				Type:    "evidence",
			},
		})
	}
	if assessment.StreamCorrupt && len(view.Diagnostics) == 0 {
		suite.Cases = append(suite.Cases, junitCase{
			Classname: "tested.execution",
			Name:      "[event stream integrity]",
			Error: &junitIssue{
				Message: "test event stream integrity failed",
				Type:    "stream",
			},
		})
	}
	finalizeJUnitSuite(suite)
	if suite.Tests == 0 {
		return nil
	}
	return suite
}

func buildCoveragePolicyJUnit(view reportView) *junitSuite {
	if view.Assessment == nil || view.Assessment.CoveragePolicy == nil {
		return nil
	}
	policy := view.Assessment.CoveragePolicy
	if policy.Available && policy.Satisfied {
		return nil
	}
	suite := &junitSuite{
		Name: "tested coverage policy",
		Cases: []junitCase{{
			Classname: "tested.policy",
			Name:      "[minimum weighted statement coverage]",
		}},
	}
	testCase := &suite.Cases[0]
	switch {
	case !policy.Available:
		testCase.Error = &junitIssue{
			Message: sanitizeXML(
				"coverage is unavailable; required minimum is " +
					policy.Minimum + "%",
			),
			Type: "coverage",
		}
	default:
		testCase.Failure = &junitIssue{
			Message: sanitizeXML(
				"weighted statement coverage " + policy.Actual +
					"% is below minimum " + policy.Minimum + "%",
			),
			Type: "coverage",
		}
	}
	finalizeJUnitSuite(suite)
	return suite
}

func finalizeJUnitSuite(suite *junitSuite) {
	if suite == nil {
		return
	}
	suite.Tests = len(suite.Cases)
	for _, testCase := range suite.Cases {
		switch {
		case testCase.Failure != nil:
			suite.Failures++
		case testCase.Error != nil:
			suite.Errors++
		case testCase.Skipped != nil:
			suite.Skipped++
		}
	}
}

func finalizeJUnitDocument(document *junitSuites) {
	if document == nil {
		return
	}
	document.Tests = 0
	document.Failures = 0
	document.Errors = 0
	document.Skipped = 0
	for _, suite := range document.Suites {
		document.Tests += suite.Tests
		document.Failures += suite.Failures
		document.Errors += suite.Errors
		document.Skipped += suite.Skipped
	}
}

func failureStatus(status string) bool {
	switch status {
	case string(result.StatusFailed), string(result.StatusIncomplete),
		string(result.StatusRunning), string(result.StatusPaused):
		return true
	default:
		return false
	}
}

func timeDuration(nanoseconds int64) time.Duration {
	return time.Duration(nanoseconds)
}

func truncationProperties(original, retained int64) []junitProp {
	return []junitProp{
		{Name: "tested.output_truncated", Value: "true"},
		{Name: "tested.output_bytes", Value: strconv.FormatInt(original, 10)},
		{Name: "tested.output_retained_bytes", Value: strconv.FormatInt(retained, 10)},
	}
}

func outputFactProperties(
	original int64,
	retained int64,
	truncated bool,
) []junitProp {
	return []junitProp{
		{
			Name:  "tested.output_bytes",
			Value: strconv.FormatInt(original, 10),
		},
		{
			Name:  "tested.output_retained_bytes",
			Value: strconv.FormatInt(retained, 10),
		},
		{
			Name:  "tested.output_truncated",
			Value: strconv.FormatBool(truncated),
		},
	}
}

func metadataProperties(
	attributes []attributeView,
	artifacts []artifactView,
) []junitProp {
	properties := make(
		[]junitProp,
		0,
		len(attributes)*4+len(artifacts)*3,
	)
	for index, attribute := range attributes {
		prefix := fmt.Sprintf("tested.attribute.%d.", index+1)
		properties = append(
			properties,
			junitProp{
				Name:  prefix + "sequence",
				Value: strconv.FormatUint(attribute.Sequence, 10),
			},
			junitProp{
				Name:  prefix + "key",
				Value: sanitizeXML(attribute.Key),
			},
			junitProp{
				Name:  prefix + "value",
				Value: sanitizeXML(attribute.Value),
			},
		)
		if attribute.Line != 0 {
			properties = append(properties, junitProp{
				Name:  prefix + "line",
				Value: strconv.FormatUint(attribute.Line, 10),
			})
		}
	}
	for index, artifact := range artifacts {
		prefix := fmt.Sprintf("tested.artifact.%d.", index+1)
		properties = append(
			properties,
			junitProp{
				Name:  prefix + "sequence",
				Value: strconv.FormatUint(artifact.Sequence, 10),
			},
			junitProp{
				Name:  prefix + "path",
				Value: sanitizeXML(artifact.Path),
			},
		)
		if artifact.Line != 0 {
			properties = append(properties, junitProp{
				Name:  prefix + "line",
				Value: strconv.FormatUint(artifact.Line, 10),
			})
		}
	}
	return properties
}
