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
		len(bundle.coverageHead) == 0 {
		t.Fatal("loaded embedded asset bundle is incomplete")
	}
	for _, expected := range []string{
		"--background:",
		"Content-Security-Policy",
		coverageThemeMarker,
	} {
		if !strings.Contains(string(bundle.coverageHead), expected) {
			t.Errorf("coverage head lacks %q", expected)
		}
	}
	if strings.Contains(string(bundle.coverageHead), "{{") {
		t.Error("coverage head contains an unresolved template action")
	}

	for _, name := range []string{"report.css", "test_output.js"} {
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
			name: "missing coverage marker",
			mutate: func(source fstest.MapFS) {
				source["assets/coverage_head.html"] = &fstest.MapFile{
					Data: []byte(
						`<style>{{template "report.css"}}</style>`,
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
							`{{template "report.css"}}</style></head>`,
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
					`{{template "report.css"}}</style>`,
			),
		},
	}
}
