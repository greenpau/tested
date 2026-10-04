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
	"html"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/greenpau/tested/pkg/coverage"
	"github.com/greenpau/tested/pkg/result"
)

func TestHTMLPackageFailureEvidenceStartsOpen(t *testing.T) {
	renderer, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []result.Status{result.StatusPassed, result.StatusFailed, result.StatusIncomplete} {
		t.Run(string(state), func(t *testing.T) {
			input := Input{Result: result.Result{Packages: []result.Package{{
				Name: "example/pkg", Status: state,
				Output:     []result.Output{{Text: "package evidence"}},
				Attributes: []result.Attribute{{Key: "owner", Value: "package-owner"}},
			}}}}
			var output bytes.Buffer
			if err := renderer.RenderTestOutputHTML(&output, input); err != nil {
				t.Fatal(err)
			}
			for _, label := range []string{"Package output", "Package metadata"} {
				want := "<details open"
				if state == result.StatusPassed {
					want += " data-collapse"
				}
				want += "><summary>" + label + "</summary>"
				if !strings.Contains(output.String(), want) {
					t.Fatalf("%s lost initial disclosure state for %s", state, label)
				}
			}
		})
	}
}

func TestPackageCoverageUsesExactFileWeightsAndOriginalIdentity(t *testing.T) {
	file := func(name string, covered, total uint64) coverage.FileSummary {
		return coverage.FileSummary{Name: name, Totals: coverage.Totals{Covered: covered, Statements: total}}
	}
	for _, tc := range []struct {
		name, pkg, percentage string
		files                 []coverage.FileSummary
		covered, total        uint64
	}{
		{name: "weighted files exclude subpackages", pkg: "example/p", percentage: "10.00%", covered: 10, total: 100,
			files: []coverage.FileSummary{file("example/p/a.go", 9, 10), file("example/p/b.go", 1, 90), file("example/p/child/c.go", 100, 100)}},
		{name: "zero covered", pkg: "example/p", percentage: "0.00%", total: 10,
			files: []coverage.FileSummary{file("example/p/a.go", 0, 10)}},
		{name: "no profile", pkg: "example/p"},
		{name: "empty file", pkg: "example/p", files: []coverage.FileSummary{file("example/p/a.go", 0, 0)}},
		{name: "unmapped absolute path", pkg: "example/p", files: []coverage.FileSummary{file("/work/example/p/a.go", 1, 1)}},
		{name: "maximum integer", pkg: "example/p", percentage: ">99.99%", covered: math.MaxUint64 - 1, total: math.MaxUint64,
			files: []coverage.FileSummary{file("example/p/a.go", math.MaxUint64-1, math.MaxUint64)}},
		{name: "overflow stays unavailable", pkg: "example/p",
			files: []coverage.FileSummary{file("example/p/a.go", math.MaxUint64, math.MaxUint64), file("example/p/b.go", 1, 1), file("example/p/c.go", 1, 1)}},
		{name: "invalid weights stay unavailable", pkg: "example/p",
			files: []coverage.FileSummary{file("example/p/a.go", 2, 1), file("example/p/b.go", 1, 1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			renderer, err := New(Options{})
			if err != nil {
				t.Fatal(err)
			}
			input := Input{Result: result.Result{Packages: []result.Package{{Name: tc.pkg}}}}
			if tc.files != nil {
				input.Coverage = &coverage.Profile{Mode: coverage.ModeSet, Files: tc.files}
			}
			got := renderer.buildView(input).Packages[0].Coverage
			if tc.percentage == "" {
				if got != nil && got.Available {
					t.Fatalf("unavailable package gained coverage: %+v", got)
				}
			} else if got == nil || !got.Available || got.Covered != tc.covered ||
				got.Statements != tc.total || got.Percentage != tc.percentage {
				t.Fatalf("package coverage = %+v, want %d/%d (%s)", got, tc.covered, tc.total, tc.percentage)
			}
		})
	}

	renderer, err := New(Options{RedactPatterns: []string{`secret-(one|two)`}})
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Result: result.Result{Packages: []result.Package{
		{Name: "example/secret-one", Elapsed: time.Millisecond, DurationSource: result.DurationGoElapsed},
		{Name: "example/secret-two", DurationSource: result.DurationGoElapsed},
	}}, Coverage: &coverage.Profile{Mode: coverage.ModeSet, Files: []coverage.FileSummary{
		file("example/secret-one/a.go", 9, 10), file("example/secret-two/a.go", 1, 10),
	}}}
	view := renderer.buildView(input)
	if view.Packages[0].Name != view.Packages[1].Name ||
		view.Packages[0].Coverage.Percentage != "90.00%" || view.Packages[1].Coverage.Percentage != "10.00%" {
		t.Fatal("redaction merged package coverage")
	}
	var output bytes.Buffer
	if err := renderer.RenderTestOutputHTML(&output, input); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-duration="1000000" data-covered="9" data-statements="10"`,
		`data-duration="0" data-covered="1" data-statements="10"`,
		`aria-controls="package-0-contents"`, `aria-controls="package-1-contents"`,
		`id="package-0-contents"`, `id="package-1-contents"`,
		`Coverage 90.00%`, `Coverage 10.00%`, `Duration 1ms`, `Duration 0s`,
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("package header missing %q", want)
		}
	}
	if strings.Contains(output.String(), "secret-one") || strings.Contains(output.String(), "secret-two") {
		t.Fatal("package header exposed original identity")
	}
}

func TestCoverageTableSortValuesAndRedactedPaths(t *testing.T) {
	renderer, err := New(Options{RedactPatterns: []string{`secret/value`}})
	if err != nil {
		t.Fatal(err)
	}
	const covered uint64 = 9007199254740993
	const statements uint64 = covered + 100
	for _, tc := range []struct {
		name, pkg, file string
	}{
		{name: "pkg/secret/value.go", pkg: "pkg", file: "[REDACTED].go"},
		{name: "root.go", pkg: ".", file: "root.go"},
		{name: "/root.go", pkg: "/", file: "root.go"},
		{name: `C:\pkg\file.go`, pkg: `C:\pkg`, file: "file.go"},
		{name: `pkg/<script>&".go`, pkg: "pkg", file: `<script>&".go`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			totals := coverage.Totals{Covered: covered, Statements: statements}
			profile := &coverage.Profile{Mode: coverage.ModeSet, Total: totals,
				Files: []coverage.FileSummary{{Name: tc.name, Totals: totals}}}
			file := renderer.buildCoverageView(profile).Files[0]
			if file.Package != tc.pkg || file.File != tc.file {
				t.Fatalf("package/file = %q/%q, want %q/%q", file.Package, file.File, tc.pkg, tc.file)
			}
			var output bytes.Buffer
			if err := renderer.RenderTestOutputHTML(&output, Input{Coverage: profile}); err != nil {
				t.Fatal(err)
			}
			page := output.String()
			for _, want := range []string{
				`data-sort="text" aria-sort="ascending">Package</th>`,
				fmt.Sprintf(`data-sort-value="%d"`, covered),
				fmt.Sprintf(`data-sort-covered="%d" data-sort-total="%d"`, covered, statements),
				`data-package-label>` + html.EscapeString(tc.pkg),
				html.EscapeString(tc.file) + `</td>`,
			} {
				if !strings.Contains(page, want) {
					t.Errorf("missing exact, escaped sorting data %q", want)
				}
			}
			if strings.Contains(page, "secret/value") || strings.Contains(page, `<script>&"`) {
				t.Fatal("coverage path bypassed redaction or escaping")
			}
		})
	}
}

