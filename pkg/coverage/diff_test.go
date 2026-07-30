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
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDiffRepositoryStates(t *testing.T) {
	gitCommand, goCommand := requireDiffCommands(t)
	projectDir := t.TempDir()
	initializeDiffRepository(t, gitCommand, projectDir)

	writeDiffTestFile(t, projectDir, "go.mod", "module example.com/coverage-diff\n\ngo 1.25\n")
	writeDiffTestFile(t, projectDir, ".gitignore", "pkg/ignored/\n")
	writeDiffTestFile(t, projectDir, "pkg/alpha/alpha.go", `package alpha

func Value() int {
	return 1
}
`)
	writeDiffTestFile(t, projectDir, "pkg/rename/old.go", renameTestSource(1))
	writeDiffTestFile(t, projectDir, "pkg/same/same.go", "package same\n\nconst Value = 1\n")
	writeDiffTestFile(t, projectDir, "pkg/tagged/active.go", "package tagged\n")
	writeDiffTestFile(
		t,
		projectDir,
		"pkg/tagged/tagged.go",
		"//go:build tested_never_tag\n\npackage tagged\n\nconst Value = 1\n",
	)
	writeDiffTestFile(t, projectDir, "pkg/deleted/deleted.go", `package deleted

// This deliberately unique body prevents Git from classifying an unrelated
// short added source as this deleted file's rename.
const (
	DeletedAlpha = "alpha"
	DeletedBeta = "beta"
	DeletedGamma = "gamma"
	DeletedDelta = "delta"
	DeletedEpsilon = "epsilon"
	DeletedZeta = "zeta"
)
`)
	runDiffTestCommand(t, projectDir, gitCommand, "add", "--", ".")
	runDiffTestCommand(t, projectDir, gitCommand, "commit", "-q", "-m", "base")
	baseCommit := strings.TrimSpace(runDiffTestCommand(
		t,
		projectDir,
		gitCommand,
		"rev-parse",
		"HEAD",
	))

	writeDiffTestFile(t, projectDir, "pkg/alpha/alpha.go", `package alpha

func Value() int {
	return 2
}

func Added() bool {
	return true
}
`)
	runDiffTestCommand(
		t,
		projectDir,
		gitCommand,
		"mv",
		"pkg/rename/old.go",
		"pkg/rename/new.go",
	)
	writeDiffTestFile(t, projectDir, "pkg/rename/new.go", renameTestSource(2))
	writeDiffTestFile(t, projectDir, "pkg/added/added.go", "package added\n\nconst Value = 1\n")
	runDiffTestCommand(t, projectDir, gitCommand, "add", "--", "pkg/added/added.go")
	writeDiffTestFile(
		t,
		projectDir,
		"pkg/untracked/untracked.go",
		"package untracked\n\nconst Value = 1",
	)
	writeDiffTestFile(
		t,
		projectDir,
		"pkg/ignored/ignored.go",
		"package ignored\n\nconst Value = 1\n",
	)
	writeDiffTestFile(
		t,
		projectDir,
		"pkg/tagged/tagged.go",
		"//go:build tested_never_tag\n\npackage tagged\n\nconst Value = 2\n",
	)
	if err := os.Remove(filepath.Join(projectDir, "pkg/deleted/deleted.go")); err != nil {
		t.Fatalf("remove deleted source: %v", err)
	}

	profileFiles := []string{
		"example.com/coverage-diff/pkg/untracked/untracked.go",
		"example.com/coverage-diff/pkg/same/same.go",
		"example.com/coverage-diff/pkg/tagged/tagged.go",
		"example.com/coverage-diff/pkg/rename/new.go",
		"example.com/coverage-diff/pkg/missing/missing.go",
		"example.com/coverage-diff/pkg/ignored/ignored.go",
		"example.com/coverage-diff/pkg/alpha/alpha.go",
		"example.com/coverage-diff/pkg/added/added.go",
		"example.com/coverage-diff/pkg/alpha/alpha.go",
	}
	got, err := BuildDiff(context.Background(), DiffOptions{
		GitCommand:   gitCommand,
		GoCommand:    goCommand,
		ProjectDir:   projectDir,
		BaseRevision: "HEAD",
		ProfileFiles: profileFiles,
	})
	if err != nil {
		t.Fatalf("BuildDiff() error = %v", err)
	}
	if got.Schema != DiffSchema {
		t.Fatalf("BuildDiff().Schema = %q, want %q", got.Schema, DiffSchema)
	}
	if got.BaseCommit != baseCommit {
		t.Fatalf("BuildDiff().BaseCommit = %q, want %q", got.BaseCommit, baseCommit)
	}
	if len(got.Files) != 8 {
		t.Fatalf("BuildDiff() file count = %d, want 8: %#v", len(got.Files), got.Files)
	}
	for _, file := range got.Files {
		if file.Status == DiffStatusUnavailable {
			continue
		}
		digest, decodeErr := hex.DecodeString(file.CurrentSHA256)
		if decodeErr != nil || len(digest) != 32 {
			t.Fatalf(
				"current source digest for %q = %q, decode error %v",
				file.ProfilePath,
				file.CurrentSHA256,
				decodeErr,
			)
		}
	}
	for i := 1; i < len(got.Files); i++ {
		if got.Files[i-1].ProfilePath >= got.Files[i].ProfilePath {
			t.Fatalf("BuildDiff() files are not uniquely sorted: %#v", got.Files)
		}
	}

	alpha := requireDiffFile(
		t,
		got,
		"example.com/coverage-diff/pkg/alpha/alpha.go",
	)
	requireDiffFileStatus(t, alpha, DiffStatusModified, "pkg/alpha/alpha.go", "pkg/alpha/alpha.go")
	requireDiffLine(t, alpha, DiffLineDelete, "return 1")
	requireDiffLine(t, alpha, DiffLineAdd, "return 2")

	added := requireDiffFile(
		t,
		got,
		"example.com/coverage-diff/pkg/added/added.go",
	)
	requireDiffFileStatus(t, added, DiffStatusAdded, "", "pkg/added/added.go")
	if len(added.Hunks) != 1 ||
		added.Hunks[0].OldStart != 0 ||
		added.Hunks[0].OldLines != 0 ||
		added.Hunks[0].NewStart != 1 {
		t.Fatalf("added file hunks = %#v, want one full-file addition", added.Hunks)
	}

	renamed := requireDiffFile(
		t,
		got,
		"example.com/coverage-diff/pkg/rename/new.go",
	)
	requireDiffFileStatus(
		t,
		renamed,
		DiffStatusRenamed,
		"pkg/rename/old.go",
		"pkg/rename/new.go",
	)
	requireDiffLine(t, renamed, DiffLineDelete, "return 1")
	requireDiffLine(t, renamed, DiffLineAdd, "return 2")

	unchanged := requireDiffFile(
		t,
		got,
		"example.com/coverage-diff/pkg/same/same.go",
	)
	requireDiffFileStatus(
		t,
		unchanged,
		DiffStatusUnchanged,
		"pkg/same/same.go",
		"pkg/same/same.go",
	)
	if len(unchanged.Hunks) != 0 {
		t.Fatalf("unchanged file has hunks: %#v", unchanged.Hunks)
	}
	tagged := requireDiffFile(
		t,
		got,
		"example.com/coverage-diff/pkg/tagged/tagged.go",
	)
	requireDiffFileStatus(
		t,
		tagged,
		DiffStatusModified,
		"pkg/tagged/tagged.go",
		"pkg/tagged/tagged.go",
	)
	requireDiffLine(t, tagged, DiffLineDelete, "Value = 1")
	requireDiffLine(t, tagged, DiffLineAdd, "Value = 2")

	untracked := requireDiffFile(
		t,
		got,
		"example.com/coverage-diff/pkg/untracked/untracked.go",
	)
	requireDiffFileStatus(
		t,
		untracked,
		DiffStatusUntracked,
		"",
		"pkg/untracked/untracked.go",
	)
	if !diffHasNoNewlineMarker(untracked) {
		t.Fatalf("untracked no-newline source lacks EOF marker: %#v", untracked.Hunks)
	}
	ignored := requireDiffFile(
		t,
		got,
		"example.com/coverage-diff/pkg/ignored/ignored.go",
	)
	requireDiffFileStatus(
		t,
		ignored,
		DiffStatusUntracked,
		"",
		"pkg/ignored/ignored.go",
	)
	untrackedCanonical, err := browserCanonicalSource(
		[]byte("package untracked\n\nconst Value = 1"),
	)
	if err != nil {
		t.Fatalf("browserCanonicalSource() error = %v", err)
	}
	if want := canonicalSourceSHA256(untrackedCanonical); untracked.CurrentSHA256 != want {
		t.Fatalf(
			"untracked current SHA-256 = %q, want %q",
			untracked.CurrentSHA256,
			want,
		)
	}

	unavailable := requireDiffFile(
		t,
		got,
		"example.com/coverage-diff/pkg/missing/missing.go",
	)
	requireDiffFileStatus(t, unavailable, DiffStatusUnavailable, "", "")
	if unavailable.Reason != diffUnavailableNotGoListed {
		t.Fatalf("unavailable reason = %q, want %q", unavailable.Reason, diffUnavailableNotGoListed)
	}

	firstJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal(BuildDiff()) error = %v", err)
	}
	secondJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("second json.Marshal(BuildDiff()) error = %v", err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatal("BuildDiff() JSON encoding is not deterministic")
	}
	if !bytes.Contains(firstJSON, []byte(`"profile_path"`)) {
		t.Fatalf("BuildDiff() JSON lacks profile_path: %s", firstJSON)
	}
	if bytes.Contains(firstJSON, []byte(projectDir)) ||
		bytes.Contains(firstJSON, []byte("tested-coverage-diff-")) {
		t.Fatalf("BuildDiff() JSON leaks a host or snapshot path: %s", firstJSON)
	}
}

