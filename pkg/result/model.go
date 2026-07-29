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

package result

import (
	"fmt"
	"time"

	"github.com/greenpau/tested/pkg/protocol"
)

// Status is the lifecycle state of a package, test occurrence, or build.
type Status string

const (
	StatusUnknown     Status = "unknown"
	StatusRunning     Status = "running"
	StatusPaused      Status = "paused"
	StatusPassed      Status = "passed"
	StatusFailed      Status = "failed"
	StatusSkipped     Status = "skipped"
	StatusBenchmarked Status = "benchmarked"
	StatusIncomplete  Status = "incomplete"
)

// Terminal reports whether a status is a completed outcome.
func (s Status) Terminal() bool {
	switch s {
	case StatusPassed, StatusFailed, StatusSkipped, StatusBenchmarked,
		StatusIncomplete:
		return true
	default:
		return false
	}
}

// Successful reports whether a status is a non-failing completed outcome.
func (s Status) Successful() bool {
	switch s {
	case StatusPassed, StatusSkipped, StatusBenchmarked:
		return true
	default:
		return false
	}
}

// TestKind identifies the testing surface represented by an occurrence.
type TestKind string

const (
	TestKindUnknown   TestKind = "unknown"
	TestKindTest      TestKind = "test"
	TestKindBenchmark TestKind = "benchmark"
	TestKindExample   TestKind = "example"
	TestKindFuzz      TestKind = "fuzz"
)

// DurationSource explains the evidence used for an elapsed duration.
type DurationSource string

const (
	DurationUnknown       DurationSource = "unknown"
	DurationGoElapsed     DurationSource = "go_elapsed"
	DurationEventEstimate DurationSource = "event_time_estimate"
)

// OccurrenceID prevents repeated invocations with the same package and full
// test name from collapsing into one result.
type OccurrenceID struct {
	Package string `json:"package"`
	Name    string `json:"name"`
	Ordinal uint64 `json:"ordinal"`
}

// String returns a human-readable stable occurrence label.
func (id OccurrenceID) String() string {
	return fmt.Sprintf("%s::%s#%d", id.Package, id.Name, id.Ordinal)
}

// Signals are best-effort classifications derived from complete output chunks.
type Signals struct {
	Race    bool `json:"race,omitempty"`
	Panic   bool `json:"panic,omitempty"`
	Timeout bool `json:"timeout,omitempty"`
}

// Any reports whether at least one exceptional signal was detected.
func (s Signals) Any() bool {
	return s.Race || s.Panic || s.Timeout
}

// OutputScope identifies the entity to which an output chunk was attributed.
type OutputScope string

const (
	OutputUnattributed OutputScope = "unattributed"
	OutputPackage      OutputScope = "package"
	OutputTest         OutputScope = "test"
	OutputBuild        OutputScope = "build"
)

// Output is a retained, attributed prefix of one protocol output event.
// OriginalBytes describes the complete event text even when Text was clipped.
type Output struct {
	Sequence      uint64        `json:"sequence"`
	Line          uint64        `json:"line,omitempty"`
	Time          *time.Time    `json:"time,omitempty"`
	Scope         OutputScope   `json:"scope"`
	Package       string        `json:"package,omitempty"`
	Test          *OccurrenceID `json:"test,omitempty"`
	ImportPath    string        `json:"import_path,omitempty"`
	Text          string        `json:"text"`
	OriginalBytes int64         `json:"original_bytes"`
}

// Attribute is one Go 1.26 attr event bound to a package or active test
// occurrence. Test is nil for package-scoped metadata.
type Attribute struct {
	Sequence uint64        `json:"sequence"`
	Line     uint64        `json:"line,omitempty"`
	Time     *time.Time    `json:"time,omitempty"`
	Package  string        `json:"package,omitempty"`
	Test     *OccurrenceID `json:"test,omitempty"`
	Key      string        `json:"key"`
	Value    string        `json:"value"`
}

// Artifact is one Go 1.26 artifacts event bound to a package or active test
// occurrence. Test is nil for package-scoped metadata.
type Artifact struct {
	Sequence uint64        `json:"sequence"`
	Line     uint64        `json:"line,omitempty"`
	Time     *time.Time    `json:"time,omitempty"`
	Package  string        `json:"package,omitempty"`
	Test     *OccurrenceID `json:"test,omitempty"`
	Path     string        `json:"path"`
}