func TestHTMLCompactRowsPreserveEvidence(t *testing.T) {
	renderer, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		status result.Status
		known  bool
	}{
		{name: "passed", status: result.StatusPassed, known: true},
		{name: "failed", status: result.StatusFailed, known: true},
		{name: "incomplete duration unavailable", status: result.StatusIncomplete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			test := result.TestOccurrence{
				ID:   result.OccurrenceID{Package: "example.com/pkg", Name: "TestCompact", Ordinal: 2},
				Kind: result.TestKindTest, Status: tc.status,
				Output:          []result.Output{{Text: "retained output"}},
				OutputTruncated: true, OutputBytes: 200, OutputRetainedBytes: 15,
				Attributes: []result.Attribute{{Key: "owner", Value: "team"}},
				Artifacts:  []result.Artifact{{Path: "/tmp/evidence"}},
			}
			if tc.known {
				test.Elapsed = time.Millisecond
				test.DurationSource = result.DurationGoElapsed
			} else {
				test.IncompleteReason = "missing terminal event"
			}
			input := Input{Result: result.Result{Finalized: true, Packages: []result.Package{{
				Name: test.ID.Package, Tests: []result.TestOccurrence{test},
			}}}}
			var output bytes.Buffer
			if err := renderer.RenderTestOutputHTML(&output, input); err != nil {
				t.Fatal(err)
			}
			_, row, found := strings.Cut(output.String(), `<div class="test-heading">`)
			if !found {
				t.Fatal("missing test heading")
			}
			heading, rest, _ := strings.Cut(row, "</div>")
			if !strings.Contains(heading, "TestCompact [attempt 2]") {
				t.Fatal("compact heading lost repeated occurrence identity")
			}
			for _, forbidden := range []string{test.ID.Package, "occurrence", "go_elapsed"} {
				if strings.Contains(heading, forbidden) {
					t.Errorf("compact heading contains redundant %q", forbidden)
				}
			}
			panel, afterPanel, _ := strings.Cut(rest, "</details>")
			for _, evidence := range []string{"occurrence 2", "retained output", "owner", "team", "/tmp/evidence"} {
				if !strings.Contains(panel, evidence) {
					t.Errorf("details lost %q", evidence)
				}
			}
			if strings.Contains(panel, "data-collapse") != (tc.status == result.StatusPassed) {
				t.Error("failed/incomplete details must stay open; passing details should collapse")
			}
			if !strings.HasPrefix(strings.TrimSpace(afterPanel), `<p class="notice">Output truncated:`) {
				t.Error("retention notice must remain outside collapsed details")
			}
			if tc.known {
				if !strings.Contains(heading, ">1ms</span>") || !strings.Contains(panel, "go_elapsed") {
					t.Error("duration or its provenance was lost")
				}
			} else if !strings.Contains(heading, `aria-label="Duration unavailable"`) ||
				!strings.Contains(panel, "duration unavailable") || !strings.Contains(rest, "missing terminal event") {
				t.Error("unknown duration or incomplete reason was lost")
			}
		})
	}
}