func TestBuildDiffValidation(t *testing.T) {
	projectDir := t.TempDir()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	oversizedRevision := strings.Repeat("a", maxDiffRevisionBytes+1)
	oversizedProfile := strings.Repeat("p", maxDiffProfilePathBytes+1)

	tests := []struct {
		name    string
		ctx     context.Context
		options DiffOptions
		target  error
	}{
		{
			name:    "nil context",
			options: DiffOptions{},
			target:  ErrInvalidDiffOptions,
		},
		{
			name: "cancelled context",
			ctx:  cancelled,
			options: DiffOptions{
				ProjectDir:   projectDir,
				BaseRevision: "HEAD",
			},
			target: context.Canceled,
		},
		{
			name: "missing project directory",
			ctx:  context.Background(),
			options: DiffOptions{
				BaseRevision: "HEAD",
			},
			target: ErrInvalidDiffOptions,
		},
		{
			name: "missing revision",
			ctx:  context.Background(),
			options: DiffOptions{
				ProjectDir: projectDir,
			},
			target: ErrInvalidDiffOptions,
		},
		{
			name: "option-shaped revision",
			ctx:  context.Background(),
			options: DiffOptions{
				ProjectDir:   projectDir,
				BaseRevision: "--help",
			},
			target: ErrInvalidDiffOptions,
		},
		{
			name: "control-bearing revision",
			ctx:  context.Background(),
			options: DiffOptions{
				ProjectDir:   projectDir,
				BaseRevision: "HEAD\nother",
			},
			target: ErrInvalidDiffOptions,
		},
		{
			name: "oversized revision",
			ctx:  context.Background(),
			options: DiffOptions{
				ProjectDir:   projectDir,
				BaseRevision: oversizedRevision,
			},
			target: ErrDiffLimit,
		},
		{
			name: "oversized profile path",
			ctx:  context.Background(),
			options: DiffOptions{
				ProjectDir:   projectDir,
				BaseRevision: "HEAD",
				ProfileFiles: []string{oversizedProfile},
			},
			target: ErrDiffLimit,
		},
		{
			name: "negative interrupt grace",
			ctx:  context.Background(),
			options: DiffOptions{
				ProjectDir:     projectDir,
				BaseRevision:   "HEAD",
				InterruptGrace: -1,
			},
			target: ErrInvalidDiffOptions,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildDiff(test.ctx, test.options)
			if !errors.Is(err, test.target) {
				t.Fatalf("BuildDiff() error = %v, want errors.Is(%v)", err, test.target)
			}
		})
	}
}

