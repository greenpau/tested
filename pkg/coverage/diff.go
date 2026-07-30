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

package coverage

import (
	"context"
	"time"
)

const (
	// DiffSchema identifies the coverage source comparison schema.
	DiffSchema = "tested/coverage-diff/v1"
)

// DiffOptions configures an explicit comparison of the current coverage
// sources with one immutable Git commit.
type DiffOptions struct {
	GitCommand     string
	GoCommand      string
	ProjectDir     string
	BaseRevision   string
	ProfileFiles   []string
	InterruptGrace time.Duration
}

// Diff is a deterministic, JSON-ready source comparison.
type Diff struct {
	Schema     string     `json:"schema"`
	BaseCommit string     `json:"base_commit"`
	Files      []DiffFile `json:"files"`
}

// DiffFile maps one current coverage profile identity to a repository path
// and its comparison with the base commit.
type DiffFile struct {
	ProfilePath string `json:"profile_path"`
	OldPath     string `json:"old_path,omitempty"`
	NewPath     string `json:"new_path,omitempty"`
	// CurrentSHA256 binds the diff to the Go cover page's displayed source:
	// CRLF and lone CR become LF, every tab becomes eight ASCII spaces, and
	// the pre element's one parser-stripped leading LF is omitted.
	CurrentSHA256 string     `json:"current_sha256,omitempty"`
	Status        DiffStatus `json:"status"`
	Reason        string     `json:"reason,omitempty"`
	Hunks         []DiffHunk `json:"hunks,omitempty"`
}

// DiffStatus describes a current coverage file relative to the base commit.
type DiffStatus string

const (
	DiffStatusUnchanged   DiffStatus = "unchanged"
	DiffStatusModified    DiffStatus = "modified"
	DiffStatusAdded       DiffStatus = "added"
	DiffStatusRenamed     DiffStatus = "renamed"
	DiffStatusUntracked   DiffStatus = "untracked"
	DiffStatusUnavailable DiffStatus = "unavailable"
)

// DiffHunk is one zero-context changed region.
type DiffHunk struct {
	OldStart int        `json:"old_start"`
	OldLines int        `json:"old_lines"`
	NewStart int        `json:"new_start"`
	NewLines int        `json:"new_lines"`
	Lines    []DiffLine `json:"lines"`
}

// DiffLine is one added or deleted source line in a zero-context hunk.
type DiffLine struct {
	Kind      DiffLineKind `json:"kind"`
	OldLine   int          `json:"old_line,omitempty"`
	NewLine   int          `json:"new_line,omitempty"`
	Text      string       `json:"text"`
	NoNewline bool         `json:"no_newline,omitempty"`
}

// DiffLineKind describes how a source line changed.
type DiffLineKind string

const (
	DiffLineDelete DiffLineKind = "delete"
	DiffLineAdd    DiffLineKind = "add"
)

// BuildDiff constructs a bounded comparison of current coverage source files
// with an explicitly selected Git base revision.
func BuildDiff(ctx context.Context, options DiffOptions) (*Diff, error) {
	return buildDiff(ctx, options)
}

// ValidateDiffBaseRevision validates an explicit Git base expression without
// starting a process or inspecting a repository.
func ValidateDiffBaseRevision(revision string) error {
	return validateBaseRevision(revision)
}
