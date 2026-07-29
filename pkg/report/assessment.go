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

// Issue describes a run-level fact that is not represented by an individual
// Go test event. Fatal issues make the overall report incomplete.
type Issue struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Fatal   bool   `json:"fatal,omitempty"`
}

// CoveragePolicy records the exact policy decision made against weighted
// statement coverage. Minimum is the exact requested decimal; Actual is a
// high-precision rendering backed by the exact Covered/Statements ratio.
type CoveragePolicy struct {
	Minimum    string `json:"minimum"`
	Actual     string `json:"actual,omitempty"`
	Covered    uint64 `json:"covered,omitempty"`
	Statements uint64 `json:"statements,omitempty"`
	Available  bool   `json:"available"`
	Satisfied  bool   `json:"satisfied"`
}

// Assessment joins process status, raw-evidence integrity, and policy state
// with the semantic result snapshot.
type Assessment struct {
	ChildStarted         bool            `json:"child_started"`
	ChildExitKnown       bool            `json:"child_exit_known"`
	ChildExitInferred    bool            `json:"child_exit_inferred,omitempty"`
	Cancellation         string          `json:"cancellation,omitempty"`
	CancellationSignal   string          `json:"cancellation_signal,omitempty"`
	RecommendedExitCode  int             `json:"recommended_exit_code,omitempty"`
	InfrastructureFailed bool            `json:"infrastructure_failed,omitempty"`
	ReportFailed         bool            `json:"report_failed,omitempty"`
	EvidenceIncomplete   bool            `json:"evidence_incomplete,omitempty"`
	StreamCorrupt        bool            `json:"stream_corrupt,omitempty"`
	Issues               []Issue         `json:"issues,omitempty"`
	CoveragePolicy       *CoveragePolicy `json:"coverage_policy,omitempty"`
}

type assessmentView struct {
	ChildStarted         bool
	ChildExitKnown       bool
	ChildExitInferred    bool
	Cancellation         string
	CancellationSignal   string
	RecommendedExitCode  int
	InfrastructureFailed bool
	ReportFailed         bool
	EvidenceIncomplete   bool
	StreamCorrupt        bool
	Issues               []Issue
	CoveragePolicy       *CoveragePolicy
}

func (r *Renderer) buildAssessmentView(input *Assessment) *assessmentView {
	if input == nil {
		return nil
	}
	view := &assessmentView{
		ChildStarted:         input.ChildStarted,
		ChildExitKnown:       input.ChildExitKnown,
		ChildExitInferred:    input.ChildExitInferred,
		Cancellation:         r.redact(input.Cancellation),
		CancellationSignal:   r.redact(input.CancellationSignal),
		RecommendedExitCode:  input.RecommendedExitCode,
		InfrastructureFailed: input.InfrastructureFailed,
		ReportFailed:         input.ReportFailed,
		EvidenceIncomplete:   input.EvidenceIncomplete,
		StreamCorrupt:        input.StreamCorrupt,
	}
	for _, issue := range input.Issues {
		view.Issues = append(view.Issues, Issue{
			Kind:    r.redact(issue.Kind),
			Message: r.redact(issue.Message),
			Fatal:   issue.Fatal,
		})
	}
	if input.CoveragePolicy != nil {
		policy := *input.CoveragePolicy
		policy.Minimum = r.redact(policy.Minimum)
		policy.Actual = r.redact(policy.Actual)
		view.CoveragePolicy = &policy
	}
	return view
}