func TestSlowestPackageContextAndDefaultOrder(t *testing.T) {
	input := Input{Result: result.Result{Finalized: true}}
	for _, pkg := range []string{"example/p", "example/q"} {
		p := result.Package{Name: pkg}
		for i, owner := range []string{"example/p", "example/q", "example/p", "example/p"} {
			if owner == pkg {
				p.Tests = append(p.Tests, result.TestOccurrence{
					ID:      result.OccurrenceID{Package: pkg, Name: fmt.Sprintf("TestRank%d", i), Ordinal: 1},
					Elapsed: time.Duration(4-i) * time.Second,
				})
			}
		}
		input.Result.Packages = append(input.Result.Packages, p)
	}
	for _, format := range []string{"html", "plain", "markdown"} {
		t.Run(format, func(t *testing.T) {
			var output bytes.Buffer
			if format == "html" {
				renderer, err := New(Options{Slowest: 4})
				if err != nil {
					t.Fatal(err)
				}
				if err := renderer.RenderTestOutputHTML(&output, input); err != nil {
					t.Fatal(err)
				}
			} else {
				console, err := NewConsole(ConsoleOptions{Writer: &output, Format: ConsoleFormat(format), Slowest: 4})
				if err != nil {
					t.Fatal(err)
				}
				if err := console.Final(input); err != nil {
					t.Fatal(err)
				}
			}
			_, slowest, found := strings.Cut(output.String(), "Slowest occurrences")
			if !found {
				t.Fatal("missing slowest occurrences")
			}
			if format == "html" {
				if strings.Count(slowest, `<span data-package-label>example/p</span>`) != 3 ||
					strings.Contains(slowest, `data-package-label class="sr-only"`) {
					t.Fatal("HTML package cells must all show their package")
				}
			} else if strings.Count(slowest, "example/p") != 2 || strings.Count(slowest, "example/q") != 1 {
				t.Fatalf("package grouping changed: %s", slowest)
			}
			order := []int{0, 1, 2, 3}
			if format == "html" {
				order = []int{0, 2, 3, 1}
			}
			for _, i := range order {
				_, rest, found := strings.Cut(slowest, fmt.Sprintf("TestRank%d", i))
				if !found {
					t.Fatalf("unexpected %s ordering at %d: %s", format, i, slowest)
				}
				slowest = rest
			}
		})
	}
}

