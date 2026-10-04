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
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/greenpau/tested/pkg/coverage"
	"github.com/greenpau/tested/pkg/result"
)

// Derive labels from already-redacted complete identities. Matching the original
// import path first prevents redaction collisions from shortening unrelated
// packages; trimming only the redacted value preserves separator-spanning rules.
func (r *Renderer) packageLabel(original, redacted string) string {
	if redacted == "" {
		return "unavailable"
	}
	if r.basePackage == "" {
		return redacted
	}
	base := r.redact(r.basePackage)
	if original == r.basePackage && redacted == base {
		return "."
	}
	if strings.HasPrefix(original, r.basePackage+"/") && strings.HasPrefix(redacted, base+"/") {
		if label := strings.TrimPrefix(redacted, base+"/"); label != "" {
			return label
		}
	}
	return redacted
}

// bindHTMLHierarchy projects normalized parent identities before redaction can
// make distinct names look alike. Missing parents stay at the package root.
// Requiring a strict name prefix also prevents malformed input from producing
// a cycle. IDs contain only deterministic indexes, never unredacted names.
func bindHTMLHierarchy(tests []result.TestOccurrence, views []occurrenceView, packageIndex int) {
	ids := make(map[result.OccurrenceID]string, len(tests))
	for i, test := range tests {
		views[i].HTMLID = fmt.Sprintf("test-%d-%d", packageIndex, i)
		ids[test.ID] = views[i].HTMLID
	}
	for i, test := range tests {
		parent := test.Parent
		if parent == nil || parent.Package != test.ID.Package ||
			parent.Name == "" || !strings.HasPrefix(test.ID.Name, parent.Name+"/") {
			continue
		}
		views[i].ParentHTMLID = ids[*parent]
	}
}

// RenderTestOutputHTML writes a self-contained searchable test report.
func (r *Renderer) RenderTestOutputHTML(writer io.Writer, input Input) error {
	if r == nil {
		return errors.New("render test HTML: renderer is nil")
	}
	if writer == nil {
		return errors.New("render test HTML: writer is nil")
	}
	if r.assets == nil || r.assets.testOutputTemplate == nil {
		return errors.New("render test HTML: embedded assets are unavailable")
	}
	view := r.buildView(input)
	sort.SliceStable(view.Packages, func(i, j int) bool {
		return view.Packages[i].Label < view.Packages[j].Label
	})
	// Select the slowest occurrences by duration, then present the HTML table
	// by package. Console summaries retain their duration ranking.
	sort.SliceStable(view.Slowest, func(i, j int) bool {
		return view.Slowest[i].PackageLabel < view.Slowest[j].PackageLabel
	})
	if view.Coverage != nil {
		sort.SliceStable(view.Coverage.Files, func(i, j int) bool {
			return view.Coverage.Files[i].PackageLabel < view.Coverage.Files[j].PackageLabel
		})
	}
	if err := r.assets.testOutputTemplate.ExecuteTemplate(
		writer,
		"test_output.html",
		view,
	); err != nil {
		return fmt.Errorf("render test HTML: %w", err)
	}
	return nil
}

// Only compact change membership enters the test report, never source hunks or
// deleted text. Coverage identity matching happens before display redaction.
type changeFilterView struct {
	BaseCommit string
	Packages   int
}

func (r *Renderer) buildChangeFilter(input Input) (*changeFilterView, map[string]bool) {
	diff := input.CoverageDiff
	if diff == nil || diff.Schema != coverage.DiffSchema || diff.BaseCommit == "" || input.Coverage == nil {
		return nil, nil
	}
	profileFiles := make(map[string]bool, len(input.Coverage.Files))
	for _, file := range input.Coverage.Files {
		profileFiles[file.Name] = true
	}
	changed := make(map[string]bool)
	for _, file := range diff.Files {
		if !profileFiles[file.ProfilePath] {
			continue
		}
		switch file.Status {
		case coverage.DiffStatusModified, coverage.DiffStatusAdded, coverage.DiffStatusRenamed, coverage.DiffStatusUntracked:
			pkg, _ := splitCoveragePath(file.ProfilePath)
			changed[pkg] = true
		}
	}
	view := &changeFilterView{BaseCommit: r.redact(diff.BaseCommit)}
	seen := make(map[string]bool)
	for _, pkg := range input.Result.Packages {
		if changed[pkg.Name] && !seen[pkg.Name] {
			view.Packages++
			seen[pkg.Name] = true
		}
	}
	return view, changed
}
