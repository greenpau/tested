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

// Package runstatus defines tested's durable child-process outcome evidence.
package runstatus

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

const (
	// Schema is the stable run.json schema identifier.
	Schema = "tested/run/v1"
	// MaximumBytes bounds status metadata independently from the unbounded raw
	// test event stream.
	MaximumBytes int64 = 1 << 20
)

// Issue records a run-level problem that cannot be reconstructed from the Go
// event stream alone.
type Issue struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Fatal   bool   `json:"fatal,omitempty"`
}

// CoveragePolicy records the exact minimum-coverage decision made by the run.
type CoveragePolicy struct {
	Minimum    string `json:"minimum"`
	Actual     string `json:"actual,omitempty"`
	Covered    uint64 `json:"covered,omitempty"`
	Statements uint64 `json:"statements,omitempty"`
	Available  bool   `json:"available"`
	Satisfied  bool   `json:"satisfied"`
}

// File binds process status to one exact raw-evidence artifact.
type File struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Evidence records the authoritative process outcome and whether tested
// captured the raw streams completely. ExitCode is nil when no trustworthy
// process status exists.
type Evidence struct {
	Schema              string          `json:"schema"`
	Command             []string        `json:"command,omitempty"`
	WorkDir             string          `json:"work_dir,omitempty"`
	StartedAt           *time.Time      `json:"started_at,omitempty"`
	FinishedAt          *time.Time      `json:"finished_at,omitempty"`
	DurationNS          int64           `json:"duration_ns,omitempty"`
	ChildStarted        bool            `json:"child_started"`
	ExitCode            *int            `json:"exit_code,omitempty"`
	Signal              string          `json:"signal,omitempty"`
	Interrupted         bool            `json:"interrupted,omitempty"`
	Cancellation        string          `json:"cancellation,omitempty"`
	CancellationSignal  string          `json:"cancellation_signal,omitempty"`
	RecommendedExitCode int             `json:"recommended_exit_code,omitempty"`
	CaptureComplete     bool            `json:"capture_complete"`
	Files               []File          `json:"files"`
	Issues              []Issue         `json:"issues,omitempty"`
	CoveragePolicy      *CoveragePolicy `json:"coverage_policy,omitempty"`
}

// Encode validates evidence and writes one deterministic JSON document.
func Encode(writer io.Writer, evidence Evidence) error {
	if writer == nil {
		return errors.New("encode run status: writer is nil")
	}
	if err := evidence.Validate(); err != nil {
		return err
	}
	normalized := evidence
	normalized.Command = append([]string(nil), evidence.Command...)
	normalized.Files = append([]File(nil), evidence.Files...)
	sort.Slice(normalized.Files, func(i, j int) bool {
		return normalized.Files[i].Name < normalized.Files[j].Name
	})
	normalized.Issues = append([]Issue(nil), evidence.Issues...)
	if evidence.CoveragePolicy != nil {
		policy := *evidence.CoveragePolicy
		normalized.CoveragePolicy = &policy
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("encode run status: %w", err)
	}
	data = append(data, '\n')
	if int64(len(data)) > MaximumBytes {
		return fmt.Errorf(
			"encode run status: document exceeds %d bytes",
			MaximumBytes,
		)
	}
	written, err := writer.Write(data)
	if err != nil {
		return fmt.Errorf("encode run status: write: %w", err)
	}
	if written != len(data) {
		return fmt.Errorf(
			"encode run status: write: %w",
			io.ErrShortWrite,
		)
	}
	return nil
}