func TestHTMLSlowestLimitSelectsDurationsBeforeOrderingPackages(t *testing.T) {
	input := Input{Result: result.Result{Finalized: true}}
	for _, pkg := range []struct {
		name, test string
		duration   time.Duration
		ordinals   []uint64
	}{
		{"example/z", "TestRepeated", 9 * time.Second, []uint64{2, 1}},
		{"example/a", "TestFast", time.Second, []uint64{1}},
		{"example/b", "TestMiddle", 8 * time.Second, []uint64{1}},
	} {
		p := result.Package{Name: pkg.name, Status: result.StatusPassed}
		for _, ordinal := range pkg.ordinals {
			p.Tests = append(p.Tests, result.TestOccurrence{
				ID:      result.OccurrenceID{Package: pkg.name, Name: pkg.test, Ordinal: ordinal},
				Elapsed: pkg.duration, DurationSource: result.DurationGoElapsed, Status: result.StatusPassed,
			})
		}
		input.Result.Packages = append(input.Result.Packages, p)
	}
	for _, tc := range []struct {
		limit int
		want  []string
	}{
		{limit: 0},
		{limit: 1, want: []string{"TestRepeated"}},
		{limit: 3, want: []string{"TestMiddle", "TestRepeated", "TestRepeated [attempt 2]"}},
		{limit: 10, want: []string{"TestFast", "TestMiddle", "TestRepeated", "TestRepeated [attempt 2]"}},
	} {
		t.Run(fmt.Sprintf("limit=%d", tc.limit), func(t *testing.T) {
			renderer, err := New(Options{Slowest: tc.limit})
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := renderer.RenderTestOutputHTML(&output, input); err != nil {
				t.Fatal(err)
			}
			_, section, found := strings.Cut(output.String(), `<h2 id="slowest-heading">`)
			if len(tc.want) == 0 {
				if found {
					t.Fatal("disabled slowest list is still rendered")
				}
				return
			}
			_, body, _ := strings.Cut(section, "<tbody>")
			body, _, _ = strings.Cut(body, "</tbody>")
			if got := strings.Count(body, "<tr>"); got != len(tc.want) {
				t.Fatalf("slowest rows = %d, want %d", got, len(tc.want))
			}
			for _, name := range tc.want {
				_, rest, found := strings.Cut(body, `<td class="mono">`+name+`</td>`)
				if !found {
					t.Fatalf("missing or out-of-order occurrence %q in %s", name, body)
				}
				body = rest
			}
		})
	}
	if input.Result.Packages[0].Name != "example/z" || input.Result.Packages[0].Tests[0].ID.Ordinal != 2 {
		t.Fatal("HTML sorting mutated source package or occurrence order")
	}
}

