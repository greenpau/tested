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
	"strings"

	"github.com/greenpau/tested/pkg/result"
)

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
	if err := r.assets.testOutputTemplate.ExecuteTemplate(
		writer,
		"test_output.html",
		r.buildView(input),
	); err != nil {
		return fmt.Errorf("render test HTML: %w", err)
	}
	return nil
}