func TestBuildDiffRevisionIsOneArgument(t *testing.T) {
	gitCommand, goCommand := requireDiffCommands(t)
	projectDir := t.TempDir()
	initializeDiffRepository(t, gitCommand, projectDir)
	writeDiffTestFile(t, projectDir, "go.mod", "module example.com/exact-argv\n\ngo 1.25\n")
	writeDiffTestFile(t, projectDir, "main.go", "package main\n\nfunc main() {}\n")
	runDiffTestCommand(t, projectDir, gitCommand, "add", "--", ".")
	runDiffTestCommand(t, projectDir, gitCommand, "commit", "-q", "-m", "base")

	sentinel := filepath.Join(projectDir, "revision-was-executed")
	_, err := BuildDiff(context.Background(), DiffOptions{
		GitCommand:   gitCommand,
		GoCommand:    goCommand,
		ProjectDir:   projectDir,
		BaseRevision: "HEAD;touch " + sentinel,
	})
	if err == nil {
		t.Fatal("BuildDiff() error = nil, want invalid Git expression")
	}
	if _, statErr := os.Stat(sentinel); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("revision was interpreted by a shell, stat error = %v", statErr)
	}
	if !strings.Contains(err.Error(), "stderr:") {
		t.Fatalf("BuildDiff() error = %q, want bounded Git stderr detail", err)
	}
}