func TestHTMLFilterFieldsAreScopedRedactedAndEscaped(t *testing.T) {
	renderer, err := New(Options{RedactPatterns: []string{"secret-[a-z]+"}})
	if err != nil {
		t.Fatal(err)
	}
	pkg, name := `pkg/secret-package"<&`, `Test/secret-name"<&`
	input := Input{Result: result.Result{
		Finalized: true,
		Packages: []result.Package{{
			Name:   pkg,
			Output: []result.Output{{Text: "package secret-log <&>"}},
			Tests: []result.TestOccurrence{{
				ID:     result.OccurrenceID{Package: pkg, Name: name, Ordinal: 2},
				Output: []result.Output{{Text: "test secret-log <&>"}},
			}},
		}},
		Builds: []result.Build{{
			ImportPath: `build/secret-path"<&`,
			Output:     []result.Output{{Text: "build secret-log <&>"}},
		}},
		UnattributedOutput: []result.Output{{Text: "unattributed secret-log <&>"}},
		Diagnostics: []result.Diagnostic{{
			Message: "diagnostic secret-log <&>", Preview: "preview secret-log <&>",
		}},
	}}
	var output bytes.Buffer
	if err := renderer.RenderTestOutputHTML(&output, input); err != nil {
		t.Fatal(err)
	}
	page := output.String()
	for _, tc := range []struct {
		field string
		value string
		count int
	}{
		{field: "package", value: `pkg/[REDACTED]"<&`, count: 2},
		{field: "test", value: `Test/[REDACTED]"<&`, count: 1},
		{field: "package", value: `build/[REDACTED]"<&`, count: 1},
	} {
		attribute := `data-` + tc.field + `="` + html.EscapeString(tc.value) + `"`
		if got := strings.Count(page, attribute); got != tc.count {
			t.Errorf("%s occurs %d times, want %d", attribute, got, tc.count)
		}
	}
	for _, scope := range []string{"package", "test", "build", "unattributed", "diagnostic", "preview"} {
		tag := "pre"
		if scope == "diagnostic" {
			tag = "p"
		}
		want := "<" + tag + " data-filter-output>" + scope + " [REDACTED] &lt;&amp;&gt;</" + tag + ">"
		if !strings.Contains(page, want) {
			t.Errorf("missing escaped, scoped output %q", want)
		}
	}
	for _, forbidden := range []string{"secret-", "data-search=", `data-test="Test/[REDACTED]&#34;&lt;&amp; [attempt 2]"`} {
		if strings.Contains(page, forbidden) {
			t.Errorf("HTML contains forbidden filter data %q", forbidden)
		}
	}
}

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