// Decode reads and strictly validates one bounded run.json document.
func Decode(reader io.Reader) (Evidence, error) {
	if reader == nil {
		return Evidence{}, errors.New("decode run status: reader is nil")
	}
	limited := io.LimitReader(reader, MaximumBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return Evidence{}, fmt.Errorf("decode run status: read: %w", err)
	}
	if int64(len(data)) > MaximumBytes {
		return Evidence{}, fmt.Errorf(
			"decode run status: document exceeds %d bytes",
			MaximumBytes,
		)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var evidence Evidence
	if err := decoder.Decode(&evidence); err != nil {
		return Evidence{}, fmt.Errorf("decode run status: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Evidence{}, errors.New("decode run status: multiple JSON values")
		}
		return Evidence{}, fmt.Errorf("decode run status: trailing data: %w", err)
	}
	if err := rejectDuplicateMembers(data); err != nil {
		return Evidence{}, fmt.Errorf("decode run status: %w", err)
	}
	if err := evidence.Validate(); err != nil {
		return Evidence{}, err
	}
	return evidence, nil
}

func rejectDuplicateMembers(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return fmt.Errorf("inspect trailing JSON data: %w", err)
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("inspect JSON value: %w", err)
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("inspect JSON object member: %w", err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object member name is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON object member %q", key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("inspect JSON object close: %w", err)
		}
		if closing != json.Delim('}') {
			return errors.New("JSON object has an invalid closing delimiter")
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("inspect JSON array close: %w", err)
		}
		if closing != json.Delim(']') {
			return errors.New("JSON array has an invalid closing delimiter")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}

// Validate checks cross-field invariants without interpreting a test result.
func (e Evidence) Validate() error {
	if e.Schema != Schema {
		return fmt.Errorf(
			"validate run status: schema %q, want %q",
			e.Schema,
			Schema,
		)
	}
	if !e.ChildStarted && e.ExitCode != nil {
		return errors.New(
			"validate run status: exit_code requires child_started",
		)
	}
	switch e.Cancellation {
	case "":
		if e.CancellationSignal != "" || e.RecommendedExitCode != 0 {
			return errors.New(
				"validate run status: cancellation details require cancellation",
			)
		}
	case "programmatic", "deadline":
		if e.CancellationSignal != "" || e.RecommendedExitCode != 130 {
			return errors.New(
				"validate run status: invalid non-signal cancellation details",
			)
		}
	case "signal":
		var expectedExitCode int
		switch e.CancellationSignal {
		case "interrupt":
			expectedExitCode = 130
		case "terminated":
			expectedExitCode = 143
		default:
			return errors.New(
				"validate run status: cancellation signal must be " +
					`"interrupt" or "terminated"`,
			)
		}
		if e.RecommendedExitCode != expectedExitCode {
			return fmt.Errorf(
				"validate run status: cancellation signal %q requires "+
					"recommended exit code %d",
				e.CancellationSignal,
				expectedExitCode,
			)
		}
	default:
		return fmt.Errorf(
			"validate run status: unknown cancellation %q",
			e.Cancellation,
		)
	}
	if e.Interrupted != (e.Cancellation != "") {
		return errors.New(
			"validate run status: interrupted and cancellation disagree",
		)
	}
	if e.ExitCode != nil && *e.ExitCode < 0 {
		return errors.New("validate run status: exit_code must not be negative")
	}
	if e.Signal != "" && (e.ExitCode == nil || *e.ExitCode == 0) {
		return errors.New(
			"validate run status: signal requires a known nonzero exit_code",
		)
	}
	if e.StartedAt != nil && e.FinishedAt != nil &&
		e.FinishedAt.Before(*e.StartedAt) {
		return errors.New(
			"validate run status: finished_at precedes started_at",
		)
	}
	if e.DurationNS < 0 {
		return errors.New("validate run status: duration_ns must not be negative")
	}
	if !e.ChildStarted && e.DurationNS != 0 {
		return errors.New(
			"validate run status: duration_ns requires child_started",
		)
	}
	seenFiles := make(map[string]bool, len(e.Files))
	hasEvents := false
	for index, file := range e.Files {
		switch file.Name {
		case "test_output.jsonl":
			hasEvents = true
		case "stderr.log", "coverage.out":
		default:
			return fmt.Errorf(
				"validate run status: file %d has unmanaged name %q",
				index+1,
				file.Name,
			)
		}
		if seenFiles[file.Name] {
			return fmt.Errorf(
				"validate run status: duplicate file binding %q",
				file.Name,
			)
		}
		seenFiles[file.Name] = true
		if file.Size < 0 {
			return fmt.Errorf(
				"validate run status: file %q has negative size",
				file.Name,
			)
		}
		if len(file.SHA256) != 64 || strings.Trim(file.SHA256, "0123456789abcdef") != "" {
			return fmt.Errorf(
				"validate run status: file %q has invalid SHA-256",
				file.Name,
			)
		}
	}
	if !hasEvents {
		return errors.New(
			"validate run status: test_output.jsonl binding is required",
		)
	}
	for index, issue := range e.Issues {
		if issue.Kind == "" {
			return fmt.Errorf(
				"validate run status: issue %d has an empty kind",
				index+1,
			)
		}
		if issue.Message == "" {
			return fmt.Errorf(
				"validate run status: issue %d has an empty message",
				index+1,
			)
		}
	}
	if e.CoveragePolicy != nil {
		if e.CoveragePolicy.Minimum == "" {
			return errors.New(
				"validate run status: coverage policy minimum is empty",
			)
		}
		if e.CoveragePolicy.Satisfied && !e.CoveragePolicy.Available {
			return errors.New(
				"validate run status: unavailable coverage cannot satisfy policy",
			)
		}
		if e.CoveragePolicy.Available && e.CoveragePolicy.Actual == "" {
			return errors.New(
				"validate run status: available coverage policy lacks actual percentage",
			)
		}
		if e.CoveragePolicy.Available &&
			e.CoveragePolicy.Statements == 0 {
			return errors.New(
				"validate run status: available coverage policy has no statements",
			)
		}
		if e.CoveragePolicy.Available && !seenFiles["coverage.out"] {
			return errors.New(
				"validate run status: available coverage policy requires " +
					"a coverage.out binding",
			)
		}
		if e.CoveragePolicy.Covered > e.CoveragePolicy.Statements {
			return errors.New(
				"validate run status: coverage policy covered count exceeds statements",
			)
		}
	}
	return nil
}