func TestBuildDiffMapsExactAndDirectSourcesFromSubdirectory(t *testing.T) {
	gitCommand, goCommand := requireDiffCommands(t)
	repositoryRoot := t.TempDir()
	initializeDiffRepository(t, gitCommand, repositoryRoot)
	writeDiffTestFile(
		t,
		repositoryRoot,
		"go.mod",
		"module example.com/subdirectory-diff\n\ngo 1.25\n",
	)
	writeDiffTestFile(
		t,
		repositoryRoot,
		"cmd/tool/tool.go",
		"package main\n\nfunc main() {}\n",
	)
	writeDiffTestFile(
		t,
		repositoryRoot,
		"pkg/shared/shared.go",
		"package shared\n\nfunc Value() int {\n\treturn 1\n}\n",
	)
	runDiffTestCommand(t, repositoryRoot, gitCommand, "add", "--", ".")
	runDiffTestCommand(t, repositoryRoot, gitCommand, "commit", "-q", "-m", "base")
	writeDiffTestFile(
		t,
		repositoryRoot,
		"pkg/shared/shared.go",
		"package shared\n\nfunc Value() int {\n\treturn 2\n}\n",
	)

	projectDir := filepath.Join(repositoryRoot, "cmd", "tool")
	absoluteProfile, err := filepath.EvalSymlinks(
		filepath.Join(repositoryRoot, "pkg", "shared", "shared.go"),
	)
	if err != nil {
		t.Fatalf("resolve absolute profile source: %v", err)
	}
	profileFiles := []string{
		"example.com/subdirectory-diff/pkg/shared/shared.go",
		"../../pkg/shared/shared.go",
		filepath.ToSlash(absoluteProfile),
	}
	got, err := BuildDiff(context.Background(), DiffOptions{
		GitCommand:   gitCommand,
		GoCommand:    goCommand,
		ProjectDir:   projectDir,
		BaseRevision: "HEAD",
		ProfileFiles: profileFiles,
	})
	if err != nil {
		t.Fatalf("BuildDiff() error = %v", err)
	}
	if len(got.Files) != len(profileFiles) {
		t.Fatalf("BuildDiff() file count = %d, want %d", len(got.Files), len(profileFiles))
	}
	var digest string
	for _, profilePath := range profileFiles {
		file := requireDiffFile(t, got, profilePath)
		requireDiffFileStatus(
			t,
			file,
			DiffStatusModified,
			"pkg/shared/shared.go",
			"pkg/shared/shared.go",
		)
		requireDiffLine(t, file, DiffLineDelete, "return 1")
		requireDiffLine(t, file, DiffLineAdd, "return 2")
		if digest == "" {
			digest = file.CurrentSHA256
		} else if file.CurrentSHA256 != digest {
			t.Fatalf(
				"equivalent source mappings have digests %q and %q",
				digest,
				file.CurrentSHA256,
			)
		}
	}
}

func TestParseZeroContextPatch(t *testing.T) {
	patch := []byte("diff --git old.go new.go\n" +
		"--- old.go\n" +
		"+++ new.go\n" +
		"@@ -1 +1 @@\n" +
		"-old value\n" +
		"\\ No newline at end of file\n" +
		"+new value\n" +
		"\\ No newline at end of file\n" +
		"@@ -4,0 +5,2 @@ func next\n" +
		"+first\n" +
		"+second\n")
	hunks, lines, err := parseZeroContextPatch(patch, 2, 4)
	if err != nil {
		t.Fatalf("parseZeroContextPatch() error = %v", err)
	}
	if lines != 4 || len(hunks) != 2 {
		t.Fatalf("parseZeroContextPatch() = %d lines, %#v", lines, hunks)
	}
	if got := hunks[0]; got.OldStart != 1 ||
		got.OldLines != 1 ||
		got.NewStart != 1 ||
		got.NewLines != 1 ||
		len(got.Lines) != 2 {
		t.Fatalf("first hunk = %#v", got)
	}
	if !hunks[0].Lines[0].NoNewline || !hunks[0].Lines[1].NoNewline {
		t.Fatalf("no-newline markers were not retained: %#v", hunks[0].Lines)
	}
	if got := hunks[1]; got.OldStart != 4 ||
		got.OldLines != 0 ||
		got.NewStart != 5 ||
		got.NewLines != 2 {
		t.Fatalf("second hunk = %#v", got)
	}
}

func TestParseZeroContextPatchRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name  string
		patch string
		hunks int
		lines int
	}{
		{
			name:  "unterminated record",
			patch: "@@ -1 +1 @@\n-old\n+new",
			hunks: 1,
			lines: 2,
		},
		{
			name:  "count mismatch",
			patch: "@@ -1,2 +1 @@\n-old\n+new\n",
			hunks: 1,
			lines: 3,
		},
		{
			name:  "context in zero-context hunk",
			patch: "@@ -1 +1 @@\n old\n",
			hunks: 1,
			lines: 2,
		},
		{
			name:  "misplaced marker",
			patch: "@@ -0,0 +1 @@\n\\ No newline at end of file\n+x\n",
			hunks: 1,
			lines: 1,
		},
		{
			name:  "hunk bound",
			patch: "@@ -1 +1 @@\n-old\n+new\n",
			hunks: 0,
			lines: 2,
		},
		{
			name:  "line bound",
			patch: "@@ -1 +1 @@\n-old\n+new\n",
			hunks: 1,
			lines: 1,
		},
		{
			name:  "invalid range",
			patch: "@@ -x +1 @@\n-old\n+new\n",
			hunks: 1,
			lines: 2,
		},
		{
			name:  "declared lines exceed cardinality before allocation",
			patch: "@@ -1,9223372036854775807 +1 @@\n-old\n+new\n",
			hunks: 1,
			lines: 2,
		},
		{
			name:  "coordinate exceeds displayed source",
			patch: "@@ -100001 +1 @@\n-old\n+new\n",
			hunks: 1,
			lines: 2,
		},
		{
			name: "overlapping hunks",
			patch: "@@ -1 +1 @@\n-old\n+new\n" +
				"@@ -1 +1 @@\n-old again\n+new again\n",
			hunks: 2,
			lines: 4,
		},
		{
			name:  "tab survived canonicalization",
			patch: "@@ -1 +1 @@\n-old\n+\tnew\n",
			hunks: 1,
			lines: 2,
		},
		{
			name:  "carriage return survived canonicalization",
			patch: "@@ -1 +1 @@\n-old\n+new\r\n",
			hunks: 1,
			lines: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := parseZeroContextPatch(
				[]byte(test.patch),
				test.hunks,
				test.lines,
			)
			if err == nil {
				t.Fatal("parseZeroContextPatch() error = nil")
			}
		})
	}
}

