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
	"testing"

	"github.com/greenpau/tested/pkg/result"
)

func TestExitCodePrecedence(t *testing.T) {
	tests := []struct {
		name  string
		state ExitState
		want  int
	}{
		{name: "success", want: ExitSuccess},
		{
			name: "interrupt dominates everything",
			state: ExitState{
				Interrupted:       true,
				ChildStarted:      true,
				ChildExitCode:     1,
				InfrastructureErr: true,
			},
			want: ExitInterrupted,
		},
		{
			name: "termination signal preserves shell projection",
			state: ExitState{
				Interrupted:       true,
				InterruptExitCode: 143,
				ChildExitKnown:    true,
				ChildExitCode:     0,
			},
			want: 143,
		},
		{
			name: "child failure dominates report failure",
			state: ExitState{
				ChildStarted:   true,
				ChildExitKnown: true,
				ChildExitCode:  7,
				ReportErr:      true,
			},
			want: 7,
		},
		{
			name:  "startup error",
			state: ExitState{InfrastructureErr: true},
			want:  ExitInfrastructure,
		},
		{
			name: "corrupt passing stream",
			state: ExitState{
				ChildStarted:   true,
				ChildExitKnown: true,
				ChildExitCode:  0,
				StreamCorrupt:  true,
			},
			want: ExitInfrastructure,
		},
		{
			name:  "offline failure",
			state: ExitState{EvidenceFailed: true},
			want:  ExitTestsFailed,
		},
		{
			name: "allowed offline failure",
			state: ExitState{
				EvidenceFailed: true,
				AllowFailures:  true,
			},
			want: ExitSuccess,
		},
		{
			name: "incomplete evidence is not an allowed failure",
			state: ExitState{
				EvidenceIncomplete: true,
				AllowFailures:      true,
			},
			want: ExitInfrastructure,
		},
		{
			name:  "coverage policy",
			state: ExitState{CoverageBelow: true},
			want:  ExitCoverage,
		},
		{
			name: "coverage policy dominates report failure",
			state: ExitState{
				CoverageBelow: true,
				ReportErr:     true,
			},
			want: ExitCoverage,
		},
		{
			name: "successful child conflicts with failing events",
			state: ExitState{
				ChildStarted:   true,
				ChildExitKnown: true,
				ChildExitCode:  0,
				EvidenceFailed: true,
			},
			want: ExitInfrastructure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ExitCode(test.state); got != test.want {
				t.Fatalf("ExitCode() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestResultIncompleteIntegrityDiagnostics(t *testing.T) {
	tests := []struct {
		name     string
		snapshot result.Result
		want     bool
	}{
		{
			name:     "complete result",
			snapshot: result.Result{Finalized: true},
		},
		{
			name: "ordinary diagnostic is handled by stream integrity",
			snapshot: result.Result{
				Finalized:       true,
				DiagnosticCount: 1,
			},
		},
		{
			name: "analyzer integrity diagnostic",
			snapshot: result.Result{
				Finalized:                true,
				DiagnosticCount:          1,
				IntegrityDiagnosticCount: 1,
			},
			want: true,
		},
		{
			name: "explicit incomplete result",
			snapshot: result.Result{
				Finalized:  true,
				Incomplete: true,
			},
			want: true,
		},
		{
			name: "summary resource capacity incomplete",
			snapshot: result.Result{
				Finalized: true,
				Summary: result.Summary{
					Incomplete: true,
				},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resultIncomplete(test.snapshot); got != test.want {
				t.Fatalf("resultIncomplete() = %t, want %t", got, test.want)
			}
		})
	}
}