func TestHTMLRelativePackageLabels(t *testing.T) {
	for _, tc := range []struct{ name, base, original, pattern, want string }{
		{"root", "example.com/root", "example.com/root", "", "."},
		{"subpackage", "example.com/root", "example.com/root/internal/tag", "", "internal/tag"},
		{"similar prefix", "example.com/root", "example.com/root-other/internal", "", "example.com/root-other/internal"},
		{"empty suffix", "example.com/root", "example.com/root/", "", "example.com/root/"},
		{"external", "example.com/root", "other.example/root/internal", "", "other.example/root/internal"},
		{"no base", "", "example.com/root/internal", "", "example.com/root/internal"},
		{"unknown", "example.com/root", "", "", "unavailable"},
		{"anchored redaction", "example.com/root", "example.com/root/secret", `^example.com/root/secret$`, "[REDACTED]"},
		{"redacted base", "example.com/root", "example.com/root/internal", `example.com/root`, "internal"},
		{"collision", "example.com/secret-root-long", "example.com/secret-other-long/internal", `secret-(root|other)-long`, "example.com/[REDACTED]/internal"},
		{"hostile", "example.com/root", `example.com/root/<script>&"`, "", `<script>&"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := Options{BasePackage: tc.base, Slowest: 10}
			if tc.pattern != "" {
				options.RedactPatterns = []string{tc.pattern}
			}
			renderer, err := New(options)
			if err != nil {
				t.Fatal(err)
			}
			input := Input{Assessment: &Assessment{}, Result: result.Result{Packages: []result.Package{{Name: tc.original,
				Tests: []result.TestOccurrence{{ID: result.OccurrenceID{Package: tc.original, Name: "TestOne", Ordinal: 1}, Elapsed: time.Second}},
			}}, Builds: []result.Build{{ImportPath: tc.original}}}}
			view := renderer.buildView(input)
			if view.Packages[0].Label != tc.want || view.Slowest[0].PackageLabel != tc.want || view.Builds[0].Label != tc.want {
				t.Fatalf("labels = %q, %q, %q; want %q", view.Packages[0].Label, view.Slowest[0].PackageLabel, view.Builds[0].Label, tc.want)
			}
			var output bytes.Buffer
			if err := renderer.RenderTestOutputHTML(&output, input); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), `<span data-package-label>`+html.EscapeString(tc.want)+`</span>`) {
				t.Fatal("slowest cell lost escaped package label")
			}
			base := renderer.redact(tc.base)
			if base == "" {
				base = "unavailable"
			}
			if !strings.Contains(output.String(), `<dt>Base package</dt><dd class="mono">`+html.EscapeString(base)+`</dd>`) {
				t.Fatal("assessment lost redacted base package context")
			}
			plainOptions := options
			plainOptions.BasePackage = ""
			plain, err := New(plainOptions)
			if err != nil {
				t.Fatal(err)
			}
			for _, render := range []func(*Renderer, *bytes.Buffer) error{
				func(r *Renderer, w *bytes.Buffer) error { return r.RenderSummaryJSON(w, input) },
				func(r *Renderer, w *bytes.Buffer) error { return r.RenderJUnitXML(w, input) },
			} {
				var before, after bytes.Buffer
				if err := render(plain, &before); err != nil {
					t.Fatal(err)
				}
				if err := render(renderer, &after); err != nil {
					t.Fatal(err)
				}
				if before.String() != after.String() {
					t.Fatal("HTML labels changed machine identities")
				}
			}
		})
	}
}

func TestHTMLCoverageRelativeLabelsRedactWholePaths(t *testing.T) {
	renderer, err := New(Options{BasePackage: "example.com/root", RedactPatterns: []string{`root/secret`, `root/private/file`, `^example.com/root/hidden/file.go$`}})
	if err != nil {
		t.Fatal(err)
	}
	profile := &coverage.Profile{Files: []coverage.FileSummary{
		{Name: "example.com/root/file.go"}, {Name: "example.com/root/internal/tag/file.go"},
		{Name: "example.com/root-other/file.go"}, {Name: "example.com/root/secret/file.go"},
		{Name: "example.com/root/private/file.go"},
		{Name: "example.com/root/hidden/file.go"},
	}}
	view := renderer.buildCoverageView(profile)
	want := map[string]string{
		"example.com/root/file.go":              ".",
		"example.com/root/internal/tag/file.go": "internal/tag",
		"example.com/root-other/file.go":        "example.com/root-other",
		"example.com/[REDACTED]/file.go":        "example.com/[REDACTED]",
		"example.com/[REDACTED].go":             "example.com",
		"[REDACTED]":                            "unavailable",
	}
	for _, file := range view.Files {
		if expected, ok := want[file.Name]; !ok || file.PackageLabel != expected {
			t.Fatalf("coverage file = %+v", file)
		}
	}
}

func TestHTMLChangedPackagesUsePublishedComparisonAndOriginalIdentity(t *testing.T) {
	renderer, err := New(Options{BasePackage: "example.com/root", RedactPatterns: []string{`secret-(alpha|bravo)-long`}})
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Coverage: &coverage.Profile{}, CoverageDiff: &coverage.Diff{
		Schema: coverage.DiffSchema, BaseCommit: strings.Repeat("a", 40),
	}}
	for _, tc := range []struct {
		pkg     string
		status  coverage.DiffStatus
		changed bool
	}{
		{"secret-alpha-long", coverage.DiffStatusModified, true},
		{"secret-bravo-long", coverage.DiffStatusUnchanged, false},
		{"added", coverage.DiffStatusAdded, true},
		{"rename", coverage.DiffStatusRenamed, true},
		{"untracked", coverage.DiffStatusUntracked, true},
		{"unknown", coverage.DiffStatusUnavailable, false},
		{"future", coverage.DiffStatus("future-status"), false},
	} {
		name := "example.com/root/" + tc.pkg
		input.Result.Packages = append(input.Result.Packages, result.Package{Name: name,
			Tests: []result.TestOccurrence{{ID: result.OccurrenceID{Package: name, Name: "TestChange", Ordinal: 1}}}})
		input.Coverage.Files = append(input.Coverage.Files, coverage.FileSummary{Name: name + "/file.go"})
		input.CoverageDiff.Files = append(input.CoverageDiff.Files, coverage.DiffFile{
			ProfilePath: name + "/file.go", Status: tc.status,
			Hunks: []coverage.DiffHunk{{Lines: []coverage.DiffLine{{Text: "deleted-source-must-not-enter-test-html"}}}},
		})
		view, changed := renderer.buildChangeFilter(input)
		if view == nil || changed[name] != tc.changed {
			t.Fatalf("change classification for %q = %v", name, changed)
		}
	}
	// A changed child does not mark its parent, and a diff outside the current
	// profile cannot mark a package. Root coverage must not be inferred by suffix.
	for _, name := range []string{"example.com/root", "example.com/root/not-in-profile"} {
		input.Result.Packages = append(input.Result.Packages, result.Package{Name: name})
	}
	input.CoverageDiff.Files = append(input.CoverageDiff.Files, coverage.DiffFile{
		ProfilePath: "example.com/root/not-in-profile/a.go", Status: coverage.DiffStatusModified,
	})
	view := renderer.buildView(input)
	if view.Changes == nil || view.Changes.Packages != 4 {
		t.Fatalf("change count = %+v", view.Changes)
	}
	var redactedChanged, redactedUnchanged int
	for _, pkg := range view.Packages {
		if strings.Contains(pkg.Name, "[REDACTED]") {
			if pkg.Changed {
				redactedChanged++
			} else {
				redactedUnchanged++
			}
		}
		for _, occurrence := range pkg.Tests {
			if occurrence.Changed != pkg.Changed {
				t.Fatal("test membership differs from its package")
			}
		}
	}
	if redactedChanged != 1 || redactedUnchanged != 1 {
		t.Fatal("redaction merged change membership")
	}
	var output bytes.Buffer
	if err := renderer.RenderTestOutputHTML(&output, input); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret-alpha-long", "secret-bravo-long", "deleted-source-must-not-enter-test-html"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("HTML leaked %q", forbidden)
		}
	}
	if !strings.Contains(output.String(), `id="changed-packages-only"`) || !strings.Contains(output.String(), `href="index.html"`) {
		t.Fatal("missing change filter or index navigation")
	}
	input.CoverageDiff.Files = nil
	output.Reset()
	if err := renderer.RenderTestOutputHTML(&output, input); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `aria-describedby="changed-packages-description" disabled`) {
		t.Fatal("empty baseline comparison must disable the filter")
	}
	for _, diff := range []*coverage.Diff{nil, {Schema: "invalid", BaseCommit: "base"}, {Schema: coverage.DiffSchema}} {
		input.CoverageDiff = diff
		output.Reset()
		if err := renderer.RenderTestOutputHTML(&output, input); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), `<input id="changed-packages-only"`) {
			t.Fatal("missing/invalid baseline must not expose a change filter")
		}
	}
}
