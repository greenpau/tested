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
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf8"
)

func TestEmbeddedReportAssetInventory(t *testing.T) {
	entries, err := fs.ReadDir(reportAssets, "assets")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("unexpected embedded asset directory %q", entry.Name())
		}
		names = append(names, entry.Name())
		data, err := reportAssets.ReadFile("assets/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if len(data) == 0 {
			t.Errorf("embedded asset %q is empty", entry.Name())
		}
		if !utf8.Valid(data) {
			t.Errorf("embedded asset %q is not valid UTF-8", entry.Name())
		}
	}
	want := []string{
		"coverage.js",
		"coverage_head.html",
		"index.html",
		"report.css",
		"test_output.html",
		"test_output.js",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("embedded assets = %v, want %v", names, want)
	}

	bundle, err := loadReportAssets(reportAssets)
	if err != nil {
		t.Fatalf("loadReportAssets() error = %v", err)
	}
	if bundle.testOutputTemplate == nil ||
		bundle.indexTemplate == nil ||
		bundle.coverageTemplate == nil ||
		len(bundle.coverageHead) == 0 {
		t.Fatal("loaded embedded asset bundle is incomplete")
	}
	for _, expected := range []string{
		"--background:",
		"Content-Security-Policy",
		coverageThemeMarker,
		coverageExplorerMarker,
	} {
		if !strings.Contains(string(bundle.coverageHead), expected) {
			t.Errorf("coverage head lacks %q", expected)
		}
	}
	if strings.Contains(string(bundle.coverageHead), "{{") {
		t.Error("coverage head contains an unresolved template action")
	}

	for _, name := range []string{
		"coverage.js",
		"report.css",
		"test_output.js",
	} {
		data, err := reportAssets.ReadFile("assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{
			"@import",
			"url(",
			"http://",
			"https://",
			"box-shadow:",
			"</style",
			"</script",
		} {
			if strings.Contains(string(data), forbidden) {
				t.Errorf("%s contains forbidden syntax %q", name, forbidden)
			}
		}
	}
}

func TestLoadReportAssetsReturnsErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(fstest.MapFS)
	}{
		{
			name: "missing JavaScript",
			mutate: func(source fstest.MapFS) {
				delete(source, "assets/test_output.js")
			},
		},
		{
			name: "missing coverage JavaScript",
			mutate: func(source fstest.MapFS) {
				delete(source, "assets/coverage.js")
			},
		},
		{
			name: "malformed test template",
			mutate: func(source fstest.MapFS) {
				source["assets/test_output.html"] = &fstest.MapFile{
					Data: []byte("{{if}}"),
				}
			},
		},
		{
			name: "malformed JavaScript template",
			mutate: func(source fstest.MapFS) {
				source["assets/test_output.js"] = &fstest.MapFile{
					Data: []byte("{{if}}"),
				}
			},
		},
		{
			name: "CSS closes style element",
			mutate: func(source fstest.MapFS) {
				source["assets/report.css"] = &fstest.MapFile{
					Data: []byte(`</STYLE><p>outside style</p>`),
				}
			},
		},
		{
			name: "CSS references external resource",
			mutate: func(source fstest.MapFS) {
				source["assets/report.css"] = &fstest.MapFile{
					Data: []byte(`body { background: url(example.png); }`),
				}
			},
		},
		{
			name: "JavaScript closes script element",
			mutate: func(source fstest.MapFS) {
				source["assets/test_output.js"] = &fstest.MapFile{
					Data: []byte(`"</SCRIPT><p>outside script</p>"`),
				}
			},
		},
		{
			name: "coverage JavaScript closes script element",
			mutate: func(source fstest.MapFS) {
				source["assets/coverage.js"] = &fstest.MapFile{
					Data: []byte(`"</SCRIPT><p>outside script</p>"`),
				}
			},
		},
		{
			name: "missing coverage marker",
			mutate: func(source fstest.MapFS) {
				source["assets/coverage_head.html"] = &fstest.MapFile{
					Data: []byte(
						`<style>{{template "report.css"}}</style>` +
							`<script ` + coverageExplorerMarker + `>` +
							`{{template "coverage.js"}}</script>`,
					),
				}
			},
		},
		{
			name: "duplicate coverage marker",
			mutate: func(source fstest.MapFS) {
				source["assets/coverage_head.html"] = &fstest.MapFile{
					Data: []byte(
						`<style ` + coverageThemeMarker + `>` +
							`{{template "report.css"}}</style>` +
							`<style ` + coverageThemeMarker + `></style>`,
						// The explorer is intentionally absent: the duplicate
						// theme must remain the first structural failure.
					),
				}
			},
		},
		{
			name: "missing coverage explorer marker",
			mutate: func(source fstest.MapFS) {
				source["assets/coverage_head.html"] = &fstest.MapFile{
					Data: []byte(
						`<style ` + coverageThemeMarker + `>` +
							`{{template "report.css"}}</style>` +
							`<script>{{template "coverage.js"}}</script>`,
					),
				}
			},
		},
		{
			name: "coverage fragment closes head",
			mutate: func(source fstest.MapFS) {
				source["assets/coverage_head.html"] = &fstest.MapFile{
					Data: []byte(
						`<style ` + coverageThemeMarker + `>` +
							`{{template "report.css"}}</style>` +
							`<script ` + coverageExplorerMarker + `>` +
							`{{template "coverage.js"}}</script></head>`,
					),
				}
			},
		},
	}

	if _, err := loadReportAssets(nil); err == nil {
		t.Fatal("loadReportAssets(nil) error = nil")
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := validReportAssetFS()
			test.mutate(source)
			if _, err := loadReportAssets(source); err == nil {
				t.Fatal("loadReportAssets() error = nil")
			}
		})
	}
}