func TestParseGitChanges(t *testing.T) {
	current := map[string]struct{}{
		"pkg/added.go":   {},
		"pkg/current.go": {},
		"pkg/same.go":    {},
	}
	data := []byte("M\x00pkg/same.go\x00" +
		"A\x00pkg/added.go\x00" +
		"R087\x00pkg/old.go\x00pkg/current.go\x00" +
		"M\x00ignored/file.go\x00")
	got, err := parseGitChanges(data, current)
	if err != nil {
		t.Fatalf("parseGitChanges() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("parseGitChanges() = %#v, want 3 changes", got)
	}
	if got["pkg/added.go"].status != DiffStatusAdded ||
		got["pkg/same.go"].status != DiffStatusModified {
		t.Fatalf("parseGitChanges() statuses = %#v", got)
	}
	rename := got["pkg/current.go"]
	if rename.status != DiffStatusRenamed ||
		rename.oldPath != "pkg/old.go" ||
		rename.newPath != "pkg/current.go" {
		t.Fatalf("parseGitChanges() rename = %#v", rename)
	}
}

func TestParseGitChangesRejectsMalformedInput(t *testing.T) {
	current := map[string]struct{}{"pkg/current.go": {}}
	tests := [][]byte{
		[]byte("M\x00pkg/current.go"),
		[]byte("X\x00pkg/current.go\x00"),
		[]byte("R101\x00pkg/old.go\x00pkg/current.go\x00"),
		[]byte("R90\x00pkg/old.go\x00"),
		[]byte("M100\x00pkg/current.go\x00"),
		[]byte("M\x00../escape.go\x00"),
	}
	for _, data := range tests {
		if _, err := parseGitChanges(data, current); err == nil {
			t.Fatalf("parseGitChanges(%q) error = nil", data)
		}
	}
}

func TestDiffCommandEnvironmentOverridesInheritedValues(t *testing.T) {
	t.Setenv("LC_ALL", "host-locale")
	t.Setenv("LANG", "host-language")
	t.Setenv("GIT_PAGER", "host-pager")
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	t.Setenv("GIT_OPTIONAL_LOCKS", "1")
	t.Setenv("GIT_NO_LAZY_FETCH", "0")
	t.Setenv("GOTOOLCHAIN", "auto")
	t.Setenv("GOPROXY", "https://proxy.example.invalid")
	t.Setenv("goproxy", "https://lowercase-proxy.example.invalid")
	t.Setenv("GONOPROXY", "*")
	t.Setenv("GOSUMDB", "sum.example.invalid")
	t.Setenv("GONOSUMDB", "*")
	t.Setenv("GOVCS", "*:all")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOENV", "/tmp/host-go-env")
	environment := diffCommandEnvironment()
	want := map[string]string{
		"LC_ALL":              "C",
		"LANG":                "C",
		"GIT_PAGER":           "cat",
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_OPTIONAL_LOCKS":  "0",
		"GIT_NO_LAZY_FETCH":   "1",
		"GOTOOLCHAIN":         "local",
		"GOPROXY":             "off",
		"GONOPROXY":           "none",
		"GOSUMDB":             "off",
		"GONOSUMDB":           "none",
		"GOVCS":               "*:off",
		"GOFLAGS":             "",
		"GOENV":               "off",
	}
	for key, value := range want {
		prefix := key + "="
		count := 0
		for _, entry := range environment {
			entryKey, _, _ := strings.Cut(entry, "=")
			if strings.EqualFold(entryKey, key) {
				count++
				if entry != prefix+value {
					t.Fatalf("%s override = %q, want %q", key, entry, prefix+value)
				}
			}
		}
		if count != 1 {
			t.Fatalf("%s entry count = %d, want 1", key, count)
		}
	}
}

func TestBoundedDiffCapture(t *testing.T) {
	budget := byteBudget{maximum: 5}
	capture := boundedCapture{
		perLimit:  4,
		aggregate: &budget,
		limitName: "test",
	}
	if n, err := capture.Write([]byte("1234")); err != nil || n != 4 {
		t.Fatalf("boundedCapture.Write() = %d, %v", n, err)
	}
	if n, err := capture.Write([]byte("5")); n != 0 || !errors.Is(err, ErrDiffLimit) {
		t.Fatalf("boundedCapture.Write(over limit) = %d, %v", n, err)
	}
	if capture.String() != "1234" || budget.used != 4 {
		t.Fatalf("boundedCapture state = %q, %d", capture.String(), budget.used)
	}
}

func TestProjectedDiffAliasesCannotAmplifyCachedHunks(t *testing.T) {
	sharedHunks := []DiffHunk{
		{
			OldStart: 1,
			OldLines: 1,
			NewStart: 0,
			NewLines: 0,
			Lines: []DiffLine{
				{
					Kind:    DiffLineDelete,
					OldLine: 1,
					Text:    "removed",
				},
			},
		},
	}
	firstFile := DiffFile{
		ProfilePath:   "./a/source.go",
		OldPath:       "source.go",
		NewPath:       "source.go",
		CurrentSHA256: strings.Repeat("a", 64),
		Status:        DiffStatusModified,
		Hunks:         sharedHunks,
	}
	secondFile := firstFile
	secondFile.ProfilePath = "./b/source.go"
	projected, err := measureDiffFileProjection(firstFile)
	if err != nil {
		t.Fatalf("measureDiffFileProjection() error = %v", err)
	}

	newDocument := func() *Diff {
		return &Diff{
			Schema:     DiffSchema,
			BaseCommit: strings.Repeat("b", 40),
			Files:      make([]DiffFile, 0, 2),
		}
	}
	emptyDocument := newDocument()
	emptyJSON, err := json.Marshal(emptyDocument)
	if err != nil {
		t.Fatalf("marshal empty diff: %v", err)
	}
	fileJSON, err := json.Marshal(firstFile)
	if err != nil {
		t.Fatalf("marshal projected file: %v", err)
	}
	baseRetained := int64(
		len(emptyDocument.Schema) + len(emptyDocument.BaseCommit),
	)

	tests := []struct {
		name       string
		limits     diffProjectionLimits
		wantDetail string
	}{
		{
			name: "hunks",
			limits: diffProjectionLimits{
				maxHunks:         1,
				maxLines:         10,
				maxRetainedBytes: 1 << 20,
				maxEncodedBytes:  1 << 20,
			},
			wantDetail: "hunk count",
		},
		{
			name: "lines",
			limits: diffProjectionLimits{
				maxHunks:         10,
				maxLines:         1,
				maxRetainedBytes: 1 << 20,
				maxEncodedBytes:  1 << 20,
			},
			wantDetail: "line count",
		},
		{
			name: "retained bytes",
			limits: diffProjectionLimits{
				maxHunks:         10,
				maxLines:         10,
				maxRetainedBytes: baseRetained + projected.retainedBytes,
				maxEncodedBytes:  1 << 20,
			},
			wantDetail: "retained",
		},
		{
			name: "encoded bytes",
			limits: diffProjectionLimits{
				maxHunks:         10,
				maxLines:         10,
				maxRetainedBytes: 1 << 20,
				maxEncodedBytes: int64(
					len(emptyJSON) + len(fileJSON),
				),
			},
			wantDetail: "serialized",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff := newDocument()
			builder := diffBuilder{projectionLimits: test.limits}
			if err := builder.initializeDiffProjection(diff); err != nil {
				t.Fatalf("initializeDiffProjection() error = %v", err)
			}
			if err := builder.appendProjectedDiffFile(diff, firstFile); err != nil {
				t.Fatalf("first appendProjectedDiffFile() error = %v", err)
			}
			if len(diff.Files) != 1 {
				t.Fatalf("projected file count = %d, want 1", len(diff.Files))
			}
			if &diff.Files[0].Hunks[0].Lines[0] ==
				&sharedHunks[0].Lines[0] {
				t.Fatal("retained hunk lines alias the cached source")
			}
			before := builder.projection
			err := builder.appendProjectedDiffFile(diff, secondFile)
			if !errors.Is(err, ErrDiffLimit) {
				t.Fatalf(
					"second appendProjectedDiffFile() error = %v, want ErrDiffLimit",
					err,
				)
			}
			if !strings.Contains(err.Error(), test.wantDetail) {
				t.Fatalf(
					"second appendProjectedDiffFile() error = %q, want %q",
					err,
					test.wantDetail,
				)
			}
			if len(diff.Files) != 1 {
				t.Fatalf(
					"failed alias retained %d files, want 1",
					len(diff.Files),
				)
			}
			if builder.projection != before {
				t.Fatalf(
					"failed alias changed projection from %#v to %#v",
					before,
					builder.projection,
				)
			}
			encoded, marshalErr := json.Marshal(diff)
			if marshalErr != nil {
				t.Fatalf("marshal retained projection: %v", marshalErr)
			}
			if int64(len(encoded)) != builder.projection.encodedBytes {
				t.Fatalf(
					"encoded projection = %d bytes, charged %d",
					len(encoded),
					builder.projection.encodedBytes,
				)
			}
		})
	}
}

