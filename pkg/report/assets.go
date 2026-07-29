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
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"sync"
)

// reportAssets contains every report-owned HTML, CSS, and JavaScript source.
//
//go:embed assets/*.html assets/*.css assets/*.js
var reportAssets embed.FS

type reportAssetBundle struct {
	testOutputTemplate *template.Template
	indexTemplate      *template.Template
	coverageHead       []byte
}

var (
	embeddedAssetsOnce   sync.Once
	embeddedAssetsBundle *reportAssetBundle
	embeddedAssetsErr    error
)

func loadEmbeddedReportAssets() (*reportAssetBundle, error) {
	embeddedAssetsOnce.Do(func() {
		embeddedAssetsBundle, embeddedAssetsErr = loadReportAssets(reportAssets)
	})
	return embeddedAssetsBundle, embeddedAssetsErr
}

func loadReportAssets(source fs.FS) (*reportAssetBundle, error) {
	if source == nil {
		return nil, errors.New("load report assets: filesystem is nil")
	}
	if err := validateStaticReportAssets(source); err != nil {
		return nil, err
	}

	testOutput, err := template.New("test_output.html").ParseFS(
		source,
		"assets/test_output.html",
		"assets/report.css",
		"assets/test_output.js",
	)
	if err != nil {
		return nil, fmt.Errorf("load report assets: parse test output: %w", err)
	}
	index, err := template.New("index.html").ParseFS(
		source,
		"assets/index.html",
		"assets/report.css",
	)
	if err != nil {
		return nil, fmt.Errorf("load report assets: parse index: %w", err)
	}
	coverage, err := template.New("coverage_head.html").ParseFS(
		source,
		"assets/coverage_head.html",
		"assets/report.css",
	)
	if err != nil {
		return nil, fmt.Errorf("load report assets: parse coverage head: %w", err)
	}
	var coverageHead bytes.Buffer
	if err := coverage.ExecuteTemplate(
		&coverageHead,
		"coverage_head.html",
		nil,
	); err != nil {
		return nil, fmt.Errorf("load report assets: render coverage head: %w", err)
	}
	if bytes.Count(
		coverageHead.Bytes(),
		[]byte(coverageThemeMarker),
	) != 1 {
		return nil, errors.New(
			"load report assets: coverage head must contain one theme marker",
		)
	}
	if bytes.Contains(coverageHead.Bytes(), coverageHeadClose) {
		return nil, errors.New(
			"load report assets: coverage head must not close the document head",
		)
	}

	return &reportAssetBundle{
		testOutputTemplate: testOutput,
		indexTemplate:      index,
		coverageHead:       append([]byte(nil), coverageHead.Bytes()...),
	}, nil
}

func validateStaticReportAssets(source fs.FS) error {
	tests := []struct {
		path      string
		forbidden []string
	}{
		{
			path: "assets/report.css",
			forbidden: []string{
				"{{",
				"@import",
				"url(",
				"http://",
				"https://",
				"</style",
			},
		},
		{
			path: "assets/test_output.js",
			forbidden: []string{
				"{{",
				"http://",
				"https://",
				"</script",
			},
		},
	}
	for _, test := range tests {
		data, err := fs.ReadFile(source, test.path)
		if err != nil {
			return fmt.Errorf(
				"load report assets: read %s: %w",
				test.path,
				err,
			)
		}
		lower := bytes.ToLower(data)
		for _, forbidden := range test.forbidden {
			if bytes.Contains(lower, []byte(forbidden)) {
				return fmt.Errorf(
					"load report assets: %s contains forbidden syntax %q",
					test.path,
					forbidden,
				)
			}
		}
	}
	return nil
}