func TestCoverageJavaScriptInteractionContract(t *testing.T) {
	data, err := reportAssets.ReadFile("assets/coverage.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, expected := range []string{
		`"DOMContentLoaded"`,
		`"tested/coverage-diff/v1"`,
		`"tested-coverage-package"`,
		`"tested-coverage-changed-only"`,
		`"tested-coverage-tab-coverage"`,
		`"tested-coverage-tab-changes"`,
		`"tested-coverage-layout-unified"`,
		`"tested-coverage-layout-split"`,
		`"tested-coverage-gap-button"`,
		`value.current_sha256`,
		`sha256CanonicalSource`,
		`"Uncovered regions"`,
		`"Changed files only"`,
		`"Changes only"`,
		`"Entire file"`,
		`isChangedDiffFile`,
		`applyFileFilters`,
		`document.createTreeWalker`,
		`document.createTextNode`,
		`dispatchNativeChange`,
		`state.ui.view.addEventListener("click"`,
		`"columnheader"`,
		`pendingFocusIndex`,
		`aria-live`,
	} {
		if !strings.Contains(source, expected) {
			t.Errorf("coverage.js lacks interaction contract %q", expected)
		}
	}
}

func TestReportJavaScriptSecurity(t *testing.T) {
	for _, name := range []string{"coverage.js", "test_output.js"} {
		t.Run(name, func(t *testing.T) {
			data, err := reportAssets.ReadFile("assets/" + name)
			if err != nil {
				t.Fatal(err)
			}
			source := string(data)
			lower := strings.ToLower(source)
			for _, forbidden := range []string{
				"innerhtml",
				"outerhtml",
				"insertadjacenthtml",
				"document.write",
				"document.writeln",
				"eval(",
				"new function",
				"fetch(",
				"xmlhttprequest",
				"websocket",
				"localstorage",
				"sessionstorage",
				"indexeddb",
				"navigator.clipboard",
				"window.open",
				"dynamic import",
			} {
				if strings.Contains(lower, forbidden) {
					t.Errorf("script contains forbidden API %q", forbidden)
				}
			}
		})
	}
}

func TestCoverageCSSInteractionContract(t *testing.T) {
	data, err := reportAssets.ReadFile("assets/report.css")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, expected := range []string{
		".tested-coverage-switch {",
		".tested-coverage-switch-track",
		".tested-coverage-switch-count",
		"contain: layout;",
		".tested-coverage-split-cell .tested-coverage-code",
	} {
		if !strings.Contains(source, expected) {
			t.Errorf("report.css lacks coverage filter style %q", expected)
		}
	}

	ruleBody := func(scope, selector string) (string, bool) {
		start := strings.Index(scope, selector)
		if start < 0 {
			return "", false
		}
		end := strings.Index(scope[start:], "}")
		if end < 0 {
			return "", false
		}
		return scope[start : start+end], true
	}
	for _, test := range []struct {
		selector  string
		forbidden string
	}{
		{
			selector:  "#topbar {",
			forbidden: "border-bottom:",
		},
		{
			selector: "#tested-coverage-coverage-controls,\n" +
				"#tested-coverage-changes-controls {",
			forbidden: "border-top:",
		},
	} {
		rule, ok := ruleBody(source, test.selector)
		if !ok {
			t.Errorf("report.css lacks complete rule %q", test.selector)
			continue
		}
		if strings.Contains(rule, test.forbidden) {
			t.Errorf(
				"report.css rule %q retains %q",
				test.selector,
				test.forbidden,
			)
		}
	}

	printStart := strings.LastIndex(source, "@media print {")
	if printStart < 0 {
		t.Fatal("report.css lacks coverage print rules")
	}
	printSource := source[printStart:]
	for _, test := range []struct {
		selector string
		required string
	}{
		{
			selector: ".tested-coverage-changes-view\n" +
				"    .tested-coverage-unified\n" +
				"    .tested-coverage-code-line {",
			required: "min-width: 0;",
		},
		{
			selector: ".tested-coverage-split-row {",
			required: "min-width: 0;",
		},
		{
			selector: ".tested-coverage-split-cell " +
				".tested-coverage-code {",
			required: "min-width: 0;",
		},
	} {
		rule, ok := ruleBody(printSource, test.selector)
		if !ok {
			t.Errorf(
				"report.css lacks complete print rule %q",
				test.selector,
			)
			continue
		}
		if !strings.Contains(rule, test.required) {
			t.Errorf(
				"report.css print rule %q lacks %q",
				test.selector,
				test.required,
			)
		}
	}
}

func validReportAssetFS() fstest.MapFS {
	return fstest.MapFS{
		"assets/report.css": {
			Data: []byte(":root { --background: white; }\n"),
		},
		"assets/test_output.html": {
			Data: []byte(
				`<!doctype html><html><head><style>` +
					`{{template "report.css"}}</style></head>` +
					`<body><script>{{template "test_output.js"}}` +
					`</script></body></html>`,
			),
		},
		"assets/test_output.js": {
			Data: []byte(`"use strict";`),
		},
		"assets/coverage.js": {
			Data: []byte(`"use strict";`),
		},
		"assets/index.html": {
			Data: []byte(
				`<!doctype html><html><head><style>` +
					`{{template "report.css"}}</style></head>` +
					`<body></body></html>`,
			),
		},
		"assets/coverage_head.html": {
			Data: []byte(
				`<style ` + coverageThemeMarker + `>` +
					`{{template "report.css"}}</style>` +
					`<script ` + coverageExplorerMarker + `>` +
					`{{template "coverage.js"}}</script>` +
					`{{with .Diff}}<script ` + coverageDataMarker +
					` type="application/json">{{.}}</script>{{end}}`,
			),
		},
	}
}