func TestBrowserCanonicalSourceAndDigest(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "tabs CRLF lone CR Unicode and trailing newline",
			raw:  "\tπ\r\nnext\rfinal\n",
			want: "        π\nnext\nfinal\n",
		},
		{
			name: "tab and no trailing newline",
			raw:  "value\t",
			want: "value        ",
		},
		{
			name: "already canonical",
			raw:  "plain\nsource\n",
			want: "plain\nsource\n",
		},
		{
			name: "pre strips exactly one leading newline",
			raw:  "\n\npackage source\n",
			want: "\npackage source\n",
		},
		{
			name: "pre strips normalized leading CRLF",
			raw:  "\r\npackage source\n",
			want: "package source\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := browserCanonicalSource([]byte(test.raw))
			if err != nil {
				t.Fatalf("browserCanonicalSource() error = %v", err)
			}
			if string(got) != test.want {
				t.Fatalf("browserCanonicalSource() = %q, want %q", got, test.want)
			}
			if canonicalSourceSHA256(got) != canonicalSourceSHA256([]byte(test.want)) {
				t.Fatal("canonical source digest does not bind canonical bytes")
			}
		})
	}
	tooManyTabs := bytes.Repeat(
		[]byte{'\t'},
		maxDiffSourceFileBytes/8+1,
	)
	if _, err := browserCanonicalSource(tooManyTabs); !errors.Is(err, ErrDiffLimit) {
		t.Fatalf("browserCanonicalSource(oversized display) error = %v", err)
	}
	tooManyLines := bytes.Repeat(
		[]byte("x\n"),
		maxDiffSourceLines+1,
	)
	if _, err := browserCanonicalSource(tooManyLines); !errors.Is(err, ErrDiffLimit) {
		t.Fatalf("browserCanonicalSource(too many lines) error = %v", err)
	}
}

