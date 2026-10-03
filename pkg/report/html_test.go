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
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/greenpau/tested/pkg/result"
)

func TestHTMLHierarchyUsesRecordedOccurrenceIdentity(t *testing.T) {
	id := func(name string, ordinal uint64) result.OccurrenceID {
		return result.OccurrenceID{Package: "pkg", Name: name, Ordinal: ordinal}
	}
	root, repeat := id("TestTree", 1), id("TestTree", 2)
	child := id("TestTree/child", 1)
	missing := id("TestMissing", 1)
	foreign := result.OccurrenceID{Package: "other", Name: root.Name, Ordinal: 1}
	tests := []struct {
		name   string
		child  result.OccurrenceID
		parent *result.OccurrenceID
		want   string
	}{
		{name: "root", child: child},
		{name: "recorded parent", child: child, parent: &root, want: "test-3-0"},
		{name: "child ordinal differs from parent", child: child, parent: &repeat, want: "test-3-1"},
		{name: "missing immediate parent uses recorded ancestor", child: id("TestTree/missing/leaf", 1), parent: &root, want: "test-3-0"},
		{name: "missing parent stays root", child: id("TestMissing/leaf", 1), parent: &missing},
		{name: "cross package stays root", child: child, parent: &foreign},
		{name: "prefix requires slash boundary", child: id("TestTreeOther/leaf", 1), parent: &root},
		{name: "self reference stays root", child: child, parent: &child},
		{name: "descendant cannot be parent", child: id("Test", 1), parent: &child},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			occurrences := []result.TestOccurrence{{ID: root}, {ID: repeat}, {ID: tc.child, Parent: tc.parent}}
			views := make([]occurrenceView, len(occurrences))
			bindHTMLHierarchy(occurrences, views, 3)
			if views[2].ParentHTMLID != tc.want {
				t.Fatalf("parent ID = %q, want %q", views[2].ParentHTMLID, tc.want)
			}
			for i, view := range views {
				if want := fmt.Sprintf("test-3-%d", i); view.HTMLID != want {
					t.Errorf("ID = %q, want %q", view.HTMLID, want)
				}
			}
		})
	}
}

func TestHTMLHierarchySurvivesRedactionAndSorting(t *testing.T) {
	renderer, err := New(Options{RedactPatterns: []string{"secret-[ab]"}})
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Result: result.Result{Finalized: true}}
	for _, pkg := range []string{"z", "a"} {
		p := result.Package{Name: pkg}
		for _, name := range []string{"Test/secret-b", "Test/secret-a"} {
			parent := result.OccurrenceID{Package: pkg, Name: name, Ordinal: 2}
			p.Tests = append(p.Tests,
				result.TestOccurrence{ID: result.OccurrenceID{Package: pkg, Name: name + "/<script>", Ordinal: 1}, Parent: &parent},
				result.TestOccurrence{ID: parent},
			)
		}
		input.Result.Packages = append(input.Result.Packages, p)
	}
	view := renderer.buildView(input)
	for p, pkg := range view.Packages {
		for _, i := range []int{1, 3} {
			if pkg.Tests[i].ParentHTMLID != pkg.Tests[i-1].HTMLID {
				t.Fatalf("package %d child %d lost its exact parent", p, i)
			}
		}
		if pkg.Tests[0].Label != pkg.Tests[2].Label {
			t.Fatal("fixture names did not collide after redaction")
		}
	}
	var first, second bytes.Buffer
	if err := renderer.RenderTestOutputHTML(&first, input); err != nil {
		t.Fatal(err)
	}
	if err := renderer.RenderTestOutputHTML(&second, input); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatal("HTML is not deterministic")
	}
	for _, secret := range []string{"secret-a", "secret-b", "/<script>"} {
		if strings.Contains(first.String(), secret) {
			t.Errorf("HTML leaked %q", secret)
		}
	}
	for p := range view.Packages {
		for i := 0; i < 4; i++ {
			id := fmt.Sprintf(`id="test-%d-%d"`, p, i)
			if strings.Count(first.String(), id) != 1 {
				t.Errorf("HTML must contain exactly one %s", id)
			}
		}
	}
	if input.Result.Packages[0].Tests[0].Parent.Name != "Test/secret-b" {
		t.Fatal("renderer mutated source identity")
	}
}