// TestOccurrence is one invocation of a full test, example, fuzz target, or
// benchmark name.
type TestOccurrence struct {
	ID                  OccurrenceID   `json:"id"`
	Parent              *OccurrenceID  `json:"parent,omitempty"`
	Kind                TestKind       `json:"kind"`
	Status              Status         `json:"status"`
	StartedAt           *time.Time     `json:"started_at,omitempty"`
	FinishedAt          *time.Time     `json:"finished_at,omitempty"`
	Elapsed             time.Duration  `json:"elapsed,omitempty"`
	DurationSource      DurationSource `json:"duration_source,omitempty"`
	PauseCount          uint64         `json:"pause_count,omitempty"`
	FirstSequence       uint64         `json:"first_sequence,omitempty"`
	LastSequence        uint64         `json:"last_sequence,omitempty"`
	IncompleteReason    string         `json:"incomplete_reason,omitempty"`
	Output              []Output       `json:"output,omitempty"`
	OutputBytes         int64          `json:"output_bytes,omitempty"`
	OutputRetainedBytes int64          `json:"output_retained_bytes,omitempty"`
	OutputTruncated     bool           `json:"output_truncated,omitempty"`
	Attributes          []Attribute    `json:"attributes,omitempty"`
	Artifacts           []Artifact     `json:"artifacts,omitempty"`
	Signals             Signals        `json:"signals,omitempty"`
}

// Package is the normalized result for one TestEvent.Package value.
type Package struct {
	Name                string           `json:"name"`
	Status              Status           `json:"status"`
	StartedAt           *time.Time       `json:"started_at,omitempty"`
	FinishedAt          *time.Time       `json:"finished_at,omitempty"`
	Elapsed             time.Duration    `json:"elapsed,omitempty"`
	DurationSource      DurationSource   `json:"duration_source,omitempty"`
	Cached              bool             `json:"cached,omitempty"`
	NoTests             bool             `json:"no_tests,omitempty"`
	FailedBuild         string           `json:"failed_build,omitempty"`
	FirstSequence       uint64           `json:"first_sequence,omitempty"`
	LastSequence        uint64           `json:"last_sequence,omitempty"`
	IncompleteReason    string           `json:"incomplete_reason,omitempty"`
	Tests               []TestOccurrence `json:"tests,omitempty"`
	Output              []Output         `json:"output,omitempty"`
	OutputBytes         int64            `json:"output_bytes,omitempty"`
	OutputRetainedBytes int64            `json:"output_retained_bytes,omitempty"`
	OutputTruncated     bool             `json:"output_truncated,omitempty"`
	Attributes          []Attribute      `json:"attributes,omitempty"`
	Artifacts           []Artifact       `json:"artifacts,omitempty"`
	Signals             Signals          `json:"signals,omitempty"`
}

// Build holds Go 1.26 build events keyed by ImportPath. BuildEvent does not
// publish a success action, so StatusUnknown with output is not incomplete.
type Build struct {
	ImportPath          string   `json:"import_path"`
	Status              Status   `json:"status"`
	FirstSequence       uint64   `json:"first_sequence,omitempty"`
	LastSequence        uint64   `json:"last_sequence,omitempty"`
	Output              []Output `json:"output,omitempty"`
	OutputBytes         int64    `json:"output_bytes,omitempty"`
	OutputRetainedBytes int64    `json:"output_retained_bytes,omitempty"`
	OutputTruncated     bool     `json:"output_truncated,omitempty"`
	Signals             Signals  `json:"signals,omitempty"`
}

// Diagnostic is the normalized projection of a recoverable evidence issue.
type Diagnostic struct {
	Kind      protocol.DiagnosticKind `json:"kind"`
	Sequence  uint64                  `json:"sequence"`
	Line      uint64                  `json:"line"`
	Bytes     int64                   `json:"bytes"`
	Preview   string                  `json:"preview,omitempty"`
	Truncated bool                    `json:"truncated,omitempty"`
	Message   string                  `json:"message"`
}

// UnknownAction summarizes forward-compatible actions without retaining
// another unbounded copy of their raw records.
type UnknownAction struct {
	Kind          protocol.EventKind `json:"kind"`
	Action        string             `json:"action"`
	Count         uint64             `json:"count"`
	FirstSequence uint64             `json:"first_sequence,omitempty"`
}

// RunMetadata is child-process evidence supplied by the runner at Finalize.
type RunMetadata struct {
	Command             []string      `json:"command,omitempty"`
	WorkDir             string        `json:"work_dir,omitempty"`
	StartedAt           time.Time     `json:"started_at,omitempty"`
	FinishedAt          time.Time     `json:"finished_at,omitempty"`
	Duration            time.Duration `json:"duration,omitempty"`
	ExitCode            int           `json:"exit_code"`
	Signal              string        `json:"signal,omitempty"`
	Interrupted         bool          `json:"interrupted,omitempty"`
	Cancellation        string        `json:"cancellation,omitempty"`
	CancellationSignal  string        `json:"cancellation_signal,omitempty"`
	RecommendedExitCode int           `json:"recommended_exit_code,omitempty"`
}

// Timing keeps measured live wall time distinct from the estimate derived
// from the minimum and maximum TestEvent timestamps.
type Timing struct {
	WallStartedAt   *time.Time    `json:"wall_started_at,omitempty"`
	WallFinishedAt  *time.Time    `json:"wall_finished_at,omitempty"`
	WallDuration    time.Duration `json:"wall_duration,omitempty"`
	WallMeasured    bool          `json:"wall_measured,omitempty"`
	EventStartedAt  *time.Time    `json:"event_started_at,omitempty"`
	EventFinishedAt *time.Time    `json:"event_finished_at,omitempty"`
	EventDuration   time.Duration `json:"event_duration,omitempty"`
	EventEstimated  bool          `json:"event_estimated,omitempty"`
	EventTimestamps uint64        `json:"event_timestamps,omitempty"`
}