func TestReadCurrentDiffSourceEnforcesPerFileBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.go")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create oversized source: %v", err)
	}
	if err := file.Truncate(maxDiffSourceFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatalf("truncate oversized source: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close oversized source: %v", err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve oversized source path: %v", err)
	}
	builder := diffBuilder{
		sourceBudget: byteBudget{maximum: maxDiffSourceTotalBytes},
	}
	if _, err := builder.readCurrentSource(path); !errors.Is(err, ErrDiffLimit) {
		t.Fatalf("readCurrentSource() error = %v, want ErrDiffLimit", err)
	}
}

func requireDiffCommands(t *testing.T) (string, string) {
	t.Helper()
	gitCommand, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git is unavailable: %v", err)
	}
	goCommand, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go is unavailable: %v", err)
	}
	return gitCommand, goCommand
}

func initializeDiffRepository(t *testing.T, gitCommand, projectDir string) {
	t.Helper()
	runDiffTestCommand(t, projectDir, gitCommand, "init", "-q")
	runDiffTestCommand(t, projectDir, gitCommand, "config", "user.name", "Tested Test")
	runDiffTestCommand(t, projectDir, gitCommand, "config", "user.email", "tested@example.invalid")
	runDiffTestCommand(t, projectDir, gitCommand, "config", "commit.gpgsign", "false")
}

func runDiffTestCommand(
	t *testing.T,
	projectDir, executable string,
	arguments ...string,
) string {
	t.Helper()
	command := exec.Command(executable, arguments...)
	command.Dir = projectDir
	command.Env = append(
		os.Environ(),
		"LC_ALL=C",
		"LANG=C",
		"GIT_TERMINAL_PROMPT=0",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf(
			"run %q %q: %v\n%s",
			executable,
			arguments,
			err,
			output,
		)
	}
	return string(output)
}

func writeDiffTestFile(
	t *testing.T,
	projectDir, relativePath, content string,
) {
	t.Helper()
	path := filepath.Join(projectDir, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create test source directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write test source %q: %v", relativePath, err)
	}
}

func renameTestSource(value int) string {
	return `package renamepkg

const (
	Alpha = 1
	Beta = 2
	Gamma = 3
	Delta = 4
	Epsilon = 5
	Zeta = 6
	Eta = 7
	Theta = 8
	Iota = 9
	Kappa = 10
)

func Value() int {
	return ` + string(rune('0'+value)) + `
}
`
}

func requireDiffFile(t *testing.T, diff *Diff, profilePath string) DiffFile {
	t.Helper()
	for _, file := range diff.Files {
		if file.ProfilePath == profilePath {
			return file
		}
	}
	t.Fatalf("BuildDiff() lacks profile %q: %#v", profilePath, diff.Files)
	return DiffFile{}
}

func requireDiffFileStatus(
	t *testing.T,
	file DiffFile,
	status DiffStatus,
	oldPath, newPath string,
) {
	t.Helper()
	if file.Status != status ||
		file.OldPath != oldPath ||
		file.NewPath != newPath {
		t.Fatalf(
			"diff file = status %q, old %q, new %q; want %q, %q, %q",
			file.Status,
			file.OldPath,
			file.NewPath,
			status,
			oldPath,
			newPath,
		)
	}
}

func requireDiffLine(
	t *testing.T,
	file DiffFile,
	kind DiffLineKind,
	textFragment string,
) {
	t.Helper()
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line.Kind == kind && strings.Contains(line.Text, textFragment) {
				if kind == DiffLineDelete && line.OldLine == 0 {
					t.Fatalf("deleted line lacks an old line number: %#v", line)
				}
				if kind == DiffLineAdd && line.NewLine == 0 {
					t.Fatalf("added line lacks a new line number: %#v", line)
				}
				return
			}
		}
	}
	t.Fatalf("diff file lacks %q line containing %q: %#v", kind, textFragment, file.Hunks)
}

func diffHasNoNewlineMarker(file DiffFile) bool {
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line.NoNewline {
				return true
			}
		}
	}
	return false
}
