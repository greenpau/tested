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

const (
	// ExitSuccess indicates a complete passing run.
	ExitSuccess = 0
	// ExitTestsFailed indicates authoritative failing test/build evidence.
	ExitTestsFailed = 1
	// ExitInfrastructure indicates CLI, execution, parsing, or reporting failure.
	ExitInfrastructure = 2
	// ExitCoverage indicates an unmet minimum-coverage policy.
	ExitCoverage = 3
	// ExitInterrupted follows the shell convention for an interrupt signal.
	ExitInterrupted = 130
)

// ExitState describes independent outcome channels in precedence order.
type ExitState struct {
	Interrupted        bool
	InterruptExitCode  int
	InfrastructureErr  bool
	ChildStarted       bool
	ChildExitKnown     bool
	ChildExitCode      int
	StreamCorrupt      bool
	ReportErr          bool
	CoverageBelow      bool
	EvidenceFailed     bool
	EvidenceIncomplete bool
	AllowFailures      bool
}

// ExitCode applies tested's outcome precedence. An owned child process is the
// authority for live test success; reporting failures are still diagnosed but
// never mask its nonzero status.
func ExitCode(state ExitState) int {
	if state.Interrupted {
		if state.InterruptExitCode > 0 {
			return state.InterruptExitCode
		}
		return ExitInterrupted
	}
	failureAllowed := state.AllowFailures &&
		state.ChildExitCode == ExitTestsFailed &&
		state.EvidenceFailed &&
		!state.InfrastructureErr &&
		!state.StreamCorrupt &&
		!state.EvidenceIncomplete
	if state.ChildExitKnown && state.ChildExitCode != 0 &&
		!failureAllowed {
		return state.ChildExitCode
	}
	if state.InfrastructureErr || state.StreamCorrupt ||
		state.EvidenceIncomplete {
		return ExitInfrastructure
	}
	if state.EvidenceFailed && !state.AllowFailures {
		if state.ChildExitKnown && state.ChildExitCode == 0 {
			return ExitInfrastructure
		}
		return ExitTestsFailed
	}
	if state.CoverageBelow {
		return ExitCoverage
	}
	if state.ReportErr {
		return ExitInfrastructure
	}
	return ExitSuccess
}