// OutcomeCounts contains deterministic lifecycle totals.
type OutcomeCounts struct {
	Total       uint64 `json:"total"`
	Passed      uint64 `json:"passed,omitempty"`
	Failed      uint64 `json:"failed,omitempty"`
	Skipped     uint64 `json:"skipped,omitempty"`
	Benchmarked uint64 `json:"benchmarked,omitempty"`
	Incomplete  uint64 `json:"incomplete,omitempty"`
	Running     uint64 `json:"running,omitempty"`
	Paused      uint64 `json:"paused,omitempty"`
	Unknown     uint64 `json:"unknown,omitempty"`
}

// Summary contains report-friendly aggregate counts.
type Summary struct {
	Packages                   OutcomeCounts `json:"packages"`
	Tests                      OutcomeCounts `json:"tests"`
	Builds                     OutcomeCounts `json:"builds"`
	TotalOutputBytes           int64         `json:"total_output_bytes,omitempty"`
	TotalOutputRetainedBytes   int64         `json:"total_output_retained_bytes,omitempty"`
	TotalOutputTruncated       bool          `json:"total_output_truncated,omitempty"`
	NormalizedEntries          uint64        `json:"normalized_entries,omitempty"`
	NormalizedEntriesRetained  uint64        `json:"normalized_entries_retained,omitempty"`
	NormalizedEntriesTruncated bool          `json:"normalized_entries_truncated,omitempty"`
	NormalizedBytes            int64         `json:"normalized_bytes,omitempty"`
	NormalizedBytesRetained    int64         `json:"normalized_bytes_retained,omitempty"`
	NormalizedBytesTruncated   bool          `json:"normalized_bytes_truncated,omitempty"`
	Incomplete                 bool          `json:"incomplete,omitempty"`
	CachedPackages             uint64        `json:"cached_packages,omitempty"`
	PackagesWithoutTests       uint64        `json:"packages_without_tests,omitempty"`
	BuildFailures              uint64        `json:"build_failures,omitempty"`
	Diagnostics                uint64        `json:"diagnostics,omitempty"`
	DiagnosticsRetained        uint64        `json:"diagnostics_retained,omitempty"`
	IntegrityDiagnostics       uint64        `json:"integrity_diagnostics,omitempty"`
	UnknownActions             uint64        `json:"unknown_actions,omitempty"`
	Race                       bool          `json:"race,omitempty"`
	Panic                      bool          `json:"panic,omitempty"`
	Timeout                    bool          `json:"timeout,omitempty"`
}

// Result is an immutable snapshot projection. Packages, tests, builds, and
// unknown-action summaries are sorted deterministically.
type Result struct {
	Finalized                  bool            `json:"finalized"`
	Incomplete                 bool            `json:"incomplete,omitempty"`
	Metadata                   RunMetadata     `json:"metadata"`
	Timing                     Timing          `json:"timing"`
	Packages                   []Package       `json:"packages,omitempty"`
	Builds                     []Build         `json:"builds,omitempty"`
	UnattributedOutput         []Output        `json:"unattributed_output,omitempty"`
	OutputBytes                int64           `json:"output_bytes,omitempty"`
	OutputRetainedBytes        int64           `json:"output_retained_bytes,omitempty"`
	OutputTruncated            bool            `json:"output_truncated,omitempty"`
	TotalOutputBytes           int64           `json:"total_output_bytes,omitempty"`
	TotalOutputRetainedBytes   int64           `json:"total_output_retained_bytes,omitempty"`
	TotalOutputTruncated       bool            `json:"total_output_truncated,omitempty"`
	NormalizedEntries          uint64          `json:"normalized_entries,omitempty"`
	NormalizedEntriesRetained  uint64          `json:"normalized_entries_retained,omitempty"`
	NormalizedEntriesTruncated bool            `json:"normalized_entries_truncated,omitempty"`
	NormalizedBytes            int64           `json:"normalized_bytes,omitempty"`
	NormalizedBytesRetained    int64           `json:"normalized_bytes_retained,omitempty"`
	NormalizedBytesTruncated   bool            `json:"normalized_bytes_truncated,omitempty"`
	Diagnostics                []Diagnostic    `json:"diagnostics,omitempty"`
	DiagnosticCount            uint64          `json:"diagnostic_count,omitempty"`
	IntegrityDiagnosticCount   uint64          `json:"integrity_diagnostic_count,omitempty"`
	DiagnosticsTruncated       bool            `json:"diagnostics_truncated,omitempty"`
	UnknownActions             []UnknownAction `json:"unknown_actions,omitempty"`
	Signals                    Signals         `json:"signals,omitempty"`
	Summary                    Summary         `json:"summary"`
}
